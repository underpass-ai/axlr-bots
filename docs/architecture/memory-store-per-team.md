# ADR: one KMP store per team, shared by its members

Status: accepted, 10 November 2026 (decisions D1 to D6 of the team memory design, `artifacts/team-memory-2026-11-10/diseno.md` in the workspace). It narrows [one KMP store per bot](memory-store-per-bot.md), which still holds for a bot in no team.

## Context

Since 8 November 2026 the node gives each bot its own store, `<state>/kmp/<bot>`, so a bot's memory never reaches the operator's. That also keeps a bot from its teammates': the QA team's playbooks record charters and plans under one about, `project:underpass-qa`, and each of its five bots wrote that about into a store only it opens. The weekly plan of `qa-jefe` asks which charters ran last week and never saw the ones `qa-explorador` recorded.

A team is already a thing the control plane knows: each name in a bot's `groups` ([teams and events](groups-and-events.md)). What a team's bots share today is the directory, delegation and events, not memory.

## Decision

The bots of a team open one store, `<state>/kmp/<team>`, and the node tells AXLR whose it is.

```text
 <state>/
   workspaces/<bot>/      the bot's files            (as before)
   kmp/<team>/            the team's memory store    (a bot in a team)
   kmp/<bot>/             the bot's memory store     (a bot in no team)
```

| What | How |
|---|---|
| Which team | The first of the bot's teams as the control plane keeps them, which is sorted by name (`application/memory_team.go`). It travels with the assignment as `memoryTeam`, on a run and on the decision that continues it |
| The store | `axlr-tui … --kmp-store <state>/kmp/<team>`, the directory made 0700 before the run |
| Whose it is | The run's policy file carries `"memory": {"team": "<team>"}` ([AXLR's contract](https://github.com/underpass-ai/AXLR/blob/main/docs/runtime-contract.md), "Policy per execution") |
| What a member reads | Every about of the store: nobody else's memory is in it |
| What a member writes | Its own about, `project:<team>.<bot>`; the team's, `project:<team>`; and its session's `ws:` about. A teammate's about is refused, so a memory is always its author's |
| Who wrote what | AXLR registers one KMP agent per member in the store |

The boundary inside the store is AXLR's, under the execution's policy, as it was for a bot alone. The node's part is the directory and the name.

KMP's store takes several processes at once: it is SQLite in WAL mode, and a second writer waits for the first (up to 10 seconds). Measured on 10 November 2026 with KMP 0.26.0: three processes writing 40 memories each into one store at the same time, 120 of 120 stored, the slowest write 0.04 s. KMP refuses a path separator in an about (`project:qa/qa-jefe`), which is why the member follows a dot.

## When the store stays the bot's

| Case | Why |
|---|---|
| A bot in no team | It has nobody to share with |
| A build of `axlr-tui` that does not read `memory.team` (its `--help` does not name the field) | It would refuse the policy, and would hold no member to its own about |
| A team whose name has a dot | AXLR writes a member's about as `project:<team>.<member>`: the dot would read as another team's member, and AXLR refuses the name |
| An older node | It ignores `memoryTeam` in the assignment |

## Alternatives

| Alternative | Why not |
|---|---|
| A store per bot and a second store for the team | Two stores per run: AXLR opens one per launch, and a bot would have to choose where each memory goes |
| Keep a store per bot and only rename the abouts | It reads as a team's memory and shares nothing |
| A field of its own on the bot for its memory team | Clearer for a bot in several teams, at the cost of a new field in the bot's definition, the API and the panel. Decision D2 took the first team; a bot in one team, as every QA and developer bot is, has no order to mind |
| A store shared by every node of a host | A team's bots run on one node today. A path outside the node's state comes with a team that spans nodes |

## Consequences

- A team's memory lives in `<state>/kmp/<team>` on the node its bots run on. `KMP_MCP_DATA_DIR=<state>/kmp/<team> kmp-mcp memories --json` lists it.
- The team's store starts empty (decision D5). The stores under `<state>/kmp/<bot>` stay where they are and are no longer opened by a bot that joined a team; moving them is a hand's job (`kmp-mcp export … --about` and `import`).
- A bot's teams are kept sorted, so "the first" is the first by name, not the first typed. The marketing bots are in several squads (`mkt-editorial`, `mkt-direccion`, …): until they share one team that sorts first, their memory is per squad.
- A bot that joins or leaves a team opens another store from its next task on. A decision that waits across that change continues its session in the new store.
- The playbooks name the abouts: what a team holds in common goes to `project:<team>`, which every member wakes at its first prompt.

## Left out

- A team whose bots run on more than one node of a host, or on more than one host (a sidecar, another machine, a microcontroller): the store is a directory of one node's state. KMP served over the network is the next step (decision D4).
- KMP's other verbs that write (`kmp_ingest`, `kmp_relabel`, `kmp_curate`, `kmp_condense`): AXLR holds `axlr_remember` and `kmp_write_memory` to the member's abouts, and a policy that allow-lists another vouches for it.
- Asking another team for its context: a team's store holds that team's memory alone.
