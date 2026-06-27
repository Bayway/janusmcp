/** Broker MCP server factory: one Server per session, sharing core state. */
import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import {
  CallToolRequestSchema,
  ListToolsRequestSchema,
  type Tool,
} from "@modelcontextprotocol/sdk/types.js";
import type { BrokerConfig } from "./config.js";
import type { UpstreamManager } from "./upstream.js";
import { BrokerState, SessionRegistry, resolveActive, type Session } from "./state.js";

export const CONTROL_PREFIX = "janus_";

export interface BrokerCore {
  cfg: BrokerConfig;
  manager: UpstreamManager;
  state: BrokerState;
}

function controlTools(): Tool[] {
  return [
    {
      name: `${CONTROL_PREFIX}list_accounts`,
      description: "List configured accounts/services and which one is active for this session.",
      inputSchema: { type: "object", properties: {}, additionalProperties: false },
    },
    {
      name: `${CONTROL_PREFIX}use_account`,
      description:
        "Set the active account/identity. Reloads upstream tools and notifies the client (tools/list_changed). " +
        "scope: 'session' (this connection only) or 'global' (all connections, persisted); defaults to the broker bindingMode.",
      inputSchema: {
        type: "object",
        properties: {
          account_id: { type: "string", description: "Account id to activate (see janus_list_accounts)." },
          scope: { type: "string", enum: ["session", "global"], description: "Override the default binding scope." },
        },
        required: ["account_id"],
        additionalProperties: false,
      },
    },
    {
      name: `${CONTROL_PREFIX}whoami`,
      description: "Return the active account for this session, plus how it was resolved.",
      inputSchema: { type: "object", properties: {}, additionalProperties: false },
    },
  ];
}

function textResult(payload: unknown) {
  return { content: [{ type: "text" as const, text: JSON.stringify(payload, null, 2) }] };
}

export function createBrokerServer(core: BrokerCore, session: Session, registry: SessionRegistry): Server {
  const { cfg, manager, state } = core;

  const server = new Server(
    { name: "janusmcp", version: "0.0.1" },
    { capabilities: { tools: { listChanged: true } } },
  );

  server.setRequestHandler(ListToolsRequestSchema, async () => {
    const active = resolveActive(session, state);
    const upstream = await manager.tools(active).catch(() => [] as Tool[]);
    return { tools: [...controlTools(), ...upstream] };
  });

  server.setRequestHandler(CallToolRequestSchema, async (req) => {
    const name = req.params.name;
    const args = (req.params.arguments ?? {}) as Record<string, unknown>;
    const active = resolveActive(session, state);

    if (name === `${CONTROL_PREFIX}list_accounts`) {
      return textResult({
        active,
        bindingMode: state.bindingMode,
        sessionId: session.id,
        accounts: cfg.accounts.map((a) => ({
          id: a.id,
          service: a.service,
          label: a.label ?? a.id,
          active: a.id === active,
        })),
      });
    }

    if (name === `${CONTROL_PREFIX}whoami`) {
      const a = manager.account(active);
      const source = session.localActive ? "session" : "global";
      return textResult({ id: a.id, label: a.label ?? a.id, service: a.service, resolvedFrom: source, sessionId: session.id });
    }

    if (name === `${CONTROL_PREFIX}use_account`) {
      if (state.bindingMode === "locked") {
        return textResult({ ok: false, error: "bindingMode=locked: switching is disabled" });
      }
      const target = String(args.account_id ?? "");
      try {
        manager.account(target); // validate
      } catch (e) {
        return textResult({ ok: false, error: (e as Error).message });
      }
      const requested = args.scope as "session" | "global" | undefined;
      const scope = requested ?? (state.bindingMode === "global" ? "global" : "session");

      if (scope === "global") {
        state.setGlobal(target);
        session.localActive = undefined; // global wins for this session too
        await manager.tools(target).catch(() => []);
        await registry.broadcastToolsChanged();
      } else {
        session.localActive = target;
        await manager.tools(target).catch(() => []);
        await server.sendToolListChanged();
      }
      return textResult({ ok: true, active: target, scope, sessionId: session.id });
    }

    // Proxy everything else to this session's active upstream.
    try {
      return await manager.callTool(active, name, args);
    } catch (e) {
      return {
        isError: true,
        content: [
          { type: "text" as const, text: `upstream error on '${name}' (account=${active}): ${(e as Error).message}` },
        ],
      };
    }
  });

  return server;
}
