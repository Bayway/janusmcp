package broker

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"golang.org/x/oauth2"

	"github.com/bayway/janusmcp/internal/oauth"
)

// newOAuthHandler builds an MCP-native OAuth handler for a remote upstream.
// It uses Dynamic Client Registration (RFC 7591) — no preconfigured client id —
// and a localhost loopback redirect. On first use the SDK drives discovery + DCR,
// then calls our fetcher to open the browser.
//
// If secrets is non-nil, the resulting access token is persisted to the vault
// under key, so a valid token survives restarts (no re-login until it expires).
func newOAuthHandler(appName string, secrets oauth.Secrets, key string) (auth.OAuthHandler, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("oauth loopback listen: %w", err)
	}
	redirect := fmt.Sprintf("http://%s/callback", ln.Addr().String())

	fetcher := func(ctx context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
		return captureAuthCode(ctx, ln, args.URL)
	}

	inner, err := auth.NewAuthorizationCodeHandler(&auth.AuthorizationCodeHandlerConfig{
		DynamicClientRegistrationConfig: &auth.DynamicClientRegistrationConfig{
			Metadata: &oauthex.ClientRegistrationMetadata{
				RedirectURIs:    []string{redirect},
				ClientName:      appName,
				ApplicationType: "native",
			},
		},
		RedirectURL:              redirect,
		AuthorizationCodeFetcher: fetcher,
	})
	if err != nil {
		return nil, err
	}
	if secrets == nil {
		return inner, nil
	}
	return &persistentOAuthHandler{inner: inner, secrets: secrets, key: key}, nil
}

// persistentOAuthHandler wraps the SDK's auth handler to cache the token in the vault.
// A persisted, still-valid token is served without any browser interaction; once it
// expires the transport falls back to the full login flow.
type persistentOAuthHandler struct {
	inner   *auth.AuthorizationCodeHandler
	secrets oauth.Secrets
	key     string
}

func (p *persistentOAuthHandler) TokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	// 1. A token obtained by the inner handler in this process.
	if ts, err := p.inner.TokenSource(ctx); err == nil && ts != nil {
		if _, terr := ts.Token(); terr == nil {
			return &persistingTokenSource{src: ts, h: p}, nil
		}
	}
	// 2. A token persisted from a previous run.
	if tok := p.load(); tok != nil && tok.Valid() {
		return oauth2.StaticTokenSource(tok), nil
	}
	// 3. Nothing yet → let the transport trigger Authorize.
	return nil, nil
}

func (p *persistentOAuthHandler) Authorize(ctx context.Context, req *http.Request, resp *http.Response) error {
	if err := p.inner.Authorize(ctx, req, resp); err != nil {
		return err
	}
	if ts, err := p.inner.TokenSource(ctx); err == nil && ts != nil {
		if tok, err := ts.Token(); err == nil {
			p.save(tok)
		}
	}
	return nil
}

func (p *persistentOAuthHandler) load() *oauth2.Token {
	raw, err := p.secrets.Get(p.key)
	if err != nil || raw == "" {
		return nil
	}
	var t oauth2.Token
	if json.Unmarshal([]byte(raw), &t) != nil {
		return nil
	}
	return &t
}

func (p *persistentOAuthHandler) save(tok *oauth2.Token) {
	if tok == nil {
		return
	}
	if b, err := json.Marshal(tok); err == nil {
		_ = p.secrets.Set(p.key, string(b))
	}
}

// persistingTokenSource saves refreshed tokens back to the vault.
type persistingTokenSource struct {
	src oauth2.TokenSource
	h   *persistentOAuthHandler
}

func (s *persistingTokenSource) Token() (*oauth2.Token, error) {
	tok, err := s.src.Token()
	if err == nil && tok != nil {
		s.h.save(tok)
	}
	return tok, err
}

// captureAuthCode opens authURL in the browser and waits for the authorization
// server to redirect back to our loopback listener, returning code + state.
func captureAuthCode(ctx context.Context, ln net.Listener, authURL string) (*auth.AuthorizationResult, error) {
	resCh := make(chan *auth.AuthorizationResult, 1)
	errCh := make(chan error, 1)

	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if e := q.Get("error"); e != "" {
			errCh <- fmt.Errorf("authorization error: %s", e)
			http.Error(w, "authorization failed", http.StatusBadRequest)
			return
		}
		resCh <- &auth.AuthorizationResult{Code: q.Get("code"), State: q.Get("state")}
		w.Header().Set("content-type", "text/html")
		_, _ = w.Write([]byte("<h3>Account collegato. Puoi chiudere questa finestra.</h3>"))
	})}
	go srv.Serve(ln)
	defer srv.Close()

	_ = oauth.OpenBrowser(authURL)
	fmt.Fprintf(os.Stderr, "[janusmcp] authorize in your browser:\n%s\n", authURL)

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-errCh:
		return nil, err
	case res := <-resCh:
		return res, nil
	}
}
