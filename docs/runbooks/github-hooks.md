# Runbook: GitHub's webhooks into the dev team

How the operator runs, on the control plane's host, the input adapter that turns GitHub's webhooks into the dev team's events: a finished CI becomes `pr.ci` and a merged pull request becomes `pr.mergeada`, published to team `dev` as the operator, at depth 0. The decision is option A of [topics between teams](../architecture/team-topics.md#where-githubs-facts-come-from); the pattern is an [input adapter](../architecture/input-adapters.md); the wire is [`github-hooks.asyncapi.json`](../reference/github-hooks.asyncapi.json). Nothing here runs by itself: the units below are files to write, the webhooks a call to make.

```text
GitHub ──HTTPS POST /github──► gateway <gateway-host>:<port>   self-signed certificate; the HMAC authenticates
                                   │ 127.0.0.1:9900 (sshd's end of the reverse tunnel, user <tunnel-user>)
                    ssh -R 127.0.0.1:9900:127.0.0.1:7890   (from the control plane's host: axlr-github-hooks-tunnel.service)
                                   ▼
host  axlr-bots github-hooks serve on 127.0.0.1:7890   (axlr-github-hooks.service)
        verifies X-Hub-Signature-256 · maps · publishes through the socket
                                   ▼
      axlr-bots serve (the control plane) ── group publish dev pr.ci / pr.mergeada
```

Only the dev team's own branches (`dev/…`, `--branch-prefix`) wake dev: a check suite or a merge of any other branch, such as a person's PR, is answered 204 and publishes nothing.

## What it maps

| GitHub sends | When | The adapter publishes to `dev` | Idempotency key |
|---|---|---|---|
| `check_suite` | `action: completed` | one `pr.ci` per pull request in `check_suite.pull_requests`, data `{repo, number, head_sha, conclusion}` | `github:<X-GitHub-Delivery>:<number>` |
| `pull_request` | `action: closed` and `pull_request.merged: true` | one `pr.mergeada`, data `{repo, number, merge_sha, head_sha}` | `github:<X-GitHub-Delivery>:<number>` |
| `ping` | the hook is created or pinged | nothing; `204` | |
| anything else, another action, a PR closed unmerged, a suite with no PR, a repository outside `--repos` | | nothing; `204` | |

| The answer | Means | GitHub records |
|---|---|---|
| `202` | published (or found published before: the same key is the same event) | success |
| `204` | verified, nothing to publish | success |
| `401` | no or a wrong `X-Hub-Signature-256` (logged without the body) | failure |
| `400`, `405`, `413` | not a delivery: malformed, not POST, body over 1 MiB | failure |
| `502` | a publish failed (the control plane is down, or refused it) | failure, so it can be redelivered |

