/** Streamable HTTP listener (localhost) with per-session transports keyed by Mcp-Session-Id. */
import { createServer, type IncomingMessage, type ServerResponse } from "node:http";
import { randomUUID } from "node:crypto";
import { StreamableHTTPServerTransport } from "@modelcontextprotocol/sdk/server/streamableHttp.js";
import { isInitializeRequest } from "@modelcontextprotocol/sdk/types.js";
import { createBrokerServer, type BrokerCore } from "./broker.js";
import { SessionRegistry, type Session } from "./state.js";

function readBody(req: IncomingMessage): Promise<unknown> {
  return new Promise((resolve, reject) => {
    const chunks: Buffer[] = [];
    req.on("data", (c) => chunks.push(c as Buffer));
    req.on("end", () => {
      const raw = Buffer.concat(chunks).toString("utf8");
      if (!raw) return resolve(undefined);
      try {
        resolve(JSON.parse(raw));
      } catch (e) {
        reject(e);
      }
    });
    req.on("error", reject);
  });
}

export function startHttpServer(core: BrokerCore, registry: SessionRegistry, host: string, port: number) {
  const transports = new Map<string, StreamableHTTPServerTransport>();

  const httpServer = createServer(async (req: IncomingMessage, res: ServerResponse) => {
    const url = new URL(req.url ?? "/", `http://${req.headers.host}`);
    if (url.pathname !== "/mcp") {
      res.writeHead(404).end("not found");
      return;
    }
    const sid = req.headers["mcp-session-id"] as string | undefined;

    try {
      if (req.method === "POST") {
        const body = await readBody(req);
        let transport: StreamableHTTPServerTransport | undefined = sid ? transports.get(sid) : undefined;

        if (!transport) {
          if (sid || !isInitializeRequest(body)) {
            res.writeHead(400, { "content-type": "application/json" });
            res.end(JSON.stringify({ jsonrpc: "2.0", error: { code: -32000, message: "no valid session" }, id: null }));
            return;
          }
          // New session: build a fresh transport + broker server bound to a Session object.
          const session: Session = { id: "" };
          const t = new StreamableHTTPServerTransport({
            sessionIdGenerator: () => randomUUID(),
            enableJsonResponse: true,
            onsessioninitialized: (id: string) => {
              session.id = id;
              transports.set(id, t);
              registry.add(session);
            },
            onsessionclosed: (id: string) => {
              transports.delete(id);
              registry.remove(id);
            },
          });
          t.onclose = () => {
            if (t.sessionId) {
              transports.delete(t.sessionId);
              registry.remove(t.sessionId);
            }
          };
          const server = createBrokerServer(core, session, registry);
          session.server = server;
          await server.connect(t);
          transport = t;
        }

        await transport.handleRequest(req, res, body);
        return;
      }

      if (req.method === "GET" || req.method === "DELETE") {
        const transport = sid ? transports.get(sid) : undefined;
        if (!transport) {
          res.writeHead(400).end("missing or unknown session");
          return;
        }
        await transport.handleRequest(req, res);
        return;
      }

      res.writeHead(405).end("method not allowed");
    } catch (e) {
      process.stderr.write(`[janusmcp] http error: ${(e as Error).stack ?? e}\n`);
      if (!res.headersSent) res.writeHead(500).end("internal error");
    }
  });

  return new Promise<ReturnType<typeof createServer>>((resolve) => {
    httpServer.listen(port, host, () => {
      process.stderr.write(`[janusmcp] http up on http://${host}:${port}/mcp\n`);
      resolve(httpServer);
    });
  });
}
