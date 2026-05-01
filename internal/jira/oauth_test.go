package jira

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildAuthURL_IncludesScopesAndClientID(t *testing.T) {
	cfg := JiraOAuthConfig{ClientID: "atlas-app-id", ClientSecret: "shh"}
	got := buildAuthURL(cfg, "http://localhost:18511/callback", "state-xyz")

	u, err := url.Parse(got)
	require.NoError(t, err)
	assert.Equal(t, "auth.atlassian.com", u.Host)

	q := u.Query()
	assert.Equal(t, "atlas-app-id", q.Get("client_id"))
	assert.Equal(t, "http://localhost:18511/callback", q.Get("redirect_uri"))
	assert.Equal(t, "code", q.Get("response_type"))
	assert.Equal(t, "state-xyz", q.Get("state"))
	assert.Equal(t, "consent", q.Get("prompt"))
	assert.Equal(t, "api.atlassian.com", q.Get("audience"))

	scope := q.Get("scope")
	for _, s := range []string{
		"read:jira-work",
		"write:jira-work",
		"read:jira-user",
		"offline_access",
	} {
		assert.Contains(t, scope, s, "missing scope %q", s)
	}
}

func TestBuildAuthURL_EscapesScopeSpacesAsPercent20(t *testing.T) {
	got := buildAuthURL(JiraOAuthConfig{ClientID: "x"}, "http://localhost/cb", "s")
	// Atlassian rejects '+' as a scope separator; the helper rewrites it to %20.
	// Special chars (':') are URL-encoded as %3A.
	assert.NotContains(t, got, "+write")
	assert.Contains(t, got, "%20write%3Ajira-work")
}

func TestPrepare_DefaultRedirect(t *testing.T) {
	res, err := Prepare(JiraOAuthConfig{ClientID: "cid"}, "")
	require.NoError(t, err)
	assert.Contains(t, res.RedirectURI, ":18511")
	assert.Contains(t, res.AuthorizeURL, "client_id=cid")
	assert.NotEmpty(t, res.State)
}

func TestPrepare_CustomRedirect(t *testing.T) {
	res, err := Prepare(JiraOAuthConfig{ClientID: "cid"}, "http://example/cb")
	require.NoError(t, err)
	assert.Equal(t, "http://example/cb", res.RedirectURI)
}

func TestPrepare_StateIsRandom(t *testing.T) {
	r1, err := Prepare(JiraOAuthConfig{ClientID: "x"}, "")
	require.NoError(t, err)
	r2, err := Prepare(JiraOAuthConfig{ClientID: "x"}, "")
	require.NoError(t, err)
	assert.NotEqual(t, r1.State, r2.State)
}

func TestExchangeCode_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		body, _ := io.ReadAll(r.Body)
		var payload map[string]string
		require.NoError(t, json.Unmarshal(body, &payload))
		assert.Equal(t, "authorization_code", payload["grant_type"])
		assert.Equal(t, "code-abc", payload["code"])

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at","refresh_token":"rt","token_type":"Bearer","expires_in":3600,"scope":"read:jira-work"}`))
	}))
	defer srv.Close()

	prev := jiraTokenEndpoint
	jiraTokenEndpoint = srv.URL
	defer func() { jiraTokenEndpoint = prev }()

	tok, err := exchangeCode(context.Background(), JiraOAuthConfig{ClientID: "cid", ClientSecret: "secret"}, "code-abc", "http://x/cb")
	require.NoError(t, err)
	assert.Equal(t, "at", tok.AccessToken)
	assert.Equal(t, "rt", tok.RefreshToken)
	assert.NotEmpty(t, tok.Expiry, "expiry should be calculated from expires_in")
}

func TestExchangeCode_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
	}))
	defer srv.Close()

	prev := jiraTokenEndpoint
	jiraTokenEndpoint = srv.URL
	defer func() { jiraTokenEndpoint = prev }()

	_, err := exchangeCode(context.Background(), JiraOAuthConfig{}, "x", "http://x/cb")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token exchange failed")
}

