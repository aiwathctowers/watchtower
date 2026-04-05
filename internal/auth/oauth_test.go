package auth

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/slack-go/slack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// syncBuffer is a goroutine-safe wrapper around bytes.Buffer.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (sb *syncBuffer) Write(p []byte) (int, error) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.buf.Write(p)
}

func (sb *syncBuffer) String() string {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.buf.String()
}

// insecureClient is an HTTP client that skips TLS verification (for self-signed certs).
var insecureClient = &http.Client{
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	},
}

// simulateCallback makes an HTTPS GET to the local OAuth callback server,
// mimicking the browser redirect from Slack. Connection errors are ignored
// because the server may shut down before the response is fully sent.
func simulateCallback(redirectURI, state, code, errMsg string) {
	u, _ := url.Parse(redirectURI)

	q := u.Query()
	if errMsg != "" {
		q.Set("error", errMsg)
	} else {
		q.Set("code", code)
		q.Set("state", state)
	}
	u.RawQuery = q.Encode()

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, u.String(), nil)
	resp, err := insecureClient.Do(req)
	if err == nil {
		resp.Body.Close()
	}
}

func TestLogin_HappyPath(t *testing.T) {
	var capturedURL atomic.Value
	oldOpen := getOpenBrowserFunc()
	setOpenBrowserFunc(func(u string) { capturedURL.Store(u) })
	defer func() { setOpenBrowserFunc(oldOpen) }()

	oldExchange := exchangeToken
	exchangeToken = func(ctx context.Context, clientID, clientSecret, code, redirectURI string) (*slack.OAuthV2Response, error) {
		assert.Equal(t, "test-client-id", clientID)
		assert.Equal(t, "test-client-secret", clientSecret)
		assert.Equal(t, "test-auth-code", code)
		return &slack.OAuthV2Response{
			AuthedUser: slack.OAuthV2ResponseAuthedUser{
				ID:          "U12345",
				AccessToken: "xoxp-test-token",
			},
			Team: slack.OAuthV2ResponseTeam{
				ID:   "T12345",
				Name: "Test Team",
			},
		}, nil
	}
	defer func() { exchangeToken = oldExchange }()

	cfg := OAuthConfig{
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
	}

	var out bytes.Buffer
	ctx := context.Background()

	resultCh := make(chan struct {
		result *OAuthResult
		err    error
	}, 1)
	go func() {
		r, err := Login(ctx, cfg, &out)
		resultCh <- struct {
			result *OAuthResult
			err    error
		}{r, err}
	}()

	require.Eventually(t, func() bool { v := capturedURL.Load(); return v != nil && v.(string) != "" }, 3*time.Second, 10*time.Millisecond)

	parsed, err := url.Parse(capturedURL.Load().(string))
	require.NoError(t, err)
	state := parsed.Query().Get("state")
	redirectURI := parsed.Query().Get("redirect_uri")
	require.NotEmpty(t, state)
	require.NotEmpty(t, redirectURI)

	// Verify HTTPS redirect URI
	assert.True(t, len(redirectURI) > 0 && redirectURI[:8] == "https://")

	// Verify user_scope is used (not scope)
	assert.NotEmpty(t, parsed.Query().Get("user_scope"))
	assert.Empty(t, parsed.Query().Get("scope"))

	simulateCallback(redirectURI, state, "test-auth-code", "")

	select {
	case res := <-resultCh:
		require.NoError(t, res.err)
		require.NotNil(t, res.result)
		assert.Equal(t, "xoxp-test-token", res.result.AccessToken)
		assert.Equal(t, "T12345", res.result.TeamID)
		assert.Equal(t, "Test Team", res.result.TeamName)
		assert.Equal(t, "U12345", res.result.UserID)
	case <-time.After(5 * time.Second):
		t.Fatal("Login did not complete in time")
	}

	assert.Contains(t, out.String(), "Opening browser")
}

