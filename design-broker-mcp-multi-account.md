# JanusMCP Broker — Documento di Design Tecnico

> Broker MCP locale e multi-account: aggiungi le credenziali una volta, usale da qualsiasi LLM, cambia identità senza riconnettere.

**Stato:** Draft v0.1 — 26 giugno 2026
**Autore:** Massimiliano Fiori
**Codename:** `janusmcp` (provvisorio)
**Licenza prevista:** open source (MIT o Apache-2.0)

---

## 1. Problema

Chi lavora con più aziende usa lo stesso tipo di server MCP (es. Supabase, GitHub, Slack) con **identità diverse** — account, email e token diversi per ogni cliente. Oggi i client LLM (Claude Desktop in primis) gestiscono **un account alla volta** per connettore: per cambiare cliente bisogna scollegare e ricollegare, rifacendo ogni volta il login OAuth.

Il protocollo MCP non ha un concetto nativo di "account" o "tenant": **una sessione = una identità = un set di credenziali**. Tutta la complessità del multi-account va quindi risolta *fuori* dal protocollo, in un componente intermedio.

### Obiettivi

1. Registrare una volta N account per uno stesso servizio e tenerli tutti disponibili.
2. Cambiare identità attiva **senza riconnettere** e senza rifare l'OAuth.
3. Funzionare con **qualsiasi client LLM** (Claude Code/Desktop, ChatGPT, Gemini, Cursor, Copilot, ...).
4. Girare **localmente sul PC dell'utente**, con installazione e UX semplici.
5. Custodire le credenziali in modo **sicuro e credibile** (è il prerequisito di adozione).

### Non-obiettivi (per l'MVP)

- Non è un aggregatore generico di server eterogenei (quello esiste già: MetaMCP, mcp-proxy). Il focus è il **multi-credenziale sullo stesso server**.
- Niente hosting cloud / multi-utente server-side: è uno strumento *local-first*.
- Niente integrazioni proprietarie con i singoli LLM: si parla solo MCP standard.

---

## 2. Idea chiave

**Non ci si integra con nessun LLM.** Il broker implementa lo standard MCP una volta sola; qualsiasi client conforme lo usa senza adattamenti. La "client-agnosticità" è una conseguenza del protocollo, non una feature da costruire.

Il broker è contemporaneamente:

- **un server MCP** verso il client LLM (downstream);
- **un client MCP** verso i server reali (upstream), istanziati per-account;
- **un credential broker** che sta in mezzo e decide quale identità usare.

```
┌──────────────┐   MCP    ┌─────────────────────────────┐   MCP    ┌──────────────────┐
│  LLM client  │ ───────► │      JanusMCP Broker        │ ───────► │ Supabase (azA)   │
│ Claude/GPT/  │ stdio /  │                             │  stdio/  ├──────────────────┤
│ Gemini/...   │ ◄─────── │  Router · Vault · Sessions  │  http    │ Supabase (azB)   │
└──────────────┘   HTTP   └─────────────────────────────┘  ◄────── ├──────────────────┤
                                                                    │ GitHub (azA) ... │
                                                                    └──────────────────┘
```

---

## 3. Vincolo critico: trasporti per client

I client LLM non sono uniformi su *come* si connettono a un server MCP. Questo determina l'architettura.

| Client | Modello | Trasporto utile in pratica |
|---|---|---|
| Claude Desktop / Code | local-first | **stdio** (nativo) |
| ChatGPT | remote-first | **Streamable HTTP** (stdio solo da CLI) |
| Gemini | misto | **Streamable HTTP** (stdio solo in VS Code) |
| Copilot / Perplexity / Grok / Mistral | remote | **Streamable HTTP** |

**Conseguenza di design:** il broker deve esporre **due trasporti contemporaneamente**, con la stessa logica dietro:

- `stdio` per i client local-first (Claude);
- **Streamable HTTP su `localhost`** per tutti gli altri.

Un solo processo, due porte d'ingresso. È questo che rende vero "gira sul PC e lo usi con qualsiasi LLM".

---

## 4. Il problema dell'esplosione del context

Il pattern ingenuo — montare N istanze dello stesso server e prefissare i tool (`azA__execute_sql`, `azB__execute_sql`, ...) — fa **esplodere il numero di tool** nella finestra di contesto: 5 account × ~20 tool = 100 tool. È il principale limite degli aggregatori attuali sul caso multi-credenziale e degrada la qualità del modello.

**Soluzione adottata: active-account model (esposizione dinamica).**

Il broker espone **un solo set di tool** — quello dell'account *attivo* — più un piccolo set di tool di controllo per cambiare identità. Cambiando account, cambia il set esposto, e il client viene notificato via `notifications/tools/list_changed`.

Tool di controllo del broker (sempre presenti):

