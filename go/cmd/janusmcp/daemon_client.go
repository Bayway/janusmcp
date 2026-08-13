package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/bayway/janusmcp/internal/broker"
	"github.com/bayway/janusmcp/internal/config"
)

type cliAccess interface {
	accountsFor(selector string) ([]string, error)
	Tools(context.Context, string) ([]*mcp.Tool, error)
	Call(context.Context, string, string, json.RawMessage) (*mcp.CallToolResult, error)
	Account(string) (*config.Account, error)
	Close()
}

func (c *cliCore) Tools(ctx context.Context, id string) ([]*mcp.Tool, error) {
	return c.manager.Tools(ctx, id)
}

func (c *cliCore) Call(ctx context.Context, id, name string, payload json.RawMessage) (*mcp.CallToolResult, error) {
	cs, err := c.manager.Session(ctx, id)
	if err != nil {
		return nil, err
	}
	return cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: payload})
}

func (c *cliCore) Account(id string) (*config.Account, error) { return c.manager.Account(id) }
func (c *cliCore) Close()                                     { c.manager.CloseAll() }

type daemonCLICore struct {
	cfg   *config.Config
	state *broker.BrokerState
	cs    *mcp.ClientSession
}

func newDaemonCLICore(ctx context.Context, meta *daemonMetadata) (*daemonCLICore, error) {
	cfg, err := config.LoadRaw(meta.ConfigPath)
	if err != nil {
		return nil, cliErr(exitSelection, "config_error", err)
	}
	// A locked broker intentionally refuses session switches. Direct mode can
	// still perform explicit one-shot account calls, so auto mode falls back.
	if cfg.BindingMode == config.BindingLocked {
		return nil, cliErr(exitUpstream, "daemon_locked", errors.New("managed daemon routing is unavailable with bindingMode=locked"))
	}
	transport := &mcp.StreamableClientTransport{
		Endpoint:             meta.Endpoint,
		HTTPClient:           daemonHTTPClient(meta.Token, 0),
		DisableStandaloneSSE: true,
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "janusmcp-cli", Version: version}, nil)
	cs, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, cliErr(exitUpstream, "daemon_connect", err)
	}
	defaultActive := cfg.DefaultAccount
	if defaultActive == "" && len(cfg.Accounts) > 0 {
		defaultActive = cfg.Accounts[0].ID
	}
	statePath := envOr("JANUS_STATE", filepath.Join(filepath.Dir(meta.ConfigPath), ".janusmcp-state.json"))
	return &daemonCLICore{
		cfg:   cfg,
		state: broker.NewBrokerState(statePath, defaultActive, cfg.BindingMode),
		cs:    cs,
	}, nil
}

func (c *daemonCLICore) accountsFor(selector string) ([]string, error) {
	if selector == "" {
		selector = c.state.GlobalActive()
	}
	ids, ok := c.cfg.AccountsForSelector(selector)
	if !ok {
		return nil, cliErrf(exitSelection, "unknown_selector", "unknown account or profile %q (see: janusmcp status)", selector)
	}
	return ids, nil
}

func (c *daemonCLICore) Tools(ctx context.Context, id string) ([]*mcp.Tool, error) {
	res, err := c.cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "janus_with_account",
		Arguments: json.RawMessage(fmt.Sprintf(
			`{"account_id":%q,"full_schema":true}`,
			id,
		)),
	})
	if err != nil {
		return nil, err
	}
	if res.IsError {
		return nil, errors.New(firstContentText(res.Content))
	}
	var payload struct {
		Tools []*mcp.Tool `json:"tools"`
		Error string      `json:"error"`
	}
	if err := json.Unmarshal([]byte(firstContentText(res.Content)), &payload); err != nil {
		return nil, fmt.Errorf("decode daemon tool list: %w", err)
	}
	if payload.Error != "" {
		return nil, errors.New(payload.Error)
	}
	return payload.Tools, nil
}

