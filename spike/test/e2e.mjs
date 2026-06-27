#!/usr/bin/env node
/**
 * End-to-end test: spawns the broker over stdio, then exercises the
 * active-account model against the bundled mock upstreams (no real creds).
 *
 * Validates: handshake, control tools, tools/list reflects the active account,
 * proxying routes to the correct identity, and switching changes the routing.
 */
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StdioClientTransport } from "@modelcontextprotocol/sdk/client/stdio.js";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { rmSync } from "node:fs";
import { tmpdir } from "node:os";

const here = dirname(fileURLToPath(import.meta.url));
const root = resolve(here, "..");

// Deterministic default: isolate persisted state in a temp file and clear it.
const STATE = resolve(tmpdir(), `janusmcp-stdio-${process.pid}.json`);
rmSync(STATE, { force: true });

let listChangedCount = 0;
const failures = [];
function check(name, cond, detail = "") {
  const ok = !!cond;
  console.log(`${ok ? "PASS" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) failures.push(name);
}

function textOf(res) {
  return (res.content ?? []).map((c) => c.text ?? "").join("");
}

const transport = new StdioClientTransport({
  command: "node",
  args: ["dist/index.js"],
  cwd: root,
  env: { ...process.env, JANUS_CONFIG: resolve(root, "config.json"), JANUS_STATE: STATE },
});

const client = new Client({ name: "e2e-test", version: "0.0.1" }, { capabilities: {} });

// Count tools/list_changed notifications from the broker (catch-all handler).
client.fallbackNotificationHandler = async (n) => {
  if (n.method === "notifications/tools/list_changed") listChangedCount++;
};

await client.connect(transport);
console.log("connected to broker\n");

// 1. tools/list contains control tools + default account (A) upstream tools
let list = await client.listTools();
let names = list.tools.map((t) => t.name);
check("control tools present", ["janus_list_accounts", "janus_use_account", "janus_whoami"].every((n) => names.includes(n)), names.join(","));
check("active-account upstream tools present", names.includes("ping") && names.includes("db_query"));

// 2. whoami + list_accounts report account A active by default
let who = JSON.parse(textOf(await client.callTool({ name: "janus_whoami", arguments: {} })));
check("default active = azienda_a", who.id === "azienda_a", JSON.stringify(who));

let accts = JSON.parse(textOf(await client.callTool({ name: "janus_list_accounts", arguments: {} })));
check("two accounts configured", accts.accounts.length === 2);

// 3. proxied call routes to account A
let ping = textOf(await client.callTool({ name: "ping", arguments: {} }));
check("ping routes to A", ping === "pong from azienda_a", ping);

// 4. switch to B
let sw = JSON.parse(textOf(await client.callTool({ name: "janus_use_account", arguments: { account_id: "azienda_b" } })));
check("switch ok", sw.ok === true && sw.active === "azienda_b", JSON.stringify(sw));
check("tools/list_changed emitted", listChangedCount >= 1, `count=${listChangedCount}`);

// 5. proxied call now routes to account B
let ping2 = textOf(await client.callTool({ name: "ping", arguments: {} }));
check("ping routes to B after switch", ping2 === "pong from azienda_b", ping2);

let q = JSON.parse(textOf(await client.callTool({ name: "db_query", arguments: { sql: "select 1" } })));
check("db_query tagged with B", q.account === "azienda_b" && q.rows[0].owner === "azienda_b", JSON.stringify(q));

// 6. unknown account is rejected gracefully
let bad = JSON.parse(textOf(await client.callTool({ name: "janus_use_account", arguments: { account_id: "nope" } })));
check("unknown account rejected", bad.ok === false);

await client.close();
rmSync(STATE, { force: true });

console.log(`\n${failures.length === 0 ? "ALL PASS ✅" : `FAILURES (${failures.length}): ` + failures.join(", ")}`);
process.exit(failures.length === 0 ? 0 : 1);
