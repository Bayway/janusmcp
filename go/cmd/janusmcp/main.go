// Command janusmcp is the local multi-account MCP broker.
//
// Subcommands (run `janusmcp help` for the full reference):
//
//	janusmcp serve                 # run the broker (default)
//	janusmcp ui                    # local control panel
//	janusmcp add <template> [id]   # add an account from a template
//	janusmcp catalog               # list account templates
//	janusmcp connect <account-id>  # connect/authorize an account
//	janusmcp status                # show login/secret status
//	janusmcp vault set <name>      # store a secret (read from stdin)
//	janusmcp vault delete <name>   # remove a secret
//	janusmcp login <provider>      # OAuth loopback login
//	janusmcp providers             # list built-in OAuth providers
//	janusmcp install <client>      # configure an LLM client
//	janusmcp uninstall <client>    # remove from an LLM client
//
// Transports (serve): JANUS_TRANSPORT=stdio|http|both (default stdio),
// JANUS_HTTP_HOST (127.0.0.1), JANUS_HTTP_PORT (7332).
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/bayway/janusmcp/internal/broker"
	"github.com/bayway/janusmcp/internal/catalog"
	"github.com/bayway/janusmcp/internal/config"
	"github.com/bayway/janusmcp/internal/oauth"
	"github.com/bayway/janusmcp/internal/vault"
	"github.com/bayway/janusmcp/internal/webui"
)

// expandProviders expands ${ENV} placeholders in the OAuth provider registry
// (LoadRaw does not resolve secrets, so client ids/urls must be expanded here).
func expandProviders(in map[string]oauth.Provider) map[string]oauth.Provider {
	out := make(map[string]oauth.Provider, len(in))
	for k, p := range in {
		p.AuthURL = config.ExpandEnv(p.AuthURL)
		p.TokenURL = config.ExpandEnv(p.TokenURL)
		p.ClientID = config.ExpandEnv(p.ClientID)
		p.ClientSecret = config.ExpandEnv(p.ClientSecret)
		out[k] = p
	}
	return out
}

// mergeProviders overlays user-defined providers on top of the built-in defaults.
func mergeProviders(base, override map[string]oauth.Provider) map[string]oauth.Provider {
	out := make(map[string]oauth.Provider, len(base)+len(override))
	for k, p := range base {
		out[k] = p
	}
	for k, p := range override {
		out[k] = p
	}
	return out
}

// buildResolver composes secret resolution: vault:<name>, oauth:<name>, then ${ENV}.
func buildResolver(v vault.Vault, store *oauth.Store) config.SecretResolver {
	ctx := context.Background()
	return func(val string) (string, error) {
		if name, ok := vault.ParseRef(val); ok {
			return v.Get(name)
		}
		if strings.HasPrefix(val, "oauth:") {
			return store.AccessToken(ctx, strings.TrimPrefix(val, "oauth:"))
		}
		return config.ExpandEnv(val), nil
	}
}

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	var err error
	switch cmd {
	case "serve":
		err = runServe()
	case "vault":
		err = runVault(os.Args[2:])
	case "login":
		err = runLogin(os.Args[2:])
	case "providers":
		err = runProviders()
	case "catalog":
		err = runCatalog()
	case "add":
		err = runAdd(os.Args[2:])
	case "connect":
		err = runConnect(os.Args[2:])
	case "status":
		err = runStatus()
	case "install":
		err = runInstall(os.Args[2:])
	case "uninstall":
		err = runUninstall(os.Args[2:])
	case "ui":
		err = runUI()
	case "version", "--version", "-v":
		fmt.Println("janusmcp", version)
	case "help", "--help", "-h":
		printUsage(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", cmd)
		printUsage(os.Stderr)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "[janusmcp] error: %v\n", err)
		os.Exit(1)
	}
}

