# ADR: the whole transcript of each execution

Status: accepted, 13 November 2026. The product owner asked for every activity of a task, uncut, as the input of an audit: the night of 13 November showed that tasks reported *Completed* hid failures (a refused plan, a skipped check, a missing binary) that only the full text showed, and that nothing kept that text off the node.

## Context

What a run did reaches the control plane today in two cut forms:

| Where | What is cut |
|---|---|
| the node's progress tracker ([live-progress](live-progress.md)) | a call's arguments to 120 characters, its result never, the model's words to 32 KiB per run |
| `RecordProgress` in the control plane | each item again (summary 160, error 240, text 4000 characters), 64 items a batch, 400 events and 256 KiB per execution |

The whole run exists in one place only: the file AXLR keeps for each session on the node, `<AXLR state>/sessions/<id>.json`, with every message, every call's arguments and every tool's result as the model saw it. On 13 November all 344 qa/dev executions of the day had theirs; the largest session on gx10 was 1.5 MiB.

Three choices were the owner's:

- **Keep it in the control plane**, not on the node and not in KMP (writing every turn to KMP costs too much).
- **SQLite**, through cgo (`github.com/mattn/go-sqlite3`), in the control plane's state.
- **As it is**: no redaction. A tool's output may hold a secret; it is kept, and the audit sends it to the judge.

## Decision

### 1. The node uploads each session before it reports

When a run ends, on every path (completed, failed, interrupted), the node reads the file of each session the run's result names (`Result.Sessions`) through the AXLR adapter (`ReadSession`) and puts it to the control plane, whole, with its SHA-256:

```text
PUT /v1/nodes/{id}/executions/{exec}/transcripts/{session}
X-Axlr-Transcript-Sha256: <hex>
<the session file's bytes>
```

As for artifacts: only the node that holds the execution, only while it runs (so before the report), and only bytes that hash to the header. One that cannot be read or is refused is logged and left out; the report stands. A session id is 1 to 64 letters, digits, `-` or `_`; the file must be a regular file of at most 32 MiB.

### 2. The control plane keeps it in SQLite

`adapters/sqlitestore` keeps one row per execution and session in `<state>/transcripts.db` (mode 0600, WAL): the execution, its task and bot, the session id, size, SHA-256, when it came, the bytes, and `seq`, the order transcripts came. A second upload of the same execution and session replaces the row with a new `seq`, so a reader following the cursor sees it again. The upload is audited (`transcript.upload`).

Only the control plane links SQLite; `axlr-node` stays pure Go. The image links `axlr-bots` statically (`-linkmode external -extldflags -static`, tags `sqlite_omit_load_extension netgo osusergo`), so the distroless static base still runs it; the build stage installs the target's C cross compiler when it builds for another architecture.

### 3. The operator reads it whole

| Route | Answer |
|---|---|
| `GET /v1/transcripts?after=&limit=` | the records after the cursor, oldest first, at most 500 |
| `GET /v1/executions/{id}/transcripts/{session}` | the bytes as the node sent them; `X-Axlr-Transcript-Sha256` |

`axlr-bots execution transcripts [--after N]` and `axlr-bots execution transcript <exec> <session> [--jsonl] [--out f]`; `--jsonl` writes one line per message of the session. The client checks the bytes against the hash.

## Consequences

- The audit of finished tasks (the next step) reads the whole run, not a summary.
- Secrets a tool printed are kept in the control plane's state. Whoever can read the operator API can read them.
- A run the control plane makes in process (no node could take it) has no transcript yet.
- Old rows are never deleted yet; at the sizes seen (median under 100 KiB) a year of the fleet stays in a few GiB. A retention rule is future work.
