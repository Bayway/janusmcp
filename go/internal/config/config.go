// Package config loads the broker configuration (JSONC) and resolves secrets.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/bayway/janusmcp/internal/oauth"
)

type BindingMode string

const (
	BindingGlobal  BindingMode = "global"
	BindingSession BindingMode = "session"
	BindingLocked  BindingMode = "locked"
)

type Account struct {
	ID      string            `json:"id"`
	Service string            `json:"service"`
	Label   string            `json:"label,omitempty"`

	// stdio upstream (default): a local process speaking MCP over stdin/stdout.
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`

	// remote upstream: set Transport:"http" and URL to a Streamable HTTP MCP endpoint.
	Transport string `json:"transport,omitempty"` // "stdio" (default) | "http"
	URL       string `json:"url,omitempty"`
	// Auth:"oauth" makes the broker perform the MCP-native OAuth flow (with dynamic
	// client registration) against the remote server, opening a browser on first use.
	Auth string `json:"auth,omitempty"` // "" | "oauth"
}

// IsHTTP reports whether the account is a remote (Streamable HTTP) upstream.
func (a Account) IsHTTP() bool { return a.Transport == "http" }

type Config struct {
	DefaultAccount string                    `json:"defaultAccount,omitempty"`
	BindingMode    BindingMode               `json:"bindingMode,omitempty"`
	Accounts       []Account                 `json:"accounts"`
	OAuthProviders map[string]oauth.Provider `json:"oauthProviders,omitempty"`
}

func (a Account) DisplayLabel() string {
	if a.Label != "" {
		return a.Label
	}
	return a.ID
}

// SecretResolver expands secret references. Implementations: env (${VAR}) and vault (vault:<name>).
type SecretResolver func(ref string) (string, error)

var envRe = regexp.MustCompile(`\$\{([A-Za-z0-9_]+)\}`)

// ExpandEnv expands ${VAR} placeholders from the process environment.
func ExpandEnv(v string) string {
	return envRe.ReplaceAllStringFunc(v, func(m string) string {
		return os.Getenv(envRe.FindStringSubmatch(m)[1])
	})
}

// parse strips JSONC comments, unmarshals and validates structure.
func parse(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal([]byte(stripJSONC(string(raw))), &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if len(cfg.Accounts) == 0 {
		return nil, fmt.Errorf("config: 'accounts' is empty")
	}
	if cfg.BindingMode == "" {
		cfg.BindingMode = BindingGlobal
	}
	for i := range cfg.Accounts {
		a := &cfg.Accounts[i]
		if a.ID == "" {
			return nil, fmt.Errorf("config: account missing id: %+v", a)
		}
		if a.IsHTTP() {
			if a.URL == "" {
				return nil, fmt.Errorf("config: http account %s missing url", a.ID)
			}
		} else if a.Command == "" {
			return nil, fmt.Errorf("config: stdio account %s missing command", a.ID)
		}
	}
	return &cfg, nil
}

// LoadRaw parses and validates without resolving any secrets. Used by `login`,
// which only needs the provider registry and must not fail on unresolved tokens.
func LoadRaw(path string) (*Config, error) { return parse(path) }

// LoadRawOrEmpty behaves like LoadRaw, but returns an empty config (no accounts)
// when the file does not exist — so `serve` can start before any account is added
// (a fresh install, or container introspection like Glama's health check). Only the
// control tools are exposed until accounts are configured. A malformed existing file
// still returns an error.
func LoadRawOrEmpty(path string) (*Config, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return &Config{BindingMode: BindingGlobal}, nil
	}
	return parse(path)
}

// Load parses, then resolves every env/arg value through the resolver. If resolve
// is nil it falls back to plain ${ENV} expansion.
func Load(path string, resolve SecretResolver) (*Config, error) {
	cfg, err := parse(path)
	if err != nil {
		return nil, err
	}
	if resolve == nil {
		resolve = func(v string) (string, error) { return ExpandEnv(v), nil }
	}
	for i := range cfg.Accounts {
		a := &cfg.Accounts[i]
		for k, v := range a.Env {
			rv, err := resolve(v)
			if err != nil {
				return nil, fmt.Errorf("account %s env %s: %w", a.ID, k, err)
			}
			a.Env[k] = rv
		}
		for j, arg := range a.Args {
			rv, err := resolve(arg)
			if err != nil {
				return nil, fmt.Errorf("account %s arg: %w", a.ID, err)
			}
			a.Args[j] = rv
		}
	}
	return cfg, nil
}

// stripJSONC removes // line comments and /* */ block comments, respecting strings,
// and tolerates trailing commas.
func stripJSONC(s string) string {
	var b strings.Builder
	inStr, inLine, inBlock := false, false, false
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		var next byte
		if i+1 < len(s) {
			next = s[i+1]
		}
		switch {
		case inLine:
			if c == '\n' {
				inLine = false
				b.WriteByte(c)
			}
		case inBlock:
			if c == '*' && next == '/' {
				inBlock = false
				i++
			}
		case inStr:
			b.WriteByte(c)
			if c == '\\' {
				if i+1 < len(s) {
					b.WriteByte(next)
					i++
				}
			} else if c == quote {
				inStr = false
			}
		default:
			if c == '"' || c == '\'' {
				inStr = true
				quote = c
				b.WriteByte(c)
			} else if c == '/' && next == '/' {
				inLine = true
				i++
			} else if c == '/' && next == '*' {
				inBlock = true
				i++
			} else {
				b.WriteByte(c)
			}
		}
	}
	out := b.String()
	trailing := regexp.MustCompile(`,(\s*[}\]])`)
	return trailing.ReplaceAllString(out, "$1")
}
