# JanusMCP Broker — implementazione Go (Fasi 0–3)

Port in Go del broker MCP multi-account, con **vault** integrato. Stesso modello dello
spike TS (active-account, scoping per-sessione/globale/locked), ma pensato per il
prodotto vero: **binario singolo** cross-platform e segreti nel **keychain del sistema**.

> ⚠️ **Primo getto, da compilare sul tuo Mac.** È stato scritto allineando le firme
> dell'SDK Go ufficiale dalla documentazione, ma **non è stato compilato** nell'ambiente
> dove l'ho prodotto (la toolchain Go non era installabile lì). Builda con i comandi qui
> sotto e, se ci sono errori, incollameli: li sistemo. I punti più probabili da aggiustare
> sono elencati in fondo.

## Struttura

```
go/
├── go.mod
├── config.example.json
├── cmd/janusmcp/main.go          # entrypoint + subcomandi (serve, vault, login)
└── internal/
    ├── config/config.go          # JSONC + risoluzione segreti (${ENV} e vault:<name>)
    ├── vault/vault.go            # keychain OS + fallback file AES-256-GCM
    ├── oauth/loopback.go         # OAuth Authorization Code + PKCE su loopback
    ├── oauth/store.go            # persistenza token nel vault + refresh automatico
    └── broker/
        ├── state.go             # stato globale persistito + registry sessioni
        ├── upstream.go          # client MCP verso gli upstream (lazy, condivisi)
        ├── server.go            # server per-sessione, control tools, proxy, active-account
        └── broker_test.go       # test integrazione (usa il mock Node dello spike)
```

## Build e test (sul Mac)

```bash
cd go
go mod tidy          # risolve SDK ufficiale, jsonschema-go, go-keyring
go build ./...
go test ./internal/broker/ -run TestActiveAccount -v   # richiede `node` su PATH
```

Il test riusa il mock upstream dello spike TS (`../spike/mock-upstream/server.mjs`):
avvia due "account" finti, verifica che `ping` venga instradato all'account attivo e
che `janus_use_account` cambi l'instradamento.

## Esecuzione

```bash
# stdio (Claude Desktop/Code)
JANUS_CONFIG=./config.json ./janusmcp serve

# Streamable HTTP su localhost (ChatGPT/Gemini/Cursor/Copilot)
JANUS_TRANSPORT=http JANUS_HTTP_PORT=7332 ./janusmcp serve
# endpoint: http://127.0.0.1:7332/mcp

# entrambi
JANUS_TRANSPORT=both ./janusmcp serve
```

### Variabili d'ambiente

| Variabile | Default | Note |
|---|---|---|
| `JANUS_TRANSPORT` | `stdio` | `stdio` \| `http` \| `both` |
| `JANUS_HTTP_HOST` / `JANUS_HTTP_PORT` | `127.0.0.1` / `7332` | listener HTTP |
| `JANUS_CONFIG` | `./config.json` | percorso config |
| `JANUS_STATE` | `<configDir>/.janusmcp-state.json` | stato globale persistito |
| `JANUS_VAULT` | `keyring` | `keyring` (OS) \| `file` (fallback cifrato) |
| `JANUS_VAULT_DIR` | `.` | dir per il backend `file` |

## Vault

I segreti non stanno nel config: si referenziano come `vault:<nome>`.

```bash
# salva un PAT nel keychain di sistema
./janusmcp vault set supabase_cliente_x      # poi incolli il token su stdin
```

Nel config:

```json
"env": { "SUPABASE_ACCESS_TOKEN": "vault:supabase_cliente_x" }
```

Backend: **macOS Keychain / Windows Credential Manager / Linux Secret Service** via
`zalando/go-keyring`; fallback **file AES-256-GCM** per ambienti headless
(`JANUS_VAULT=file`), con chiave a 32 byte in `janusmcp-vault.key` (chmod 0600).

## OAuth loopback

Flusso completo Authorization Code + PKCE su loopback `127.0.0.1`, con persistenza del
token nel vault e **refresh automatico** quando scade.

1. Definisci i provider in `config.json` → `oauthProviders` (vedi `config.example.json`).
2. Fai il login una volta:

```bash
./janusmcp login github cliente_y_github   # apre il browser, cattura il code, salva il token
```

   In alternativa puoi avviare il login **da dentro il client** col tool `janus_login`
   (`{provider, name}`): apre il browser, salva il token nel keychain, **non passa mai
   dalla chat**. Il tool è esposto solo quando OAuth/vault è configurato.

3. Referenzia l'access token nell'account con `oauth:<name>`:

```json
"env": { "ACCESS_TOKEN": "oauth:cliente_y_github" }
```

I secret ref vengono risolti **al momento del connect di ogni upstream** (lazy, per-spawn),
non una volta all'avvio: così `oauth:<name>` fornisce un access token valido a ogni nuova
connessione, rinnovandolo e ri-salvandolo se scaduto.

