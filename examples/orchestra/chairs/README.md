# The chairs

36 bots in the team `orchestra`, one per chair, each on its own node of the [stage](../stage/stage.sh), each with the [score service](../score-service/README.md)'s four tools for its own part.

| | |
|---|---|
| [`chair.json`](chair.json) | the template: mission, level `act-within-policy`, the four tools, the team, the subscriptions, the budgets |
| [`chairs.py`](chairs.py) | `render <dir>`, `up [chair…]`, `down [chair…]`, `list` |
| [`test_chairs.py`](test_chairs.py) | the 36 definitions, no control plane |

## One chair

| Field | Value | Why |
|---|---|---|
| `id` | `vn1-1` … `hn-2` | the score service's chair: the same id names the bot, its node (`stage-vn1-1`) and its MCP URL (`/mcp/vn1-1`) |
| `mission` | «Violins I, chair 1 of 10. …» | the first sentence is the purpose the team's page shows, so the panel reads the section |
| `level` | `act-within-policy` | the four tools run without a person; there is nothing else to call |
| `allowedTools` | `enter`, `dynamics`, `stop`, `listen` | the score service's, through the node's own `mcp.json` |
| `subscriptions` | `info.cue.<family>` and `info.cue.all`, mode `message`, from `operator` | a cue reaches an idle chair as a task and a playing one as a line in its task; only the conductor's |
| `events.intakePerMinute` | 60 | past 10 a minute a delivery would become a note and the chair would miss its cue |
| `placement` | `{"nodes": ["stage-<id>"]}` | its own node: a node runs one execution at a time |
| `budgets` | 30 model calls, 30 tool calls, 600 s | one `enter`, then a `listen` of 8 bars (about 13 s at 152) at a time: about ten minutes of play |

`stop.*` needs no subscription: every member hears it, as an urgent task.

The tempo is not the chairs' business: the score service reads `info.tempo` from the bus itself, so a conductor's hand that beats 30 times a minute never fills the chairs' intake.

## Up, down, list

```bash
python3 chairs.py up            # 36 nodes started, 36 mcp.json written, 36 bots registered (or updated) and enabled
python3 chairs.py list          # by section: 10/10 1:E 2:E …; ! marks a node not Online
python3 chairs.py down          # 36 bots stopped (paused, tasks cancelled), 36 nodes stopped
```

Every bot uses the model key the stage's nodes read from `~/.config/axlr-bots/env`. Nothing is published.

## Checked

29 Oct 2026, gx10, control plane `main` 9d4d2b9, `anthropic/claude-haiku-5.5`:

| Check | Result |
|---|---|
| `chairs.py up` | 36 bots Enabled in `orchestra`, 36 nodes `stage-<id>` Online, dedicated |
| `chairs.py list`, `group get orchestra` | by section: Violins I 10/10, Violins II 8/8, Violas 6/6, Cellos 5/5, Basses 3/3, Oboes 2/2, Horns 2/2; each member's purpose «Basses, chair 1 of 3.» |
| memory, 36 idle | 350 MB of nodes |
| one cue `info.cue.all` | 36 deliveries, 36 tasks; 36 chairs playing. Entries: 1 chair at bar 1 (it started the music), 10 at bar 2, 13 at 3, 12 at 4: each chair came in when it heard the cue |
| memory, 36 playing | 36 runs, 1.6 GB more |
| then `stop.all` | the sound cut by the service; 36 urgent tasks Completed; the last 30 chairs the state lists called `stop` 5.9–9.2 s after the cut |

The rehearsal with the conductor, and the table of latencies, is piece 7.
