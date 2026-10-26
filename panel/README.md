<p align="center"><picture><source media="(prefers-color-scheme: dark)" srcset="assets/brand/axlr-bots-emblem-stacked-ink-dark.svg"><img src="assets/brand/axlr-bots-emblem-stacked-ink-light.svg" alt="AXLR Bots" width="300"></picture></p>

<p align="center"><strong>The control panel of AXLR Bots: see the fleet, send tasks, say yes or no to a bot, from one window.</strong></p>

[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](../LICENSE)

AXLR Bots Panel is a native desktop application for [AXLR Bots](../README.md), Underpass's control plane for bots that live as infrastructure. It talks to a running control plane through its operator API (HTTPS with the fleet CA, the bearer token from its file) and shows nodes, bots, tasks and executions with status pills, icons and colour, in few words. It is written in Rust with [Iced](https://iced.rs) and draws with the GPU or without one.

<p align="center"><img src="docs/screenshots/overview-light.png" alt="Home: nodes online, bots on, tasks running, waiting, failed and expired today, cost today" width="860"></p>

## Install

User-level, no root. You need Rust 1.88 or newer and a control plane with its operator address on (`axlr-bots serve --api-listen 127.0.0.1:7879`, as the [install runbook](../docs/runbooks/install.md) sets up). Build the control plane from the same commit as the panel: the two ship together in this repository. A control plane older than the panel's routes answers the first **Connect** with *Check settings: this control plane has no /v1/overview*.

```bash
cd panel
make install        # ~/.local/bin/axlr-bots-panel, the icon, and the menu entry "AXLR Bots"
```

`cargo install --path .` installs the binary alone. `make uninstall` removes what `make install` put.

## Run

```bash
axlr-bots-panel
```

The first run opens **Settings** with three values read from this host's install: the API address (`https://127.0.0.1:7879`), the fleet CA (`~/.local/state/axlr-bots/ca/ca.crt`) and the token file (`~/.local/state/axlr-bots/api.token`). **Connect** checks them against the control plane and saves them in `~/.config/axlr-bots-panel/config.toml`. The token stays in its file: the panel reads it and never shows or logs it.

| Flag or variable | Does |
|---|---|
| `--theme light\|dark` | start in that theme; light is the default (the sun and moon button switches it) |
| `--screen overview\|nodes\|bots\|bot:<id>\|tasks\|task:<id>\|settings` | open that screen once connected |
| `ICED_BACKEND=tiny-skia` | draw on the CPU, for a host without a working GPU driver |

The panel reads the control plane every 3 seconds, in the background; the window never waits on the network.

## Screens

| Screen | Light | Dark |
|:--|:--:|:--:|
| **Home**: nodes online, bots on, tasks running, waiting, failed and expired today, cost today; the tasks that need you; the nodes; the latest tasks | <img src="docs/screenshots/overview-light.png" width="300"> | <img src="docs/screenshots/overview-dark.png" width="300"> |
| **Nodes**: status, labels, last heartbeat, certificate expiry, AXLR version; a one-time host token with copy buttons; revoke with a second click | <img src="docs/screenshots/nodes-light.png" width="300"> | <img src="docs/screenshots/nodes-dark.png" width="300"> |
| **Bots**: status, level, model, placement, triggers; turn on or pause; add one from a form or a definition file | <img src="docs/screenshots/bots-light.png" width="300"> | <img src="docs/screenshots/bots-dark.png" width="300"> |
| **Now**: what each bot is doing right now, in the Bots list and on its page: waits for you, working on a task (for how long, its newest step), queued, idle or paused, with a link to the task (shots of the sample fleet) | <img src="docs/screenshots/now-bots-light.png" width="300"> | <img src="docs/screenshots/now-bot-light.png" width="300"> |
| **Talk to it**: on a bot's page, a chat. Each message runs as a new task of the bot, carrying the last three turns so it keeps the thread; its answer, or where the task stands, shows under it, and a click opens the task. The bot answers between runs, not during one (shot of the sample fleet) | <img src="docs/screenshots/chat-light.png" width="300"> | |
| **New bot**: three questions a person can answer: a name, what it should do in their words, where it runs. It asks before acting by default; how much it may do alone, its tools and its model sit under Advanced, closed (shots of the sample fleet) | <img src="docs/screenshots/new-bot-light.png" width="300"> | <img src="docs/screenshots/new-bot-advanced-light.png" width="300"> |
| **Bot**: mission, model, tools and tool rules, level, budgets, placement, triggers, its events as they happen, its latest tasks | <img src="docs/screenshots/bot-light.png" width="300"> | <img src="docs/screenshots/bot-dark.png" width="300"> |
| **Tasks**: across bots, filtered by status and bot; send a task (one idempotency key per form); stop one | <img src="docs/screenshots/tasks-light.png" width="300"> | <img src="docs/screenshots/tasks-dark.png" width="300"> |
| **Task, waiting**: the call the bot stopped on and its arguments, Approve or Deny | <img src="docs/screenshots/task-waiting-light.png" width="300"> | <img src="docs/screenshots/task-waiting-dark.png" width="300"> |
| **Task, done**: the answer, as Markdown or as text to select, every run with its node, outcome, tool calls and their decisions, usage and cost, trace and policy; the timeline | <img src="docs/screenshots/task-light.png" width="300"> | <img src="docs/screenshots/task-dark.png" width="300"> |
| **Copy**: a button beside each block copies it: the task, the answer, the call waiting and each argument, a failure, a run's error, the timeline, a bot's mission and events, an error banner. "Select text" turns the answer into text to select a part of (shot of the sample fleet) | <img src="docs/screenshots/task-copy-light.png" width="300"> | |
| **Control in one click**: Approve or Deny a waiting call wherever it shows (Home, Tasks, the bot page), with what the bot wants to call. On a bot's page, Pause and Stop are always in sight (Stop also cancels its open tasks); under Advanced, closed by default, "How much may it do alone?" (Ask me before acting, Act on its own within its rules, Only watch and report) and the bot's triggers, placement, tools, model and budgets (shots of the sample fleet) | <img src="docs/screenshots/control-bot-light.png" width="300"> | <img src="docs/screenshots/control-advanced-light.png" width="300"> |
| **Settings**: the API address, the CA and the token file; the theme | <img src="docs/screenshots/settings-light.png" width="300"> | <img src="docs/screenshots/settings-dark.png" width="300"> |