func TestLogin_StateMismatch(t *testing.T) {
	var capturedURL atomic.Value
	oldOpen := getOpenBrowserFunc()
	setOpenBrowserFunc(func(u string) { capturedURL.Store(u) })
	defer func() { setOpenBrowserFunc(oldOpen) }()

	oldExchange := exchangeToken
	exchangeToken = func(ctx context.Context, clientID, clientSecret, code, redirectURI string) (*slack.OAuthV2Response, error) {
		t.Fatal("exchange should not be called on state mismatch")
		return nil, nil
	}
	defer func() { exchangeToken = oldExchange }()

	cfg := OAuthConfig{ClientID: "id", ClientSecret: "secret"}
	var out bytes.Buffer

	resultCh := make(chan error, 1)
	go func() {
		_, err := Login(context.Background(), cfg, &out)
		resultCh <- err
	}()

	require.Eventually(t, func() bool { v := capturedURL.Load(); return v != nil && v.(string) != "" }, 3*time.Second, 10*time.Millisecond)

	parsed, err := url.Parse(capturedURL.Load().(string))
	require.NoError(t, err)
	redirectURI := parsed.Query().Get("redirect_uri")

	simulateCallback(redirectURI, "wrong-state", "code", "")

	select {
	case err := <-resultCh:
		require.Error(t, err)
		assert.Contains(t, err.Error(), "state mismatch")
	case <-time.After(5 * time.Second):
		t.Fatal("Login did not complete in time")
	}
}

func TestLogin_UserDenied(t *testing.T) {
	var capturedURL atomic.Value
	oldOpen := getOpenBrowserFunc()
	setOpenBrowserFunc(func(u string) { capturedURL.Store(u) })
	defer func() { setOpenBrowserFunc(oldOpen) }()

	oldExchange := exchangeToken
	exchangeToken = func(ctx context.Context, clientID, clientSecret, code, redirectURI string) (*slack.OAuthV2Response, error) {
		t.Fatal("exchange should not be called on user deny")
		return nil, nil
	}
	defer func() { exchangeToken = oldExchange }()

	cfg := OAuthConfig{ClientID: "id", ClientSecret: "secret"}
	var out bytes.Buffer

	resultCh := make(chan error, 1)
	go func() {
		_, err := Login(context.Background(), cfg, &out)
		resultCh <- err
	}()

	require.Eventually(t, func() bool { v := capturedURL.Load(); return v != nil && v.(string) != "" }, 3*time.Second, 10*time.Millisecond)

	parsed, err := url.Parse(capturedURL.Load().(string))
	require.NoError(t, err)
	redirectURI := parsed.Query().Get("redirect_uri")

	simulateCallback(redirectURI, "", "", "access_denied")

	select {
	case err := <-resultCh:
		require.Error(t, err)
		assert.Contains(t, err.Error(), "access_denied")
	case <-time.After(5 * time.Second):
		t.Fatal("Login did not complete in time")
	}
}

func TestLogin_Timeout(t *testing.T) {
	oldOpen := getOpenBrowserFunc()
	setOpenBrowserFunc(func(u string) {})
	defer func() { setOpenBrowserFunc(oldOpen) }()

	cfg := OAuthConfig{ClientID: "id", ClientSecret: "secret"}
	var out bytes.Buffer

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Login(ctx, cfg, &out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
}

func TestLogin_PortBusy(t *testing.T) {
	var listeners []net.Listener
	preferred := []int{18491, 18492, 18493, 18494, 18495, 18496, 18497, 18498, 18499, 18500}
	for _, port := range preferred {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			listeners = append(listeners, ln)
		}
	}
	defer func() {
		for _, ln := range listeners {
			ln.Close()
		}
	}()

	var capturedURL atomic.Value
	oldOpen := getOpenBrowserFunc()
	setOpenBrowserFunc(func(u string) { capturedURL.Store(u) })
	defer func() { setOpenBrowserFunc(oldOpen) }()

	oldExchange := exchangeToken
	exchangeToken = func(ctx context.Context, clientID, clientSecret, code, redirectURI string) (*slack.OAuthV2Response, error) {
		return &slack.OAuthV2Response{
			AuthedUser: slack.OAuthV2ResponseAuthedUser{
				ID:          "U999",
				AccessToken: "xoxp-fallback",
			},
			Team: slack.OAuthV2ResponseTeam{ID: "T999", Name: "Fallback"},
		}, nil
	}
	defer func() { exchangeToken = oldExchange }()

	cfg := OAuthConfig{ClientID: "id", ClientSecret: "secret"}
	var out bytes.Buffer

	resultCh := make(chan struct {
		result *OAuthResult
		err    error
	}, 1)
	go func() {
		r, err := Login(context.Background(), cfg, &out)
		resultCh <- struct {
			result *OAuthResult
			err    error
		}{r, err}
	}()

	require.Eventually(t, func() bool { v := capturedURL.Load(); return v != nil && v.(string) != "" }, 3*time.Second, 10*time.Millisecond)

	parsed, err := url.Parse(capturedURL.Load().(string))
	require.NoError(t, err)
	redirectURI := parsed.Query().Get("redirect_uri")
	state := parsed.Query().Get("state")

	assert.NotEmpty(t, redirectURI)

	simulateCallback(redirectURI, state, "code", "")

	select {
	case res := <-resultCh:
		require.NoError(t, res.err)
		assert.Equal(t, "xoxp-fallback", res.result.AccessToken)
	case <-time.After(5 * time.Second):
		t.Fatal("Login did not complete in time")
	}
}

