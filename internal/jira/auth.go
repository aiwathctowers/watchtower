package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"watchtower/internal/auth"
)

const (
	defaultRedirectPort     = 18511 // separate range from Calendar (18501-18510)
	callbackPath            = "/callback"
	loginTimeout            = 5 * time.Minute
	jiraAuthEndpoint        = "https://auth.atlassian.com/authorize"
	jiraTokenEndpoint       = "https://auth.atlassian.com/oauth/token"
	jiraAccessibleResources = "https://api.atlassian.com/oauth/token/accessible-resources"
)

// DefaultJiraClientID and DefaultJiraClientSecret are injected at build time via -ldflags:
//
//	-X watchtower/internal/jira.DefaultJiraClientID=...
//	-X watchtower/internal/jira.DefaultJiraClientSecret=...
var (
	DefaultJiraClientID     string
	DefaultJiraClientSecret string
)

// JiraOAuthConfig holds credentials for the Jira Cloud OAuth 2.0 (3LO) flow.
type JiraOAuthConfig struct {
	ClientID     string
	ClientSecret string
}

// OAuthToken represents an OAuth2 token (stored as JSON).
type OAuthToken struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
	Expiry       string `json:"expiry,omitempty"` // ISO8601 calculated from ExpiresIn
}

// IsExpired returns true if the token has expired (or will expire within 60 seconds).
func (t *OAuthToken) IsExpired() bool {
	if t.Expiry == "" {
		return true
	}
	exp, err := time.Parse(time.RFC3339, t.Expiry)
	if err != nil {
		return true
	}
	return time.Now().After(exp.Add(-60 * time.Second))
}

// TokenStore persists and loads OAuth2 tokens for Jira.
type TokenStore struct {
	path string // ~/.local/share/watchtower/{workspace}/jira_token.json
}

// NewTokenStore creates a TokenStore for the given workspace directory.
func NewTokenStore(workspaceDir string) *TokenStore {
	return &TokenStore{
		path: filepath.Join(workspaceDir, "jira_token.json"),
	}
}

// Path returns the token file path.
func (s *TokenStore) Path() string {
	return s.path
}

// Load reads the token from disk.
func (s *TokenStore) Load() (*OAuthToken, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, err
	}
	var token OAuthToken
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, fmt.Errorf("parsing token: %w", err)
	}
	return &token, nil
}

// Save writes the token to disk.
func (s *TokenStore) Save(token *OAuthToken) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("creating token directory: %w", err)
	}
	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling token: %w", err)
	}
	return os.WriteFile(s.path, data, 0o600)
}

// Delete removes the token file.
func (s *TokenStore) Delete() error {
	err := os.Remove(s.path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// Exists checks whether a token file is present.
func (s *TokenStore) Exists() bool {
	_, err := os.Stat(s.path)
	return err == nil
}

// CloudResource represents an accessible Jira Cloud site.
type CloudResource struct {
	ID        string `json:"id"`
	URL       string `json:"url"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatarUrl"`
}

// PrepareResult holds the data needed by the desktop app to start the OAuth flow.
type PrepareResult struct {
	AuthorizeURL string `json:"authorize_url"`
	RedirectURI  string `json:"redirect_uri"`
	State        string `json:"state"`
}

// Prepare generates an OAuth authorization URL for the desktop app flow.
func Prepare(cfg JiraOAuthConfig, customRedirectURI string) (*PrepareResult, error) {
	state, err := auth.RandomState()
	if err != nil {
		return nil, fmt.Errorf("generating state: %w", err)
	}

	redirectURI := customRedirectURI
	if redirectURI == "" {
		redirectURI = fmt.Sprintf("http://localhost:%d%s", defaultRedirectPort, callbackPath)
	}

	authorizeURL := buildAuthURL(cfg, redirectURI, state)

	return &PrepareResult{
		AuthorizeURL: authorizeURL,
		RedirectURI:  redirectURI,
		State:        state,
	}, nil
}

func buildAuthURL(cfg JiraOAuthConfig, redirectURI, state string) string {
	params := url.Values{
		"audience":      {"api.atlassian.com"},
		"client_id":     {cfg.ClientID},
		"scope":         {"read:jira-work read:jira-user"},
		"redirect_uri":  {redirectURI},
		"state":         {state},
		"response_type": {"code"},
		"prompt":        {"consent"},
	}
	return jiraAuthEndpoint + "?" + strings.ReplaceAll(params.Encode(), "+", "%20")
}

// exchangeCode exchanges an authorization code for tokens via raw HTTP POST.
func exchangeCode(ctx context.Context, cfg JiraOAuthConfig, code, redirectURI string) (*OAuthToken, error) {
	payload := map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     cfg.ClientID,
		"client_secret": cfg.ClientSecret,
		"code":          code,
		"redirect_uri":  redirectURI,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, jiraTokenEndpoint, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	hc := &http.Client{Timeout: 30 * time.Second}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token exchange request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed (%d): %s", resp.StatusCode, respBody)
	}

	var token OAuthToken
	if err := json.Unmarshal(respBody, &token); err != nil {
		return nil, fmt.Errorf("decoding token response: %w", err)
	}

	// Calculate expiry from expires_in.
	if token.ExpiresIn > 0 {
		token.Expiry = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second).UTC().Format(time.RFC3339)
	}

	return &token, nil
}

