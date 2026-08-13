// Code-execution mode: invoke upstream tools on demand from the terminal instead
// of loading every tool definition into an LLM context.
//
//	janusmcp tools [selector]            # compact tool list (active account/profile)
//	janusmcp schema <tool> [selector]    # full JSON definition of one tool
//	janusmcp call <tool> [flags]         # call a tool, print its result
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/bayway/janusmcp/internal/broker"
	"github.com/bayway/janusmcp/internal/config"
	"github.com/bayway/janusmcp/internal/oauth"
)

// cliCore wires the same stack `serve` uses (vault → oauth store → resolver →
// upstream manager → persisted state) for a one-shot CLI invocation.
type cliCore struct {
	cfg     *config.Config
	manager *broker.UpstreamManager
	state   *broker.BrokerState
}

func newCLICore() (*cliCore, error) {
	v, err := buildVault()
	if err != nil {
		return nil, cliErr(exitAuth, "credential_store", err)
	}
	cpath, _ := filepath.Abs(configPath())
	cdir := filepath.Dir(cpath)
	cfg, err := config.LoadRaw(cpath)
	if err != nil {
		return nil, cliErr(exitSelection, "config_error", err)
	}
	store := oauth.NewStore(v, expandProviders(mergeProviders(oauth.DefaultProviders(), cfg.OAuthProviders)))

	defaultActive := cfg.DefaultAccount
	if defaultActive == "" && len(cfg.Accounts) > 0 {
		defaultActive = cfg.Accounts[0].ID
	}
	statePath := envOr("JANUS_STATE", filepath.Join(cdir, ".janusmcp-state.json"))

	return &cliCore{
		cfg:     cfg,
		manager: broker.NewUpstreamManager(cfg, cdir, buildResolver(v, store), v),
		state:   broker.NewBrokerState(statePath, defaultActive, cfg.BindingMode),
	}, nil
}

// accountsFor resolves a selector (account id or profile name; "" → the persisted
// active selector) to the account ids it covers.
func (c *cliCore) accountsFor(selector string) ([]string, error) {
	if selector == "" {
		selector = c.state.GlobalActive()
	}
	ids, ok := c.cfg.AccountsForSelector(selector)
	if !ok {
		return nil, cliErrf(exitSelection, "unknown_selector", "unknown account or profile %q (see: janusmcp status)", selector)
	}
	return ids, nil
}

// parseInterleaved parses fs accepting flags before AND after positionals
// (stdlib flag stops at the first positional, so `call ping --account b`
// would silently ignore --account). Returns the positional arguments.
func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return pos, nil
		}
		pos = append(pos, rest[0])
		args = rest[1:]
	}
}