func TestExchangeCode_BadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	prev := jiraTokenEndpoint
	jiraTokenEndpoint = srv.URL
	defer func() { jiraTokenEndpoint = prev }()

	_, err := exchangeCode(context.Background(), JiraOAuthConfig{}, "x", "http://x/cb")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decoding token response")
}

func TestComplete_RejectsEmptyCode(t *testing.T) {
	_, err := Complete(context.Background(), JiraOAuthConfig{}, "", "http://x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no authorization code")
}

func TestRefreshToken_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]string
		require.NoError(t, json.Unmarshal(body, &payload))
		assert.Equal(t, "refresh_token", payload["grant_type"])
		assert.Equal(t, "rt-old", payload["refresh_token"])

		_, _ = w.Write([]byte(`{"access_token":"at-new","refresh_token":"rt-new","expires_in":3600}`))
	}))
	defer srv.Close()

	prev := jiraTokenEndpoint
	jiraTokenEndpoint = srv.URL
	defer func() { jiraTokenEndpoint = prev }()

	tok, err := RefreshToken(context.Background(), JiraOAuthConfig{ClientID: "c", ClientSecret: "s"}, "rt-old")
	require.NoError(t, err)
	assert.Equal(t, "at-new", tok.AccessToken)
	assert.Equal(t, "rt-new", tok.RefreshToken)
	assert.NotEmpty(t, tok.Expiry)
}

func TestRefreshToken_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer srv.Close()

	prev := jiraTokenEndpoint
	jiraTokenEndpoint = srv.URL
	defer func() { jiraTokenEndpoint = prev }()

	_, err := RefreshToken(context.Background(), JiraOAuthConfig{}, "rt")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refresh token failed")
}

func TestRefreshToken_BadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{not json`))
	}))
	defer srv.Close()

	prev := jiraTokenEndpoint
	jiraTokenEndpoint = srv.URL
	defer func() { jiraTokenEndpoint = prev }()

	_, err := RefreshToken(context.Background(), JiraOAuthConfig{}, "rt")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decoding refresh response")
}

func TestFetchAccessibleResources_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer at-token", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`[{"id":"site-1","url":"https://acme.atlassian.net","name":"Acme","avatarUrl":"https://x/a.png"}]`))
	}))
	defer srv.Close()

	prev := jiraAccessibleResources
	jiraAccessibleResources = srv.URL
	defer func() { jiraAccessibleResources = prev }()

	res, err := FetchAccessibleResources(context.Background(), "at-token")
	require.NoError(t, err)
	require.Len(t, res, 1)
	assert.Equal(t, "site-1", res[0].ID)
	assert.Equal(t, "Acme", res[0].Name)
	assert.Equal(t, "https://acme.atlassian.net", res[0].URL)
}

func TestFetchAccessibleResources_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer srv.Close()

	prev := jiraAccessibleResources
	jiraAccessibleResources = srv.URL
	defer func() { jiraAccessibleResources = prev }()

	_, err := FetchAccessibleResources(context.Background(), "bad-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accessible resources failed")
}

func TestFetchAccessibleResources_BadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not an array`))
	}))
	defer srv.Close()

	prev := jiraAccessibleResources
	jiraAccessibleResources = srv.URL
	defer func() { jiraAccessibleResources = prev }()

	_, err := FetchAccessibleResources(context.Background(), "x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decoding")
}

func TestGetOpenBrowserFunc_NotNil(t *testing.T) {
	assert.NotNil(t, getOpenBrowserFunc())
}

// Sanity-check that exchangeCode marshals payloads in JSON (not form-encoded).
func TestExchangeCode_PostsJSONBody(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got = string(body)
		_, _ = w.Write([]byte(`{"access_token":"a"}`))
	}))
	defer srv.Close()

	prev := jiraTokenEndpoint
	jiraTokenEndpoint = srv.URL
	defer func() { jiraTokenEndpoint = prev }()

	_, err := exchangeCode(context.Background(), JiraOAuthConfig{ClientID: "c", ClientSecret: "s"}, "code", "http://cb")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(got, "{"), "expected JSON body, got: %q", got)
}
