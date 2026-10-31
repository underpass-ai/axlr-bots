# The chairs

36 bots in the team `orchestra`, one per chair, each on its own node of the [stage](../stage/stage.sh), each with the [score service](../score-service/README.md)'s four tools for its own line and Jev to choose each phrase: as AXLR bots on Haiku that call `axlr_judge` ([`chair.json`](chair.json)), or as **Jev bots** with no language model, whose node runs the fixed program itself ([`chair-jev.json`](chair-jev.json), `ORCHESTRA_RUNTIME=jev`).

| | |
|---|---|
| [`chair.json`](chair.json) | the template: mission, level `act-within-policy`, the four tools and `axlr_judge`, the team, the subscriptions, the budgets |
| [`chair-jev.json`](chair-jev.json) | the same chair as a Jev bot: `runtime.kind: jev`, the program `phrases` → Jev → `play` → `listen`, `stop` on `stop.*`; the four tools, no model ([bot definition](../../../docs/reference/bot-definition.md#runtime)) |
| [`chairs.py`](chairs.py) | `render <dir>`, `up [chair…]`, `down [chair…]`, `list` |
| [`measure_take.py`](measure_take.py) | one take measured: the cue, the phrases, Jev's calls and latency, what the bots reported against Jev's answers, the cost, the estimate for 36 |
| [`test_chairs.py`](test_chairs.py) | the 36 definitions, no control plane |

## One chair

| Field | Value | Why |
|---|---|---|
| `id` | `vn1-1` … `hn-2` | the score service's chair: the same id names the bot, its node (`stage-vn1-1`) and its MCP URL (`/mcp/vn1-1`) |
| `mission` | «Violins I, chair 1 of 10. …» | the first sentence is the purpose the team's page shows, so the panel reads the section. Then the loop: on a cue `phrases`; each `ask` to `axlr_judge` as given; `play` Jev's choice with what Jev answered as `judge`; `listen`; never a phrase Jev did not pick. It names the tools so the bot calls them with `axlr_call_tool` without searching first (searching cost four model calls and 5.5 s, and the first entry, on 30 Oct) |
| `level` | `act-within-policy` | the tools run without a person; there is nothing else to call |
| `allowedTools` | `phrases`, `play`, `listen`, `stop`, `axlr_judge` | the score service's, through the node's own `mcp.json`, and AXLR's Jev tool: with an allow list AXLR offers no tool the list leaves out |
| `subscriptions` | `info.cue.<family>`, `info.cue.<section>` and `info.cue.all`, mode `message`, from `operator` | a cue reaches an idle chair as a task and a playing one as a line in its task; only the conductor's. `info.cue.vn1` cues one section |
| `events.intakePerMinute` | 60 | past 10 a minute a delivery would become a note and the chair would miss its cue |
| `placement` | `{"nodes": ["stage-<id>"]}` | its own node: a node runs one execution at a time |
| `model.maxContextTokens` | 64,000 | a phrase adds about 1,300 tokens (the question for Jev twice, the answers) |
| `budgets` | 400 tool calls, 160 model calls (a Jev chair: 128 Jev calls), 1,800 s | one cue, one task, the whole movement: 414 bars are 52 phrases of 8; `phrases` at the cue, then `play`, `listen` and a second `listen` when a slow phrase outlasts `listen.maxSeconds`, about three calls a phrase; a Jev chair asks Jev once a phrase, a model chair spends a model call on each tool call. 1,800 s covers the movement and the count-in at the slowest tempo the conductor may swipe to (60 bpm: 28 min). Until 31 Oct 2026 the budgets were 64 model calls, 128 (Jev) or 64 tool calls and 600 s, sized for one take of 20 phrases: in the first whole movement every chair fell silent before the end, 27 at `budget_exceeded: 128/129 tool calls`, 9 at `interrupted: context deadline exceeded` ([the orchestra's README](../README.md#13-a-whole-movement-in-one-task)) |

Each node's AXLR reads its own `settings.json` (`stage.sh settings <chair>`, which `chairs.py up` runs): `jev.tool` on, `jev-1.13.0`, `final_check` off, and `turn_tool_calls` 400, since a whole movement is one turn (128 until 31 Oct 2026, with the budget). Jev's key is `TYPESAFE_API_KEY` in `~/.config/axlr-bots/env`, which the node's shell reads with the model key; nothing prints it. Jev sees only the question the chair sends: the state the score service wrote, the question and the options.

`stop.*` needs no subscription: every member hears it, as an urgent task.

Both templates also subscribe to `info.expression` (the conductor's expression, read by Jev from how the hands move: `dolce`, `cantabile`, `brillante`, `appassionato`); a Jev chair passes the last one it heard into its next `play` as `character` (`pass: ["dynamics", "character"]`), a model chair is told to. The service has applied it to the voices already; the chair's `play` keeps it on the phrases it asks for.

The tempo is not the chairs' business: the score service reads `info.tempo` from the bus itself, so a conductor's hand that beats 30 times a minute never fills the chairs' intake.

## Jev bots: the same chairs without a language model

With `chair.json` the model only shuffles the tool calls: Jev already makes every musical choice. `chair-jev.json` drops the model. The node runs the program: on a cue, `phrases`; each `ask` to Jev; `play` with the phrase number from Jev's choice, Jev's verdict as `judge` and the cue's `dynamics`; `listen`; until the score says `done`. On `stop.*`, `stop` once. A cue heard while the chair plays changes the dynamics of its next phrase.

```bash
ORCHESTRA_RUNTIME=jev python3 chairs.py up ob-1 vc-1
```

The node needs `TYPESAFE_API_KEY` in `~/.config/axlr-bots/env` (it is: the AXLR chairs read it there too); it registers the runtime `jev` and the control plane assigns the chair to no node without it. `chairs.py up` registers a chair anew or updates it, so a chair moves between the two templates with one `up`. The score service is the same: it answers the fixed program with `done` where it answers a model with a note.

## Up, down, list

```bash
python3 chairs.py up            # 36 nodes started, 36 mcp.json written, 36 bots registered (or updated) and enabled
python3 chairs.py list          # by section: 10/10 1:E 2:E …; ! marks a node not Online
python3 chairs.py down          # 36 bots stopped (paused, tasks cancelled), 36 nodes stopped
```

Every bot uses the model key the stage's nodes read from `~/.config/axlr-bots/env`. Nothing is published.

## A take, measured

```bash
python3 measure_take.py --chairs ob-1 vc-1 --bars 40 --json take.json   # publishes the cue, then stop.all past bar 40
```

It publishes the cue (`info.cue.all` by default) and later `stop.all` on the team as the operator, so **every Enabled chair of the team hears both**: to measure two chairs, pause the others (`axlr-bots bot pause <id>`) and enable them after. Per chair it prints the phrases played, how many of the first `--bars` came on time, how long before each phrase the bot chose it, the `axlr_judge` calls and their duration (from the run's receipt), whether what the bot reported in `play` is Jev's answer (from the bot's AXLR session), the model requests and their cost, and then the estimate for 36 chairs. [The orchestra's README](../README.md#6c-each-chair-plays-its-own-line-jev-picks-the-phrase) has the numbers of 30 Oct.

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

The rehearsal with the conductor, and the table of latencies, is piece 7. The chairs checked above ran the tools of before phrases (`enter`, `dynamics`); after this change, `chairs.py up` gives every chair the tools and the settings above.

31 Oct 2026, gx10, control plane `main` c5924d0 (binaries of 30 Oct), the 36 chairs as Jev bots, the orchestra playing (a running execution keeps the budgets it was assigned: the take under way, cued at 20:43:37Z, ended as the update ran, 27 chairs at `budget_exceeded: 128/129 tool calls` and 9 at `interrupted: context deadline exceeded` between 20:52:30Z and 20:53:38Z, the same 27 + 9 as the take before; the next cue takes the new budgets):

| Check | Result |
|---|---|
| `ORCHESTRA_RUNTIME=jev python3 chairs.py up` | 36 nodes `running` (none restarted), 36 `settings.json` rewritten with `turn_tool_calls` 400, 36 bots updated in place (`PATCH /v1/bots/<id>`) and Enabled |
| `GET /v1/bots`, the 36 of `orchestra` | 36 Enabled; budgets 128 Jev calls, 400 tool calls, 1,800 s; every mission says `info.expression`; every chair subscribes to `info.expression`; `runtime.jev.act.pass` is `dynamics, character`; `updatedAt` after the update |
