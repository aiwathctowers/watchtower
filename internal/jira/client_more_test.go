package jira

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeTestClient constructs a Client wired to the given baseURL and a token
// store seeded with a valid (non-expired) access token.
func makeTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	dir := t.TempDir()
	store := NewTokenStore(dir)

	tok := &OAuthToken{
		AccessToken:  "at-valid",
		RefreshToken: "rt-valid",
		TokenType:    "Bearer",
		ExpiresIn:    3600,
		Expiry:       time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}
	require.NoError(t, store.Save(tok))

	c := &Client{
		cloudID:     "cloud-x",
		baseURL:     baseURL,
		oauthCfg:    JiraOAuthConfig{ClientID: "cid", ClientSecret: "secret"},
		tokenStore:  store,
		httpClient:  &http.Client{Timeout: 5 * time.Second},
		rateLimiter: NewRateLimiter(),
		logger:      log.New(io.Discard, "", 0),
	}
	return c
}

func TestNewClient_Initialization(t *testing.T) {
	store := NewTokenStore(t.TempDir())
	c := NewClient("c1", JiraOAuthConfig{}, store)
	assert.Equal(t, "c1", c.cloudID)
	assert.Contains(t, c.baseURL, "/ex/jira/c1")
	assert.NotNil(t, c.httpClient)
	assert.NotNil(t, c.rateLimiter)
	assert.NotNil(t, c.logger)
}

func TestClient_SetLogger(t *testing.T) {
	c := makeTestClient(t, "http://localhost")
	custom := log.New(io.Discard, "x", 0)
	c.SetLogger(custom)
	assert.Same(t, custom, c.logger)
}

func TestClient_Get_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer at-valid", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Accept"))
		_, _ = w.Write([]byte(`{"name":"Acme"}`))
	}))
	defer srv.Close()

	c := makeTestClient(t, srv.URL)
	var got struct {
		Name string `json:"name"`
	}
	require.NoError(t, c.get(context.Background(), "/rest/api/3/project/ABC", &got))
	assert.Equal(t, "Acme", got.Name)
}

func TestClient_Get_NonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
	}))
	defer srv.Close()

	c := makeTestClient(t, srv.URL)
	var got map[string]any
	err := c.get(context.Background(), "/missing", &got)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 404")
}

func TestClient_Get_BadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	c := makeTestClient(t, srv.URL)
	var got map[string]any
	err := c.get(context.Background(), "/x", &got)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decoding")
}

func TestClient_RefreshOn401(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch calls {
		case 1:
			// First call uses old token → reject as 401.
			w.WriteHeader(http.StatusUnauthorized)
		default:
			// After refresh, second call must use refreshed token.
			assert.Equal(t, "Bearer at-refreshed", r.Header.Get("Authorization"))
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}))
	defer srv.Close()

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"at-refreshed","refresh_token":"rt2","expires_in":3600}`))
	}))
	defer tokenSrv.Close()

	prev := jiraTokenEndpoint
	jiraTokenEndpoint = tokenSrv.URL
	defer func() { jiraTokenEndpoint = prev }()

	c := makeTestClient(t, srv.URL)
	var got map[string]any
	require.NoError(t, c.get(context.Background(), "/x", &got))
	assert.Equal(t, true, got["ok"])
	assert.Equal(t, 2, calls)
}

func TestClient_SearchIssues(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/rest/api/3/search/jql")
		q := r.URL.Query()
		assert.Equal(t, "project = ABC", q.Get("jql"))
		assert.Equal(t, "10", q.Get("maxResults"))
		_, _ = w.Write([]byte(`{"issues":[{"key":"ABC-1"}],"total":1,"nextPageToken":"page2"}`))
	}))
	defer srv.Close()

	c := makeTestClient(t, srv.URL)
	res, err := c.SearchIssues(context.Background(), "project = ABC", 10, "")
	require.NoError(t, err)
	require.Len(t, res.Issues, 1)
	assert.Equal(t, "ABC-1", res.Issues[0].Key)
	assert.Equal(t, "page2", res.NextPageToken)
}

func TestClient_SearchIssues_Paginated(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query().Get("nextPageToken")
		_, _ = w.Write([]byte(`{"issues":[],"total":0}`))
	}))
	defer srv.Close()

	c := makeTestClient(t, srv.URL)
	_, err := c.SearchIssues(context.Background(), "x", 50, "abc-token")
	require.NoError(t, err)
	assert.Equal(t, "abc-token", got)
}

func TestClient_FetchAllBoards_Pagination(t *testing.T) {
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n++
		switch n {
		case 1:
			_, _ = w.Write([]byte(`{"isLast":false,"values":[{"id":1,"name":"A"},{"id":2,"name":"B"}]}`))
		default:
			_, _ = w.Write([]byte(`{"isLast":true,"values":[{"id":3,"name":"C"}]}`))
		}
	}))
	defer srv.Close()

	c := makeTestClient(t, srv.URL)
	boards, err := c.FetchAllBoards(context.Background())
	require.NoError(t, err)
	require.Len(t, boards, 3)
	assert.Equal(t, 1, boards[0].ID)
	assert.Equal(t, 3, boards[2].ID)
}

func TestClient_FetchAllBoards_StopOnEmptyPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"isLast":false,"values":[]}`))
	}))
	defer srv.Close()

	c := makeTestClient(t, srv.URL)
	boards, err := c.FetchAllBoards(context.Background())
	require.NoError(t, err)
	assert.Empty(t, boards)
}

