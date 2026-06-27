/** Upstream manager: lazy MCP client connections (shared across sessions) + tool cache. */
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StdioClientTransport } from "@modelcontextprotocol/sdk/client/stdio.js";
import type { Tool } from "@modelcontextprotocol/sdk/types.js";
import type { AccountConfig, BrokerConfig } from "./config.js";

export class UpstreamManager {
  private clients = new Map<string, Client>();
  private toolCache = new Map<string, Tool[]>();

  constructor(private cfg: BrokerConfig, private configDir: string) {}

  account(id: string): AccountConfig {
    const a = this.cfg.accounts.find((x) => x.id === id);
    if (!a) throw new Error(`unknown account: ${id}`);
    return a;
  }

  /** Lazily connect (or reuse) the upstream client for an account. Shared across sessions. */
  async connect(id: string): Promise<Client> {
    const existing = this.clients.get(id);
    if (existing) return existing;

    const a = this.account(id);
    const transport = new StdioClientTransport({
      command: a.command,
      args: a.args,
      env: { ...(process.env as Record<string, string>), ...(a.env ?? {}) },
      cwd: this.configDir,
    });
    const client = new Client({ name: "janusmcp", version: "0.0.1" }, { capabilities: {} });
    await client.connect(transport);
    this.clients.set(id, client);
    return client;
  }

  async tools(id: string): Promise<Tool[]> {
    const cached = this.toolCache.get(id);
    if (cached) return cached;
    const client = await this.connect(id);
    const res = await client.listTools();
    const tools = res.tools ?? [];
    this.toolCache.set(id, tools);
    return tools;
  }

  async callTool(id: string, name: string, args: Record<string, unknown> | undefined) {
    const client = await this.connect(id);
    return client.callTool({ name, arguments: args ?? {} });
  }

  async closeAll() {
    for (const c of this.clients.values()) {
      try {
        await c.close();
      } catch {
        /* ignore */
      }
    }
  }
}