// Complete exchanges an authorization code for tokens.
func Complete(ctx context.Context, cfg JiraOAuthConfig, code, redirectURI string) (*OAuthToken, error) {
	if code == "" {
		return nil, fmt.Errorf("no authorization code provided")
	}
	return exchangeCode(ctx, cfg, code, redirectURI)
}

// RefreshToken refreshes an expired access token using a refresh token.
func RefreshToken(ctx context.Context, cfg JiraOAuthConfig, refreshToken string) (*OAuthToken, error) {
	payload := map[string]string{
		"grant_type":    "refresh_token",
		"client_id":     cfg.ClientID,
		"client_secret": cfg.ClientSecret,
		"refresh_token": refreshToken,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, jiraTokenEndpoint, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	hc := &http.Client{Timeout: 30 * time.Second}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("refresh token request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("refresh token failed (%d): %s", resp.StatusCode, respBody)
	}

	var token OAuthToken
	if err := json.Unmarshal(respBody, &token); err != nil {
		return nil, fmt.Errorf("decoding refresh response: %w", err)
	}

	if token.ExpiresIn > 0 {
		token.Expiry = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second).UTC().Format(time.RFC3339)
	}

	return &token, nil
}

// FetchAccessibleResources fetches the list of Jira Cloud sites accessible with the given token.
func FetchAccessibleResources(ctx context.Context, accessToken string) ([]CloudResource, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jiraAccessibleResources, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	hc := &http.Client{Timeout: 30 * time.Second}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("accessible resources request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("accessible resources failed (%d): %s", resp.StatusCode, body)
	}

	var resources []CloudResource
	if err := json.Unmarshal(body, &resources); err != nil {
		return nil, fmt.Errorf("decoding accessible resources: %w", err)
	}

	return resources, nil
}

// callbackResult is sent from the HTTP callback handler to the Login goroutine.
type callbackResult struct {
	code  string
	state string
	err   string
}

// openBrowserFunc can be replaced in tests.
var (
	openBrowserMu   sync.Mutex
	openBrowserFunc = auth.OpenBrowser
)

func getOpenBrowserFunc() func(string) {
	openBrowserMu.Lock()
	defer openBrowserMu.Unlock()
	return openBrowserFunc
}

// LoginOptions configures the Login flow behaviour.
type LoginOptions struct {
	SkipBrowserOpen bool
}

