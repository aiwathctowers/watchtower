package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"fmt"
	"html"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/slack-go/slack"
)

const (
	slackAuthorizeURL = "https://slack.com/oauth/v2/authorize"
	callbackPath      = "/callback"
	loginTimeout      = 5 * time.Minute

	// Default Slack App credentials for the open-source Watchtower CLI.
	//
	// YES, the client secret is intentionally committed to source code.
	// This is standard practice for public OAuth apps distributed as open-source
	// CLIs (see: GitHub CLI, Slack CLI, etc.). For public apps the client secret
	// is NOT a security boundary — OAuth security relies on redirect URI
	// validation and user consent. Keeping these here lets users run
	// `watchtower auth login` without any extra setup.
	//
	// Override with WATCHTOWER_OAUTH_CLIENT_ID / WATCHTOWER_OAUTH_CLIENT_SECRET
	// env vars if you run your own Slack app.
	DefaultClientID     = "REDACTED_CLIENT_ID"
	DefaultClientSecret = "REDACTED_CLIENT_SECRET"
)

// UserScopes are the Slack user token scopes required by Watchtower.
var UserScopes = []string{
	"channels:history", "channels:read",
	"groups:history", "groups:read",
	"im:history", "im:read",
	"mpim:history", "mpim:read",
	"search:read",
	"users:read", "users:read.email",
	"files:read", "reactions:read", "team:read",
}

// exchangeToken is the function used to exchange an OAuth code for a token.
// Tests replace this to avoid hitting the real Slack API.
var exchangeToken = func(ctx context.Context, clientID, clientSecret, code, redirectURI string) (*slack.OAuthV2Response, error) {
	return slack.GetOAuthV2ResponseContext(ctx, http.DefaultClient, clientID, clientSecret, code, redirectURI)
}

// openBrowserFunc can be replaced in tests to avoid opening a real browser.
var openBrowserFunc = openBrowser

// OAuthConfig holds the Slack app credentials for the OAuth flow.
type OAuthConfig struct {
	ClientID     string
	ClientSecret string
}

// OAuthResult holds the result of a successful OAuth login.
type OAuthResult struct {
	AccessToken string
	TeamID      string
	TeamName    string
	UserID      string
	ExpiresIn   int
}

// callbackResult is sent from the HTTP callback handler to the Login goroutine.
type callbackResult struct {
	code  string
	state string
	err   string
}