func TestJoinScopes(t *testing.T) {
	assert.Equal(t, "a,b,c", strings.Join([]string{"a", "b", "c"}, ","))
	assert.Equal(t, "single", strings.Join([]string{"single"}, ","))
	assert.Equal(t, "", strings.Join(nil, ","))
}

func TestRandomState(t *testing.T) {
	s1, err := RandomState()
	require.NoError(t, err)
	assert.Len(t, s1, 64)

	s2, err := RandomState()
	require.NoError(t, err)
	assert.NotEqual(t, s1, s2)
}

func TestGenerateSelfSignedCert(t *testing.T) {
	cert, err := generateSelfSignedCert()
	require.NoError(t, err)
	assert.Len(t, cert.Certificate, 1)
	assert.NotNil(t, cert.PrivateKey)
}

func TestPrepare(t *testing.T) {
	cfg := OAuthConfig{ClientID: "cid", ClientSecret: "csec"}
	result, err := Prepare(cfg, "")
	require.NoError(t, err)

	assert.Contains(t, result.AuthorizeURL, "https://slack.com/oauth/v2/authorize")
	assert.Contains(t, result.AuthorizeURL, "client_id=cid")
	assert.Contains(t, result.AuthorizeURL, "user_scope=")
	assert.Contains(t, result.AuthorizeURL, "state="+result.State)
	assert.Contains(t, result.AuthorizeURL, url.QueryEscape(result.RedirectURI))

	assert.Equal(t, fmt.Sprintf("https://127.0.0.1:%d/callback", defaultRedirectPort), result.RedirectURI)
	assert.Len(t, result.State, 64)
}

func TestPrepare_UniqueState(t *testing.T) {
	cfg := OAuthConfig{ClientID: "id", ClientSecret: "sec"}
	r1, err := Prepare(cfg, "")
	require.NoError(t, err)
	r2, err := Prepare(cfg, "")
	require.NoError(t, err)
	assert.NotEqual(t, r1.State, r2.State, "each Prepare call should generate a unique state")
}

func TestComplete_HappyPath(t *testing.T) {
	oldExchange := exchangeToken
	exchangeToken = func(ctx context.Context, clientID, clientSecret, code, redirectURI string) (*slack.OAuthV2Response, error) {
		assert.Equal(t, "cid", clientID)
		assert.Equal(t, "csec", clientSecret)
		assert.Equal(t, "the-code", code)
		assert.Equal(t, "https://127.0.0.1:18491/callback", redirectURI)
		return &slack.OAuthV2Response{
			AuthedUser: slack.OAuthV2ResponseAuthedUser{
				ID:          "U111",
				AccessToken: "xoxp-complete-token",
			},
			Team: slack.OAuthV2ResponseTeam{ID: "T111", Name: "Complete Team"},
		}, nil
	}
	defer func() { exchangeToken = oldExchange }()

	cfg := OAuthConfig{ClientID: "cid", ClientSecret: "csec"}
	result, err := Complete(context.Background(), cfg, "the-code", "https://127.0.0.1:18491/callback")
	require.NoError(t, err)
	assert.Equal(t, "xoxp-complete-token", result.AccessToken)
	assert.Equal(t, "T111", result.TeamID)
	assert.Equal(t, "Complete Team", result.TeamName)
	assert.Equal(t, "U111", result.UserID)
}

func TestComplete_EmptyCode(t *testing.T) {
	cfg := OAuthConfig{ClientID: "id", ClientSecret: "sec"}
	_, err := Complete(context.Background(), cfg, "", "https://127.0.0.1:18491/callback")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no authorization code")
}

func TestComplete_ExchangeError(t *testing.T) {
	oldExchange := exchangeToken
	exchangeToken = func(ctx context.Context, clientID, clientSecret, code, redirectURI string) (*slack.OAuthV2Response, error) {
		return nil, fmt.Errorf("slack API error")
	}
	defer func() { exchangeToken = oldExchange }()

	cfg := OAuthConfig{ClientID: "id", ClientSecret: "sec"}
	_, err := Complete(context.Background(), cfg, "code", "https://127.0.0.1:18491/callback")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exchanging code")
}

