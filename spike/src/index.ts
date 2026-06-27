#!/usr/bin/env node
/**
 * JanusMCP Broker — entrypoint.
 *
 * Transports:
 *   - stdio (default): single local session, for Claude Desktop/Code.
 *   - http: Streamable HTTP on localhost with per-session state (Mcp-Session-Id),
 *           for ChatGPT/Gemini/Cursor/Copilot and concurrent multi-client use.
 *   - both: run both at once.
 *
 * Select via JANUS_TRANSPORT=stdio|http|both (default stdio) and
 * JANUS_HTTP_PORT / JANUS_HTTP_HOST for the HTTP listener.
 *
 * Identity scoping (design §4.1): per-call > per-session > global (persisted),
 * controlled by bindingMode (global|session|locked) in config.
 */

import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";

import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { loadConfig } from "./config.js";
import { UpstreamManager } from "./upstream.js";
import { BrokerState, SessionRegistry, type Session } from "./state.js";
import { createBrokerServer, type BrokerCore } from "./broker.js";
import { startHttpServer } from "./http.js";

async function main() {
  const __dirname = dirname(fileURLToPath(import.meta.url));
  const projectRoot = resolve(__dirname, "..");
  const configPath = resolve(process.env.JANUS_CONFIG ?? resolve(projectRoot, "config.json"));
  const configDir = dirname(configPath);

  const cfg = loadConfig(configPath);
  const defaultActive = cfg.defaultAccount ?? cfg.accounts[0].id;
  const bindingMode = cfg.bindingMode ?? "global";

  const statePath = process.env.JANUS_STATE ?? resolve(configDir, ".janusmcp-state.json");
  const manager = new UpstreamManager(cfg, configDir);
  const state = new BrokerState(statePath, defaultActive, bindingMode);
  const registry = new SessionRegistry();
  const core: BrokerCore = { cfg, manager, state };

  const transport = (process.env.JANUS_TRANSPORT ?? "stdio").toLowerCase();
  const wantHttp = transport === "http" || transport === "both";
  const wantStdio = transport === "stdio" || transport === "both";

  const shutdown = async () => {
    await manager.closeAll();
    process.exit(0);
  };
  process.on("SIGINT", shutdown);
  process.on("SIGTERM", shutdown);

  if (wantHttp) {
    const host = process.env.JANUS_HTTP_HOST ?? "127.0.0.1";
    const port = Number(process.env.JANUS_HTTP_PORT ?? 7332);
    await startHttpServer(core, registry, host, port);
  }

  if (wantStdio) {
    const session: Session = { id: "stdio" };
    const server = createBrokerServer(core, session, registry);
    session.server = server;
    registry.add(session);
    await server.connect(new StdioServerTransport());
    process.stderr.write(
      `[janusmcp] stdio up. active=${state.globalActive} bindingMode=${bindingMode} accounts=${cfg.accounts.length}\n`,
    );
  }

  if (!wantHttp && !wantStdio) {
    process.stderr.write(`[janusmcp] nothing to do: JANUS_TRANSPORT=${transport}\n`);
    process.exit(1);
  }
}

main().catch((e) => {
  process.stderr.write(`[janusmcp] fatal: ${(e as Error).stack ?? e}\n`);
  process.exit(1);
});
