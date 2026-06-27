/** Config loading: JSONC parsing, ${ENV} expansion, validation. */
import { readFileSync } from "node:fs";

export interface AccountConfig {
  id: string;
  service: string;
  label?: string;
  command: string;
  args?: string[];
  env?: Record<string, string>;
}

export type BindingMode = "global" | "session" | "locked";

export interface BrokerConfig {
  defaultAccount?: string;
  bindingMode?: BindingMode;
  accounts: AccountConfig[];
}

/** Tolerant JSON loader: strips // and block comments (JSONC), respecting strings. */
export function parseJsonc(text: string): unknown {
  let out = "";
  let i = 0;
  const n = text.length;
  let inStr = false;
  let quote = "";
  while (i < n) {
    const c = text[i];
    const c2 = text[i + 1];
    if (inStr) {
      out += c;
      if (c === "\\") {
        out += c2 ?? "";
        i += 2;
        continue;
      }
      if (c === quote) inStr = false;
      i++;
      continue;
    }
    if (c === '"' || c === "'") {
      inStr = true;
      quote = c;
      out += c;
      i++;
      continue;
    }
    if (c === "/" && c2 === "/") {
      while (i < n && text[i] !== "\n") i++;
      continue;
    }
    if (c === "/" && c2 === "*") {
      i += 2;
      while (i < n && !(text[i] === "*" && text[i + 1] === "/")) i++;
      i += 2;
      continue;
    }
    out += c;
    i++;
  }
  out = out.replace(/,(\s*[}\]])/g, "$1"); // tolerate trailing commas
  return JSON.parse(out);
}

/** Expand ${VAR} placeholders from process.env in a string. */
export function expandEnv(value: string): string {
  return value.replace(/\$\{([A-Z0-9_]+)\}/gi, (_, name) => process.env[name] ?? "");
}

export function loadConfig(configPath: string): BrokerConfig {
  const raw = readFileSync(configPath, "utf8");
  const cfg = parseJsonc(raw) as BrokerConfig;
  if (!cfg.accounts?.length) throw new Error("config: 'accounts' is empty");
  for (const a of cfg.accounts) {
    if (!a.id || !a.command) throw new Error(`config: account missing id/command: ${JSON.stringify(a)}`);
    a.env = Object.fromEntries(Object.entries(a.env ?? {}).map(([k, v]) => [k, expandEnv(String(v))]));
    a.args = (a.args ?? []).map(expandEnv);
  }
  return cfg;
}