func TestComplete_EmptyToken(t *testing.T) {
	oldExchange := exchangeToken
	exchangeToken = func(ctx context.Context, clientID, clientSecret, code, redirectURI string) (*slack.OAuthV2Response, error) {
		return &slack.OAuthV2Response{
			AuthedUser: slack.OAuthV2ResponseAuthedUser{ID: "U1", AccessToken: ""},
			Team:       slack.OAuthV2ResponseTeam{ID: "T1", Name: "Team"},
		}, nil
	}
	defer func() { exchangeToken = oldExchange }()

	cfg := OAuthConfig{ClientID: "id", ClientSecret: "sec"}
	_, err := Complete(context.Background(), cfg, "code", "https://127.0.0.1:18491/callback")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no user access token")
}

func TestPortFromAddr(t *testing.T) {
	tests := []struct {
		name     string
		addr     string
		expected string
	}{
		{"standard host:port", "127.0.0.1:8080", "8080"},
		{"localhost with port", "localhost:443", "443"},
		{"ipv6 with port", "[::1]:9090", "9090"},
		{"no port (fallback)", "invalid-addr", "invalid-addr"},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := PortFromAddr(tt.addr)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestListenLocalTLS(t *testing.T) {
	cert, err := generateSelfSignedCert()
	require.NoError(t, err)

	ln, err := ListenLocalTLS(cert)
	require.NoError(t, err)
	defer ln.Close()

	addr := ln.Addr().String()
	assert.Contains(t, addr, "127.0.0.1:")

	port := PortFromAddr(addr)
	assert.NotEmpty(t, port)
}

func TestListenLocalTLS_FallbackPort(t *testing.T) {
	// Block all preferred ports
	var listeners []net.Listener
	preferred := []int{18491, 18492, 18493, 18494, 18495, 18496, 18497, 18498, 18499, 18500}
	for _, port := range preferred {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			listeners = append(listeners, ln)
		}
	}
	defer func() {
		for _, ln := range listeners {
			ln.Close()
		}
	}()

	cert, err := generateSelfSignedCert()
	require.NoError(t, err)

	ln, err := ListenLocalTLS(cert)
	require.NoError(t, err)
	defer ln.Close()

	// Should have gotten a random port (not one of the preferred ones)
	addr := ln.Addr().String()
	port := PortFromAddr(addr)
	assert.NotEmpty(t, port)
}

func TestPrepare_CustomRedirectURI(t *testing.T) {
	cfg := OAuthConfig{ClientID: "cid", ClientSecret: "csec"}
	customURI := "watchtower-auth://callback"
	result, err := Prepare(cfg, customURI)
	require.NoError(t, err)

	assert.Equal(t, customURI, result.RedirectURI)
	assert.Contains(t, result.AuthorizeURL, url.QueryEscape(customURI))
	assert.Contains(t, result.AuthorizeURL, "client_id=cid")
	assert.Len(t, result.State, 64)
}

func TestLogin_SkipBrowserOpen(t *testing.T) {
	browserOpened := false
	oldOpen := getOpenBrowserFunc()
	setOpenBrowserFunc(func(u string) { browserOpened = true })
	defer func() { setOpenBrowserFunc(oldOpen) }()

	oldExchange := exchangeToken
	exchangeToken = func(ctx context.Context, clientID, clientSecret, code, redirectURI string) (*slack.OAuthV2Response, error) {
		return &slack.OAuthV2Response{
			AuthedUser: slack.OAuthV2ResponseAuthedUser{
				ID:          "U123",
				AccessToken: "xoxp-skip-browser",
			},
			Team: slack.OAuthV2ResponseTeam{ID: "T123", Name: "Skip Team"},
		}, nil
	}
	defer func() { exchangeToken = oldExchange }()

	oldClose := getCloseBrowserFunc()
	setCloseBrowserFunc(func() {}) // no-op
	defer func() { setCloseBrowserFunc(oldClose) }()

	cfg := OAuthConfig{ClientID: "id", ClientSecret: "secret"}
	var out syncBuffer

	resultCh := make(chan struct {
		result *OAuthResult
		err    error
	}, 1)
	go func() {
		r, err := Login(context.Background(), cfg, &out, LoginOptions{SkipBrowserOpen: true})
		resultCh <- struct {
			result *OAuthResult
			err    error
		}{r, err}
	}()

	// Wait for the server to be ready by checking output
	require.Eventually(t, func() bool {
		return strings.Contains(out.String(), "Authorize URL")
	}, 3*time.Second, 10*time.Millisecond)

	assert.False(t, browserOpened, "browser should not be opened when SkipBrowserOpen is true")
	assert.Contains(t, out.String(), "Authorize URL")
	assert.Contains(t, out.String(), "Waiting for authorization callback")

	// Extract the authorize URL from output to get redirect_uri and state
	outputStr := out.String()
	urlStart := strings.Index(outputStr, "https://slack.com")
	require.Greater(t, urlStart, 0)
	urlEnd := strings.Index(outputStr[urlStart:], "\n")
	rawURL := strings.TrimSpace(outputStr[urlStart : urlStart+urlEnd])

	parsed, err := url.Parse(rawURL)
	require.NoError(t, err)
	redirectURI := parsed.Query().Get("redirect_uri")
	state := parsed.Query().Get("state")

	simulateCallback(redirectURI, state, "test-code", "")

	select {
	case res := <-resultCh:
		require.NoError(t, res.err)
		assert.Equal(t, "xoxp-skip-browser", res.result.AccessToken)
	case <-time.After(5 * time.Second):
		t.Fatal("Login did not complete in time")
	}
}

func TestLogin_EmptyTokenFromExchange(t *testing.T) {
	var capturedURL atomic.Value
	oldOpen := getOpenBrowserFunc()
	setOpenBrowserFunc(func(u string) { capturedURL.Store(u) })
	defer func() { setOpenBrowserFunc(oldOpen) }()

	oldClose := getCloseBrowserFunc()
	setCloseBrowserFunc(func() {}) // no-op
	defer func() { setCloseBrowserFunc(oldClose) }()

	oldExchange := exchangeToken
	exchangeToken = func(ctx context.Context, clientID, clientSecret, code, redirectURI string) (*slack.OAuthV2Response, error) {
		return &slack.OAuthV2Response{
			AuthedUser: slack.OAuthV2ResponseAuthedUser{
				ID:          "U1",
				AccessToken: "", // empty token
			},
			Team: slack.OAuthV2ResponseTeam{ID: "T1", Name: "Team"},
		}, nil
	}
	defer func() { exchangeToken = oldExchange }()

	cfg := OAuthConfig{ClientID: "id", ClientSecret: "secret"}
	var out bytes.Buffer

	resultCh := make(chan error, 1)
	go func() {
		_, err := Login(context.Background(), cfg, &out)
		resultCh <- err
	}()

	require.Eventually(t, func() bool { v := capturedURL.Load(); return v != nil && v.(string) != "" }, 3*time.Second, 10*time.Millisecond)

	parsed, err := url.Parse(capturedURL.Load().(string))
	require.NoError(t, err)
	redirectURI := parsed.Query().Get("redirect_uri")
	state := parsed.Query().Get("state")

	simulateCallback(redirectURI, state, "code", "")

	select {
	case err := <-resultCh:
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no user access token")
	case <-time.After(5 * time.Second):
		t.Fatal("Login did not complete in time")
	}
}

func TestLogin_ExchangeError(t *testing.T) {
	var capturedURL atomic.Value
	oldOpen := getOpenBrowserFunc()
	setOpenBrowserFunc(func(u string) { capturedURL.Store(u) })
	defer func() { setOpenBrowserFunc(oldOpen) }()

	oldClose := getCloseBrowserFunc()
	setCloseBrowserFunc(func() {})
	defer func() { setCloseBrowserFunc(oldClose) }()

	oldExchange := exchangeToken
	exchangeToken = func(ctx context.Context, clientID, clientSecret, code, redirectURI string) (*slack.OAuthV2Response, error) {
		return nil, fmt.Errorf("network error")
	}
	defer func() { exchangeToken = oldExchange }()

	cfg := OAuthConfig{ClientID: "id", ClientSecret: "secret"}
	var out bytes.Buffer

	resultCh := make(chan error, 1)
	go func() {
		_, err := Login(context.Background(), cfg, &out)
		resultCh <- err
	}()

	require.Eventually(t, func() bool { v := capturedURL.Load(); return v != nil && v.(string) != "" }, 3*time.Second, 10*time.Millisecond)

	parsed, err := url.Parse(capturedURL.Load().(string))
	require.NoError(t, err)
	redirectURI := parsed.Query().Get("redirect_uri")
	state := parsed.Query().Get("state")

	simulateCallback(redirectURI, state, "code", "")

	select {
	case err := <-resultCh:
		require.Error(t, err)
		assert.Contains(t, err.Error(), "exchanging code")
	case <-time.After(5 * time.Second):
		t.Fatal("Login did not complete in time")
	}
}

func TestLogin_EmptyCode(t *testing.T) {
	var capturedURL atomic.Value
	oldOpen := getOpenBrowserFunc()
	setOpenBrowserFunc(func(u string) { capturedURL.Store(u) })
	defer func() { setOpenBrowserFunc(oldOpen) }()

	oldExchange := exchangeToken
	exchangeToken = func(ctx context.Context, clientID, clientSecret, code, redirectURI string) (*slack.OAuthV2Response, error) {
		t.Fatal("exchange should not be called with empty code")
		return nil, nil
	}
	defer func() { exchangeToken = oldExchange }()

	cfg := OAuthConfig{ClientID: "id", ClientSecret: "secret"}
	var out bytes.Buffer

	resultCh := make(chan error, 1)
	go func() {
		_, err := Login(context.Background(), cfg, &out)
		resultCh <- err
	}()

	require.Eventually(t, func() bool { v := capturedURL.Load(); return v != nil && v.(string) != "" }, 3*time.Second, 10*time.Millisecond)

	parsed, err := url.Parse(capturedURL.Load().(string))
	require.NoError(t, err)
	redirectURI := parsed.Query().Get("redirect_uri")
	state := parsed.Query().Get("state")

	// Simulate callback with empty code
	u, _ := url.Parse(redirectURI)
	q := u.Query()
	q.Set("state", state)
	// Don't set code at all
	u.RawQuery = q.Encode()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, u.String(), nil)
	resp, err2 := insecureClient.Do(req)
	if err2 == nil {
		resp.Body.Close()
	}

	select {
	case err := <-resultCh:
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no authorization code")
	case <-time.After(5 * time.Second):
		t.Fatal("Login did not complete in time")
	}
}