// Login performs the Jira OAuth2 (3LO) flow via a local HTTP callback server.
// Plain HTTP (not TLS) is used intentionally for loopback redirect URIs.
func Login(ctx context.Context, cfg JiraOAuthConfig, out io.Writer, opts ...LoginOptions) (*OAuthToken, error) {
	var opt LoginOptions
	if len(opts) > 0 {
		opt = opts[0]
	}

	listener, err := listenLocal()
	if err != nil {
		return nil, fmt.Errorf("starting local server: %w", err)
	}
	defer listener.Close()

	addr := listener.Addr().String()
	redirectURI := fmt.Sprintf("http://localhost:%s%s", auth.PortFromAddr(addr), callbackPath)

	state, err := auth.RandomState()
	if err != nil {
		return nil, fmt.Errorf("generating state: %w", err)
	}

	authorizeURL := buildAuthURL(cfg, redirectURI, state)

	resultCh := make(chan callbackResult, 1)

	mux := http.NewServeMux()
	mux.HandleFunc(callbackPath, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if errMsg := q.Get("error"); errMsg != "" {
			resultCh <- callbackResult{err: errMsg}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, strings.Replace(jiraCallbackErrorPage, "{{ERROR}}", html.EscapeString(errMsg), 1))
			return
		}
		resultCh <- callbackResult{
			code:  q.Get("code"),
			state: q.Get("state"),
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, jiraCallbackSuccessPage)
	})

	server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go server.Serve(listener) //nolint:errcheck
	defer func() {
		time.Sleep(500 * time.Millisecond)
		server.Close()
	}()

	if opt.SkipBrowserOpen {
		fmt.Fprintf(out, "Authorize URL:\n\n  %s\n\n", authorizeURL)
		fmt.Fprintf(out, "Waiting for authorization callback...\n")
	} else {
		fmt.Fprintf(out, "Opening browser for Jira authorization...\n")
		fmt.Fprintf(out, "If the browser doesn't open, visit this URL:\n\n  %s\n\n", authorizeURL)
		getOpenBrowserFunc()(authorizeURL)
	}

	ctx, cancel := context.WithTimeout(ctx, loginTimeout)
	defer cancel()

	var cb callbackResult
	select {
	case cb = <-resultCh:
	case <-ctx.Done():
		return nil, fmt.Errorf("login timed out after %s", loginTimeout)
	}

	if cb.err != "" {
		return nil, fmt.Errorf("jira authorization denied: %s", cb.err)
	}
	if cb.state != state {
		return nil, fmt.Errorf("state mismatch: possible CSRF attack")
	}
	if cb.code == "" {
		return nil, fmt.Errorf("no authorization code received")
	}

	token, err := exchangeCode(ctx, cfg, cb.code, redirectURI)
	if err != nil {
		return nil, fmt.Errorf("exchanging code for token: %w", err)
	}

	return token, nil
}

// listenLocal tries preferred ports (18511-18520), then falls back to a random port.
func listenLocal() (net.Listener, error) {
	preferred := []int{18511, 18512, 18513, 18514, 18515, 18516, 18517, 18518, 18519, 18520}
	for _, port := range preferred {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			return ln, nil
		}
	}
	return net.Listen("tcp", "127.0.0.1:0")
}

const jiraCallbackSuccessPage = `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>Watchtower — Jira Connected</title>
<style>body{font-family:sans-serif;display:flex;align-items:center;justify-content:center;min-height:100vh;background:#0f0f0f;color:#e5e5e5}
.card{background:#1a1a1a;border:1px solid #2a2a2a;border-radius:16px;padding:48px;max-width:440px;text-align:center}
h1{font-size:20px;margin-bottom:8px}p{color:#888;font-size:14px}</style></head>
<body><div class="card"><h1>Jira Connected</h1><p>Jira Cloud has been linked to Watchtower. You can close this tab.</p></div>
<script>setTimeout(function(){try{window.close()}catch(e){}},2000);</script></body></html>`

const jiraCallbackErrorPage = `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>Watchtower — Error</title>
<style>body{font-family:sans-serif;display:flex;align-items:center;justify-content:center;min-height:100vh;background:#0f0f0f;color:#e5e5e5}
.card{background:#1a1a1a;border:1px solid #2a2a2a;border-radius:16px;padding:48px;max-width:440px;text-align:center}
h1{font-size:20px;margin-bottom:8px}p{color:#888;font-size:14px}</style></head>
<body><div class="card"><h1>Authorization Failed</h1><p>{{ERROR}}</p></div></body></html>`
