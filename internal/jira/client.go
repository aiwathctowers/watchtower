package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// Client is an authenticated HTTP client for the Jira Cloud REST API.
type Client struct {
	cloudID     string
	baseURL     string
	oauthCfg    JiraOAuthConfig
	tokenStore  *TokenStore
	httpClient  *http.Client
	rateLimiter *RateLimiter
	logger      *log.Logger
	mu          sync.Mutex
}

// NewClient creates a Jira API client for the given cloud ID.
func NewClient(cloudID string, oauthCfg JiraOAuthConfig, tokenStore *TokenStore) *Client {
	return &Client{
		cloudID:     cloudID,
		baseURL:     fmt.Sprintf("https://api.atlassian.com/ex/jira/%s", cloudID),
		oauthCfg:    oauthCfg,
		tokenStore:  tokenStore,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		rateLimiter: NewRateLimiter(),
		logger:      log.New(os.Stderr, "[jira] ", log.LstdFlags),
	}
}

// SetLogger replaces the client's logger.
func (c *Client) SetLogger(l *log.Logger) {
	c.logger = l
}

// do executes an authenticated HTTP request with automatic token refresh on 401
// and backoff on 429 (max 3 retries).
func (c *Client) do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	fullURL := c.baseURL + path

	for attempt := 0; attempt <= 3; attempt++ {
		if err := c.rateLimiter.Wait(ctx); err != nil {
			return nil, err
		}

		token, err := c.getAccessToken(ctx)
		if err != nil {
			return nil, fmt.Errorf("getting access token: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, method, fullURL, body)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("request %s %s: %w", method, path, err)
		}

		if resp.StatusCode == http.StatusUnauthorized && attempt < 3 {
			resp.Body.Close()
			if refreshErr := c.refreshAccessToken(ctx); refreshErr != nil {
				return nil, fmt.Errorf("refreshing token after 401: %w", refreshErr)
			}
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests && attempt < 3 {
			resp.Body.Close()
			wait := BackoffDuration(attempt)
			c.logger.Printf("rate limited, backing off %s (attempt %d)", wait, attempt+1)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
			continue
		}

		return resp, nil
	}

	return nil, fmt.Errorf("max retries exceeded for %s %s", method, path)
}

// getAccessToken loads the current token, refreshing if expired.
func (c *Client) getAccessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	token, err := c.tokenStore.Load()
	if err != nil {
		return "", fmt.Errorf("loading token: %w", err)
	}

	if token.IsExpired() {
		newToken, err := RefreshToken(ctx, c.oauthCfg, token.RefreshToken)
		if err != nil {
			return "", err
		}
		// Preserve refresh token if not returned.
		if newToken.RefreshToken == "" {
			newToken.RefreshToken = token.RefreshToken
		}
		if err := c.tokenStore.Save(newToken); err != nil {
			return "", fmt.Errorf("saving refreshed token: %w", err)
		}
		return newToken.AccessToken, nil
	}

	return token.AccessToken, nil
}

// refreshAccessToken forces a token refresh.
func (c *Client) refreshAccessToken(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	token, err := c.tokenStore.Load()
	if err != nil {
		return fmt.Errorf("loading token: %w", err)
	}

	newToken, err := RefreshToken(ctx, c.oauthCfg, token.RefreshToken)
	if err != nil {
		return err
	}
	if newToken.RefreshToken == "" {
		newToken.RefreshToken = token.RefreshToken
	}
	if err := c.tokenStore.Save(newToken); err != nil {
		return fmt.Errorf("saving refreshed token: %w", err)
	}
	return nil
}

// get performs a GET request and decodes the JSON response into result.
func (c *Client) get(ctx context.Context, path string, result interface{}) error {
	resp, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: status %d: %s", path, resp.StatusCode, body)
	}

	if err := json.Unmarshal(body, result); err != nil {
		return fmt.Errorf("decoding GET %s: %w", path, err)
	}
	return nil
}

// getWithQuery performs a GET request with query parameters and decodes the JSON response.
func (c *Client) getWithQuery(ctx context.Context, path string, params url.Values, result interface{}) error {
	if len(params) > 0 {
		path = path + "?" + params.Encode()
	}
	return c.get(ctx, path, result)
}

// SearchIssues executes a JQL search query.
func (c *Client) SearchIssues(ctx context.Context, jql string, startAt, maxResults int) (*SearchResult, error) {
	params := url.Values{
		"jql":        {jql},
		"startAt":    {fmt.Sprintf("%d", startAt)},
		"maxResults": {fmt.Sprintf("%d", maxResults)},
		"fields":     {strings.Join(searchFields, ",")},
	}
	var result SearchResult
	if err := c.getWithQuery(ctx, "/rest/api/3/search", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// searchFields lists the issue fields to request from the Jira API.
var searchFields = []string{
	"summary", "description", "issuetype", "status", "assignee", "reporter",
	"priority", "created", "updated", "duedate", "labels", "components",
	"issuelinks", "sprint", "epic", "parent", "resolutiondate", "fixVersions",
}

// GetProjectVersions fetches all fix versions (releases) for a project.
func (c *Client) GetProjectVersions(ctx context.Context, projectKey string) ([]FixVersion, error) {
	path := fmt.Sprintf("/rest/api/3/project/%s/versions", projectKey)
	var versions []FixVersion
	if err := c.get(ctx, path, &versions); err != nil {
		return nil, fmt.Errorf("fetching versions for project %s: %w", projectKey, err)
	}
	return versions, nil
}
