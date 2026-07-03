# Google Workspace (Gmail / Drive / Calendar / Chat) with JanusMCP

Google offers **remote MCP servers** for Gmail, Drive, Calendar and Chat. Unlike
Supabase/Notion/etc., they do **not** support dynamic client registration: you must bring
your **own** OAuth client (created in the Google Cloud Console). JanusMCP supports this via
the `oauthClientId` / `oauthClientSecret` / `scopes` fields on a remote account, which the
`gmail`, `google-drive`, `google-calendar` and `google-chat` catalog templates preset for
you.

The multi-account angle is the whole point: create the OAuth client **once**, then add one
account per client/identity and log into each with a different Google account — no
reconnecting to switch between them.

## 1. One-time setup in Google Cloud (per project, not per account)

1. Pick or create a Google Cloud project.
2. Enable the MCP APIs you need:

   ```bash
   gcloud services enable \
     gmailmcp.googleapis.com \
     drivemcp.googleapis.com \
     calendarmcp.googleapis.com \
     chatmcp.googleapis.com \
     --project=PROJECT_ID
   ```

   Some tools also need the underlying standard API (e.g. Gmail, Drive); enable
   `gmail.googleapis.com` / `drive.googleapis.com` if you hit a missing-API error.
3. Configure the **OAuth consent screen** and add the scopes you want (see the scopes each
   template requests, below).
4. Create an **OAuth client** of type **Desktop app**. Note its **client ID** and
   **client secret**.

## 2. Provide the client credentials to JanusMCP

The templates default to reading the client from the environment:

```bash
export GOOGLE_OAUTH_CLIENT_ID='...apps.googleusercontent.com'
export GOOGLE_OAUTH_CLIENT_SECRET='...'
```

Prefer keeping the secret out of the environment? Put it in the vault and point the account
at it:

```bash
./bin/janusmcp vault set google_client_secret     # paste the secret
```

```jsonc
// in config.json, on the account:
"oauthClientSecret": "vault:google_client_secret"
```

## 3. Add accounts

```bash
./bin/janusmcp add gmail gmail_clienteA
./bin/janusmcp add google-calendar cal_clienteA
# a second identity for the same service:
./bin/janusmcp add gmail gmail_clienteB
```

Each produces a remote OAuth account, e.g.:

```json
{
  "id": "gmail_clienteA",
  "service": "gmail",
  "transport": "http",
  "url": "https://gmailmcp.googleapis.com/mcp/v1",
  "auth": "oauth",
  "oauthClientId": "${GOOGLE_OAUTH_CLIENT_ID}",
  "oauthClientSecret": "${GOOGLE_OAUTH_CLIENT_SECRET}",
  "scopes": ["openid", "email", "https://www.googleapis.com/auth/gmail.readonly"]
}
```

Adjust `scopes` to match what you enabled on the consent screen (e.g. add
`https://www.googleapis.com/auth/gmail.compose` for sending, or swap `drive.readonly` for
`drive.file`).

## 4. Log in and use

```bash
./bin/janusmcp connect gmail_clienteA     # opens the browser; log in as client A
./bin/janusmcp connect gmail_clienteB     # log in as client B
```

The broker discovers Google's authorization server, uses **your** pre-registered client
(no dynamic registration), captures the token on a localhost loopback and stores it in the
vault (`remote_oauth_<account_id>`). It auto-refreshes, so you only re-login when the token
truly expires.

Then, in any connected client: `janus_use_account` between `gmail_clienteA` and
`gmail_clienteB` to switch identity without reconnecting.

## Default scopes per template

| Template          | URL                                        | Default scopes (readonly)                              |
|-------------------|--------------------------------------------|--------------------------------------------------------|
| `gmail`           | `https://gmailmcp.googleapis.com/mcp/v1`   | `gmail.readonly`                                       |
| `google-drive`    | `https://drivemcp.googleapis.com/mcp/v1`   | `drive.readonly`                                       |
| `google-calendar` | `https://calendarmcp.googleapis.com/mcp/v1`| `calendar.readonly`                                    |
| `google-chat`     | `https://chatmcp.googleapis.com/mcp/v1`    | `chat.spaces.readonly`, `chat.messages.readonly`       |

(All also request `openid` and `email` to identify the logged-in account.)

## Troubleshooting

- **`no registration endpoint and no preregistered client`** → `oauthClientId` is empty.
  Set `GOOGLE_OAUTH_CLIENT_ID` (and secret), or put the values directly on the account.
- **`invalid_scope` / consent errors** → the scope isn't added on your OAuth consent
  screen, or the corresponding MCP API isn't enabled in the project.
- **`access_denied`** → the Google account you logged in with isn't allowed by the consent
  screen (add it as a test user while the app is in "Testing").