- `list_accounts()` → elenca account e servizi configurati e quale è attivo.
- `use_account(account_id, scope?)` → imposta l'identità attiva; ricarica i tool upstream. `scope` ∈ {`session`, `global`} (default da config, vedi §4.1).
- `whoami()` → ritorna identità attiva e scope.
- (opzionale) `with_account(account_id, tool, args)` → invocazione one-shot senza cambiare lo stato attivo, utile per operazioni cross-cliente.

Trade-off: il client lavora su un'identità per volta (coerente con il modello mentale dell'utente "ora sto sul cliente A"), in cambio di un context pulito e di un comportamento identico su tutti gli LLM.

> Variante avanzata (post-MVP): "profili" che raggruppano più server *e* credenziali insieme (es. profilo *Azienda A* = Supabase-A + GitHub-A + Slack-A), così `use_account("aziendaA")` attiva l'intero stack del cliente in un colpo.

### 4.1 Scoping dell'identità: globale, per-sessione, per-chiamata (configurabile)

L'identità attiva non deve essere un singolo valore globale: va risolta a **livelli con precedenza**, così lo stesso broker serve sia chi vuole "un cliente per tutto" sia chi tiene più clienti aperti in parallelo.

**Risoluzione dell'account attivo (dal più specifico al più generale):**

1. **Per-chiamata** — `with_account(account_id, ...)` o un parametro `account` esplicito sul tool. Override puntuale, non cambia lo stato.
2. **Per-sessione** — account attivo legato alla singola connessione/sessione MCP. Due chat/tab usano clienti diversi sullo stesso broker.
3. **Globale** — default condiviso e **persistito**, sopravvive ai riavvii. Fallback quando la sessione non ha ancora scelto.

**Cosa identifica una "sessione":**

- **Streamable HTTP:** la sessione è nativa (header `Mcp-Session-Id`). Stato per-sessione vero e proprio: tab A → Azienda A, tab B → Azienda B, **stesso processo broker**. È qui che il per-sessione brilla.
- **stdio:** il processo *è* la sessione (una connessione per processo). Il per-sessione coincide col per-processo, in memoria; il globale è ciò che viene persistito su disco e condiviso tra processi/riavvii.

**Knob di configurazione** (`janusmcp.toml`), default a livello broker con override per servizio/account/profilo:

```toml
[broker]
binding_mode = "session"   # "global" | "session" | "locked"

[service.supabase]
binding_mode = "session"   # override per servizio

[account.azienda_prod]
binding_mode = "locked"    # fissato: use_account disabilitato (sicurezza su ambienti prod)
```

- `global` — un'unica identità attiva per tutti; `use_account` cambia per tutte le sessioni.
- `session` — ogni sessione ha la sua; `use_account` agisce solo sulla sessione corrente, con fallback al default globale finché non si sceglie.
- `locked` — account fissato, `use_account` disabilitato (riduce il rischio di operare sul cliente sbagliato).

**Implicazioni implementative:**

- Lo stato globale è **persistito** (state file); lo stato di sessione è **in-memory, keyed per session id**, effimero.
- `tools/list_changed` va emesso solo a chi è interessato: in `global` broadcast a tutte le sessioni, in `session` solo alla sessione che ha fatto lo switch.
- **Snapshot per-chiamata:** all'inizio di ogni `tools/call` si congela l'account risolto, così uno switch globale concorrente non dirotta una chiamata già in volo.
- **Fallback per client "ciechi":** se un client non mantiene la sessione o non reagisce a `list_changed`, si degrada automaticamente a `global` + parametro `account` opzionale sui tool.

---

## 5. Componenti

### 5.1 Downstream MCP server (verso il client)
Implementa l'handshake MCP, espone i tool di controllo + i tool dell'account attivo, gestisce `tools/list`, `tools/call`, e le notifiche `list_changed`. Doppio listener: stdio + HTTP localhost. Mantiene una **tabella di stato per-sessione** (keyed su `Mcp-Session-Id` in HTTP, sul processo in stdio) sopra lo stato globale persistito (vedi §4.1).

### 5.2 Upstream MCP manager (verso i servizi reali)
Per ogni account configurato istanzia/gestisce una connessione client MCP verso il server reale (locale via stdio o remoto via HTTP). Strategie possibili:
- **lazy:** apre la connessione upstream solo quando l'account diventa attivo (meno risorse, primo accesso più lento);
- **warm pool:** tiene aperte le connessioni usate di recente (switch più rapido).
Fa anche il **fetching/caching degli schemi** dei tool upstream per non interrogarli a ogni switch.

