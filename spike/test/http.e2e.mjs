#!/usr/bin/env node
/**
 * End-to-end test for the Streamable HTTP transport with per-session state.
 *
 * Two concurrent clients = two sessions against ONE broker process. Validates:
 *  - per-session isolation: a `session`-scope switch in client 1 does NOT affect client 2;
 *  - global propagation: a `global`-scope switch flips every session without a local override;
 *  - notification targeting: `session` scope notifies only that session; `global` broadcasts.
 */
import { spawn } from "node:child_process";
import { rmSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StreamableHTTPClientTransport } from "@modelcontextprotocol/sdk/client/streamableHttp.js";

const here = dirname(fileURLToPath(import.meta.url));
const root = resolve(here, "..");
const PORT = 7399;
const URL_MCP = `http://127.0.0.1:${PORT}/mcp`;

const failures = [];
function check(name, cond, detail = "") {
  const ok = !!cond;
  console.log(`${ok ? "PASS" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) failures.push(name);
}
const textOf = (res) => (res.content ?? []).map((c) => c.text ?? "").join("");
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

const STATE = resolve(tmpdir(), `janusmcp-http-${process.pid}.json`);
rmSync(STATE, { force: true });

// --- boot broker in HTTP mode ---
const child = spawn("node", ["dist/index.js"], {
  cwd: root,
  env: {
    ...process.env,
    JANUS_TRANSPORT: "http",
    JANUS_HTTP_PORT: String(PORT),
    JANUS_CONFIG: resolve(root, "config.session.json"),
    JANUS_STATE: STATE,
  },
});
let bootErr = "";
child.stderr.on("data", (d) => (bootErr += d.toString()));

async function waitForBoot() {
  for (let i = 0; i < 50; i++) {
    if (bootErr.includes("http up")) return;
    await sleep(100);
  }
  throw new Error("broker did not boot:\n" + bootErr);
}

function makeClient(name) {
  const transport = new StreamableHTTPClientTransport(new URL(URL_MCP));
  const client = new Client({ name, version: "0.0.1" }, { capabilities: {} });
  const counter = { listChanged: 0 };
  client.fallbackNotificationHandler = async (n) => {
    if (n.method === "notifications/tools/list_changed") counter.listChanged++;
  };
  return { client, transport, counter };
}

let c1, c2;
try {
  await waitForBoot();

  c1 = makeClient("client-1");
  c2 = makeClient("client-2");
  await c1.client.connect(c1.transport);
  await c2.client.connect(c2.transport);
  const sid1 = c1.transport.sessionId;
  const sid2 = c2.transport.sessionId;
  check("two distinct sessions", sid1 && sid2 && sid1 !== sid2, `${sid1} vs ${sid2}`);

  // 1. both default to global A
  check("c1 default A", textOf(await c1.client.callTool({ name: "ping", arguments: {} })) === "pong from azienda_a");
  check("c2 default A", textOf(await c2.client.callTool({ name: "ping", arguments: {} })) === "pong from azienda_a");

  // 2. session-scope switch in c1 only
  await c1.client.callTool({ name: "janus_use_account", arguments: { account_id: "azienda_b" } }); // bindingMode=session
  await sleep(150);
  check("c1 → B after session switch", textOf(await c1.client.callTool({ name: "ping", arguments: {} })) === "pong from azienda_b");
  check("c2 still A (isolation)", textOf(await c2.client.callTool({ name: "ping", arguments: {} })) === "pong from azienda_a");
  check("only c1 notified (session scope)", c1.counter.listChanged >= 1 && c2.counter.listChanged === 0, `c1=${c1.counter.listChanged} c2=${c2.counter.listChanged}`);

  // 3. global-scope switch from c1 flips c2 (which has no local override)
  const before = c2.counter.listChanged;
  await c1.client.callTool({ name: "janus_use_account", arguments: { account_id: "azienda_b", scope: "global" } });
  await sleep(200);
  check("c2 → B after global switch", textOf(await c2.client.callTool({ name: "ping", arguments: {} })) === "pong from azienda_b");
  check("c2 notified on global broadcast", c2.counter.listChanged > before, `before=${before} after=${c2.counter.listChanged}`);

  await c1.client.close();
  await c2.client.close();
} catch (e) {
  console.error("ERROR:", e.message);
  failures.push("exception: " + e.message);
} finally {
  child.kill("SIGTERM");
  rmSync(STATE, { force: true });
}

console.log(`\n${failures.length === 0 ? "ALL PASS ✅" : `FAILURES (${failures.length}): ` + failures.join(", ")}`);
process.exit(failures.length === 0 ? 0 : 1);