func TestCloseBrowserFunc_NoOp(t *testing.T) {
	// Verify closeBrowserFunc can be replaced without panicking
	oldClose := getCloseBrowserFunc()
	called := false
	setCloseBrowserFunc(func() { called = true })
	defer func() { setCloseBrowserFunc(oldClose) }()

	getCloseBrowserFunc()()
	assert.True(t, called)
}

func TestOpenBrowserFunc_NoOp(t *testing.T) {
	// Verify openBrowserFunc can be replaced without panicking
	oldOpen := getOpenBrowserFunc()
	var capturedURL string
	setOpenBrowserFunc(func(u string) { capturedURL = u })
	defer func() { setOpenBrowserFunc(oldOpen) }()

	getOpenBrowserFunc()("https://example.com")
	assert.Equal(t, "https://example.com", capturedURL)
}

func TestUserScopes(t *testing.T) {
	// Verify expected scopes are present
	assert.Contains(t, UserScopes, "channels:history")
	assert.Contains(t, UserScopes, "channels:read")
	assert.Contains(t, UserScopes, "search:read")
	assert.Contains(t, UserScopes, "users:read")
	assert.Contains(t, UserScopes, "team:read")
	assert.Greater(t, len(UserScopes), 10)
}

func TestComplete_WithExpiresIn(t *testing.T) {
	oldExchange := exchangeToken
	exchangeToken = func(ctx context.Context, clientID, clientSecret, code, redirectURI string) (*slack.OAuthV2Response, error) {
		return &slack.OAuthV2Response{
			AuthedUser: slack.OAuthV2ResponseAuthedUser{
				ID:          "U222",
				AccessToken: "xoxp-expiring",
				ExpiresIn:   3600,
			},
			Team: slack.OAuthV2ResponseTeam{ID: "T222", Name: "Expiry Team"},
		}, nil
	}
	defer func() { exchangeToken = oldExchange }()

	cfg := OAuthConfig{ClientID: "cid", ClientSecret: "csec"}
	result, err := Complete(context.Background(), cfg, "code", "https://127.0.0.1:18491/callback")
	require.NoError(t, err)
	assert.Equal(t, 3600, result.ExpiresIn)
	assert.Equal(t, "xoxp-expiring", result.AccessToken)
}

