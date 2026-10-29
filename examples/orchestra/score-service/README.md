# score-service

The orchestra's world: the score by parts, the clock, what each chair asked for, the synthesizer and the sound. Each chair's bot reaches it as four MCP tools on loopback; the operator reaches it only through the team's bus.

> SIMULATION. Each chair is one LLM bot. The notes come from a score, the sound from a synthesizer; the bots decide only when to enter, how loud, and when to stop.

```text
chair's node ── AXLR ── POST /mcp/vn1-3 ──▶ score-service ──▶ FluidSynth (in memory) ──▶ GET /audio.wav ──▶ OBS / ffplay
                         enter · dynamics · stop · listen     │ clock, parts, chairs
operator ─ POST /v1/groups/orchestra/events ─▶ control plane ─┘ the service long-polls the team's timeline:
                                                                 stop.all → silence; info.tempo {bpm} → the clock
/stage ────────────────── GET /api/state?version=N&wait=20 ────▶ take, bar, tempo, each chair, the facts, the last bus event
```

| File | What it is |
|---|---|
| [`score.py`](score.py) | the MIDI read into one part per section: notes in ticks, the bars, the tempo |
| [`orchestra.py`](orchestra.py) | the clock and the chairs: `enter`, `dynamics`, `stop`, `listen`, `stop_all`, `set_tempo`, `render` |
| [`synth.py`](synth.py) | FluidSynth rendering into memory, the live WAV stream, the render loop paced by the wall clock |
| [`bus.py`](bus.py) | the service's own subscription: the team's timeline, the operator's `stop.all` and `info.tempo` |
| [`server.py`](server.py) | HTTP on loopback: `/mcp/<chair>`, `/api/state`, `/api/score`, `/audio.wav`, `/stage` |
| [`static/`](static/) | the stage page: `stage.html`, `stage.js`, `stage.css`, Lexend and DotGothic16 (OFL, licences beside them) |
| [`mcp_http.py`](mcp_http.py) | the MCP tool table, copied from [the fleet](../../drone-fleet/app/mcp_http.py) as the [adapter template](../../world-adapter/README.md) says |
| [`check_take.py`](check_take.py) | piece 3's check by hand: two parts, a `stop.all`, how long the sound took to go |
| [`config.json`](config.json) | the sections and their chairs, the dynamics, the tempo range, the soundfonts, the team, the control plane |
| [`score/`](score/README.md) | the MIDI, its source and its licence |

## The music