### 5.3 Router
Mappa la chiamata in arrivo dal client all'upstream corretto in base all'account attivo (o all'`account_id` nei tool one-shot). Gestisce il rewriting di eventuali prefissi e l'inoltro di errori upstream.

### 5.4 Credential Vault
Custodia delle credenziali, ancorata al sistema operativo (vedi §6). Espone un'interfaccia interna: `get(account_id)`, `put(account_id, secret)`, `delete`, `list_metadata` (mai i segreti in chiaro nei log o nel config).

### 5.5 OAuth Broker
Gestisce i flussi OAuth per-account con redirect su loopback locale (vedi §7), il refresh automatico dei token e la persistenza nel vault.

### 5.6 Config & Control UI
Una mini-UI web servita su `localhost` (più una `janusmcp.toml`/`json` per chi preferisce file) per: aggiungere/rimuovere account, lanciare i login OAuth, vedere lo stato, impostare scope e read-only.

---

## 6. Sicurezza: il vault ancorato all'OS

È il vero motore di adozione: l'utente affida al broker le credenziali di lavoro di **più clienti**. Niente token in chiaro in un JSON versionato.

- **macOS:** Keychain
- **Windows:** Credential Manager (DPAPI)
- **Linux:** Secret Service / libsecret (con fallback a file cifrato keyring-compatibile)

Principi:
- segreti **a riposo sempre cifrati**, gestiti dal keystore OS;
- il config su disco contiene **solo metadati** (id account, servizio, scope, project_ref), mai i token;
- **scoping minimo** per default (es. `project_ref` su Supabase, repo specifici su GitHub) e modalità **read-only** opzionale per ridurre il blast radius;
- **redazione nei log** di qualsiasi materiale sensibile;
- audit locale opzionale delle chiamate (chi/quale account/quale tool/quando).

---

## 7. OAuth loopback locale

Risolve direttamente il dolore "un account alla volta" del connettore desktop.

1. L'utente clicca "Aggiungi account" nella Control UI e sceglie il servizio.
2. Il broker apre il browser sull'authorization endpoint del provider, con `redirect_uri = http://127.0.0.1:<porta>/callback` e `state` anti-CSRF (PKCE dove supportato).
3. Il provider reindirizza al loopback; il broker cattura il `code`, lo scambia per access/refresh token.
4. I token finiscono nel **vault** legati all'`account_id`; il refresh è automatico e trasparente.
5. Per i servizi che usano **Personal Access Token** (es. Supabase), in alternativa all'OAuth si incolla il PAT una volta nella UI e va nel vault.

Niente copia-incolla di token a ogni cambio cliente, niente riconnessioni.

---

## 8. Flussi principali

**Aggiunta account**
`UI → scegli servizio → OAuth loopback / PAT → vault.put(account_id) → config.append(metadata)`

**Switch di identità**
`client → use_account("aziendaB") → manager attiva upstream B (lazy/warm) → vault.get(B) → schemi tool B → tools/list_changed → il client vede i tool di B`

**Chiamata a un tool**
`client → tools/call(execute_sql) → router → upstream attivo (B) con credenziali di B → risposta → client`

**Operazione one-shot cross-cliente**
`client → with_account("aziendaA", "list_projects", {}) → router instrada ad A senza cambiare l'attivo (resta B)`

---

## 9. Stack tecnologico (proposta)

| Esigenza | Scelta consigliata | Motivazione |
|---|---|---|
| Linguaggio | **Go** (alt. Rust) | binario singolo cross-platform, zero runtime, distribuzione "scaricalo e parte" |
| Trasporti MCP | SDK MCP ufficiale + stdio/Streamable HTTP | conformità al protocollo, due listener nello stesso processo |
| Vault | wrapper sui keystore OS (Keychain/DPAPI/libsecret) | sicurezza credibile, nessun segreto su file |
| Control UI | web su `localhost` (UI minimale embeddata) | indipendente dal client, cross-platform |
| Packaging | binario singolo + `npx`/`uvx` + Docker | massima copertura di installazione |

> Se il time-to-market conta più del binario singolo, un'implementazione TypeScript/Node con l'SDK MCP ufficiale accelera l'MVP; il prezzo è una runtime da installare. Go resta preferito per "lo usano tutti senza pensarci".

---

## 10. Roadmap MVP

**Fase 0 — Spike di protocollo (1 settimana)**
Broker minimo stdio che fa da passthrough verso **un** upstream Supabase. Valida handshake, `tools/list`, `tools/call`, `list_changed`.

**Fase 1 — Multi-account su Supabase (MVP)**
- Vault con keystore OS.
- `list_accounts` / `use_account` / `whoami`.
- Active-account model con `tools/list_changed`.
- Config dei metadati + PAT via UI minimale.
- Solo trasporto **stdio** (target: Claude Desktop/Code).

**Fase 2 — Dual transport**
- Listener **Streamable HTTP** su localhost → sblocca ChatGPT, Gemini, Cursor, Copilot.
- Hardening sicurezza (binding solo loopback, token di sessione locale).

**Fase 3 — OAuth loopback + secondo connettore**
- Flusso OAuth completo con refresh.
- Aggiunta di **GitHub** come secondo servizio per validare la genericità.

**Fase 4 — Profili & polish per l'adozione**
- Profili multi-server per cliente.
- `with_account` one-shot.
- Installer a un comando, docs, esempi di config per ogni client LLM.

**Fase 5 — Community**
- Allineamento alle convenzioni upstream (vedi §12), template per aggiungere nuovi connettori, contribuzione/feedback al gruppo MCP.

---

## 11. Rischi e mitigazioni

| Rischio | Impatto | Mitigazione |
|---|---|---|
| Esplosione del context con tanti account | Alto | Active-account model, niente N istanze montate |
| Differenze di trasporto tra client | Alto | Dual-transport (stdio + HTTP) fin dalla Fase 2 |
| Fiducia sulle credenziali | Alto | Vault su keystore OS, scoping minimo, read-only, redazione log |
| Latenza al primo switch (lazy) | Medio | Warm pool per gli account usati di recente |
| Refresh OAuth e scadenze token | Medio | OAuth broker con refresh automatico e retry |
| Sovrapposizione con aggregatori esistenti | Medio | Posizionamento netto sul multi-credenziale, non sull'aggregazione generica |
| Drift dello standard MCP | Medio | Dipendere dall'SDK ufficiale, seguire le discussioni upstream |

---

## 12. Prior art e posizionamento

Esiste un ecosistema maturo di **aggregatori/gateway** (MetaMCP — modello Servers→Namespaces→Endpoints; mcp-proxy; IBM ContextForge; Microsoft MCP Gateway; Obot). Una survey Q1 2026 su 17 tool segnala che tutti convergono su "aggregazione flat + RBAC" e **nessuno copre bene il modello multi-dimensionale** — incluso il caso *stesso servizio, N identità*.

**Differenziatore in una riga:** non "aggrego server diversi", ma "**gestisco identità multiple per lo stesso servizio senza saturare il context, da qualsiasi LLM, in locale**".

Prima di scrivere codice, leggere la discussione upstream ufficiale sui server che fanno da proxy-client verso molti server (`modelcontextprotocol` Discussion #94): utile per allinearsi alle convenzioni ed eventualmente contribuire lì invece di partire da zero.

---

## 12-bis. Modalità CLI / code-execution (riduzione token)

Oltre all'esposizione come server MCP, valutare una modalità in cui l'agente consuma il
broker via **CLI / esecuzione di codice** invece di caricare in contesto le definizioni di
tutti i tool. È la direzione "code execution with MCP": l'agente scopre e invoca i tool
*on-demand* (es. `janusmcp call <account> <tool> <json-args>`, più comandi di discovery),
così il costo in token non cresce col numero di tool/upstream. Si sposa con il modello
active-account (già pensato per tenere il contesto leggero) e con gli upstream multipli:
invece di N×tool nel prompt, l'agente chiama ciò che serve. Trade-off da studiare: UX per
l'agente, scoperta degli schemi, e quando convenga CLI vs MCP nativo. Vedi roadmap.

## 13. Domande aperte

- ~~Active-account globale o per-sessione?~~ **Risolto (§4.1):** entrambi, configurabili via `binding_mode` con risoluzione a livelli (per-chiamata → per-sessione → globale). Resta da decidere il default out-of-the-box (`session` proposto per chi lavora multi-cliente).
- Come gestire i client che **non** reagiscono a `tools/list_changed`? Serve un fallback (es. parametro `account` opzionale sui tool).
- Naming/branding e modello di estensione per i connettori di terze parti.
- Strategia di test cross-client (matrice Claude/GPT/Gemini/Cursor) e CI cross-platform.

---

### Fonti consultate
- MetaMCP — MCP Aggregator/Gateway: https://github.com/metatool-ai/metamcp
- mcp-proxy (TBXark): https://github.com/TBXark/mcp-proxy
- State of the Ecosystem Q1 2026: https://www.heyitworks.tech/blog/mcp-aggregation-gateway-proxy-tools-q1-2026
- MCP across AI platforms (Claude/ChatGPT/Gemini/Copilot): https://chatforest.com/guides/mcp-across-ai-platforms/
- Connect to remote MCP servers: https://modelcontextprotocol.io/docs/develop/connect-remote-servers
- Proxy-client servers — modelcontextprotocol Discussion #94: https://github.com/modelcontextprotocol/modelcontextprotocol/discussions/94
- Supabase MCP Server: https://supabase.com/docs/guides/getting-started/mcp