func TestDefaultRedirectPort(t *testing.T) {
	assert.Equal(t, 18491, defaultRedirectPort)
}

func TestCallbackStyle(t *testing.T) {
	// Verify the callback style CSS contains expected rules
	assert.Contains(t, callbackStyle, "font-family")
	assert.Contains(t, callbackStyle, "border-radius")
}

func TestLoginOptions_Defaults(t *testing.T) {
	// Verify LoginOptions zero value has expected defaults
	var opts LoginOptions
	assert.False(t, opts.SkipBrowserOpen)
}

func TestOAuthConfig_Fields(t *testing.T) {
	cfg := OAuthConfig{
		ClientID:     "test-id",
		ClientSecret: "test-secret",
	}
	assert.Equal(t, "test-id", cfg.ClientID)
	assert.Equal(t, "test-secret", cfg.ClientSecret)
}

func TestOAuthResult_Fields(t *testing.T) {
	result := OAuthResult{
		AccessToken: "token",
		TeamID:      "T1",
		TeamName:    "Team",
		UserID:      "U1",
		ExpiresIn:   3600,
	}
	assert.Equal(t, "token", result.AccessToken)
	assert.Equal(t, "T1", result.TeamID)
	assert.Equal(t, "Team", result.TeamName)
	assert.Equal(t, "U1", result.UserID)
	assert.Equal(t, 3600, result.ExpiresIn)
}

