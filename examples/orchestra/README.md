# The orchestra

36 bots, one chair each, play the first movement of Mozart's Symphony No. 25 in G minor (public domain, [why not No. 40](score-service/score/README.md)); the operator conducts with a hand in front of a webcam. Each chair is a real AXLR bot with a mission, a policy and a budget, and a voice of its own: it plays its own line of the score on its own instrument, one phrase of eight bars at a time, and asks Jev, AXLR's external judge, which phrase to play next. The notes are Mozart's, the clock is a sequencer, the sound is a synthesizer.

> **SIMULATION.** Each chair is one LLM bot that plays its own line of the score, phrase by phrase, as Jev picks them. The notes come from the score, the sound from a synthesizer.

| Piece | Where | State |
|---|---|---|
| 1. The stage: nodes and a team, beside the control plane | [`stage/`](stage/) | done |
| 2. What `stop.all` does to a long task | [`stage/measure_stop.py`](stage/measure_stop.py) | done, [measured](#2-stopall-on-a-long-task) |
| 3. `score-service`: the parts, the clock, the synthesizer | [`score-service/`](score-service/README.md) | done: `stop.all` silences it in 0.4 s |
| 4. 36 chairs in the team `orchestra` | [`chairs/`](chairs/README.md) | done: 36 Enabled, one cue, 36 playing |
| 5. `/stage`: the view | [`score-service/static/`](score-service/README.md#the-stage) | done: 36 dots on at entry, off at the stop |
| 6b. The backup: three buttons in the desktop panel | [`panel/shortcuts.toml`](panel/shortcuts.toml) | done: Strings, Winds, Stop all on the team's page |
| 6c. Each chair plays its own line; Jev picks each phrase | [below](#6c-each-chair-plays-its-own-line-jev-picks-the-phrase) | here: two chairs measured live |
| 6. `conductor-cam` | `conductor-cam/` | |
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

## 6c. Each chair plays its own line; Jev picks the phrase

Until 6b a section was rendered once and its chairs only gated it: one violin of ten made the whole section's part sound. Now every chair sounds by itself, and only what its bot asked for:

| | Before | Now |
|---|---|---|
| Sound | one synthesizer channel per section, its volume from the share of chairs playing | one channel per chair: its instrument, its pan on the stage, its own few cents of tuning and milliseconds of onset |
| What a chair plays | the section's part, while the chair is "in" | its line (strings in unison; Oboe I and II the upper and lower note of the track's chords; Horns I–II and III–IV a track each), phrase by phrase |
| Tools | `enter`, `dynamics`, `stop`, `listen` | `phrases`, `play`, `listen`, `stop`, and AXLR's `axlr_judge` |
| Who picks the phrase | nobody: the whole part | Jev, at each fork, among the next phrases of the chair's line; the bot plays its pick |
| Who starts the clock | the first `enter` | the conductor's cue, with an 8-bar count-in (12.6 s) for the chairs to choose their first phrase |
| After `stop.all` | an `enter` started a new take | silence until the next cue: a `play` is refused |

A phrase is 8 bars (12.6 s at 152), the same bars in every line: 52 phrases. A chair's fork opens when its last phrase begins (0 to 7 beats later by chair, so 36 chairs do not ask at once): `listen` returns with the candidates (the next three, or the one begun if two bars are left) and `ask`, the question for Jev, which the bot passes to `axlr_judge` as it is, then plays Jev's choice with `play(phrase, dynamics, judge)` while the current phrase sounds. The service never sees Jev: `/api/state` shows each chair's phrase now and next as the service saw them, and Jev's pick and probability as the bot reported them, each with its source; [`chairs/measure_take.py`](chairs/measure_take.py) checks those reports against Jev's answers in the bots' own AXLR sessions.

**Measured, 30 Oct 2026, gx10:** two chairs, Oboe I (`ob-1`) and the first cello (`vc-1`), `anthropic/claude-haiku-5.5`, `jev-1.13.0`, cue `info.cue.all`, `stop.all` at bar 42. The recording is not in the repo (gx10, `~/Documents/ai/artifacts/viral/orchestra-phrases/orquesta-2-sillas-oboe-chelo.wav`, 81 s): the count-in's silence, then the oboe a little left and high, the cello right and low, until the cut.

| Chair | Phrases played | On time (bars 1–40) | Chose ahead of the phrase | Jev calls | Jev latency, median/max | Reported = Jev's answer | Model requests | Cost |
|---|---|---|---|---|---|---|---|---|
| `ob-1` | 1, 2, 3, 4, 5, 6 | 5 of 5 | 5.1 s (the first), then 8.0–8.6 s | 7 | 140/169 ms | 6 of 6 | 18 | $0.0073 |
| `vc-1` | 1, 2, 3, 4, 5, 6 | 5 of 5 | 6.5 s, then 8.0–8.8 s | 7 | 141/177 ms | 6 of 6 | 18 | $0.0072 |

Jev's probability for the phrase it picked: 0.69–0.75 for the first (in the count-in), 0.85–0.99 after. The seventh call is the fork for phrase 7, which `stop.all` cut. Per chair and phrase: about 2.3 model requests, 1.1 Jev calls, $0.0011. The silence came 1.06 s after `stop.all` (0.39 s in the take before): under a bar.

The first take showed two faults, fixed before this one: each bot spent four model calls searching for its tools (5.5 s; the first entry came 1–2 bars late with a 6-bar count-in), and a run still alive after `stop.all` asked for phrase 7 and its `play` started the music again.

**36 chairs, the video's first 40 bars** (this take, 18 times over; an estimate, not measured): about 250 Jev calls, 650 model requests, **$0.24 of model** for the playing and $0.02 for the 36 stops. At the fork's spread, up to about 11 Jev calls a second for 3 s in each phrase. What two chairs cannot show: Jev's and the model's latency with 36 asking at once; the margin is the 8 s a chair had left before each phrase.