The screenshots are the live control plane of the [port fleet](../docs/runbooks/drone-fleet.md): eight nodes (the fleet base, three quadcopters, two rovers, a supply rover and a plane, each a node agent beside its autopilot in a pod), nine bots, and four pilots waiting for a person before they take off. [The live run](docs/live-run.md) shows a task sent to a node and a write approved, both from the panel.

## Made to be read fast

Ink on a warm white page, graphite at night; the emblem's copper is the one accent, and it marks the place you are in and the main act of a screen. Every status is a pill with a colour, an icon and one or two words: green ✓ done or on, copper ▶ working, amber ⚠ needs a person, red ✕ failed or off, grey ⏸ stopped or waiting its turn. A count that needs a person or says something failed tints its whole card.

Words are set in [Lexend](https://www.lexend.com) (made for reading fluency) and big numbers in [DotGothic16](https://github.com/fontworks-fonts/DotGothic16), a thin pixel face in the spirit of the emblem; both are embedded with the binary (`assets/fonts`, SIL Open Font License, the licences beside them). Body text is 16 px and secondary text 14 px, left-aligned, never italic; a number is 40 px. The two themes keep every text colour at 4.5:1 or more against its background, and a pill's word against the pill's own tint (tests check it). Lines are 1 px, corners slightly rounded, nothing casts a shadow. The icons are 16 × 16 pixel glyphs in the house style of the emblem (`assets/icons/make_icons.py` draws them).

## How it works

```text
 ui (Iced)          App::apply(message) → state + effects      views draw the state
    │                                    │
    ▼                                    ▼
 application        use cases: RefreshFleet · LoadTaskDetail · SubmitTask · DecideTask · CancelTask · SwitchBot
                    RegisterBot · MintEnrollmentToken · RevokeNode · FollowBotEvents · Connect …
    │  ports: ControlPlane · Clock · KeySource · FilePicker · DefinitionReader · ConfigStore · Connector
    ▼
 adapters           http (reqwest + rustls, fleet CA only, bearer token) · system (clock, uuid keys, files,
                    XDG file dialog) · config (TOML settings, defaults from the axlr-bots unit) · fake (in memory)
    │
    ▼
 domain             Node · Bot · Task · Execution · ExecutionResult · Event · Overview · EnrollmentToken …
                    value types mirroring the operator API's JSON, validated as they are read
```

- **Hexagonal, one file per type.** The domain depends on nothing but serde and chrono; the use cases see the control plane through the `ControlPlane` port; the adapters implement the ports; `main.rs` wires them.
- **The update logic does no input or output.** `App::apply` turns a message into the new state and a list of effects; the runtime runs the effects as Iced tasks. Tests drive `apply` message by message against the in-memory control plane, and draw every screen with Iced's headless simulator.
- **Nothing twice.** A task form keeps one idempotency key from the moment it opens, so sending twice names one task. A refresh carries a generation, and an older answer is dropped.
- **Acts that cannot be undone ask twice**: revoking a node, stopping a task, denying a call.

The routes the panel reads beyond the original operator API (the task list across bots, a task's timeline, the overview, pausing a bot, a node's certificate expiry) are documented in the [operator API reference](../docs/reference/operator-api.md).

## Development

```bash
cd panel
make check          # cargo fmt --check, cargo clippy -D warnings, cargo test (two jobs)
make coverage       # cargo llvm-cov over domain, application, adapters and the update logic
AXLR_PANEL_LIVE=1 cargo test --test live_control_plane -- --nocapture   # read the running control plane
```

The HTTPS adapter is tested against a local TLS server with a CA made for the test, including a server whose certificate chains to another CA (refused) and a wrong token (`401`).

## License

Apache License 2.0, like the rest of AXLR Bots; see [LICENSE](../LICENSE) and [NOTICE](../NOTICE). Part of [Underpass AI](https://underpassai.com).
