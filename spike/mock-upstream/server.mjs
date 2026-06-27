#!/usr/bin/env node
/**
 * Mock upstream MCP server for offline testing of the broker.
 * Simulates a per-account backend: its tools report which account (MOCK_ACCOUNT)
 * answered, so we can prove the broker routes calls to the active identity.
 */
import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { CallToolRequestSchema, ListToolsRequestSchema } from "@modelcontextprotocol/sdk/types.js";

const ACCOUNT = process.env.MOCK_ACCOUNT ?? "unknown";

const server = new Server(
  { name: `mock-upstream:${ACCOUNT}`, version: "0.0.1" },
  { capabilities: { tools: {} } },
);

const tools = [
  {
    name: "ping",
    description: "Health check that reports which mock account answered.",
    inputSchema: { type: "object", properties: {}, additionalProperties: false },
  },
  {
    name: "db_query",
    description: "Fake DB query; echoes the SQL and tags the answering account.",
    inputSchema: {
      type: "object",
      properties: { sql: { type: "string" } },
      required: ["sql"],
      additionalProperties: false,
    },
  },
];

server.setRequestHandler(ListToolsRequestSchema, async () => ({ tools }));

server.setRequestHandler(CallToolRequestSchema, async (req) => {
  const { name, arguments: args = {} } = req.params;
  if (name === "ping") {
    return { content: [{ type: "text", text: `pong from ${ACCOUNT}` }] };
  }
  if (name === "db_query") {
    return {
      content: [
        {
          type: "text",
          text: JSON.stringify({ account: ACCOUNT, sql: args.sql, rows: [{ id: 1, owner: ACCOUNT }] }),
        },
      ],
    };
  }
  return { isError: true, content: [{ type: "text", text: `unknown tool: ${name}` }] };
});

await server.connect(new StdioServerTransport());
process.stderr.write(`[mock-upstream:${ACCOUNT}] up\n`);
