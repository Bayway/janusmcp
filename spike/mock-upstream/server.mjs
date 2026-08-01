#!/usr/bin/env node
/**
 * Mock upstream MCP server for offline testing of the broker.
 * Simulates a per-account backend: its tools report which account (MOCK_ACCOUNT)
 * answered, so we can prove the broker routes calls to the active identity.
 */
import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { CallToolRequestSchema, ListToolsRequestSchema } from "@modelcontextprotocol/sdk/types.js";
import { randomUUID } from "node:crypto";

const ACCOUNT = process.env.MOCK_ACCOUNT ?? "unknown";
const INSTANCE_ID = `${ACCOUNT}:${process.pid}:${randomUUID()}`;

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
  {
    name: "instance_id",
    description: "Reports the mock process identity so tests can verify connection reuse.",
    inputSchema: { type: "object", properties: {}, additionalProperties: false },
  },
  {
    name: "structured_result",
    description: "Returns both text and structured MCP content.",
    inputSchema: { type: "object", properties: {}, additionalProperties: false },
    outputSchema: {
      type: "object",
      properties: { account: { type: "string" }, instanceId: { type: "string" } },
      required: ["account", "instanceId"],
      additionalProperties: false,
    },
  },
  {
    name: "multi_content",
    description: "Returns multiple text content blocks.",
    inputSchema: { type: "object", properties: {}, additionalProperties: false },
  },
  {
    name: "fail",
    description: "Returns a deterministic MCP tool error.",
    inputSchema: { type: "object", properties: {}, additionalProperties: false },
  },
  {
    name: "delay",
    description: "Waits for the requested number of milliseconds before responding.",
    inputSchema: {
      type: "object",
      properties: { ms: { type: "integer", minimum: 0, maximum: 30000 } },
      required: ["ms"],
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
  if (name === "instance_id") {
    return { content: [{ type: "text", text: INSTANCE_ID }] };
  }
  if (name === "structured_result") {
    const structuredContent = { account: ACCOUNT, instanceId: INSTANCE_ID };
    return {
      content: [{ type: "text", text: JSON.stringify(structuredContent) }],
      structuredContent,
    };
  }
  if (name === "multi_content") {
    return {
      content: [
        { type: "text", text: `first from ${ACCOUNT}` },
        { type: "text", text: `second from ${ACCOUNT}` },
      ],
    };
  }
  if (name === "fail") {
    return { isError: true, content: [{ type: "text", text: `forced failure from ${ACCOUNT}` }] };
  }
  if (name === "delay") {
    const ms = Number(args.ms ?? 0);
    await new Promise((resolve) => setTimeout(resolve, ms));
    return { content: [{ type: "text", text: `waited ${ms}ms on ${ACCOUNT}` }] };
  }
  return { isError: true, content: [{ type: "text", text: `unknown tool: ${name}` }] };
});

await server.connect(new StdioServerTransport());
process.stderr.write(`[mock-upstream:${ACCOUNT}] up\n`);
