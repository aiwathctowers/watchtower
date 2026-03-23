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
	"sync/atomic"
	"testing"
	"time"

	"github.com/slack-go/slack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// insecureClient is an HTTP client that skips TLS verification (for self-signed certs).
var insecureClient = &http.Client{
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
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
	oldOpen := openBrowserFunc
	openBrowserFunc = func(u string) { capturedURL.Store(u) }
	defer func() { openBrowserFunc = oldOpen }()

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
	oldOpen := openBrowserFunc
	openBrowserFunc = func(u string) { capturedURL.Store(u) }
	defer func() { openBrowserFunc = oldOpen }()

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
	oldOpen := openBrowserFunc
	openBrowserFunc = func(u string) { capturedURL.Store(u) }
	defer func() { openBrowserFunc = oldOpen }()

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
	oldOpen := openBrowserFunc
	openBrowserFunc = func(u string) {}
	defer func() { openBrowserFunc = oldOpen }()

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
	oldOpen := openBrowserFunc
	openBrowserFunc = func(u string) { capturedURL.Store(u) }
	defer func() { openBrowserFunc = oldOpen }()

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
	s1, err := randomState()
	require.NoError(t, err)
	assert.Len(t, s1, 64)

	s2, err := randomState()
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
