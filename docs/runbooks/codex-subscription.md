# Run bots on a Codex subscription

A bot's model is whatever its node's AXLR can reach. With an API key that is OpenRouter or a provider's API; with `axlr-node codex-proxy` it is the ChatGPT subscription the host's user signed in to with `codex login`: no API key, and the use counts against the plan's Codex limits.

```text
axlr (per execution) ──POST /v1/responses──▶ axlr-node codex-proxy (127.0.0.1:8790)
                                               │ Authorization: the login's token
                                               │ chatgpt-account-id: the login's account
                                               ▼
                                     chatgpt.com/backend-api/codex/responses
```

The proxy reads `~/.codex/auth.json` on every request, so a login Codex refreshed, or a new `codex login`, is used at once. Two days before the access token expires (`--refresh-before`) it trades the refresh token for new tokens at `auth.openai.com/oauth/token`, as Codex does, and writes them back in one rename, mode 0600, keeping every other field. The refresh token rotates: when Codex refreshed first the proxy's attempt is refused, and it takes the login Codex wrote. A request the backend refuses with 401 is renewed and sent once more.

The backend is Codex's own, not OpenAI's public API: it takes AXLR's Responses requests as they are (tool calls, `store: false`, encrypted reasoning), but it may change without notice and its limits are the plan's.

## 1. Sign in

```bash
codex login
```

`codex login status` says `Logged in using ChatGPT`. A login with an API key is refused: the proxy is for the subscription.

## 2. Run the proxy

Add a key to the environment file the node already reads (`~/.config/axlr-bots/env`, mode 0600), so only the node's runs spend the subscription:

```bash
echo "AXLR_CODEX_PROXY_KEY=$(openssl rand -hex 24)" >> ~/.config/axlr-bots/env
```

Then a user unit, `~/.config/systemd/user/axlr-codex-proxy.service`:

```ini
[Unit]
Description=AXLR Bots Codex proxy (the host user's ChatGPT login)
Before=axlr-node.service

[Service]
Type=simple
EnvironmentFile=%h/.config/axlr-bots/env
ExecStart=%h/.local/bin/axlr-node codex-proxy --listen 127.0.0.1:8790
Restart=on-failure
RestartSec=5s
NoNewPrivileges=yes

[Install]
WantedBy=default.target
```

```bash
systemctl --user daemon-reload
systemctl --user enable --now axlr-codex-proxy
systemctl --user restart axlr-node   # reads the key from the env file
```

The proxy refuses an address that is not loopback, and exits at start when the login cannot be read. Its journal names each request's status and time, and every renewal; never a token.

| Flag | Default | Environment |
|---|---|---|
| `--listen` | `127.0.0.1:8790` | `AXLR_CODEX_PROXY_LISTEN` |
| `--codex-home` | `~/.codex` | `CODEX_HOME` |
| `--refresh-before` | `48h` | `AXLR_CODEX_PROXY_REFRESH_BEFORE` |
| `--upstream` | `https://chatgpt.com/backend-api/codex` | `AXLR_CODEX_PROXY_UPSTREAM` |
| `--token-url` | `https://auth.openai.com/oauth/token` | `AXLR_CODEX_PROXY_TOKEN_URL` |
| (the key) | none: any local process may use it | `AXLR_CODEX_PROXY_KEY` |

## 3. Name the models in AXLR

One `local_models` entry per model in the node's `~/.config/axlr/settings.json`; `model` is the name Codex knows (`jq '.models[].slug' ~/.codex/models_cache.json`), `id` the one bots use:

```json
"local_models": [
  {"id": "codex/gpt-6.1-sol", "name": "GPT-6.1 Sol (Codex)", "url": "http://127.0.0.1:8790/v1", "model": "gpt-6.1-sol",
   "api": "responses", "api_key_env": "AXLR_CODEX_PROXY_KEY", "context_tokens": 272000}
]
```

The proxy's URL is loopback, but its traffic leaves the host: a bot that runs on it needs `allowRemoteProviders: true`.

## 4. Point the bots at it

Set `model.model` to the entry's id in the bot's definition and update the bot (`axlr-bots bot update <bot> --file …`, or the team's register script):

```json
"model": {"model": "codex/gpt-6.1-sol", "routingPolicy": "cost-optimized", "allowRemoteProviders": true, "maxContextTokens": 32000}
```

## Check it

Run the bot (`axlr-bots task submit …`) and read the execution: `axlr-bots execution get <id>` shows the model `codex/…` and its tokens, and `journalctl --user -u axlr-codex-proxy` shows `/v1/responses 200` for each model call.

## When it does not work

| What you see | Why | Do |
|---|---|---|
| `invalid_api_key` in the execution | The node's env lacks `AXLR_CODEX_PROXY_KEY`, or the proxy has another | Same key in both; restart `axlr-node` after editing the env file |
| `the Codex login: … run codex login` | No `auth.json`, an API-key login, or the refresh token expired or was revoked | `codex login` again; the proxy reads the new login at once |
| `connection refused` on `127.0.0.1:8790` | The proxy is not running | `systemctl --user status axlr-codex-proxy` |
| 429 from the backend | The plan's Codex limit | Wait for the window to reset, or move the bot back to an API model |
