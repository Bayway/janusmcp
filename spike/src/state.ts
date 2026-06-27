/** Broker state: persisted global active account + live session registry. */
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import type { Server } from "@modelcontextprotocol/sdk/server/index.js";
import type { BindingMode } from "./config.js";

export interface Session {
  id: string;
  /** Per-session active account; undefined → falls back to the global active account. */
  localActive?: string;
  server?: Server;
}

/** Global active account, persisted to disk so it survives restarts (design §4.1). */
export class BrokerState {
  globalActive: string;
  readonly bindingMode: BindingMode;

  constructor(private statePath: string, defaultActive: string, bindingMode: BindingMode) {
    this.bindingMode = bindingMode;
    let persisted: string | undefined;
    try {
      if (existsSync(statePath)) persisted = JSON.parse(readFileSync(statePath, "utf8"))?.globalActive;
    } catch {
      /* ignore corrupt state */
    }
    this.globalActive = persisted ?? defaultActive;
  }

  setGlobal(accountId: string) {
    this.globalActive = accountId;
    try {
      writeFileSync(this.statePath, JSON.stringify({ globalActive: accountId }, null, 2));
    } catch {
      /* best-effort persistence */
    }
  }
}

/** Resolve the effective active account for a session (per-session overrides global). */
export function resolveActive(session: Session, state: BrokerState): string {
  return session.localActive ?? state.globalActive;
}

/** Tracks all live sessions so a global switch can notify every connected client. */
export class SessionRegistry {
  private sessions = new Map<string, Session>();

  add(s: Session) {
    this.sessions.set(s.id, s);
  }
  remove(id: string) {
    this.sessions.delete(id);
  }
  get(id: string) {
    return this.sessions.get(id);
  }
  count() {
    return this.sessions.size;
  }

  /** Notify every session that its tool list may have changed (after a global switch). */
  async broadcastToolsChanged() {
    for (const s of this.sessions.values()) {
      try {
        await s.server?.sendToolListChanged();
      } catch {
        /* ignore dead sessions */
      }
    }
  }
}