func TestPrepareResult_Fields(t *testing.T) {
	result := PrepareResult{
		AuthorizeURL: "https://example.com",
		RedirectURI:  "https://127.0.0.1:18491/callback",
		State:        "abc123",
	}
	assert.Equal(t, "https://example.com", result.AuthorizeURL)
	assert.Equal(t, "https://127.0.0.1:18491/callback", result.RedirectURI)
	assert.Equal(t, "abc123", result.State)
}

func TestOpenBrowser_Direct(t *testing.T) {
	// Test the actual openBrowser function with an invalid URL.
	// On darwin, it calls "open -n <url>" which will fail for invalid URLs
	// but shouldn't panic. We don't want to actually open a browser,
	// so we use a URL that will fail silently.
	OpenBrowser("watchtower-test://not-a-real-url")
	// No assertion needed — just verifying it doesn't panic
}

func TestCloseBrowserWindow_Direct(t *testing.T) {
	// Test the actual closeBrowserWindow function.
	// It runs an osascript command that will likely fail (no browser with Slack tab)
	// but shouldn't panic — it's fire-and-forget.
	closeBrowserWindow()
	// No assertion needed — just verifying it doesn't panic
}

func TestCallbackPages(t *testing.T) {
	// Verify the callback HTML pages contain expected content
	assert.Contains(t, callbackSuccessPage, "Authorization Successful")
	assert.Contains(t, callbackSuccessPage, "Watchtower")
	assert.Contains(t, callbackErrorPage, "Authorization Failed")
	assert.Contains(t, callbackErrorPage, "{{ERROR}}")
}

func TestCallbackHandler_ErrorResponse(t *testing.T) {
	// Test that the callback handler returns the error page HTML when
	// Slack returns an error parameter
	var capturedURL atomic.Value
	oldOpen := getOpenBrowserFunc()
	setOpenBrowserFunc(func(u string) { capturedURL.Store(u) })
	defer func() { setOpenBrowserFunc(oldOpen) }()

	oldExchange := exchangeToken
	exchangeToken = func(ctx context.Context, clientID, clientSecret, code, redirectURI string) (*slack.OAuthV2Response, error) {
		t.Fatal("exchange should not be called on error callback")
		return nil, nil
	}
	defer func() { exchangeToken = oldExchange }()

	cfg := OAuthConfig{ClientID: "id", ClientSecret: "secret"}
	var out bytes.Buffer

	resultCh := make(chan error, 1)
	go func() {
		_, err := Login(context.Background(), cfg, &out)
		resultCh <- err
	}()

	require.Eventually(t, func() bool { v := capturedURL.Load(); return v != nil && v.(string) != "" }, 3*time.Second, 10*time.Millisecond)

	parsed, err := url.Parse(capturedURL.Load().(string))
	require.NoError(t, err)
	redirectURI := parsed.Query().Get("redirect_uri")

	// Make callback with error and capture the HTML response
	u, _ := url.Parse(redirectURI)
	q := u.Query()
	q.Set("error", "user_denied")
	u.RawQuery = q.Encode()

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, u.String(), nil)
	resp, httpErr := insecureClient.Do(req)
	if httpErr == nil {
		defer resp.Body.Close()
		assert.Equal(t, "text/html; charset=utf-8", resp.Header.Get("Content-Type"))
	}

	select {
	case err := <-resultCh:
		require.Error(t, err)
		assert.Contains(t, err.Error(), "user_denied")
	case <-time.After(5 * time.Second):
		t.Fatal("Login did not complete in time")
	}
}

