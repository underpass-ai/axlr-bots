# World adapter: the template

The shape a new [input adapter](../../docs/architecture/input-adapters.md) starts from: a small process beside a world service that turns the world's signal into what a bot already understands, as text. Python, the standard library only; it runs and its tests pass with nothing installed and no network.

```text
world-adapter/
  adapter.py        observe(bot_id) -> str          one reading per look, paced, labelled
                    watch() -> Iterator[Event]      the world's changes as events (name, message, data, to)
                    Publisher                       POST /v1/groups/{team}/events as the operator; prints without a url
  config.json       the team, the world, rate.perMinute, the control plane's address, the label
  test_adapter.py   a frame → the expected text; a change → the expected event; a flood of calls → the rate holds
```

## The signal

A fake world in memory (`World`): a dozen things on a unit square with a fence and a mark, drifting a little on every tick from a seeded generator, so every run is the same. One bot per thing: the bot's id is the thing's id (`thing-0` … `thing-11`). A real adapter replaces `World` with a client of the world service (`world.url` in the config) and keeps the rest: the pacing, the text, the events, the publisher, the tests.

## What the text looks like

`observe(bot_id)` answers one line, positions as fractions of the frame from its top-left corner (y grows south), distances in steps (one step is what a thing moves in a tick), and the reading's age:

```text
thing-0 at 0.39, 0.29, inside the fence; the mark at 0.50, 0.50 is 8 steps south-east; nearest: thing-7 at 0.27, 0.33, 4 steps south-west. Reading 0 s old; exact, simulated.
```

A bot that is not in the world is a `LookupError`: serve it as a tool error (`isError: true`), which the model reads; never as an exception that ends the call.

`watch()` answers the world's changes as events, with the kind that fits each ([the kinds](../../docs/architecture/pubsub-events.md#topics-and-kinds)):

| Change | Event | Who hears it |
|---|---|---|
| a thing crosses the fence | `warn.fence`, addressed `to` its bot: «thing-4 crossed the fence at 0.09, 0.42; come back inside», `data` with the thing, x, y and the tick | that bot, as a line in its running task or a task when idle; no subscription needed |
| a thing reaches the mark | `info.reached`: «thing-3 reached the mark at 0.50, 0.50» | the bots subscribed to `info.*` or `info.reached`, as a note in their next prompt by default |

## Which model

None. The reading is a lookup in a position table and says so: `exact, simulated`. An adapter that reads a frame with a vision model names the model in its config and in this section, and ends its text with the model's name instead of `exact`.

## What it costs

A reading of the fake world costs nothing and answers at once. The pacing is the point: `rate.perMinute` (6 by default) bounds how many times a minute the world is read, whoever asks; past it `observe` answers from the last reading and says how old it is, so 300 bots asking at once cost the world six readings a minute and every bot still gets text. `watch` reads the world once per tick at the adapter's own `period`, outside that budget, and refreshes the reading `observe` answers from. With a model behind the reading, `rate.perMinute` is the bill: write the price per call here.

On the control plane's side, each look is one tool call against the bot's `maxToolCallsPerTask`, and each event a delivery on the receiver's own budget ([input adapters, rule 2](../../docs/architecture/input-adapters.md#rules)).

## The honesty label

On every screen that shows the world:

> Simulated world. Each creature is one LLM bot; it sees only what this adapter reports, as text.

It is `label` in the config and `LABEL` in the code.

## Run and test

```bash
python3 adapter.py --ticks 40
```

prints the label, a reading, the events of forty ticks as they would be published, and a last reading; without `--ticks` it runs at `period` seconds a tick until stopped. With `controlPlane.url`, `tokenFile` and `caFile` set, `Publisher` sends each event to `POST /v1/groups/{team}/events` with the operator token (read from its file on each call, never logged) and an `Idempotency-Key` derived from the body, so the same change again makes nothing twice ([operator API](../../docs/reference/operator-api.md#routes)).

```bash
python3 -m unittest -v test_adapter
```

The three tests, and two small ones around them: a frame becomes the expected text; a change becomes the expected event; 300 calls in 30 seconds read the world six times, every call answered, the stale ones saying their age. CI runs them.

## Wiring the three outputs

| Output | How |
|---|---|
| tool result | serve `observe` as an MCP tool on loopback, as the fleet's companion computer does: copy [`mcp_http.py`](../drone-fleet/app/mcp_http.py) beside the adapter, `tools.add(adapter.TOOL, lambda a: {"text": adapter.observe(a["bot"])})` (raise `mcp_http.ToolError` on a `LookupError`), name it in the sidecar's `mcp.json` and in the bot's `allowedTools` ([drone fleet](../../docs/runbooks/drone-fleet.md)). Or write each bot's reading to a file in its read-only workspace, as `fleet.json` is, and allow `local_read` on that path |
| event on the bus | `Publisher.publish(event)` for each event of `watch()`; the token and the CA from a Secret mounted read-only. `error.*` and `done.*` are refused: the control plane's only |
| note | nothing in the adapter: the bot subscribes to `info.*` with mode `note` (the default), and its next task's prompt carries the last 8 notes under "Recent events in your team" ([subscriptions](../../docs/reference/bot-definition.md#subscriptions)) |

## Acceptance for a new adapter

The tests pass with no network and no model; one bot's look returns text within the stated time, a refusal as a tool error; 300 bots at once hold `rate.perMinute`, each answered with its reading's age; a change lands with the kind that fits; the label is on the screen.
