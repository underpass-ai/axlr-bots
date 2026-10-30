# The orchestra

36 bots, one chair each, play the first movement of Mozart's Symphony No. 25 in G minor (public domain, [why not No. 40](score-service/score/README.md)); the operator conducts with their hands, in a Meta Quest 3S. Each chair is a real AXLR bot with a mission, a policy and a budget, and a voice of its own: it plays its own line of the score on its own instrument, one phrase of eight bars at a time, and asks Jev, AXLR's external judge, which phrase to play next. The notes are Mozart's, the clock is a sequencer, the sound is a synthesizer.

> **SIMULATION.** Each chair is one LLM bot that plays its own line of the score, phrase by phrase, as Jev picks them. The conductor's hands are read by Jev from the headset's hand tracking. The notes come from the score, the sound from a synthesizer.

| Piece | Where | State |
|---|---|---|
| 1. The stage: nodes and a team, beside the control plane | [`stage/`](stage/) | done |
| 2. What `stop.all` does to a long task | [`stage/measure_stop.py`](stage/measure_stop.py) | done, [measured](#2-stopall-on-a-long-task) |
| 3. `score-service`: the parts, the clock, the synthesizer | [`score-service/`](score-service/README.md) | done: `stop.all` silences it in 0.4 s |
| 4. 36 chairs in the team `orchestra` | [`chairs/`](chairs/README.md) | done: 36 Enabled, one cue, 36 playing |
| 5. `/stage`: the view | [`score-service/static/`](score-service/README.md#the-stage) | done: 36 dots on at entry, off at the stop |
| 6b. The backup: three buttons in the desktop panel | [`panel/shortcuts.toml`](panel/shortcuts.toml) | done: Strings, Winds, Stop all on the team's page |
| 6c. Each chair plays its own line; Jev picks each phrase | [below](#6c-each-chair-plays-its-own-line-jev-picks-the-phrase) | done: two chairs measured live |
| 6. The conductor's hands: a Quest 3S, Jev reads each gesture | [below](#6-the-conductors-hands) | here: the path checked with made-up postures; the headset not yet |
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

## 6. The conductor's hands

The conductor wears a Meta Quest 3S and sees the room (passthrough). The headset's Quest Browser opens [`/hands`](score-service/static/hands.html). It reads each hand's 25 joints in every frame (WebXR hand input) and turns them into measurements: how far each finger bends, where the palm faces, where the hand is from the head. When a posture holds still for 0.3 s and differs from the last one sent, the page sends the measurements to the score service over one WebSocket ([`ws.py`](score-service/ws.py)). The service asks Jev, one choice per hand: nothing, palm up, palm down or fist. It does what the gesture means at once, and publishes a cue on the team's bus as the operator, with the events the panel's buttons already use (6b).

The orchestra stands on the room's floor around the conductor ([`room.js`](score-service/static/room.js), the hall of the podium view): one lantern per bot, lit by what the service says its bot did. What the hands sent runs across the floor as a ring. A panel floats above it with what each hand holds, Jev's reading, what was done and sent, and each posture's round trip.

```text
hands ─ Quest 3S (WebXR, 25 joints a hand) ─ /hands: bends, palm, place ─ held 0.3 s ─▶ WebSocket /ws/hands
   score-service ─▶ Jev (TypeSafe, jev-1.13.0): "which signal does the left hand give?" ─▶ nothing / open / palm down / fist
   score-service ─▶ the sound at once ─▶ POST /v1/groups/orchestra/events (a cue, as the operator) ─▶ the chairs
   score-service ─▶ WebSocket: the state as soon as it changes ─▶ the lanterns and the panel
```

| Gesture | Left hand | Right hand | Both hands |
|---|---|---|---|
| palm up, hand flat | `info.cue.strings` `{"dynamics": "f"}` | `info.cue.winds` `f` | `info.cue.all` `f` |
| palm down | `info.cue.strings` `p` | `info.cue.winds` `p` | `info.cue.all` `p` |
| fist | fade the strings out (1.5 s) | fade the winds out | fade everyone out |
| a palm up or down, raised or lowered | the strings louder or softer, a fade | the winds | — |
| anything else | nothing | nothing | nothing |

| Rule | Why |
|---|---|
| **Nothing is the default**: a hand Jev does not read as a gesture with probability 0.80 or more gives no signal; Jev failing is no signal | a hand in the air is not a command |
| A posture is read once it holds still 0.3 s and has changed; readings are at least 0.5 s apart | the two-frame rule of the design, on hand tracking instead of photos; at most two Jev calls a second |
| The service does what a gesture means before anything reaches the bus, and answers the page at once; the events are published in parallel, and the service does not apply its own echo | a publish takes 2–3 s on the control plane today; the sound and the lanterns must not wait for it |
| A fade is the service's alone: no event. A fist fades its chairs to nothing over 1.5 s; they keep their phrases and their bots go on choosing (never `done`), and the cue that names them fades them back in over 0.5 s. A flat hand's height, read ten times a second over the WebSocket without Jev, is its group's gain while Jev's last reading of that hand is a palm up or a palm down | a `stop.*` ended each bot's run (a new one took about 7 s with a model); a pause that dropped the phrases made each chair wait for its next phrase, 3–12 s, measured with 36 Jev bots; the conductor asked for fades, never a jump, and for the vertical open hand to become a flat one |
| An event that changes nothing is not sent, and the panel says why: a pause when nothing of the group plays, a cue when the group already plays at that level, the same gesture again within 3 s | a gesture held is not a command repeated |

What changed in the service for this, and why:

| Change | Why |
|---|---|
| A `stop.*` other than `stop.all` (from the panel) pauses only the chairs it is addressed to, or the section or family its name ends with; the others play on | the operator may still stop one group from the panel |
| A cue with `dynamics` while the music plays sets that level on its chairs at once, and on the phrases they already asked for | a gesture answered 12 s later, at the next phrase, is not conducting |
| Jev is called by the service itself ([`jev.py`](score-service/jev.py)), not through `axlr_judge` | `axlr_judge` takes text from a bot's run; no bot is behind the hands. The bots still ask Jev for their phrases through `axlr_judge` |
| `/ws/hands`: postures up, the state and Jev's readings down, on one connection; only a page served from loopback may open it | a request per posture over the headset's Wi-Fi was slow; no other site the headset opens may conduct |

No image leaves the headset: Jev reads the measurements as text. Image input to Jev's API exists (`TypeSafe-Danger-Zone-Images`, not production-ready) and was tried before this design with drawn frames (180–200 ms, about 760 tokens a frame); the Quest Browser gives no web page the headset's camera, so the hand joints are the input.

**Checked, 30 Oct 2026, gx10, with made-up postures** (the page's `?sim=1` buttons, marked `simulated`; Jev live, nothing published): open hand → `open` 1.00 → `info.cue.strings f` (dry run); a hand at rest → `nothing` 0.93; two fists → `fist` 0.95 and 0.90 → not sent, nothing played. Jev answered in 150–200 ms; one hand is about 600 input tokens, two about 820: **$0.000035 a reading** at $0.042 per million, at most two readings a second.

**Live in the headset, 30 Oct 2026, two chairs** (`vc-1`, `ob-1`; the other 34 paused): real hands read as gestures (a fist 0.88–0.99, a palm down 0.94–0.99); relaxed hands read below 0.80 and gave nothing. In the first version, the strings' fist took about 7 s to silence: five stops were published one after another, and the sound waited for the bus's echo. Applying the gesture in the service, the parallel publish and the WebSocket fixed it. The bots still answer a cue seconds later: a publish takes 2–3 s, and a model's run about 7 s to come in.

**Orchestra 9, 30 Oct 2026: the section the conductor looks at, and the headset's own gestures.** The page keeps a selection: the instrument the head points at (300 ms on it), locked with a short pinch and freed with another, or moved with a thumb swipe left or right. A gesture acts on it; with nothing selected, on the hand's old group (left strings, right winds); both hands the same gesture, on everyone. The headset reads its own gestures (the `oculus-hand` gamepad: button 0 the pinch, 5–9 the thumb's microgestures since Quest Browser 38.1) and the page sends them to the service as `native` messages, no Jev: a pinch held and moved is the section's gain from where it was (`pinchSpan` metres for the whole range), a thumb tap its entrance (a cue with no level, while the music plays and the section is locked), a swipe forward or backward the tempo by `tempoStep`. A pointing hand, index out, is a baton: the page measures its beats ([`posture.js`](score-service/static/posture.js) `Beats`: three even intervals) and the service sets the clock at once; `info.tempo` reaches the bus at most every `tempoPublishSeconds`, since a publish holds the control plane 2–3 s. Jev reads two more postures: a finger on the lips, pianissimo for everyone (`info.cue.all p` and every gain to `hush.gain`), and the pointing hand, which is no signal by itself. Only a flat hand's height conducts a gain: a hand that pinches, points or closes moves nothing that way. A pinch while looking at the palm is the system's (the left hand ends the session): pinch low. Around the orchestra, a zen night ([`zen.js`](score-service/static/zen.js)): one giant tree over a luminous meadow under the stars.

**Orchestra 10, 30 Oct 2026: a mirror lake under the aurora.** The default world ([`aurora.js`](score-service/static/aurora.js)): the conductor and the 36 lanterns stand on still water at night. One GLSL function draws the sky (a deep gradient, stars and a faint milky way, a low moon, two ridges of mountains in haze, and the aurora, three curtains ahead and to the left) and the water draws the same function on the reflected ray, so the reflection is exact and costs no second render: no shadows, no post-processing, five draw calls, a Quest 3S budget. The world listens to the orchestra: the aurora burns with how many chairs play and pulses on the beat, the water's swell grows with the music, each lantern has its reflection (a streak of its own colour and brightness) and a flame at its top, a cue runs across the water as a ring, and `stop.all` stills the lake and dims the aurora in a second. The aurora and the moon stay ahead-left and ahead-right of the podium wherever the conductor entered looking. `?world=zen|meadow|room` keeps the other worlds.

### Run it on the headset

```bash
adb connect 192.168.1.86:5555               # the Quest over Wi-Fi (adb tcpip 5555 once on USB); accept "Always allow" in the headset
adb reverse tcp:8790 tcp:8790               # the headset's localhost:8790 is gx10's score service: no port opened on the LAN
```

In the headset's Quest Browser: `http://localhost:8790/hands`, then look ahead and press *Enter*: the orchestra is placed ahead of where you look. `?scale=` sizes it to the room (default 0.6: the front row 1.5 m away). The page plays the orchestra's sound (`/audio.wav`) in the headset when its box is ticked, so the headset's own recording has the sound. `server.py --hands-dry-run` reads the hands without publishing. On gx10, `http://127.0.0.1:8790/hands?sim=1&view=1` tries the path and shows the hall without the headset. A sleeping headset drops the Wi-Fi link: the page says "Failed to fetch" until `adb connect` and `adb reverse` again.

## 7. Jev bots, and a bus that pushes

Measured on 30 Oct 2026 with the AXLR chairs: a cue took 1.9–3.1 s to publish and a chair heard `stop.ob` 7 s before its `stop` call (the assignment, AXLR's start, Haiku through OpenRouter). Two changes:

- **The bus pushes** (PR #71): the control plane wakes the nodes' long polls as it records a change and pushes a cancel or a message to the node that runs the execution over a stream it holds open, instead of the node asking every 250 ms and renewing every 10 s. The event log is indexed; listings skip the files that did not change. On a copy of gx10's state with the 36 chairs: `info.cue.all` published in 313 ms and reached the last of 36 nodes in 760 ms (12.98 s and 16.80 s before); `stop.all` 230 ms and 656 ms (9.16 s and 12.87 s).
- **Jev bots** (this PR): the chairs as `runtime.kind: jev` ([the chairs](chairs/README.md#jev-bots-the-same-chairs-without-a-language-model)). No model, no OpenRouter: the node runs `phrases` → Jev → `play` → `listen` itself, each decision one call to Jev (~0.2 s), and `stop` once on `stop.*`. Cost per phrase: Jev's input tokens at $0.042 per million, about $0.0001.
- **Bot → bot, and the bus port** (PR 3): a Jev chair tells `info.chair.played` after each phrase (the program's `tell`) and every chair hears its teammates' (`info.chair.*`, mode `message`), through the control plane's rules; the pushes reach the nodes through the bus port, over the direct stream or a NATS broker ([bus port](../../docs/architecture/bus-port.md), `nats.conf` here for the broker's permissions). Measured bot → bot: see the PR.