// Login performs the Slack OAuth V2 flow:
//  1. Starts a temporary HTTPS server on localhost (self-signed cert)
//  2. Opens the Slack authorize URL in the user's browser
//  3. Waits for the callback with an authorization code
//  4. Exchanges the code for a user token
func Login(ctx context.Context, cfg OAuthConfig, out io.Writer) (*OAuthResult, error) {
	// Generate self-signed TLS cert for 127.0.0.1
	tlsCert, err := generateSelfSignedCert()
	if err != nil {
		return nil, fmt.Errorf("generating TLS certificate: %w", err)
	}

	// Find an available port
	listener, err := listenLocalTLS(tlsCert)
	if err != nil {
		return nil, fmt.Errorf("starting local server: %w", err)
	}
	defer listener.Close()

	// Extract port from the underlying TCP listener address
	addr := listener.Addr().String()
	redirectURI := fmt.Sprintf("https://127.0.0.1:%s%s", portFromAddr(addr), callbackPath)

	// Generate random state
	state, err := randomState()
	if err != nil {
		return nil, fmt.Errorf("generating state: %w", err)
	}

	// Build authorize URL with user_scope (not scope) for user tokens
	params := url.Values{
		"client_id":    {cfg.ClientID},
		"user_scope":   {strings.Join(UserScopes, ",")},
		"redirect_uri": {redirectURI},
		"state":        {state},
	}
	authorizeURL := slackAuthorizeURL + "?" + params.Encode()

	resultCh := make(chan callbackResult, 1)

	mux := http.NewServeMux()
	mux.HandleFunc(callbackPath, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if errMsg := q.Get("error"); errMsg != "" {
			resultCh <- callbackResult{err: errMsg}
			fmt.Fprintf(w, "<html><body><h2>Authorization failed: %s</h2><p>You can close this tab.</p></body></html>", html.EscapeString(errMsg))
			return
		}
		resultCh <- callbackResult{
			code:  q.Get("code"),
			state: q.Get("state"),
		}
		fmt.Fprint(w, "<html><body><h2>Authorization successful!</h2><p>You can close this tab and return to the terminal.</p></body></html>")
	})

	server := &http.Server{Handler: mux}
	go server.Serve(listener) //nolint:errcheck
	defer func() {
		// Grace period to let the browser receive the HTML response before closing.
		time.Sleep(500 * time.Millisecond)
		server.Close()
	}()

	// Print URL and try to open browser
	fmt.Fprintf(out, "Opening browser for Slack authorization...\n")
	fmt.Fprintf(out, "If the browser doesn't open, visit this URL:\n\n  %s\n\n", authorizeURL)
	fmt.Fprintf(out, "Note: your browser may show a security warning for the localhost redirect.\n")
	fmt.Fprintf(out, "This is expected — click \"Advanced\" → \"Proceed\" to continue.\n\n")
	openBrowserFunc(authorizeURL)

	// Wait for callback or timeout
	ctx, cancel := context.WithTimeout(ctx, loginTimeout)
	defer cancel()

	var cb callbackResult
	select {
	case cb = <-resultCh:
	case <-ctx.Done():
		return nil, fmt.Errorf("login timed out after %s", loginTimeout)
	}

	if cb.err != "" {
		return nil, fmt.Errorf("slack authorization denied: %s", cb.err)
	}

	if cb.state != state {
		return nil, fmt.Errorf("state mismatch: possible CSRF attack")
	}

	if cb.code == "" {
		return nil, fmt.Errorf("no authorization code received")
	}

	// Exchange code for token
	resp, err := exchangeToken(ctx, cfg.ClientID, cfg.ClientSecret, cb.code, redirectURI)
	if err != nil {
		return nil, fmt.Errorf("exchanging code for token: %w", err)
	}

	result := &OAuthResult{
		AccessToken: resp.AuthedUser.AccessToken,
		TeamID:      resp.Team.ID,
		TeamName:    resp.Team.Name,
		UserID:      resp.AuthedUser.ID,
		ExpiresIn:   resp.AuthedUser.ExpiresIn,
	}

	if result.AccessToken == "" {
		return nil, fmt.Errorf("no user access token in response (did you configure user_scope in your Slack app?)")
	}

	return result, nil
}

// generateSelfSignedCert creates a short-lived self-signed TLS certificate for 127.0.0.1.
func generateSelfSignedCert() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, err
	}

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "Watchtower OAuth Callback"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(10 * time.Minute),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}

	return tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  key,
	}, nil
}

// listenLocalTLS tries preferred ports with TLS, then falls back to a random port.
func listenLocalTLS(cert tls.Certificate) (net.Listener, error) {
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	preferred := []int{18491, 18492, 18493, 18494, 18495, 18496, 18497, 18498, 18499, 18500}
	for _, port := range preferred {
		ln, err := tls.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port), tlsConfig)
		if err == nil {
			return ln, nil
		}
	}
	return tls.Listen("tcp", "127.0.0.1:0", tlsConfig)
}

// portFromAddr extracts the port from a "host:port" address string.
func portFromAddr(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return port
}

// randomState generates a cryptographically random hex string.
func randomState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func openBrowser(rawURL string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", rawURL)
	case "linux":
		cmd = exec.Command("xdg-open", rawURL)
	default:
		return
	}
	if err := cmd.Start(); err == nil {
		go cmd.Wait() // reap child process to avoid zombie
	}
}
