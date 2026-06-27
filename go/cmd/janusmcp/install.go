package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// mcpEntry is the standard stdio MCP server config shape used by Claude/Cursor.
type mcpEntry struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env,omitempty"`
}

// runInstall auto-configures an LLM client to launch this broker — no manual JSON.
func runInstall(args []string) error {
	if len(args) < 1 || args[0] == "list" || args[0] == "--list" || args[0] == "-l" {
		return installList()
	}
	self, err := selfPath()
	if err != nil {
		return err
	}
	cfg, _ := filepath.Abs(configPath())
	entry := mcpEntry{Command: self, Args: []string{"serve"}, Env: map[string]string{"JANUS_CONFIG": cfg}}

	home, _ := os.UserHomeDir()

	switch args[0] {
	case "claude-desktop":
		path, err := claudeDesktopConfigPath()
		if err != nil {
			return err
		}
		return installToFile("Claude Desktop", path, "mcpServers", entry, "Restart Claude Desktop")

	case "cursor":
		return installToFile("Cursor", filepath.Join(home, ".cursor", "mcp.json"), "mcpServers", entry, "Reload Cursor")

	case "vscode":
		path, err := vscodeUserConfigPath()
		if err != nil {
			return err
		}
		// VS Code uses the "servers" key (not "mcpServers").
		return installToFile("VS Code", path, "servers", entry, "Reload VS Code")

	case "gemini":
		return installToFile("Gemini CLI", filepath.Join(home, ".gemini", "settings.json"), "mcpServers", entry, "Restart your Gemini CLI session")

	case "claude-code":
		return execAdd("Claude Code", "claude", self, cfg)

	case "codex":
		return execAdd("Codex", "codex", self, cfg)

	case "chatgpt":
		return installChatGPT()

	case "print", "--print":
		out, _ := json.MarshalIndent(map[string]any{"mcpServers": map[string]any{"janusmcp": entry}}, "", "  ")
		fmt.Println(string(out))
		fmt.Println("\n// Paste the \"janusmcp\" object into your client's MCP config")
		fmt.Println("// (VS Code uses the key \"servers\" instead of \"mcpServers\").")
		return nil

	default:
		return fmt.Errorf("unknown client %q\nsupported: claude-desktop | claude-code | cursor | vscode | gemini | codex | chatgpt | print", args[0])
	}
}

// installList prints the supported clients and, for file-based ones, whether JanusMCP
// is already configured.
func installList() error {
	home, _ := os.UserHomeDir()
	vscode, _ := vscodeUserConfigPath()
	claude, _ := claudeDesktopConfigPath()

	type target struct{ name, kind, path string }
	targets := []target{
		{"claude-desktop", "file", claude},
		{"cursor", "file", filepath.Join(home, ".cursor", "mcp.json")},
		{"vscode", "file", vscode},
		{"gemini", "file", filepath.Join(home, ".gemini", "settings.json")},
		{"claude-code", "cli", "via `claude mcp add`"},
		{"codex", "cli", "via `codex mcp add`"},
		{"chatgpt", "manual", "remote/HTTPS via the app UI"},
		{"print", "util", "print a JSON block to paste anywhere"},
	}
	fmt.Println("Install targets (use: janusmcp install <target>):")
	for _, t := range targets {
		status := ""
		if t.kind == "file" {
			if b, err := os.ReadFile(t.path); err == nil && strings.Contains(string(b), "\"janusmcp\"") {
				status = "  [configured ✓]"
			}
		}
		fmt.Printf("  %-15s %s%s\n", t.name, t.path, status)
	}
	return nil
}

func installToFile(label, path, key string, entry mcpEntry, restartHint string) error {
	if err := upsertMCPServer(path, key, "janusmcp", entry); err != nil {
		return err
	}
	fmt.Printf("Configured %s: %s\n", label, path)
	fmt.Printf("→ %s, then run `janusmcp ui` to add accounts and log in.\n", restartHint)
	return nil
}

// execAdd uses a client's own CLI (`claude mcp add` / `codex mcp add`) which share the
// same flags. Falls back to printing the command if the CLI isn't on PATH.
func execAdd(label, bin, self, cfg string) error {
	addArgs := []string{"mcp", "add", "janusmcp", "--env", "JANUS_CONFIG=" + cfg, "--", self, "serve"}
	if label == "Claude Code" {
		addArgs = append([]string{"mcp", "add", "janusmcp", "--scope", "user", "--env", "JANUS_CONFIG=" + cfg, "--", self, "serve"})
	}
	if _, err := exec.LookPath(bin); err != nil {
		fmt.Printf("The `%s` CLI was not found. Run this manually:\n  %s %s\n", bin, bin, strings.Join(addArgs, " "))
		return nil
	}
	cmd := exec.Command(bin, addArgs...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s add failed: %w", bin, err)
	}
	fmt.Printf("→ Configured %s. Run `janusmcp ui` to add accounts and log in.\n", label)
	return nil
}

func installChatGPT() error {
	fmt.Println("ChatGPT supports only REMOTE MCP servers over HTTPS (no local stdio), configured in the app UI.")
	fmt.Println("To use JanusMCP with ChatGPT:")
	fmt.Println("  1. Run the broker in HTTP mode:")
	fmt.Println("       JANUS_TRANSPORT=http JANUS_HTTP_PORT=7332 janusmcp serve")
	fmt.Println("  2. Expose http://127.0.0.1:7332/mcp over HTTPS (e.g. `cloudflared tunnel` or `ngrok http 7332`).")
	fmt.Println("  3. In ChatGPT: Settings → Connectors/Apps → Advanced → enable Developer Mode,")
	fmt.Println("     then add the HTTPS URL and click \"Scan Tools\".")
	return nil
}

func vscodeUserConfigPath() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", "Code", "User", "mcp.json"), nil
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), "Code", "User", "mcp.json"), nil
	default:
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".config", "Code", "User", "mcp.json"), nil
	}
}

func claudeDesktopConfigPath() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json"), nil
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), "Claude", "claude_desktop_config.json"), nil
	default:
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".config", "Claude", "claude_desktop_config.json"), nil
	}
}

// upsertMCPServer adds/updates one server entry under key, preserving the rest of the file.
func upsertMCPServer(path, key, name string, entry mcpEntry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	root := map[string]any{}
	if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
		if err := json.Unmarshal(b, &root); err != nil {
			return fmt.Errorf("existing %s is not valid JSON: %w", path, err)
		}
	}
	servers, _ := root[key].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers[name] = entry
	root[key] = servers
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o644)
}

func selfPath() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	return filepath.Abs(p)
}
