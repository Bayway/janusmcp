// Package webui serves a small local control panel on localhost: list accounts,
// see login/secret status, trigger browser logins, add accounts from the catalog,
// and store PATs in the vault. It is a setup tool (its own process), not a runtime
// controller of a running broker.
package webui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/bayway/janusmcp/internal/broker"
	"github.com/bayway/janusmcp/internal/catalog"
	"github.com/bayway/janusmcp/internal/config"
	"github.com/bayway/janusmcp/internal/vault"
)

type Options struct {
	ConfigPath string
	Vault      vault.Vault
	Resolver   config.SecretResolver
	Host       string
	Port       int
}

type server struct {
	opt Options
}

// Serve starts the control panel and blocks until ctx is cancelled.
func Serve(ctx context.Context, opt Options) error {
	s := &server{opt: opt}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/accounts", s.handleAccounts)
	mux.HandleFunc("/api/templates", s.handleTemplates)
	mux.HandleFunc("/api/add", s.handleAdd)
	mux.HandleFunc("/api/login", s.handleLogin)
	mux.HandleFunc("/api/secret", s.handleSecret)

	addr := fmt.Sprintf("%s:%d", opt.Host, opt.Port)
	srv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	fmt.Fprintf(os.Stderr, "[janusmcp] control panel on http://%s/\n", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *server) loadCfg() (*config.Config, error) {
	if _, err := os.Stat(s.opt.ConfigPath); err != nil {
		return &config.Config{BindingMode: config.BindingSession}, nil
	}
	return config.LoadRaw(s.opt.ConfigPath)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

type accountView struct {
	ID        string   `json:"id"`
	Service   string   `json:"service"`
	Label     string   `json:"label"`
	Transport string   `json:"transport"`
	Auth      string   `json:"auth"`
	Status    string   `json:"status"`            // "ready" | "needs-login" | "needs-secret"
	Missing   []string `json:"missing,omitempty"` // vault secret names still missing
}

func (s *server) handleAccounts(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.loadCfg()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	out := make([]accountView, 0, len(cfg.Accounts))
	for _, a := range cfg.Accounts {
		v := accountView{ID: a.ID, Service: a.Service, Label: a.DisplayLabel(), Auth: a.Auth, Status: "ready"}
		v.Transport = a.Transport
		if v.Transport == "" {
			v.Transport = "stdio"
		}
		switch {
		case a.IsHTTP() && a.Auth == "oauth":
			if !s.vaultHas("remote_oauth_" + a.ID) {
				v.Status = "needs-login"
			}
		default:
			for _, val := range a.Env {
				if name := refName(val, "vault:"); name != "" && !s.vaultHas(name) {
					v.Status = "needs-secret"
					v.Missing = append(v.Missing, name)
				}
				if name := refName(val, "oauth:"); name != "" && !s.vaultHas("oauth_"+name) {
					v.Status = "needs-login"
				}
			}
		}
		out = append(out, v)
	}
	writeJSON(w, 200, map[string]any{"active": cfg.DefaultAccount, "accounts": out})
}

func refName(val, prefix string) string {
	if strings.HasPrefix(val, prefix) {
		return strings.TrimPrefix(val, prefix)
	}
	return ""
}

func (s *server) vaultHas(name string) bool {
	if s.opt.Vault == nil {
		return false
	}
	v, err := s.opt.Vault.Get(name)
	return err == nil && v != ""
}

func (s *server) handleTemplates(w http.ResponseWriter, r *http.Request) {
	type tv struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	t := catalog.Templates()
	out := make([]tv, 0, len(t))
	for _, name := range catalog.Names() {
		out = append(out, tv{Name: name, Description: t[name].Description})
	}
	writeJSON(w, 200, out)
}

func (s *server) handleAdd(w http.ResponseWriter, r *http.Request) {
	var in struct{ Template, ID string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad request"})
		return
	}
	tmpl, ok := catalog.Templates()[in.Template]
	if !ok {
		writeJSON(w, 400, map[string]string{"error": "unknown template"})
		return
	}
	id := in.ID
	if id == "" {
		id = in.Template
	}
	cfg, err := s.loadCfg()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	for _, a := range cfg.Accounts {
		if a.ID == id {
			writeJSON(w, 409, map[string]string{"error": "id already exists"})
			return
		}
	}
	cfg.Accounts = append(cfg.Accounts, tmpl.Build(id))
	if cfg.DefaultAccount == "" {
		cfg.DefaultAccount = id
	}
	if cfg.BindingMode == "" {
		cfg.BindingMode = config.BindingSession
	}
	if err := s.writeCfg(cfg); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "id": id, "notes": tmpl.Notes(id)})
}

func (s *server) writeCfg(cfg *config.Config) error {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.opt.ConfigPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.opt.ConfigPath, append(b, '\n'), 0o644)
}

func (s *server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var in struct{ ID string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad request"})
		return
	}
	cfg, err := s.loadCfg()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	dir := filepath.Dir(s.opt.ConfigPath)
	mgr := broker.NewUpstreamManager(cfg, dir, s.opt.Resolver, s.opt.Vault)
	defer mgr.CloseAll()

	// Connecting an oauth upstream triggers the browser login; the token is persisted.
	if _, err := mgr.Session(r.Context(), in.ID); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *server) handleSecret(w http.ResponseWriter, r *http.Request) {
	if s.opt.Vault == nil {
		writeJSON(w, 500, map[string]string{"error": "no vault"})
		return
	}
	var in struct{ Name, Value string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Name == "" {
		writeJSON(w, 400, map[string]string{"error": "name and value required"})
		return
	}
	if err := s.opt.Vault.Set(in.Name, in.Value); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("content-type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(indexHTML))
}
