# ADR: one KMP store per bot, under the node's state

Status: accepted, 8 November 2026 (the QA of the bots' memory cycle, `artifacts/qa-kmp-bots/qa.md` in the workspace, F1). Vision §12.3 (the execution's evidence) and the [runtime contract](runtime-contract.md): the control plane and the node decide where and as whom a bot runs; AXLR decides how.

## Context

A bot's memory is written by `axlr-tui`, which opens a KMP store per launch: the checkout's `.kernel` when the workspace is a git repository, else the directory `mcp.json` pins, else the per-user default. The node runs every bot in `<state>/workspaces/<bot>`, a plain directory, with the operator's `~/.config/axlr/mcp.json`, whose installer pin of the per-user default AXLR reads as the installer's old one and sets aside. So every bot on this host resolved to the operator's own store, `~/.local/share/kmp/default`: 1 093 bot sessions had landed in it by 8 November 2026, beside the operator's memory, and two bots on one host shared one store, apart only by their abouts. The node passed nothing about memory: `--axlr-mcp-config` gives one `mcp.json` to every run, which is one store for all bots.

## Decision

The node gives each bot its own store and tells AXLR which: `axlr-tui --exec … --kmp-store <state>/kmp/<bot>`, and the same directory on the `--decide` that continues the execution, so the session reopens the store it wrote. The flag wins in AXLR over `mcp.json`'s pin and over the workspace ([AXLR's contract](https://github.com/underpass-ai/AXLR/blob/main/docs/runtime-contract.md), "Memory store"). The node creates the directory (mode 0700, the bot's memories are nobody else's) before the run; AXLR prepares it as any store it opens. The flag is feature-detected in `axlr-tui --help` like `-policy`: an older binary gets nothing and its runs resolve the store as before.

```text
 <state>/
   workspaces/<bot>/      the bot's files           (as before)
   kmp/<bot>/             the bot's memory store    (new; --kmp-store)
   traces/<execution>.*   what the run said          (as before)
```

The store is keyed by the bot id alone, as the workspace is, so a store and a workspace of one bot sit side by side; the bot's abouts inside it stay `project:<bot>` (the workspace's name) and `ws:<session>`.

## Alternatives

| Alternative | Why not |
|---|---|
| `git init` the workspace, so AXLR picks `.kernel` | Works today (QA case 18) but by a side effect: a bot that runs `git` in its workspace would see a repository that is not its task's, and the choice would live in AXLR's heuristics, not in the contract. |
| One `mcp.json` per bot through `--axlr-mcp-config` | A file per bot per host to keep in step with the operator's; the pin it carries is still read as the installer's when it names the per-user default. |
| Key the store by tenant and bot, `kmp/<tenant>/<bot>` | A decision's request carries no tenant today, and the workspace is keyed by the bot alone; the tenant goes in when both do (H9 of the study: two tenants with one bot id). |
| Let the control plane name the store in the assignment | The store is a host's path, which the control plane does not know; the node owns the host's state. |

## Consequences

- A bot's memory lives in `<state>/kmp/<bot>` on the host it is placed on; it moves with the node's state (a sidecar's volume) and never reaches the operator's store. `kmp-mcp memories --json` with `KMP_MCP_DATA_DIR=<state>/kmp/<bot>` lists it.
- Each store registers its own KMP agent (AXLR keeps one per store), so bots and the console no longer share an identity.
- What is already in the operator's store stays there; moving it is a hand's job (`kmp-mcp export … --about project:<bot>` and `import`), not the node's.
- The policy, the workspace and the store are the three things the node gives a run about *where* it works; the [runtime contract](runtime-contract.md) lists the flag.

## Left out

The boundary inside the store (which abouts a bot may read and write, whether `observe` may remember, waking the previous task) is AXLR's, under the execution's policy; the QA's F2 to F4 are fixed there. The bots' sessions still sit in the operator's `~/.local/state/axlr/sessions` (H8 of the study).
