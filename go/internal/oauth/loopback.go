// Package oauth implements an OAuth 2.0 Authorization Code + PKCE flow using a
// localhost loopback redirect. This is the mechanism that lets the broker add an
// account without the user pasting tokens by hand.
//
// STATUS: skeleton. The flow (PKCE, loopback callback, code->token exchange) is
// implemented against generic endpoints; per-provider config (Supabase, GitHub, …)
// and refresh scheduling are wired but not yet exercised end-to-end. Tokens are
// meant to be persisted via internal/vault.
package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Provider describes an OAuth endpoint pair and client credentials.
type Provider struct {
	Name         string   `json:"name,omitempty"`
	AuthURL      string   `json:"authURL"`
	TokenURL     string   `json:"tokenURL"`
	ClientID     string   `json:"clientId"`
	ClientSecret string   `json:"clientSecret,omitempty"` // optional for public clients using PKCE
	Scopes       []string `json:"scopes,omitempty"`
}

// Token is the result of a successful exchange.
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	ExpiresIn    int       `json:"expires_in"`
	ObtainedAt   time.Time `json:"obtained_at"`
}

func (t Token) Expiry() time.Time {
	if t.ExpiresIn == 0 {
		return time.Time{}
	}
	return t.ObtainedAt.Add(time.Duration(t.ExpiresIn) * time.Second)
}

// Login runs the interactive loopback flow and returns a Token.
func Login(ctx context.Context, p Provider) (*Token, error) {
	verifier := randString(64)
	challenge := s256(verifier)
	state := randString(24)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("loopback listen: %w", err)
	}
	defer ln.Close()
	redirectURI := fmt.Sprintf("http://%s/callback", ln.Addr().String())

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	srv := &http.Server{Handler: callbackHandler(state, codeCh, errCh)}
	go srv.Serve(ln)
	defer srv.Close()

	authURL := buildAuthURL(p, redirectURI, challenge, state)
	_ = OpenBrowser(authURL)
	fmt.Printf("If your browser didn't open, visit:\n%s\n", authURL)

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-errCh:
		return nil, err
	case code := <-codeCh:
		return exchange(ctx, p, code, verifier, redirectURI)
	case <-time.After(5 * time.Minute):
		return nil, fmt.Errorf("oauth: timed out waiting for callback")
	}
}

func callbackHandler(wantState string, codeCh chan<- string, errCh chan<- error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if e := q.Get("error"); e != "" {
			errCh <- fmt.Errorf("oauth error: %s", e)
			http.Error(w, "auth failed", http.StatusBadRequest)
			return
		}
		if q.Get("state") != wantState {
			errCh <- fmt.Errorf("oauth: state mismatch (possible CSRF)")
			http.Error(w, "state mismatch", http.StatusBadRequest)
			return
		}
		codeCh <- q.Get("code")
		w.Header().Set("content-type", "text/html")
		_, _ = w.Write([]byte("<h3>Account collegato. Puoi chiudere questa finestra.</h3>"))
	})
}

func buildAuthURL(p Provider, redirectURI, challenge, state string) string {
	v := url.Values{}
	v.Set("response_type", "code")
	v.Set("client_id", p.ClientID)
	v.Set("redirect_uri", redirectURI)
	v.Set("state", state)
	v.Set("code_challenge", challenge)
	v.Set("code_challenge_method", "S256")
	if len(p.Scopes) > 0 {
		v.Set("scope", strings.Join(p.Scopes, " "))
	}
	return p.AuthURL + "?" + v.Encode()
}

func exchange(ctx context.Context, p Provider, code, verifier, redirectURI string) (*Token, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("code_verifier", verifier)
	return postToken(ctx, p, form)
}

// Refresh exchanges a refresh_token for a new access token. The provider's
// refresh response may omit refresh_token; callers should keep the old one.
func Refresh(ctx context.Context, p Provider, refreshToken string) (*Token, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	return postToken(ctx, p, form)
}

// postToken posts a form to the token endpoint, adding client credentials.
func postToken(ctx context.Context, p Provider, form url.Values) (*Token, error) {
	form.Set("client_id", p.ClientID)
	if p.ClientSecret != "" {
		form.Set("client_secret", p.ClientSecret)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("content-type", "application/x-www-form-urlencoded")
	req.Header.Set("accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("token endpoint returned %d", resp.StatusCode)
	}
	var t Token
	if err := json.NewDecoder(resp.Body).Decode(&t); err != nil {
		return nil, fmt.Errorf("decode token: %w", err)
	}
	t.ObtainedAt = time.Now()
	return &t, nil
}

// OpenBrowser opens a URL in the user's default browser (best-effort).
func OpenBrowser(u string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", u).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", u).Start()
	default:
		return exec.Command("xdg-open", u).Start()
	}
}

func randString(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func s256(v string) string {
	sum := sha256.Sum256([]byte(v))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