// printUsage writes the full command reference. Kept in sync with the README.
func printUsage(w io.Writer) {
	fmt.Fprint(w, `JanusMCP — one MCP endpoint, every account.

Usage:
  janusmcp <command> [args]

Commands:
  serve                      Run the broker (default if no command given).
                             Transports via env: JANUS_TRANSPORT=stdio|http|both
                             (default stdio), JANUS_HTTP_HOST, JANUS_HTTP_PORT.
  ui                         Open the local control panel (add accounts, log in).
  add <template> [id]        Add an account from a template (see: catalog).
  catalog                    List the built-in account templates.
  connect <account-id>       Connect an account; for remote OAuth, opens the browser.
  status                     Show each account's login/secret status (no values).
  vault set <name>           Store a secret (read from stdin) in the OS keychain.
  vault delete <name>        Remove a stored secret.
  login <provider> <name>    OAuth loopback login; token stored as "oauth:<name>".
  providers                  List built-in OAuth providers.
  install <client>           Configure an LLM client to launch JanusMCP.
  uninstall <client>         Remove JanusMCP from an LLM client's config.
  version                    Print the version.
  help                       Show this help.

Clients (install/uninstall):
  claude-desktop | claude-code | cursor | vscode | gemini | codex | chatgpt | print
  (install/uninstall list   shows targets and which are configured.)

Control tools (inside any client): janus_list_accounts, janus_use_account,
janus_whoami, janus_login.

Docs: https://github.com/bayway/janusmcp
`)
}

func buildVault() (vault.Vault, error) {
	if strings.EqualFold(os.Getenv("JANUS_VAULT"), "file") {
		dir := envOr("JANUS_VAULT_DIR", ".")
		return vault.NewFile(filepath.Join(dir, "janusmcp-vault.enc"), filepath.Join(dir, "janusmcp-vault.key"))
	}
	return vault.NewKeyring(), nil
}

// configPath resolves the config location: JANUS_CONFIG, else ./config.json if it
// exists (dev/local), else a stable per-user path (used by installers and the .mcpb).
func configPath() string {
	if p := os.Getenv("JANUS_CONFIG"); p != "" {
		return p
	}
	if _, err := os.Stat("config.json"); err == nil {
		return "config.json"
	}
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "janusmcp", "config.json")
	}
	return "config.json"
}

func runServe() error {
	v, err := buildVault()
	if err != nil {
		return err
	}
	cpath, _ := filepath.Abs(configPath())
	cdir := filepath.Dir(cpath)

	// Parse without resolving: secrets (incl. fresh OAuth tokens) are resolved
	// per upstream spawn by the manager, not once at startup.
	cfg, err := config.LoadRawOrEmpty(cpath)
	if err != nil {
		return err
	}
	providers := expandProviders(mergeProviders(oauth.DefaultProviders(), cfg.OAuthProviders))
	store := oauth.NewStore(v, providers)
	resolver := buildResolver(v, store)

	defaultActive := cfg.DefaultAccount
	if defaultActive == "" && len(cfg.Accounts) > 0 {
		defaultActive = cfg.Accounts[0].ID
	}
	statePath := envOr("JANUS_STATE", filepath.Join(cdir, ".janusmcp-state.json"))

	core := &broker.Core{
		Cfg:      cfg,
		Manager:  broker.NewUpstreamManager(cfg, cdir, resolver, v),
		State:    broker.NewBrokerState(statePath, defaultActive, cfg.BindingMode),
		Registry: broker.NewSessionRegistry(),
		Store:    store,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	defer core.Manager.CloseAll()

	transport := strings.ToLower(envOr("JANUS_TRANSPORT", "stdio"))
	wantHTTP := transport == "http" || transport == "both"
	wantStdio := transport == "stdio" || transport == "both"

	if wantHTTP {
		host := envOr("JANUS_HTTP_HOST", "127.0.0.1")
		port := envOr("JANUS_HTTP_PORT", "7332")
		mux := http.NewServeMux()
		mux.Handle("/mcp", core.HTTPHandler())
		addr := host + ":" + port
		srv := &http.Server{Addr: addr, Handler: mux}
		go func() {
			fmt.Fprintf(os.Stderr, "[janusmcp] http up on http://%s/mcp\n", addr)
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				fmt.Fprintf(os.Stderr, "[janusmcp] http error: %v\n", err)
			}
		}()
		defer srv.Close()
	}

	if wantStdio {
		fmt.Fprintf(os.Stderr, "[janusmcp] stdio up. active=%s bindingMode=%s accounts=%d\n",
			core.State.GlobalActive(), cfg.BindingMode, len(cfg.Accounts))
		return core.RunStdio(ctx)
	}

	if !wantHTTP {
		return fmt.Errorf("nothing to do: JANUS_TRANSPORT=%s", transport)
	}
	<-ctx.Done()
	return nil
}