func TestClient_FetchBoardIssueCount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/rest/agile/1.0/board/42/issue")
		_, _ = w.Write([]byte(`{"total":17,"issues":[]}`))
	}))
	defer srv.Close()

	c := makeTestClient(t, srv.URL)
	n, err := c.FetchBoardIssueCount(context.Background(), 42)
	require.NoError(t, err)
	assert.Equal(t, 17, n)
}

func TestClient_GetAccessToken_Refreshes(t *testing.T) {
	dir := t.TempDir()
	store := NewTokenStore(dir)
	// Save expired token.
	require.NoError(t, store.Save(&OAuthToken{
		AccessToken:  "old",
		RefreshToken: "rt",
		Expiry:       time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
	}))

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"new","refresh_token":"rt2","expires_in":3600}`))
	}))
	defer tokenSrv.Close()
	prev := jiraTokenEndpoint
	jiraTokenEndpoint = tokenSrv.URL
	defer func() { jiraTokenEndpoint = prev }()

	c := &Client{
		baseURL:     "http://x",
		oauthCfg:    JiraOAuthConfig{ClientID: "c", ClientSecret: "s"},
		tokenStore:  store,
		httpClient:  &http.Client{Timeout: 3 * time.Second},
		rateLimiter: NewRateLimiter(),
		logger:      log.New(io.Discard, "", 0),
	}
	at, err := c.getAccessToken(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "new", at)

	// Token store should have been overwritten.
	loaded, err := store.Load()
	require.NoError(t, err)
	assert.Equal(t, "new", loaded.AccessToken)
}

func TestClient_GetAccessToken_PreservesRefreshTokenOnEmptyResponse(t *testing.T) {
	dir := t.TempDir()
	store := NewTokenStore(dir)
	require.NoError(t, store.Save(&OAuthToken{
		AccessToken:  "old",
		RefreshToken: "keep-me",
		Expiry:       time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
	}))

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// No refresh_token in response.
		_, _ = w.Write([]byte(`{"access_token":"new","expires_in":3600}`))
	}))
	defer tokenSrv.Close()
	prev := jiraTokenEndpoint
	jiraTokenEndpoint = tokenSrv.URL
	defer func() { jiraTokenEndpoint = prev }()

	c := &Client{
		oauthCfg:   JiraOAuthConfig{},
		tokenStore: store,
		httpClient: &http.Client{Timeout: 3 * time.Second},
		logger:     log.New(io.Discard, "", 0),
	}
	_, err := c.getAccessToken(context.Background())
	require.NoError(t, err)

	loaded, err := store.Load()
	require.NoError(t, err)
	assert.Equal(t, "keep-me", loaded.RefreshToken, "client must preserve refresh_token when missing in response")
}
