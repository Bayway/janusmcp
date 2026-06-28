package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// runUninstall removes the JanusMCP entry from an LLM client's config — the inverse
// of runInstall. File-based clients have the "janusmcp" key deleted (the rest of the
// file is preserved); CLI-based clients use their own `mcp remove`.
func runUninstall(args []string) error {
	if len(args) < 1 || args[0] == "list" || args[0] == "--list" || args[0] == "-l" {
		return installList()
	}
	home, _ := os.UserHomeDir()

	switch args[0] {
	case "claude-desktop":
		path, err := claudeDesktopConfigPath()
		if err != nil {
			return err
		}
		return uninstallFromFile("Claude Desktop", path, "mcpServers", "Restart Claude Desktop")

	case "cursor":
		return uninstallFromFile("Cursor", filepath.Join(home, ".cursor", "mcp.json"), "mcpServers", "Reload Cursor")

	case "vscode":
		path, err := vscodeUserConfigPath()
		if err != nil {
			return err
		}
		return uninstallFromFile("VS Code", path, "servers", "Reload VS Code")

	case "gemini":
		return uninstallFromFile("Gemini CLI", filepath.Join(home, ".gemini", "settings.json"), "mcpServers", "Restart your Gemini CLI session")

	case "claude-code":
		return execRemove("Claude Code", "claude")

	case "codex":
		return execRemove("Codex", "codex")

	case "chatgpt":
		fmt.Println("ChatGPT: remove the JanusMCP connector from the app UI (Settings → Connectors/Apps).")
		return nil

	default:
		return fmt.Errorf("unknown client %q\nsupported: claude-desktop | claude-code | cursor | vscode | gemini | codex | chatgpt", args[0])
	}
}

func uninstallFromFile(label, path, key, restartHint string) error {
	removed, err := removeMCPServer(path, key, "janusmcp")
	if err != nil {
		return err
	}
	if !removed {
		fmt.Printf("JanusMCP was not configured in %s (%s) — nothing to do.\n", label, path)
		return nil
	}
	fmt.Printf("Removed JanusMCP from %s: %s\n", label, path)
	fmt.Printf("→ %s.\n", restartHint)
	return nil
}

// execRemove uses a client's own CLI (`claude mcp remove` / `codex mcp remove`).
// Falls back to printing the command if the CLI isn't on PATH.
func execRemove(label, bin string) error {
	rmArgs := []string{"mcp", "remove", "janusmcp"}
	if _, err := exec.LookPath(bin); err != nil {
		fmt.Printf("The `%s` CLI was not found. Run this manually:\n  %s %s\n", bin, bin, strings.Join(rmArgs, " "))
		return nil
	}
	cmd := exec.Command(bin, rmArgs...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s remove failed: %w", bin, err)
	}
	fmt.Printf("→ Removed JanusMCP from %s.\n", label)
	return nil
}

// removeMCPServer deletes one server entry under key, preserving the rest of the file.
// Returns whether an entry was actually removed.
func removeMCPServer(path, key, name string) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	root := map[string]any{}
	if len(b) > 0 {
		if err := json.Unmarshal(b, &root); err != nil {
			return false, fmt.Errorf("existing %s is not valid JSON: %w", path, err)
		}
	}
	servers, _ := root[key].(map[string]any)
	if servers == nil {
		return false, nil
	}
	if _, ok := servers[name]; !ok {
		return false, nil
	}
	delete(servers, name)
	root[key] = servers
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return false, err
	}
	return true, os.WriteFile(path, append(out, '\n'), 0o644)
}