func runVault(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: janusmcp vault <set|delete> <name>")
	}
	v, err := buildVault()
	if err != nil {
		return err
	}
	action, name := args[0], args[1]
	switch action {
	case "set":
		fmt.Fprint(os.Stderr, "secret (end with newline): ")
		sc := bufio.NewScanner(os.Stdin)
		sc.Scan()
		if err := v.Set(name, strings.TrimRight(sc.Text(), "\r\n")); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "stored %q\n", name)
		return nil
	case "delete":
		return v.Delete(name)
	default:
		return fmt.Errorf("unknown vault action %q", action)
	}
}

// runStatus prints each account's login/secret status (no secret values are shown).
func runStatus() error {
	v, err := buildVault()
	if err != nil {
		return err
	}
	cpath, _ := filepath.Abs(configPath())
	cfg, err := config.LoadRaw(cpath)
	if err != nil {
		return err
	}
	has := func(name string) bool {
		s, e := v.Get(name)
		return e == nil && s != ""
	}

	fmt.Printf("%-22s %-9s %-7s %s\n", "ACCOUNT", "TRANSPORT", "AUTH", "STATUS")
	for _, a := range cfg.Accounts {
		transport := a.Transport
		if transport == "" {
			transport = "stdio"
		}
		status, detail := "ready", ""
		if a.IsHTTP() && a.Auth == "oauth" {
			if has("remote_oauth_" + a.ID) {
				status = "logged-in ✓"
			} else {
				status = "needs-login"
			}
		} else {
			for _, val := range a.Env {
				if strings.HasPrefix(val, "vault:") {
					n := strings.TrimPrefix(val, "vault:")
					if !has(n) {
						status, detail = "needs-secret", "missing vault:"+n
					}
				}
				if strings.HasPrefix(val, "oauth:") {
					n := strings.TrimPrefix(val, "oauth:")
					if !has("oauth_" + n) {
						status = "needs-login"
					}
				}
			}
		}
		fmt.Printf("%-22s %-9s %-7s %s %s\n", a.ID, transport, a.Auth, status, detail)
	}
	return nil
}

// runConnect connects (and, for remote OAuth accounts, logs into via browser) an
// account, persisting its token. This is the terminal way to do the browser login.
func runConnect(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: janusmcp connect <account-id>")
	}
	id := args[0]
	v, err := buildVault()
	if err != nil {
		return err
	}
	cpath, _ := filepath.Abs(configPath())
	cdir := filepath.Dir(cpath)
	cfg, err := config.LoadRaw(cpath)
	if err != nil {
		return err
	}
	store := oauth.NewStore(v, expandProviders(mergeProviders(oauth.DefaultProviders(), cfg.OAuthProviders)))
	mgr := broker.NewUpstreamManager(cfg, cdir, buildResolver(v, store), v)
	defer mgr.CloseAll()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	fmt.Fprintf(os.Stderr, "[janusmcp] connecting %q (a browser may open to authorize)…\n", id)
	if _, err := mgr.Session(ctx, id); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "[janusmcp] %q connected/authorized.\n", id)
	return nil
}

