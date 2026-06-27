# JanusMCP Broker — spike (Fasi 0–2)

Broker MCP locale che fa da proxy verso un upstream **attivo per-account**.
Dimostra il cuore del progetto: **active-account model** — un solo set di tool esposto
alla volta (quello dell'account attivo) + tool di controllo, con `tools/list_changed`
allo switch. Niente esplosione del context, niente scollega/ricollega.

Vedi il design completo in [`../design-broker-mcp-multi-account.md`](../design-broker-mcp-multi-account.md).

## Cosa fa (e cosa no, in questa fase)

- ✅ Server MCP su **stdio** (Claude Desktop/Code) **e Streamable HTTP** su localhost (ChatGPT/Gemini/Cursor/Copilot).
- ✅ Client MCP verso **N upstream** definiti in `config.json` (connessione **lazy**, condivisa tra sessioni).
- ✅ Tool di controllo: `janus_list_accounts`, `janus_use_account`, `janus_whoami`.
- ✅ Proxy di `tools/list` e `tools/call` verso l'account attivo; `list_changed` allo switch.
- ✅ **Scoping dell'identità** (§4.1): per-chiamata → **per-sessione** (`Mcp-Session-Id`) → **globale persistito**.
- ✅ `bindingMode: global | session | locked` con override `scope` su `janus_use_account`.
- ✅ Stato globale **persistito** su disco (sopravvive ai riavvii); broadcast `list_changed` a tutte le sessioni su switch globale.
- ✅ Espansione di `${VAR}` da env nei token/args (i segreti non stanno nel file).
- ⏳ Vault su keystore OS e OAuth loopback → Fase 3.

## Trasporti e variabili d'ambiente

| Variabile | Default | Note |
|---|---|---|
| `JANUS_TRANSPORT` | `stdio` | `stdio` \| `http` \| `both` |
| `JANUS_HTTP_HOST` | `127.0.0.1` | bind solo loopback |
| `JANUS_HTTP_PORT` | `7332` | endpoint `http://host:port/mcp` |
| `JANUS_CONFIG` | `./config.json` | percorso del config |
| `JANUS_STATE` | `<configDir>/.janusmcp-state.json` | dove persiste l'account globale |

In HTTP ogni client riceve una sessione (`Mcp-Session-Id`): due chat/tab possono lavorare
su clienti diversi sullo stesso processo broker (`bindingMode: session`). Uno switch
`scope: global` sposta la baseline per tutte le sessioni senza override locale.

## Requisiti

Node ≥ 20.

## Install, build, test

```bash
npm install
npm run build
npm run test:all   # stdio + HTTP multi-sessione, offline contro i mock → "ALL PASS ✅"
# npm test         # solo stdio
# npm run test:http  # solo HTTP (isolamento per-sessione + propagazione globale)
```

Il test avvia il broker, lista i tool, verifica che `ping` venga instradato all'account A,
fa `janus_use_account azienda_b`, controlla che arrivi `list_changed` e che `ping` ora
risponda da B. Tutto senza credenziali reali.

## Configurazione

`config.json` (vedi `config.example.json` per la versione commentata):

```json
{
  "defaultAccount": "azienda_a",
  "bindingMode": "global",
  "accounts": [
    { "id": "azienda_a", "service": "mock", "command": "node",
      "args": ["mock-upstream/server.mjs"], "env": { "MOCK_ACCOUNT": "azienda_a" } }
  ]
}
```

Percorso del config: variabile `JANUS_CONFIG`, altrimenti `./config.json`.

### Esempio reale: due account Supabase

```json
{
  "defaultAccount": "cliente_x",
  "accounts": [
    { "id": "cliente_x", "service": "supabase", "label": "Cliente X",
      "command": "npx",
      "args": ["-y", "@supabase/mcp-server-supabase@latest", "--read-only", "--project-ref=REF_X"],
      "env": { "SUPABASE_ACCESS_TOKEN": "${SUPABASE_PAT_CLIENTE_X}" } },
    { "id": "cliente_y", "service": "supabase", "label": "Cliente Y",
      "command": "npx",
      "args": ["-y", "@supabase/mcp-server-supabase@latest", "--read-only", "--project-ref=REF_Y"],
      "env": { "SUPABASE_ACCESS_TOKEN": "${SUPABASE_PAT_CLIENTE_Y}" } }
  ]
}
```

I PAT si passano via env (`export SUPABASE_PAT_CLIENTE_X=...`), non nel file.

## Uso con Claude Desktop

In `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "janusmcp": {
      "command": "node",
      "args": ["/percorso/assoluto/JanusMCP/spike/dist/index.js"],
      "env": { "JANUS_CONFIG": "/percorso/assoluto/JanusMCP/spike/config.json" }
    }
  }
}
```

Poi, in chat: `janus_list_accounts` per vedere gli account, `janus_use_account` per
attivare il cliente su cui lavorare. Gli altri tool (es. quelli di Supabase) appaiono
e instradano automaticamente sull'account attivo.

## Uso via HTTP (ChatGPT / Gemini / Cursor / Copilot)

```bash
JANUS_TRANSPORT=http JANUS_HTTP_PORT=7332 node dist/index.js
# endpoint MCP: http://127.0.0.1:7332/mcp
```

Poi aggiungi quell'URL come server MCP remoto nel client. Ogni client = una sessione.

## Architettura (spike, Fasi 0–2)

```
client LLM ──stdio─┐
                   ├─► Broker ──stdio──► upstream account attivo (per sessione)
altri LLM ──HTTP───┘    ├─ control tools (janus_*) gestiti localmente
   (Mcp-Session-Id)     ├─ UpstreamManager: connessioni lazy + cache, condivise tra sessioni
                        ├─ BrokerState: globale persistito + SessionRegistry (broadcast)
                        └─ resolveActive: per-sessione → globale; list_changed mirato/broadcast
```

File chiave: `src/config.ts`, `src/upstream.ts`, `src/state.ts`, `src/broker.ts`,
`src/http.ts`, `src/index.ts`; `mock-upstream/server.mjs`; `test/e2e.mjs`, `test/http.e2e.mjs`.

## Prossimi passi

Fase 3 (vault su keystore OS + OAuth loopback), Fase 4 (profili multi-server,
`with_account` one-shot, installer a un comando). Roadmap completa nel design doc.