// runTools prints a compact one-line-per-tool list, designed to be cheap to read
// for an agent: name + first sentence of the description. --json emits full
// definitions (schemas included) instead.
func runTools(args []string) error {
	fs := flag.NewFlagSet("tools", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit full tool definitions as JSON")
	timeout := fs.String("timeout", "", "cancel after a duration such as 30s or 2m")
	forceDirect := fs.Bool("direct", false, "bypass a running managed daemon")
	forceDaemon := fs.Bool("daemon", false, "require the managed daemon")
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return jsonIf(cliErr(exitUsage, "invalid_arguments", err), *asJSON)
	}
	var selector string
	if len(pos) > 0 {
		selector = pos[0]
	}

	ctx, stop, err := cliContext(*timeout)
	if err != nil {
		return jsonIf(err, *asJSON)
	}
	defer stop()
	c, err := openCLIAccess(ctx, *forceDirect, *forceDaemon)
	if err != nil {
		return jsonIf(err, *asJSON)
	}
	defer c.Close()
	ids, err := c.accountsFor(selector)
	if err != nil {
		return jsonIf(err, *asJSON)
	}

	if *asJSON {
		out := map[string][]*mcp.Tool{}
		for _, id := range ids {
			tools, err := c.Tools(ctx, id)
			if err != nil {
				return jsonIf(normalizeContextError(ctx, classifyUpstreamError("upstream_error", fmt.Errorf("account %s: %w", id, err))), true)
			}
			out[id] = tools
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}

	for _, id := range ids {
		tools, err := c.Tools(ctx, id)
		if err != nil {
			return normalizeContextError(ctx, classifyUpstreamError("upstream_error", fmt.Errorf("account %s: %w", id, err)))
		}
		a, _ := c.Account(id)
		fmt.Printf("# %s (%s) — %d tools\n", id, a.Service, len(tools))
		for _, t := range tools {
			fmt.Printf("%-32s %s\n", t.Name, firstSentence(t.Description))
		}
	}
	fmt.Println("\n# schema: janusmcp schema <tool>   call: janusmcp call <tool> --args '<json>'")
	return nil
}

// firstSentence trims a tool description to its first sentence/line, capped.
func firstSentence(s string) string {
	if i := strings.IndexAny(s, "\n"); i >= 0 {
		s = s[:i]
	}
	if i := strings.Index(s, ". "); i >= 0 {
		s = s[:i+1]
	}
	s = strings.TrimSpace(s)
	if len(s) > 120 {
		s = s[:117] + "..."
	}
	return s
}

// findTool locates a tool by name across the selector's accounts. A profile where
// several accounts define the same name is ambiguous and needs an explicit account.
func findTool(ctx context.Context, c cliAccess, selector, name string) (accountID string, tool *mcp.Tool, err error) {
	ids, err := c.accountsFor(selector)
	if err != nil {
		return "", nil, err
	}
	var owners []string
	for _, id := range ids {
		tools, terr := c.Tools(ctx, id)
		if terr != nil {
			return "", nil, normalizeContextError(ctx, classifyUpstreamError("upstream_error", fmt.Errorf("account %s: %w", id, terr)))
		}
		for _, t := range tools {
			if t.Name == name {
				owners = append(owners, id)
				if tool == nil {
					accountID, tool = id, t
				}
			}
		}
	}
	if tool == nil {
		return "", nil, cliErrf(exitSelection, "tool_not_found", "tool %q not found (see: janusmcp tools)", name)
	}
	if len(owners) > 1 {
		return "", nil, cliErrf(exitSelection, "ambiguous_tool", "tool %q exists on several accounts (%s): pick one with --account",
			name, strings.Join(owners, ", "))
	}
	return accountID, tool, nil
}

// runSchema prints the full JSON definition (input schema included) of one tool.
func runSchema(args []string) error {
	fs := flag.NewFlagSet("schema", flag.ContinueOnError)
	account := fs.String("account", "", "account id or profile to search (default: active)")
	timeout := fs.String("timeout", "", "cancel after a duration such as 30s or 2m")
	forceDirect := fs.Bool("direct", false, "bypass a running managed daemon")
	forceDaemon := fs.Bool("daemon", false, "require the managed daemon")
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return jsonErr(cliErr(exitUsage, "invalid_arguments", err))
	}
	if len(pos) < 1 {
		return jsonErr(cliErrf(exitUsage, "invalid_arguments", "usage: janusmcp schema <tool> [--account <id|profile>] [--timeout <duration>]"))
	}

	ctx, stop, err := cliContext(*timeout)
	if err != nil {
		return jsonErr(err)
	}
	defer stop()
	c, err := openCLIAccess(ctx, *forceDirect, *forceDaemon)
	if err != nil {
		return jsonErr(err)
	}
	defer c.Close()

	id, tool, err := findTool(ctx, c, *account, pos[0])
	if err != nil {
		return jsonErr(normalizeContextError(ctx, err))
	}
	out := struct {
		Account string `json:"account"`
		*mcp.Tool
	}{Account: id, Tool: tool}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// runCall invokes one tool and prints its result content to stdout. Arguments come
// from --args '<json>' or, when piped, stdin. A tool error exits non-zero.
func runCall(args []string) error {
	fs := flag.NewFlagSet("call", flag.ContinueOnError)
	account := fs.String("account", "", "account id or profile to search (default: active)")
	rawArgs := fs.String("args", "", "tool arguments as a JSON object (default: {} or stdin when piped)")
	asJSON := fs.Bool("json", false, "emit a stable JSON result envelope")
	timeout := fs.String("timeout", "", "cancel after a duration such as 30s or 2m")
	forceDirect := fs.Bool("direct", false, "bypass a running managed daemon")
	forceDaemon := fs.Bool("daemon", false, "require the managed daemon")
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return jsonIf(cliErr(exitUsage, "invalid_arguments", err), *asJSON)
	}
	if len(pos) < 1 {
		return jsonIf(cliErrf(exitUsage, "invalid_arguments", "usage: janusmcp call <tool> [--account <id|profile>] [--args '<json>'] [--json] [--timeout <duration>]"), *asJSON)
	}
	name := pos[0]

	payload := json.RawMessage("{}")
	if *rawArgs != "" {
		payload = json.RawMessage(*rawArgs)
	} else if st, err := os.Stdin.Stat(); err == nil && st.Mode()&os.ModeCharDevice == 0 {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return jsonIf(cliErr(exitUsage, "stdin_error", err), *asJSON)
		}
		if len(strings.TrimSpace(string(b))) > 0 {
			payload = json.RawMessage(b)
		}
	}
	var check map[string]any
	if err := json.Unmarshal(payload, &check); err != nil {
		return jsonIf(cliErrf(exitUsage, "invalid_json", "--args must be a JSON object: %v", err), *asJSON)
	}
	if check == nil {
		return jsonIf(cliErr(exitUsage, "invalid_json", fmt.Errorf("--args must be a JSON object, not null")), *asJSON)
	}

	ctx, stop, err := cliContext(*timeout)
	if err != nil {
		return jsonIf(err, *asJSON)
	}
	defer stop()
	c, err := openCLIAccess(ctx, *forceDirect, *forceDaemon)
	if err != nil {
		return jsonIf(err, *asJSON)
	}
	defer c.Close()

	id, _, err := findTool(ctx, c, *account, name)
	if err != nil {
		return jsonIf(normalizeContextError(ctx, err), *asJSON)
	}
	res, err := c.Call(ctx, id, name, payload)
	if err != nil {
		return jsonIf(normalizeContextError(ctx, classifyUpstreamError("upstream_protocol", err)), *asJSON)
	}
	// Under MCP 2026-07-28 a tool may answer with input_required instead of a
	// result, expecting the caller to collect input and retry. The CLI is
	// non-interactive and cannot, so report it as its own outcome rather than
	// pretending the call produced an empty result.
	if res.NeedsInput() {
		err := fmt.Errorf(
			"tool %q needs interactive input (account %s): run it from an MCP client that can answer elicitation requests",
			name, id)
		return jsonIf(cliErr(exitInputRequired, "input_required", err), *asJSON)
	}
	if *asJSON {
		envelope := map[string]any{
			"ok":      !res.IsError,
			"account": id,
			"tool":    name,
			"isError": res.IsError,
			"content": res.Content,
		}
		if res.StructuredContent != nil {
			envelope["structuredContent"] = res.StructuredContent
		}
		if res.IsError {
			envelope["error"] = map[string]string{"code": "tool_error", "message": firstContentText(res.Content)}
		}
		if err := json.NewEncoder(os.Stdout).Encode(envelope); err != nil {
			return cliErr(exitGeneric, "output_error", err)
		}
		if res.IsError {
			return renderedErr(exitTool, "tool_error", fmt.Errorf("tool %q returned an error (account %s)", name, id))
		}
		return nil
	}
	for _, content := range res.Content {
		if tc, ok := content.(*mcp.TextContent); ok {
			fmt.Println(tc.Text)
			continue
		}
		if b, err := json.Marshal(content); err == nil {
			fmt.Println(string(b))
		}
	}
	if res.IsError {
		return cliErrf(exitTool, "tool_error", "tool %q returned an error (account %s)", name, id)
	}
	return nil
}

