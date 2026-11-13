# Runbook: the transcript audit

How the operator runs the input adapter that reads the **whole transcript** of every execution ([transcripts](../architecture/transcripts.md)) and tells a team the operator names (`--team`), under an event name the operator names (`--event`), about each one that hides a failure. Deterministic: the loop, the cutting into blocks and the verdict are code; Jev (TypeSafe's judge, no language model) answers one choice per block, `clean` or `hidden_error`. It reads and publishes through the control plane's socket only, as the operator. The wire is [`transcript-audit.asyncapi.json`](../reference/transcript-audit.asyncapi.json).

```text
axlr-bots serve (the control plane, transcripts in <state>/transcripts.db)
   │ GET /v1/transcripts?after=<cursor>                 the transcripts not audited yet
   │ GET /v1/executions/{id}/transcripts/{session}      each one whole, checked against its SHA-256
   ▼
axlr-bots transcript-audit serve
   session → one JSON line per message (the mission left out)
   lines → blocks of at most 24 KiB (a long line in labelled pieces)
   each block → Jev: clean | hidden_error           (TYPESAFE_API_KEY)
   every verdict → <state>/transcript-audit/verdicts.jsonl   (its highest seq is the cursor)
   │ any block hidden_error:
   │ POST /v1/groups/<team>/events  <event>, idempotency key transcript:<seq>
   ▼
the team: the members that subscribe to <event> hear it
```

## What a block is

| Part | Rule |
|---|---|
| lines | one compact JSON line per message of the session, the first message (the mission and the playbook: instructions, not activity) left out |
| a tool's result | labelled with the call it answers: its tool and what it named (`local_exec arreglo plan a-1`, `local_read playbook.md`) |
| block | whole lines up to 24 KiB of Jev's state, header included (`Task … of bot …, execution …. Block i of n.`) |
| a longer line | pieces, each starting `[Piece k of n of one transcript line: the result of tool call …]`, so a piece of a playbook read is not taken for what happened |

The question is fixed in `adapters/transcriptaudit/auditor.go`: a tool or program that refused, errored or could not run; a check skipped, omitted or not really done, even if the bot mentions it; missing data, files or setup; a result reported as fine although something went wrong. A daily cap, a test that fails as designed or nothing to do are not hidden failures.

Measured on 24 tasks of 12 and 13 November 2026 labelled by hand (12 with a hidden failure, 12 clean): 9 of 12 found, 1 of 12 clean flagged, the same in two runs; it misses the skips the bot itself names. About 0.03 USD for the 24.

## Run it

```bash
TYPESAFE_API_KEY=… axlr-bots --socket <state>/axlr-bots.sock transcript-audit serve \
  --team qa --event transcripcion.fallo
```

| Flag | Default | |
|---|---|---|
| `--team`, `--event` | none | required |
| `--store` | `<state>/transcript-audit` | its verdicts and its lock |
| `--model` | `jev-1.13.0` | pinned, never `-latest` |
| `--interval` | `30s` | between rounds |
| `--once` | | one round, prints the cursor |
| `--dry-run` | | records the verdicts, publishes nothing |

In the cluster it runs as a container of the control plane's pod, beside `activity-audit`, with the state volume and `TYPESAFE_API_KEY` from the `axlr-bots-env` secret.

## What fails

| Seen | Meaning | Do |
|---|---|---|
| `TYPESAFE_API_KEY is required for Jev` | no key | give the container the secret |
| `does not match the SHA-256 the control plane answered` | the bytes changed on the way | the round stops there and retries; look at the control plane |
| a round that stops on a transcript | Jev or the publish failed | nothing is recorded for it: the next round audits it again |
| `skipped` in the verdicts | the transcript is not a session with messages | nothing to judge |

## Read what it decided

```bash
tail -n 5 <state>/transcript-audit/verdicts.jsonl
axlr-bots execution transcript <execution> <session> --jsonl
```

Each verdict names the blocks Jev flagged, with the probability and the block's first 400 characters.