Risoluzione dei secret ref, in ordine: `vault:<name>` (segreto grezzo) → `oauth:<name>`
(access token fresco) → `${ENV}` (variabile d'ambiente).

Test del refresh (token endpoint finto, nessuna rete esterna):

```bash
go test ./internal/oauth/ -run TestAccessTokenRefresh -v
```

> Nota: una connessione upstream è longeva; il token è passato come env allo spawn. Il
> refresh agisce a ogni nuovo connect. Un refresh continuo su una singola connessione
> longeva richiederebbe il restart dell'upstream (miglioramento futuro).

## Onboarding rapido: catalogo + `add`

Invece di scrivere il config a mano, scaffolda un account da un template:

```bash
./bin/janusmcp catalog                 # elenca i template disponibili
./bin/janusmcp add supabase            # Supabase hosted, login via browser (OAuth)
./bin/janusmcp add supabase-pat acme   # Supabase locale via PAT (account id "acme")
```

`add` crea/aggiorna `config.json` e stampa i passi rimanenti (es. project ref, `vault set`).
Template inclusi (`janusmcp catalog`): server remoti con login via browser —
`supabase`, `github`, `figma`, `notion`, `sentry`, `stripe`, `hubspot`, `paypal` — più i
generici `http-oauth` (qualsiasi MCP remoto OAuth), `supabase-pat` (locale) e `stdio` (locale).

**Template personalizzati:** crea `~/.config/janusmcp/templates.json` (o `$JANUS_TEMPLATES`)
per aggiungerne di tuoi; vengono uniti ai built-in:

```json
{
  "myserver": {
    "description": "Il mio server MCP remoto",
    "account": { "transport": "http", "url": "https://example.com/mcp", "auth": "oauth" },
    "notes": ["Login nel browser al primo uso."]
  }
}
```

**Nota:** `add` scrive solo il config — non fa il login. Per un account OAuth remoto il
browser si apre quando l'account viene *usato*. Per farlo subito da terminale:

```bash
./bin/janusmcp connect supabase        # apre il browser per autorizzare e salva il token
```

### Pannello di controllo (mini-UI)

```bash
./bin/janusmcp ui                      # apre http://127.0.0.1:7333 nel browser
```

Mostra gli account con lo stato (ready / needs login / needs secret), con bottoni per
fare il **Login** via browser, aggiungere account dal catalogo, e impostare i PAT nel vault.

### Collegamento ai client con un comando

```bash
./bin/janusmcp install claude-desktop   # scrive claude_desktop_config.json
./bin/janusmcp install claude-code      # esegue `claude mcp add` per te
./bin/janusmcp install cursor           # scrive ~/.cursor/mcp.json
./bin/janusmcp install vscode           # scrive il mcp.json utente (chiave "servers")
./bin/janusmcp install gemini           # scrive ~/.gemini/settings.json
./bin/janusmcp install codex            # esegue `codex mcp add` per te
./bin/janusmcp install chatgpt          # istruzioni guidate (remoto/HTTPS, via UI)
./bin/janusmcp install print            # stampa il blocco JSON da incollare ovunque
./bin/janusmcp install --list           # elenca i target (e quali sono già configurati)
```

Auto-configura il client per lanciare questo stesso binario; poi `janusmcp ui` per gli account.
ChatGPT è solo remoto su HTTPS (niente file): il comando stampa i passi (HTTP mode + tunnel + Developer Mode).
Per Claude Desktop esiste anche il bundle **one-click `.mcpb`** (doppio click per installare):
vedi [`../mcpb/`](../mcpb/).

## Provider e test con Supabase

Elenca i provider OAuth preconfigurati:

```bash
./bin/janusmcp providers     # github, supabase, google (override in config 'oauthProviders')
```

Per provare il broker con **due account Supabase reali** (PAT nel vault, switch senza
riconnettere) segui la guida passo-passo: [`docs/supabase.md`](docs/supabase.md).
Config pronta: [`config.supabase.example.json`](config.supabase.example.json).

## Uso con Claude Desktop

```json
{
  "mcpServers": {
    "janusmcp": {
      "command": "/percorso/assoluto/JanusMCP/go/janusmcp",
      "args": ["serve"],
      "env": { "JANUS_CONFIG": "/percorso/assoluto/JanusMCP/go/config.json" }
    }
  }
}
```

## Punti probabili da aggiustare al primo build

Assunzioni fatte sull'API dell'SDK Go che potrebbero richiedere una piccola correzione:

1. **Firma di `mcp.ToolHandler`** — ho assunto `func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error)`. Se differisce, adeguare gli handler in `server.go`.
2. **`req.Params.Arguments`** — assunto `json.RawMessage`. Se è già `map[string]any`, togliere gli `json.Unmarshal` e usarlo diretto.
3. **`mcp.NewStreamableHTTPHandler(getServer, opts)`** — firma del callback `func(*http.Request) *mcp.Server` e tipo di ritorno (`http.Handler`).
4. **`Server.RemoveTools` / `AddTool` post-init** — verificare che emettano `tools/list_changed` automaticamente; in caso contrario aggiungere la notifica esplicita.
5. **`mcp.CallToolParams.Arguments`** — assunto `map[string]any`.
6. **`jsonschema.Schema`** campi (`Enum []any`, `Properties map[string]*Schema`, `Required []string`).

Lo spike TS in `../spike/` resta come **riferimento verificato** del comportamento atteso.
