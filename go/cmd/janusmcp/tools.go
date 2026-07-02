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
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

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
		return nil, err
	}
	cpath, _ := filepath.Abs(configPath())
	cdir := filepath.Dir(cpath)
	cfg, err := config.LoadRaw(cpath)
	if err != nil {
		return nil, err
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
		return nil, fmt.Errorf("unknown account or profile %q (see: janusmcp status)", selector)
	}
	return ids, nil
}

func cliContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
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
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return err
	}
	var selector string
	if len(pos) > 0 {
		selector = pos[0]
	}

	c, err := newCLICore()
	if err != nil {
		return err
	}
	defer c.manager.CloseAll()
	ids, err := c.accountsFor(selector)
	if err != nil {
		return err
	}

	ctx, stop := cliContext()
	defer stop()

	if *asJSON {
		out := map[string][]*mcp.Tool{}
		for _, id := range ids {
			tools, err := c.manager.Tools(ctx, id)
			if err != nil {
				return fmt.Errorf("account %s: %w", id, err)
			}
			out[id] = tools
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}

	for _, id := range ids {
		tools, err := c.manager.Tools(ctx, id)
		if err != nil {
			return fmt.Errorf("account %s: %w", id, err)
		}
		a, _ := c.manager.Account(id)
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
func (c *cliCore) findTool(ctx context.Context, selector, name string) (accountID string, tool *mcp.Tool, err error) {
	ids, err := c.accountsFor(selector)
	if err != nil {
		return "", nil, err
	}
	var owners []string
	for _, id := range ids {
		tools, terr := c.manager.Tools(ctx, id)
		if terr != nil {
			return "", nil, fmt.Errorf("account %s: %w", id, terr)
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
		return "", nil, fmt.Errorf("tool %q not found (see: janusmcp tools)", name)
	}
	if len(owners) > 1 {
		return "", nil, fmt.Errorf("tool %q exists on several accounts (%s): pick one with --account",
			name, strings.Join(owners, ", "))
	}
	return accountID, tool, nil
}

// runSchema prints the full JSON definition (input schema included) of one tool.
func runSchema(args []string) error {
	fs := flag.NewFlagSet("schema", flag.ContinueOnError)
	account := fs.String("account", "", "account id or profile to search (default: active)")
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return err
	}
	if len(pos) < 1 {
		return fmt.Errorf("usage: janusmcp schema <tool> [--account <id|profile>]")
	}

	c, err := newCLICore()
	if err != nil {
		return err
	}
	defer c.manager.CloseAll()

	ctx, stop := cliContext()
	defer stop()

	id, tool, err := c.findTool(ctx, *account, pos[0])
	if err != nil {
		return err
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
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return err
	}
	if len(pos) < 1 {
		return fmt.Errorf("usage: janusmcp call <tool> [--account <id|profile>] [--args '<json>']")
	}
	name := pos[0]

	payload := json.RawMessage("{}")
	if *rawArgs != "" {
		payload = json.RawMessage(*rawArgs)
	} else if st, err := os.Stdin.Stat(); err == nil && st.Mode()&os.ModeCharDevice == 0 {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		if len(strings.TrimSpace(string(b))) > 0 {
			payload = json.RawMessage(b)
		}
	}
	var check map[string]any
	if err := json.Unmarshal(payload, &check); err != nil {
		return fmt.Errorf("--args must be a JSON object: %w", err)
	}

	c, err := newCLICore()
	if err != nil {
		return err
	}
	defer c.manager.CloseAll()

	ctx, stop := cliContext()
	defer stop()

	id, _, err := c.findTool(ctx, *account, name)
	if err != nil {
		return err
	}
	cs, err := c.manager.Session(ctx, id)
	if err != nil {
		return err
	}
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: payload})
	if err != nil {
		return err
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
		return fmt.Errorf("tool %q returned an error (account %s)", name, id)
	}
	return nil
}