Mozart, Symphony No. 25 in G minor, K. 183, first movement: from the [Mutopia Project](https://www.mutopiaproject.org/cgibin/piece-info.cgi?id=1380), **public domain** ([source and licence](score/README.md)). The plan named No. 40; Mutopia has no full score of it, and the operator chose No. 25.

| Section | Chairs | Part (track) | Sound (GM program) |
|---|---|---|---|
| Violins I | 10 | `eight` | 40 violin |
| Violins II | 8 | `nine` | 40 violin |
| Violas | 6 | `ten` | 41 viola |
| Cellos | 5 | `eleven` | 42 cello |
| Basses | 3 | `eleven`, an octave down | 43 contrabass |
| Oboes | 2 | `one` | 68 oboe |
| Horns | 2 | `two` + `three` | 60 French horn |
| **36** | | | |

A section is one part rendered once. It sounds while at least one of its chairs plays, and its amplitude is the chairs' mean dynamic (`p` 0.45, `mf` 0.7, `f` 1.0) times the square root of the share playing: one violin of ten is 10 dB under the ten; eight down to seven is 0.6 dB, a step you hear in a quiet passage more than in a loud one.

## The tools, as a chair sees them

The chair is the URL: each chair's node names `http://127.0.0.1:8790/mcp/<chair>` in its own `mcp.json` ([`stage.sh mcp`](../stage/stage.sh)), so a chair can only speak for itself.

| Tool | Arguments | Does |
|---|---|---|
| `enter` | `bar?`, `dynamics?` (`p`, `mf`, `f`) | the part sounds from the start of that bar; without `bar`, or a bar already past, the next bar. When nothing plays, the entry starts the music at bar 1 (a new take). Answers the bar, and how many seconds until it |
| `dynamics` | `level` | softer or louder from now on |
| `stop` | — | this chair stops at once; the section goes on with the others |
| `listen` | `bars?` (8) | blocks until the bars pass, the chair is stopped or the music stops, at most 20 s; answers the bar, the tempo, its state and its section's (`Violins I: 7 of 10 playing`) |

A refusal (an unknown chair, a bar past the end, `fff`) is a tool error the model reads, never an exception.

There is no stop button: the only way to stop everyone is the control plane's `stop.all` on the team. The service hears it on the bus and silences every part at once (all sound off, no release); the bots hear it as an urgent task each and call `stop` seconds later ([piece 2](../README.md#2-stopall-on-a-long-task)).

## Run

FluidSynth's library (`libfluidsynth.so.3`) and a General MIDI soundfont are system packages; the Python side is a venv:

```bash
python3 -m venv ~/.local/share/axlr-stage/venv
~/.local/share/axlr-stage/venv/bin/pip install -r requirements.txt
~/.local/share/axlr-stage/venv/bin/python server.py --control-plane https://127.0.0.1:7879 --team orchestra --record take.wav
ffplay -nodisp http://127.0.0.1:8790/audio.wav       # or OBS: Media Source, input http://127.0.0.1:8790/audio.wav
```

| Soundfont | Licence | Package |
|---|---|---|
| `FluidR3_GM.sf2`, used when present | MIT | `fluid-soundfont-gm` |
| `TimGM6mb.sf2`, the fallback | GPL-2 | `timgm6mb-soundfont` |

The soundfont renders the sound; it is not shipped here. It sounds like a General MIDI synthesizer, and the page says so.

## Check

```bash
~/.local/share/axlr-stage/venv/bin/python -m unittest -v test_score_service    # no synthesizer, no network
python3 check_take.py --team stage --wav take.wav                                # with the service running and following team stage
```

`check_take.py`, three runs on gx10 (29 Oct 2026, TimGM6mb, 152 bpm, one bar 1.58 s): violin I chair 1 enters at bar 1, oboe chair 1 at the next bar three seconds later, both sound together on the one clock; the operator's `stop.all` at bar 10:

| Run | event → cut (bus) | cut → under −60 dBFS | total | under one bar |
|---|---|---|---|---|
| 1 | 0.33 s | ≤ 0.1 s | ≤ 0.43 s | yes |
| 2 | 0.30 s | ≤ 0.1 s | ≤ 0.40 s | yes |
| 3 | 0.30 s | ≤ 0.1 s | ≤ 0.40 s | yes |

The cut is measured from the event's `createdAt` on the control plane to the service's cut on the same host's clock; the sound in the WAV is read from the cut's frame. That is the stop's latency, not the audio path's: OBS adds its own buffer, labelled apart. All 36 chairs at `f` peak at −1.8 dBFS, unclipped.

## The stage

`http://127.0.0.1:8790/stage` on the host the service runs on: one SVG, nothing from the network.

| Part | What it shows | From |
|---|---|---|
| the chairs | 36 dots in their sections around the podium: an outline waiting, dashed when an entry is scheduled, copper while playing (fainter at `p`), grey once stopped; each section's count playing | each chair's state in `/api/state`: what the chair's own `enter` and `stop` did, never a guess |
| the score strip | each part's notes per bar over 32 bars, copper for a section that sounds, the playhead | `/api/score`; the playhead moves on at the service's tempo between two answers and snaps back at each |
| the bus | the operator's last event, as published: name, message, data | the service's own subscription |
| the cut | «silence 0.14 s after stop.all from operator, at bar 40»: the sound's clock; the bots stop on their own | the service's cut |
| the ribbon | SIMULATION, always, and the label under the stage | `label` in the config |

`?hud=0` keeps the stage, the strip and the ribbon only, for a recording with OBS; `?theme=dark` or `light`, else the system's.

![The stage while it plays: the strings and the winds in, the winds' cue on the bus as published](../../../docs/assets/orchestra/stage-playing.jpg)

![The same stage after stop.all: every chair off, the silence 0.14 s after the event](../../../docs/assets/orchestra/stage-stopped.jpg)

Checked on gx10, 29 Oct 2026: 24 chairs entered by hand lit at once; `info.cue.winds` published on the bus lit the four winds as their bots entered; `stop.all` turned every chair grey and the page showed the silence at 0.14 s.