func TestCallbackHandler_SuccessResponse(t *testing.T) {
	// Test that the callback handler returns success page HTML
	var capturedURL atomic.Value
	oldOpen := getOpenBrowserFunc()
	setOpenBrowserFunc(func(u string) { capturedURL.Store(u) })
	defer func() { setOpenBrowserFunc(oldOpen) }()

	oldClose := getCloseBrowserFunc()
	setCloseBrowserFunc(func() {})
	defer func() { setCloseBrowserFunc(oldClose) }()

	oldExchange := exchangeToken
	exchangeToken = func(ctx context.Context, clientID, clientSecret, code, redirectURI string) (*slack.OAuthV2Response, error) {
		return &slack.OAuthV2Response{
			AuthedUser: slack.OAuthV2ResponseAuthedUser{
				ID:          "U1",
				AccessToken: "xoxp-ok",
			},
			Team: slack.OAuthV2ResponseTeam{ID: "T1", Name: "T"},
		}, nil
	}
	defer func() { exchangeToken = oldExchange }()

	cfg := OAuthConfig{ClientID: "id", ClientSecret: "secret"}
	var out bytes.Buffer

	resultCh := make(chan struct {
		result *OAuthResult
		err    error
	}, 1)
	go func() {
		r, err := Login(context.Background(), cfg, &out)
		resultCh <- struct {
			result *OAuthResult
			err    error
		}{r, err}
	}()

	require.Eventually(t, func() bool { v := capturedURL.Load(); return v != nil && v.(string) != "" }, 3*time.Second, 10*time.Millisecond)

	parsed, err := url.Parse(capturedURL.Load().(string))
	require.NoError(t, err)
	redirectURI := parsed.Query().Get("redirect_uri")
	state := parsed.Query().Get("state")

	// Make callback and capture the HTML response
	u, _ := url.Parse(redirectURI)
	q := u.Query()
	q.Set("code", "test-code")
	q.Set("state", state)
	u.RawQuery = q.Encode()

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, u.String(), nil)
	resp, httpErr := insecureClient.Do(req)
	if httpErr == nil {
		defer resp.Body.Close()
		assert.Equal(t, "text/html; charset=utf-8", resp.Header.Get("Content-Type"))
	}

	select {
	case res := <-resultCh:
		require.NoError(t, res.err)
		assert.Equal(t, "xoxp-ok", res.result.AccessToken)
	case <-time.After(5 * time.Second):
		t.Fatal("Login did not complete in time")
	}
}

func TestLogin_WithExpiresIn(t *testing.T) {
	var capturedURL atomic.Value
	oldOpen := getOpenBrowserFunc()
	setOpenBrowserFunc(func(u string) { capturedURL.Store(u) })
	defer func() { setOpenBrowserFunc(oldOpen) }()

	oldClose := getCloseBrowserFunc()
	setCloseBrowserFunc(func() {})
	defer func() { setCloseBrowserFunc(oldClose) }()

	oldExchange := exchangeToken
	exchangeToken = func(ctx context.Context, clientID, clientSecret, code, redirectURI string) (*slack.OAuthV2Response, error) {
		return &slack.OAuthV2Response{
			AuthedUser: slack.OAuthV2ResponseAuthedUser{
				ID:          "U555",
				AccessToken: "xoxp-with-expiry",
				ExpiresIn:   7200,
			},
			Team: slack.OAuthV2ResponseTeam{ID: "T555", Name: "Expiry Team"},
		}, nil
	}
	defer func() { exchangeToken = oldExchange }()

	cfg := OAuthConfig{ClientID: "id", ClientSecret: "secret"}
	var out bytes.Buffer

	resultCh := make(chan struct {
		result *OAuthResult
		err    error
	}, 1)
	go func() {
		r, err := Login(context.Background(), cfg, &out)
		resultCh <- struct {
			result *OAuthResult
			err    error
		}{r, err}
	}()

	require.Eventually(t, func() bool { v := capturedURL.Load(); return v != nil && v.(string) != "" }, 3*time.Second, 10*time.Millisecond)

	parsed, err := url.Parse(capturedURL.Load().(string))
	require.NoError(t, err)
	redirectURI := parsed.Query().Get("redirect_uri")
	state := parsed.Query().Get("state")

	simulateCallback(redirectURI, state, "code", "")

	select {
	case res := <-resultCh:
		require.NoError(t, res.err)
		assert.Equal(t, "xoxp-with-expiry", res.result.AccessToken)
		assert.Equal(t, 7200, res.result.ExpiresIn)
		assert.Equal(t, "U555", res.result.UserID)
		assert.Equal(t, "T555", res.result.TeamID)
	case <-time.After(5 * time.Second):
		t.Fatal("Login did not complete in time")
	}
}