// runUI starts the local control panel and opens it in the browser.
func runUI() error {
	v, err := buildVault()
	if err != nil {
		return err
	}
	cpath, _ := filepath.Abs(configPath())
	var providers map[string]oauth.Provider
	if cfg, err := config.LoadRaw(cpath); err == nil {
		providers = cfg.OAuthProviders
	}
	store := oauth.NewStore(v, expandProviders(mergeProviders(oauth.DefaultProviders(), providers)))

	host := envOr("JANUS_UI_HOST", "127.0.0.1")
	port, _ := strconv.Atoi(envOr("JANUS_UI_PORT", "7333"))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	_ = oauth.OpenBrowser(fmt.Sprintf("http://%s:%d/", host, port))
	return webui.Serve(ctx, webui.Options{
		ConfigPath: cpath,
		Vault:      v,
		Resolver:   buildResolver(v, store),
		Host:       host,
		Port:       port,
	})
}

func runCatalog() error {
	fmt.Println("Account templates (use: janusmcp add <template> [id]):")
	t := catalog.Templates()
	for _, name := range catalog.Names() {
		fmt.Printf("  %-13s %s\n", name, t[name].Description)
	}
	return nil
}

func runAdd(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: janusmcp add <template> [id]   (see: janusmcp catalog)")
	}
	tmplName := args[0]
	tmpl, ok := catalog.Templates()[tmplName]
	if !ok {
		return fmt.Errorf("unknown template %q. Available: %s", tmplName, strings.Join(catalog.Names(), ", "))
	}
	id := tmplName
	if len(args) >= 2 {
		id = args[1]
	}

	cpath, _ := filepath.Abs(configPath())
	cfg := &config.Config{}
	if _, statErr := os.Stat(cpath); statErr == nil {
		loaded, err := config.LoadRaw(cpath)
		if err != nil {
			return fmt.Errorf("read existing config: %w", err)
		}
		cfg = loaded
	}
	for _, a := range cfg.Accounts {
		if a.ID == id {
			return fmt.Errorf("an account with id %q already exists in %s", id, cpath)
		}
	}

	cfg.Accounts = append(cfg.Accounts, tmpl.Build(id))
	if cfg.DefaultAccount == "" {
		cfg.DefaultAccount = id
	}
	if cfg.BindingMode == "" {
		cfg.BindingMode = config.BindingSession
	}

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(cpath), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	if err := os.WriteFile(cpath, append(out, '\n'), 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	fmt.Printf("Added account %q (%s) to %s\n", id, tmplName, cpath)
	for _, n := range tmpl.Notes(id) {
		fmt.Println("  →", n)
	}
	return nil
}

func runProviders() error {
	p := oauth.DefaultProviders()
	names := make([]string, 0, len(p))
	for k := range p {
		names = append(names, k)
	}
	sort.Strings(names)
	fmt.Println("Built-in OAuth providers (override or add in config 'oauthProviders'):")
	for _, name := range names {
		pr := p[name]
		fmt.Printf("  %-10s auth=%s\n             token=%s scopes=%v\n", name, pr.AuthURL, pr.TokenURL, pr.Scopes)
	}
	fmt.Println("\nNote: most Supabase setups use a Personal Access Token via the vault")
	fmt.Println("(env SUPABASE_ACCESS_TOKEN: \"vault:<name>\"), not OAuth. See docs/supabase.md.")
	return nil
}

func runLogin(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: janusmcp login <provider> <name>\n" +
			"  <provider> must be defined in config 'oauthProviders'; the token is stored under <name>,\n" +
			"  referenced from an account env as \"oauth:<name>\"")
	}
	provider, name := args[0], args[1]

	v, err := buildVault()
	if err != nil {
		return err
	}
	cpath, _ := filepath.Abs(configPath())
	rawCfg, err := config.LoadRaw(cpath)
	if err != nil {
		return err
	}
	store := oauth.NewStore(v, expandProviders(mergeProviders(oauth.DefaultProviders(), rawCfg.OAuthProviders)))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := store.Login(ctx, provider, name); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "logged in: token stored as %q (reference it with \"oauth:%s\")\n", name, name)
	return nil
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