A redelivery keeps its `X-GitHub-Delivery` (GitHub's webhook best practices say so), so it names the same keys: what was published is answered from before, only what is missing is published. A check suite is one app's checks on one commit, and GitHub Actions can complete more than one for the same commit (expect one per workflow run): each is its own delivery, so dev may hear several `pr.ci` for the same `head_sha`, one per suite. A suite whose `pull_requests` is empty (a pull request from a fork can give one) makes no `pr.ci`.

## Before you start

| Check | How |
|---|---|
| The control plane runs | `systemctl --user status axlr-bots` active; its socket is `~/.local/state/axlr-bots/axlr-bots.sock`. The adapter publishes through that socket only and never opens the state directory: with the daemon down every delivery is `502` |
| Team `dev` exists | `axlr-bots group get dev` names its members. Until dev's bots subscribe to `pr.ci` and `pr.mergeada` (cut 6 of the ADR) the events land on the timeline and wake nobody |
| The tunnel's key and host key | `<tunnel-key>` (mode 600) and `<known-hosts-file>`, the gateway's pinned host key. `ssh -i <tunnel-key> -o UserKnownHostsFile=<known-hosts-file> -o StrictHostKeyChecking=yes <tunnel-user>@<gateway-host> true` must not ask anything |
| The gateway | on the gateway host, set up outside this repository; confirm it there: TLS on `:8443` with its self-signed certificate, `/github` proxied to `http://127.0.0.1:9900/github`, and `<tunnel-user>` allowed that forward and nothing else (for example `restrict,port-forwarding,permitlisten="127.0.0.1:9900"` before its key in `authorized_keys`). A security group that lets in only GitHub's hook ranges (`gh api meta --jq .hooks`) is optional hardening |
| `gh` for redeliver | signed in as an admin of the four repositories with the hooks scope: `gh auth status` lists `admin:repo_hook` (or `repo`); `gh auth refresh -s admin:repo_hook` if it lacks it |

## 1. The secret

One secret for the four hooks, in a file only you can read (the adapter refuses a file its group or others may read):

```bash
umask 077
openssl rand -hex 32 > ~/.config/axlr-bots/github-hooks.secret
chmod 600 ~/.config/axlr-bots/github-hooks.secret
```

## 2. The two user units

Write both files; nothing installs them for you. The adapter's unit does not read `~/.config/axlr-bots/env`: the process that faces the internet gets the hook's secret and the socket, not the fleet's keys.

`~/.config/systemd/user/axlr-github-hooks.service`, the adapter:

```ini
[Unit]
Description=AXLR Bots GitHub hooks adapter (POST /github → team dev)
After=axlr-bots.service
Wants=axlr-bots.service

[Service]
Type=simple
ExecStart=%h/.local/bin/axlr-bots --state %h/.local/state/axlr-bots --socket %h/.local/state/axlr-bots/axlr-bots.sock github-hooks serve --listen 127.0.0.1:7890 --secret-file %h/.config/axlr-bots/github-hooks.secret --team dev
Restart=on-failure
RestartSec=5
NoNewPrivileges=yes

[Install]
WantedBy=default.target
```

`~/.config/systemd/user/axlr-github-hooks-tunnel.service`, the reverse tunnel. `ssh -f` with `ExitOnForwardFailure=yes` goes to the background only once the remote forward is up (`man ssh`, `-f`), so `Type=forking` makes `ExecStartPost` run when the gateway can reach the adapter again: the redelivery of what failed while the tunnel was down happens then, on the tunnel's start, not on a timer. The `-` before it keeps a failed redelivery from failing the tunnel.

```ini
[Unit]
Description=AXLR Bots GitHub hooks: reverse SSH tunnel to the gateway
After=network-online.target axlr-github-hooks.service
Wants=network-online.target axlr-github-hooks.service

[Service]
Type=forking
ExecStart=/usr/bin/ssh -f -N -T -i <tunnel-key> -o IdentitiesOnly=yes -o BatchMode=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile=<known-hosts-file> -o ExitOnForwardFailure=yes -o ServerAliveInterval=30 -o ServerAliveCountMax=3 -R 127.0.0.1:9900:127.0.0.1:7890 <tunnel-user>@<gateway-host>
ExecStartPost=-%h/.local/bin/axlr-bots github-hooks redeliver --since 24h --gh %h/.local/bin/gh
Restart=always
RestartSec=10

[Install]
WantedBy=default.target
```

`--since 24h` redelivers what failed in the last day; GitHub keeps three days of deliveries, so after a longer outage run it by hand with `--since 72h`.

```bash
systemctl --user daemon-reload
systemctl --user enable --now axlr-github-hooks.service axlr-github-hooks-tunnel.service
journalctl --user -u axlr-github-hooks -n 5     # "github-hooks: listening on http://127.0.0.1:7890/github, publishing to team dev …"
```

Check the adapter locally, with a signed ping and an unsigned one:

```bash
body='{"zen":"check"}'
sig="sha256=$(printf %s "$body" | openssl dgst -sha256 -hmac "$(cat ~/.config/axlr-bots/github-hooks.secret)" -hex | sed 's/^.* //')"
curl -s -o /dev/null -w '%{http_code}\n' -H 'X-GitHub-Event: ping' -H 'X-GitHub-Delivery: local-1' -H "X-Hub-Signature-256: $sig" -d "$body" http://127.0.0.1:7890/github   # 204
curl -s -o /dev/null -w '%{http_code}\n' -H 'X-GitHub-Event: ping' -d "$body" http://127.0.0.1:7890/github                                                              # 401
```

## 3. The webhooks

One hook per repository, events `check_suite` and `pull_request`, content type json, `insecure_ssl=1` (the gateway's certificate is self-signed: the HMAC is the authentication), the secret from the file. The body goes on stdin so the secret never reaches a command line:

```bash
for repo in underpass-ai/kmp underpass-ai/made underpass-ai/axlr underpass-ai/axlr-bots; do
  jq -n --rawfile secret ~/.config/axlr-bots/github-hooks.secret '{
    name: "web", active: true, events: ["check_suite", "pull_request"],
    config: {url: "https://<gateway-host>:<port>/github", content_type: "json", insecure_ssl: "1", secret: ($secret | rtrimstr("\n"))}
  }' | gh api --method POST "repos/$repo/hooks" --input - --jq '"\(.id) \(.config.url)"'
done
```

GitHub pings each new hook: `journalctl --user -u axlr-github-hooks` shows no refusal, and the hook's **Recent deliveries** on GitHub show `204`. A repository other than the four needs `--repos` on the adapter too; one outside the list is answered `204` and logged.

## 4. Verify

| Do | Expect |
|---|---|
| open a pull request on one of the four and let CI run | `axlr-bots group events dev` shows `group.event.published` with `name: pr.ci`, `by: operator`, data `{repo, number, head_sha, conclusion}`, once per finished suite |
| merge it | one `pr.mergeada` with `merge_sha` |
| redeliver one delivery from GitHub's page | `202`, the log says `(new false)`, no second event on the timeline |
| `systemctl --user stop axlr-bots`, then push | GitHub records `502`; start it again and `axlr-bots github-hooks redeliver` prints the deliveries it asked again, and each lands once |
| `systemctl --user restart axlr-github-hooks-tunnel` after the tunnel was down | the deliveries GitHub could not make (status `0`) are asked again by the unit's `ExecStartPost` |

`axlr-bots github-hooks redeliver` reads the hooks of the repositories through `gh api` (`repos/{repo}/hooks`, `…/hooks/{id}/deliveries`), keeps the active hooks whose URL's path is `/github` (or the one `--hook-url` names), and asks a redelivery (`POST …/deliveries/{id}/attempts`) of the newest attempt of each event whose every attempt failed (any status but 2xx, `0` when nothing answered) and whose newest attempt is within `--since`. An event with any successful attempt was published and is left alone. It prints what it asked, as JSON.

## Turn it off

```bash
# GitHub stops sending: deactivate (or delete) each hook
for repo in underpass-ai/kmp underpass-ai/made underpass-ai/axlr underpass-ai/axlr-bots; do
  for id in $(gh api "repos/$repo/hooks" --jq '.[] | select(.config.url == "https://<gateway-host>:<port>/github") | .id'); do
    gh api --method PATCH "repos/$repo/hooks/$id" -F active=false --jq '"\(.id) active=\(.active)"'
  done
done
# the control plane's host stops listening
systemctl --user disable --now axlr-github-hooks-tunnel.service axlr-github-hooks.service
```

With the tunnel down and the hooks still active, GitHub records each delivery as failed and nothing reaches dev; the next start of the tunnel redelivers the last day. Nothing in the control plane needs undoing: the events published stay on dev's timeline.

## When it does not work

| Symptom | Cause |
|---|---|
| every delivery `401` | the hook's secret is not the file's (a trailing space, another file); set it again with the `PATCH` of `config.secret` |
| every delivery `502`, the log says `not published: … connect` | the control plane is down or on another socket: `--socket` in the unit must be the daemon's |
| `502` with `validation` in the log | the control plane refused the event: no team `--team`, or several tenants have one |
| status `0` on GitHub, nothing in the log | the tunnel or the gateway: `systemctl --user status axlr-github-hooks-tunnel`, then the gateway |
| the adapter does not start | the secret file is readable by group or others, or `--listen` is not loopback (`--public` only if you mean it) |
| `redeliver` fails with `HTTP 404` or `403` | `gh` lacks the hooks scope or admin on the repository |