func jsonIf(err error, enabled bool) error {
	if enabled && err != nil {
		return jsonErr(err)
	}
	return err
}

func firstContentText(content []mcp.Content) string {
	for _, item := range content {
		if tc, ok := item.(*mcp.TextContent); ok && strings.TrimSpace(tc.Text) != "" {
			return tc.Text
		}
	}
	return "tool returned an error"
}

// runUse changes the persisted global account/profile used by subsequent CLI
// invocations and new MCP sessions.
func runUse(args []string) error {
	fs := flag.NewFlagSet("use", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit JSON")
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return jsonIf(cliErr(exitUsage, "invalid_arguments", err), *asJSON)
	}
	if len(pos) != 1 {
		return jsonIf(cliErrf(exitUsage, "invalid_arguments", "usage: janusmcp use <account|profile> [--json]"), *asJSON)
	}
	cpath, _ := filepath.Abs(configPath())
	cfg, err := config.LoadRaw(cpath)
	if err != nil {
		return jsonIf(cliErr(exitSelection, "config_error", err), *asJSON)
	}
	if cfg.BindingMode == config.BindingLocked {
		return jsonIf(cliErr(exitSelection, "switching_locked", fmt.Errorf("bindingMode=locked: switching is disabled")), *asJSON)
	}
	selector := pos[0]
	if _, ok := cfg.AccountsForSelector(selector); !ok {
		return jsonIf(cliErrf(exitSelection, "unknown_selector", "unknown account or profile %q (see: janusmcp status)", selector), *asJSON)
	}
	statePath := envOr("JANUS_STATE", filepath.Join(filepath.Dir(cpath), ".janusmcp-state.json"))
	state := broker.NewBrokerState(statePath, cfg.DefaultAccount, cfg.BindingMode)
	if err := state.SetGlobal(selector); err != nil {
		return jsonIf(cliErr(exitSelection, "state_write_error", err), *asJSON)
	}
	if *asJSON {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"ok": true, "active": selector, "scope": "global"})
	}
	fmt.Printf("active=%s scope=global\n", selector)
	return nil
}