// Call runs one tool on a specific account.
//
// This used to switch the session's active account and then call the tool: two
// requests that only work if they land in the same server-side session. MCP
// 2026-07-28 has no sessions, so the pair is not portable. janus_with_account
// carries the account and the call together and returns the upstream result
// verbatim, which keeps runCall's rendering and --json envelope identical.
func (c *daemonCLICore) Call(ctx context.Context, id, name string, payload json.RawMessage) (*mcp.CallToolResult, error) {
	if len(payload) == 0 || string(payload) == "null" {
		payload = json.RawMessage("{}")
	}
	args, err := json.Marshal(map[string]any{
		"account_id": id,
		"tool":       name,
		"arguments":  payload,
	})
	if err != nil {
		return nil, fmt.Errorf("encode daemon call: %w", err)
	}
	// json.RawMessage, not []byte: a plain byte slice marshals to a base64
	// string and the broker would see no arguments at all.
	res, err := c.cs.CallTool(ctx, &mcp.CallToolParams{Name: "janus_with_account", Arguments: json.RawMessage(args)})
	if err != nil {
		return nil, err
	}
	if brokerErr := brokerFailure(res); brokerErr != nil {
		return nil, brokerErr
	}
	return res, nil
}

// brokerFailure recognises janus_with_account's own {"ok":false,"error":…}
// envelope, which it returns as plain text with isError unset. A real upstream
// result never has that exact shape.
func brokerFailure(res *mcp.CallToolResult) error {
	if res.IsError || len(res.Content) != 1 {
		return nil
	}
	var env struct {
		OK    *bool  `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(firstContentText(res.Content)), &env); err != nil {
		return nil
	}
	if env.OK != nil && !*env.OK && env.Error != "" {
		return errors.New(env.Error)
	}
	return nil
}

func (c *daemonCLICore) Account(id string) (*config.Account, error) {
	for i := range c.cfg.Accounts {
		if c.cfg.Accounts[i].ID == id {
			return &c.cfg.Accounts[i], nil
		}
	}
	return nil, fmt.Errorf("unknown account: %s", id)
}

func (c *daemonCLICore) Close() { _ = c.cs.Close() }

func daemonPreference(forceDirect, forceDaemon bool) (string, error) {
	if forceDirect && forceDaemon {
		return "", cliErr(exitUsage, "invalid_arguments", errors.New("--direct and --daemon cannot be used together"))
	}
	if forceDirect {
		return "off", nil
	}
	if forceDaemon {
		return "require", nil
	}
	pref := strings.ToLower(strings.TrimSpace(envOr("JANUS_DAEMON", "auto")))
	if pref != "auto" && pref != "require" && pref != "off" {
		return "", cliErrf(exitUsage, "invalid_daemon_mode", "JANUS_DAEMON must be auto, require, or off: %q", pref)
	}
	return pref, nil
}

func openCLIAccess(ctx context.Context, forceDirect, forceDaemon bool) (cliAccess, error) {
	pref, err := daemonPreference(forceDirect, forceDaemon)
	if err != nil {
		return nil, err
	}
	if pref != "off" {
		meta, metaErr := loadDaemonMetadata()
		if metaErr == nil {
			health, healthErr := daemonHealth(meta, 750*time.Millisecond)
			cpath, _ := filepath.Abs(configPath())
			modTime, size := configSignature(cpath)
			mismatch := meta.ConfigPath != cpath || meta.Version != version ||
				meta.ConfigModTimeNS != modTime || meta.ConfigSize != size ||
				(health != nil && (health.Version != version || health.ConfigPath != cpath))
			if healthErr == nil && !mismatch {
				access, err := newDaemonCLICore(ctx, meta)
				if err == nil {
					return access, nil
				}
				healthErr = err
			}
			if mismatch {
				if pref == "require" {
					return nil, cliErr(exitUpstream, "daemon_mismatch", errors.New("daemon configuration or version differs; run 'janusmcp daemon restart'"))
				}
				fmt.Fprintln(os.Stderr, "[janusmcp] warning: daemon configuration or version differs; using direct mode (run 'janusmcp daemon restart')")
			} else if pref == "require" {
				return nil, cliErr(exitUpstream, "daemon_unavailable", healthErr)
			}
		} else if pref == "require" {
			return nil, cliErr(exitUpstream, "daemon_unavailable", errors.New("managed daemon is not running"))
		}
	}
	return newCLICore()
}
