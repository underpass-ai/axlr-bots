# The orchestra

36 bots, one chair each, play the first movement of Mozart's Symphony No. 25 in G minor (public domain, [why not No. 40](score-service/score/README.md)); the operator conducts with a hand in front of a webcam. Each chair is a real AXLR bot with a mission, a policy and a budget: it decides when it enters, how loud, and when it stops. The notes are Mozart's, the clock is a sequencer, the sound is a synthesizer.

> **SIMULATION.** Each chair is one LLM bot. The notes come from a score, the sound from a synthesizer; the bots decide only when to enter, how loud, and when to stop.

| Piece | Where | State |
|---|---|---|
| 1. The stage: nodes and a team, beside the control plane | [`stage/`](stage/) | done |
| 2. What `stop.all` does to a long task | [`stage/measure_stop.py`](stage/measure_stop.py) | done, [measured](#2-stopall-on-a-long-task) |
| 3. `score-service`: the parts, the clock, the synthesizer | [`score-service/`](score-service/README.md) | done: `stop.all` silences it in 0.4 s |
| 4. 36 chairs in the team `orchestra` | [`chairs/`](chairs/README.md) | here: 36 Enabled, one cue, 36 playing |
| 5. `/stage`: the view | `score-service/static/` | |
| 6. `conductor-cam` and the phone's three buttons | `conductor-cam/` | |
| 7. Dress rehearsal and the latency table | this file | |

## 1. The stage

A node runs **one execution at a time** ([`agent.go`](../../adapters/nodeagent/agent.go)): a section of eight violins that enters together needs eight nodes. The stage is one `axlr-node` per chair on the control plane's host, each over the control plane's Unix socket (no enrollment, no TLS, no new port), dedicated (it takes only the bots placed on it) and with its own `HOME`, so its AXLR reads its own `~/.config/axlr/mcp.json` and never the host's.

```text
host gx10 ─ axlr-bots serve (systemd --user) ── socket ──┬── axlr-node stage-probe-1 (HOME=<stage>/probe-1, dedicated)
                                                         ├── axlr-node stage-probe-2
                                                         └── … one per chair
```

| Per chair | Measured on gx10 (aarch64), one long task running |
|---|---|
| `axlr-node` | 10 MB |
| `axlr-tui` (the run) | 42 MB |
| `axlr-node mcp-relay` | 8 MB |
| 36 chairs | about 2 GB |

```bash
stage/stage.sh start probe-1 probe-2            # stage-probe-1, stage-probe-2: Online, dedicated, labels app=stage,chair=…
axlr-bots node list
for n in 1 2; do
  sed "s/stage-probe-N/stage-probe-$n/g" stage/probe-bot.json > /tmp/probe-$n.json
  axlr-bots bot register --file /tmp/probe-$n.json && axlr-bots bot enable stage-probe-$n
done
axlr-bots group publish stage stop.all --message "Stop now." --key probe-1
curl --cacert ~/.local/state/axlr-bots/ca/ca.crt -H "Authorization: Bearer $(cat ~/.local/state/axlr-bots/api.token)" \
     https://127.0.0.1:7879/v1/groups/stage/topics      # stop.all, publisher operator
stage/stage.sh stop
```

The state is `~/.local/state/axlr-stage/<node>` (`AXLR_STAGE`), mode 0700: AXLR refuses a diagnostic directory another account may write. The model key is read from `~/.config/axlr-bots/env` by the node's shell, as the systemd unit does, and never printed. On Kubernetes the same stage is a StatefulSet of [sidecars](../../docs/runbooks/sidecar.md), one per chair, in its own namespace; this directory is the version for one host.

**Checked (29 Oct 2026, control plane `main` c63d109 on gx10):** both nodes Online and dedicated; `GET /v1/groups/stage/topics` answers `stop.all` with publisher `operator`; `stop.all` reached both bots as an urgent task each.

## 2. `stop.all` on a long task

[`stage/measure_stop.py`](stage/measure_stop.py) gives each probe bot a task that holds (one `local_exec` of `sleep 120`), waits until both tools are running, publishes `stop.all` as the operator, and reads four clocks per bot, in seconds after the publish:

| Clock | What it is | Source |
|---|---|---|
| cancelled | the long task ends `Cancelled`, preempted by the urgent task | the control plane (`bot.task.preempted`) |
| run gone | the long task's process is gone from the host | this host's process table: a cancelled run reports nothing |
| urgent started | the urgent task `stop.all` made starts on the bot's node | the control plane |
| answered | the bot read the stop and answered | the control plane |

```bash
python3 stage/measure_stop.py --team stage --bot stage-probe-1 --bot stage-probe-2 \
  --objective "Hold: call the sleep once as your mission says." --process "sleep 120$" --settle 3
```

Four runs, two bots each, 29 Oct 2026, `anthropic/claude-haiku-5.5` over OpenRouter:

| Run | cancelled | run gone | urgent started | answered |
|---|---|---|---|---|
| 1 | 0.00 | 5.4 | 5.3–5.4 | 6.9–7.0 |
| 2 | 0.00 | 5.4 | 5.3–5.5 | 6.6–6.7 |
| 3 | 0.00 | 1.9 | 1.8–1.9 | 3.1–3.2 |
| 4 | 0.00 | 9.5 | 9.5–9.6 | 10.7–11.1 |

What it does: **it interrupts, it does not wait.** The long task is cancelled the moment the event is published, but the run goes on until its node renews its lease (every 10 s), when the node stops it, tool included; the node's one slot is then free and the urgent task starts at once. So the bot-side stop takes **between 0 and 10 s, plus about 1.5 s** for the bot to read the stop and answer, depending on where the lease renewal falls. A bot never finishes its long tool first.

For the orchestra this means two clocks, labelled apart on screen:

| Clock | Expected | Why |
|---|---|---|
| audio silence | under a bar | the score service listens to the team's timeline itself and cuts every part when it sees `stop.all` (piece 3) |
| the 36 bots stopped | 2–12 s | the urgent task per chair, as measured above |
