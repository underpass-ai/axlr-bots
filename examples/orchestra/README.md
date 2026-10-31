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
| 11. The conductor's expression: Jev reads how the hands move; the voices darker or brighter | [below](#6-the-conductors-hands) | spike: measured on the synthesizer and with Jev on made-up motion; the headset not yet |
| 12. A level kept with a double pinch; the section looked at conducts | [below](#6-the-conductors-hands) | done: a flat hand's level is provisional until pinched twice; entrance and expression go where the conductor looks; the headset not yet |
| 13. A whole movement in one task: the chairs' budgets | [`chairs/`](chairs/README.md) | done: 36 live chairs updated in place on 31 Oct 2026 |
| 13b. A tap is a tap, a relaxed hand still conducts, the expression on the conductor's own scale, a sticky gaze | [below](#6-the-conductors-hands) | done: from the headset log of 31 Oct; tested, the headset not yet |
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

**Orchestra 10b, 31 Oct 2026: a rave in an abandoned factory** ([`rave.js`](score-service/static/rave.js), `?world=rave`), for an electronic take on the piece, 2000s style: a concrete hall with columns, trusses and moonlight through holes in the roof; over the stage a truss of eight moving heads whose cones sweep the crowd and the band through the haze; from the back, green laser sheets and a cage of sixteen beams; a VJ screen behind the orchestra (a tunnel of squares, the seven sections as an equalizer, the tempo and the bar in DotGothic16) mirrored in the puddles of the wet floor; two speaker stacks that pump on the beat; a strobe once a bar, on its first beat, at a third of white and never faster; a crowd of 150 silhouettes behind the DJ bobbing on the beat with glow sticks. `stop.all` is a blackout: lights, screen, lasers and the dancing stop in a second and only the exit signs stay. The world is built in the hall's frame and follows it, so the stage is ahead of wherever the conductor entered looking. About fourteen draw calls, no shadows, no post-processing. The preview takes `?yaw=180` to look back at the crowd.

**Orchestra 11, 31 Oct 2026: the conductor's expression, a spike.** First, what was already there: each chair sees only its own line (the section's track, or Oboe I/II and Horns I–II/III–IV apart), cut into 8-bar phrases; `phrases`/`listen` offer it the next phrases of that line with their timing, and Jev picks one; the cues (`info.cue.<family|section|all>`) say when to come in. So a chair knows what to play and when. There is no book: one movement, one line per chair, nothing to choose a part from. On that, the expression: the page sums up how each hand moved over the last 2.5 s ([`posture.js`](score-service/static/posture.js) `Motion`: speed and peaks, how far it rose and swept, how often it turned from rising to falling, how high, how open) and sends the sums every 2 s while a hand moves (`motion`); the service puts them to Jev as text with the scale of a conductor's motion, one choice: dolce, cantabile, brillante, appassionato or nothing ([`hands.py`](score-service/hands.py) `motion`). A character read at `expression.threshold` (0.55) or more, while the music plays (an event reaches an idle chair as a task, and a Jev chair's task starts the music: hands warming up before the downbeat cue nobody), goes on the locked section, else everyone, at once ([`orchestra.py`](score-service/orchestra.py) `character`): the voices' timbre, never their level (the cues') nor their gain (the hands'): the velocity the notes are struck at (every note of the score is written at 127), the filter cutoff of the voices (`fluid_synth_set_gen`, an offset in cents) and the vibrato wheel. It is published as `info.expression {"character"}` when it changes, at most every 6 s; both chair templates subscribe, and a Jev chair passes the last character it heard into its next `play` (`pass: ["dynamics", "character"]`), so the bots ask for what the conductor showed; `play` takes `character`, and Jev's phrase question says the conductor's expression. No camera: the Quest Browser gives the page no image, so the expression is read from the hands, as an orchestra reads a conductor.

Measured, 31 Oct 2026, gx10, TimGM6mb, six seconds of each line's first phrase, spectral centroid against the plain voice: velocity 90 is −6 dB and the same centroid (+0.2 %: on this soundfont velocity is loudness only); cutoff −2400 cents takes the violins' centroid from 2480 to 1629 Hz (−34 %), the violas' −31 %, the cellos' −40 %, the oboes' −9 %; +1200 cents adds 15 % on the violins, 25 % on the cellos, nothing on the oboes (their filter is open); the vibrato wheel at 80 adds 8–14 % on the strings. So dolce is velocity ×0.8, −2400 cents and vibrato 20; cantabile vibrato 35; brillante +1200 cents and vibrato 45; appassionato +1200 cents and vibrato 80. Jev live (`jev-1.13.0`, dry run) on made-up sums written to the scale: dolce 0.94, cantabile 0.99, brillante 0.99, appassionato 0.99, a still hand nothing 0.99, a steady baton cantabile 0.85; 165–190 ms, about 775 input tokens, $0.00003 a reading; without the scale sentence the same sums read 0.50–0.62. Unmeasured: real conducting motion in the headset, and whether a change is heard as such over 36 chairs. `?sim=1` has four buttons that send the made-up sums.

**Orchestra 12, 31 Oct 2026: a level kept with a double pinch; the section looked at conducts.** Live in the headset (31 Oct, 36 chairs, about ten minutes), the conductor's sections kept turning down by themselves: after a palm up or down the flat hand's height was the group's gain, absolute (−55 cm → 0, +15 cm → 1), until Jev read another gesture, so an arm lowered to rest took its section down with it (the take ended with every section between 0.44 and 0.89), and a level set with a pinch held was overwritten as soon as the hand opened. Now the height is a **provisional level**: from where the hand began when Jev read it, over `height.span` (0.7 m), heard at once and shown on the panel as waiting. A **double pinch** of either hand keeps it (`pinch-double`, a native gesture: two taps within 350 ms; a tap alone still locks or frees the section, after those 350 ms). Not kept, it comes back where it was when the hand drops faster than `height.dropSpeed` (0.8 m/s), when no height arrives for `height.silentSeconds` (0.6 s: the hand closed, pointed or left; a pinching hand still reports its height, marked, so the double pinch itself does not count as leaving), when it holds still unconfirmed for `height.pendingSeconds` (5 s), or when Jev reads the hand as something else. Kept or back, the hand is free: it conducts a level again when Jev next reads a palm up or down, from the level then. A pinch held takes the level over from the flat hand (no jump, nothing comes back) and keeps it when released. The **section the conductor looks at** (300 ms on it, no pinch) is the target for everything now: the entrance by thumb tap and the expression too, which went to everyone unless a section was locked; the lock only keeps a section while the head turns. `server.py --hands-log FILE` appends every reading, native gesture, motion and level kept or reverted as one JSON line each: the session, for reading afterwards (the service keeps 200 records in memory and shows 12). Also seen in that session's bus events: 36 chair tasks failed, 27 at the 128-tool-call budget and 9 at the deadline; a chair comes back with the next cue, but falls silent in between: the budget is to be raised, apart from this.

**Orchestra 13b, 31 Oct 2026: a tap is a tap, a relaxed hand still conducts, the expression on the conductor's own scale, a sticky gaze.** Read from the headset session of 31 Oct (`--hands-log`, 832 lines, 5.5 min). *Tap or hold*: no double pinch arrived in all the session, although tried many times; of 61 pinches the page took for holds, 53 lasted under 0.5 s (median 0.09 s): the hand-tracking jumps more than the page's 3 cm tap rule as the fingers meet, so a quick pinch became a hold, and 11 of them moved a level by 15 % or more in under 0.3 s (the oboes 1.00 → 0.46 in 6 ms). Now a pinch is a hold only once it has lasted 350 ms, whatever the hand did before, and the hold starts from the hand's height then; released earlier it is a tap (one locks or frees the section, two within 350 ms keep a level); a move more than 15 cm from the last one sent is the tracking's and skipped, unless the next sample agrees ([`posture.js`](score-service/static/posture.js) `Pinch`, tested in node). *The hand changed*: 25 levels came back because Jev re-read the flat hand as `nothing` (0.5–0.9). Now `nothing`, or a reading under the threshold, keeps the hand conducting; only another gesture read at the threshold (a fist, a pointing hand, a finger on the lips, the other level gesture) brings the level back; the same gesture read while looking at another section keeps the level with the section it began on. *The hand left*: 22 levels came back with the hand still in view: the page sent heights only while all four fingers were straight, and 0.6 s of silence is shorter than a double pinch. Now the page sends them while the four fingers are not curled (straight or half bent: `uncurled`), and `height.silentSeconds` is 1.5 s; the drop rule (0.8 m/s down) stays, and caught 18, mostly the arm going to rest. *Expression*: Jev read appassionato 94 times in 123 at hand speeds of 0.25–2.46 m/s (median 0.69), against a scale that said fiery over 1.2 m/s, because conducting always beats: peaks are 3–8 times the average, which the scale called accents. The conductor's real ranges, over 266 sums: speed q25 0.16, median 0.41, q75 0.69, q90 0.98 m/s; rise median 0.23, q75 0.55 m; turns 2.0 per 2.5 s; openness 0.60. The scale now says those ranges, dolce under ~0.2 m/s, cantabile ~0.2–0.5, brillante ~0.5–0.9 light with small rises, appassionato over ~0.9 with rises over ~0.5 m, and that beat peaks are the beat in every character. The character changed 23 times in 5 min and the conductor did not hear it: a character is applied only when two readings in a row agree (`expression.agreeReads`, 2), and the characters are stronger: dolce velocity ×0.65, −3600 cents, brillante +1800 cents, appassionato +2400 cents and vibrato 90. Measured on TimGM6mb (notes held 2 s, spectral centroid against the plain voice; the old values in brackets): dolce −51 % on the violins (−35 %), −49 % violas (−37 %), −35 % cellos (−35 %), −14 % oboes (−6 %), −24 % horns (−18 %), and 8–11 dB softer (4–5: the velocity also closes the filter); brillante +44 % violins (+36 %), +46 % violas, +80 % cellos (+57 %), +14 % horns; appassionato +40 %, +45 %, +97 %, +15 %; the oboes' filter is open, so only dolce changes them. Whether a change is now heard over 36 chairs is for the headset to say. *Gaze*: the section looked at is taken after 200 ms (300), and once one is, another takes over only when the head points 4° inside its slice ([`posture.js`](score-service/static/posture.js) `gazeOn`), so the narrow slices do not flicker; a lock still freezes it. The slices: violins 72° (108–180), oboes 18° (90–108), horns 18° (72–90), violas 22° (50–72), cellos 22° (28–50), basses 28° (0–28). Widening the oboes and horns is not worth it now: with the 4° margin, the zone that takes them is 94–104° and 76–86°, which is where they sit (`seating`: 95–105° and 75–85°), and both slices already overlap their neighbours' seats (the second violins sit down to 103°, the violas up to 77°); wider, they would take those. The next headset log says whether that is enough. 4 node tests and 5 service tests new; 122 service, 12 chairs, 15 node, green.

### Run it on the headset

On the cluster ([`examples/helm/orchestra`](../helm/orchestra/README.md)) the headset's Quest Browser opens `https://<score.host>/hands` (and `https://<rave.host>/hands`) on the Wi-Fi: HTTPS with a Let's Encrypt certificate is a secure context, so WebXR needs no `adb reverse`. The services answer only their own names (`--public-host`, `--internal-host`): the public name gets the page, the sound and `/ws/hands`, never the chairs' MCP. On one host, as before:

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

## 13. A whole movement in one task

31 Oct 2026, the first whole movement with the conductor in the headset, 36 Jev chairs, about ten minutes: every chair fell silent before the end. The team's events (`GET /v1/groups/orchestra/events`) hold 36 `bot.task.failed` for that take, 27 `budget_exceeded: 128 tool calls` (or 129: `play` and `listen` are counted as they return), 9 `interrupted: context deadline exceeded`; a chair ran 600 s to the second (va-3: started 20:12:30Z, failed 20:22:30Z, 98 tool calls, 48 Jev calls), another spent its 129 calls in 9 min (va-6, 43 Jev calls). The budgets in [`chairs/chair-jev.json`](chairs/chair-jev.json) and [`chair.json`](chairs/chair.json), 64 model calls, 128 (or 64) tool calls and `taskTimeoutSeconds` 600, were sized on 29 Oct for one take of 20 phrases, 160 bars, four minutes; the node applies them per execution (the Jev runtime counts each tool call and each question to Jev, and runs under a context of `taskTimeoutSeconds`), and a chair's task is now the whole movement: 414 bars at 152 bpm are 10.9 min, 52 phrases of 8 bars, `phrases` once and then `play`, `listen` and often a second `listen` each phrase (a phrase of 8 bars at 152 bpm is 12.6 s, `listen` waits 20 s at most; slower, two), one question to Jev each phrase. The chair came back on the next cue and was silent in between.

Now 400 tool calls, 128 Jev calls (`maxModelCallsPerTask`, the Jev chair's only cost, about $0.0001 a phrase: 48 of 64 were spent in the 600 s, so a whole movement would have hit it next) and 1,800 s, in both templates; the node's AXLR `turn_tool_calls` ([`stage/stage.sh`](stage/stage.sh) `settings`) 400 to match for a model chair, whose model calls are 160 (three a phrase). The deadline is sized from the slowest tempo the conductor's swipe may ask for (`tempo.min` 60 bpm in [`score-service/config.json`](score-service/config.json): 414 bars and the 8-bar count-in are 28 min), not from 152: it is the end of a chair the world never ends, not of the music. [`chairs/test_chairs.py`](chairs/test_chairs.py) derives these bounds from the config and the movement's 414 bars, for both templates. The 36 live chairs were updated in place with `ORCHESTRA_RUNTIME=jev python3 chairs.py up` (`PATCH /v1/bots/<id>`, nothing restarted) while the orchestra played: a running execution keeps the budgets it was assigned, so that take ended as the one before (again 27 at the tool calls, 9 at the deadline, 20:52:30–20:53:38Z) and the next cue takes the new budgets; at the same time they got the current template, the subscription to `info.expression` and `character` in `play` ([piece 11](#6-the-conductors-hands)), which the chairs registered on 30 Oct did not have. Seen in the same hour and not touched: 18 `Jev did not answer 3 times: the ask offers 1 options; Jev chooses among 2 to 16`, a chair asked to choose when the score service offered one candidate phrase, a question for the service, not the chairs.
