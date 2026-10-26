# ADR: delegation between bots

Status: accepted, 25 October 2026. Vision §10.3 ("límite de profundidad y presupuesto de delegación"), §11 (`maxDelegationDepth`), §14 ("una delegación circular se corta por presupuesto y profundidad"); master plan Phase 3, "delegation limits (depth and budget) when a bot asks another".

## Context

Until now a bot could not ask another bot anything: a task reached a bot by hand, by a webhook or by a timer. The model runs inside `axlr-tui --exec` on a node; the node talks to the control plane over mutual TLS, outbound only; the control plane holds the bots, the tasks and the policy. A delegation is a task one bot's execution creates for another bot, so it must go through the control plane, which alone knows both bots, and every limit must be checked there: a node, or the model behind it, is never trusted to say how deep it is or how much it may spend.

## Decision

### 1. How the model asks

The node agent serves a local MCP server with one tool, `axlr_bots_delegate`, to each execution of a bot that may delegate:

```text
 model ──tool call──▶ axlr-tui --exec ──stdio──▶ axlr-node mcp-relay ──unix socket──▶ axlr-node (agent)
                                                                                         │ mTLS, the node's certificate
                                                                                         ▼
                                       POST /v1/nodes/{node}/executions/{exec}/delegations   (create the child)
                                       GET  /v1/nodes/{node}/executions/{exec}/delegations/{task}?wait=20s   (its state)
```

```json
{
  "name": "axlr_bots_delegate",
  "description": "Ask another bot of this fleet to do a task, governed by the control plane. …",
  "inputSchema": {
    "type": "object",
    "properties": {
      "bot":       {"type": "string", "enum": ["bot-b"], "description": "the id of the bot to ask; only these are allowed"},
      "objective": {"type": "string", "minLength": 1, "maxLength": 4000, "description": "what the other bot should do, in full: it sees this objective and its own mission, nothing of this conversation"},
      "wait":      {"type": "boolean", "default": true, "description": "true: wait for its answer, until it ends or this task's deadline; false: return its task id at once"}
    },
    "required": ["bot", "objective"],
    "additionalProperties": false
  }
}
```

- **Wiring.** For each such execution the agent opens `<node-state>/run/<execution>.sock` (its directory 0700) and the AXLR adapter writes a manifest, `<node-state>/mcp/<execution>.json`: `{"id": "axlr-bots", "command": "<axlr-node>", "args": ["mcp-relay", "--socket", "<sock>"], "allow_tools": ["axlr_bots_delegate"]}`, passed with `--plugin`. `axlr-tui --exec` already loads `--plugin` manifests (AXLR `docs/configuration.md`, flags), so **AXLR needs no change**. The relay is a pipe between its stdin/stdout and the socket; the agent speaks MCP's newline-delimited JSON-RPC (`initialize`, `tools/list`, `tools/call`, `ping`). The socket and the manifest are removed when the execution ends.
- **Why a socket and a relay, not HTTP on loopback.** The manifest takes a stdio command or an `http(s)` URL. A loopback port in a pod is shared with the service's container, so it would need a secret in the URL; a socket in the node's state needs no port and no secret, and the agent, which owns it, knows which execution each call belongs to and when that execution is waiting (below).
- **Approval.** AXLR registers a `--plugin` server with manual approval, and `--approve none|local` approves no MCP tool, so the call would stop the run in `needs_decision`. When the tool is wired and the runtime enforces the policy, the adapter raises `--approve` to `all`: the policy's allow list (always explicit for the control plane's bots) still forbids every tool the bot does not name, `act-with-approval` still adds a card to every allowed write and MCP call ("adds a card even under `--approve all`", AXLR runtime contract), and the destructive list still asks. Without policy enforcement the tool is not wired.
- **In the policy.** The control plane adds `axlr_bots_delegate` to the allowed tools of the assignment of every bot whose definition names targets, so the policy file lists it like any other tool and an `observe` bot may call it (AXLR's `observe` allows the MCP tools the allow list names).

### 2. Who may delegate to whom

Deny by default, on both sides:

```json
"delegation": {"to": ["bot-b"], "maxDepth": 1, "budgetShare": 0.5},
"triggers": [{"type": "manual"}, {"type": "delegation"}]
```

| Field | Meaning |
|---|---|
| `delegation.to` | the bots this bot may ask, by id (at most 16, not itself); absent or empty: it may ask none and is not offered the tool |
| `delegation.maxDepth` | how deep a chain through this bot may go, 1 to 8; absent: `budgets.maxDelegationDepth` when that is set, else 1 |
| `delegation.budgetShare` | the share of this bot's task that a child may take, in (0, 1]; absent: 0.5 |
| `delegation.split` | how that share is taken: `geometric`, of what the task has not granted yet, or `even`, of the whole pool divided by `maxChildren`; absent: `geometric` (§3) |
| `delegation.maxChildren` | the most children one task may ask for, 1 to 32; needed by `even`; with `geometric` it only caps the count |
| trigger `delegation` | the target's consent: a bot takes delegated tasks only when it declares this trigger, as it takes webhooks only when it declares one |

A child must belong to the delegator's tenant and be Enabled.

### 3. How depth and budget propagate

Each task created by a delegation records its **lineage**: the parent task, the parent's execution and bot, its depth, the chain's depth limit, the chain of bots above it (root first), and the **grant** it was given (model calls, tool calls, deadline). The parent records its children and what it has granted so far. A delegation from task *T* (bot *B*) to bot *C* is checked in this order, and the first check that fails is the refusal's `reason`:

| # | Check | Refusal |
|---|---|---|
| 1 | the calling node holds the execution's lease and the execution and *T* are running | an error, not a refusal |
| 2 | *B*'s `delegation.to` names *C* | `not_allowed` |
| 3 | *C* exists, in *B*'s tenant, Enabled, with a `delegation` trigger | `target_unavailable` |
| 4 | `depth(T) + 1 ≤ limit`, where `limit = min(T's chain limit, B's maxDepth)` and a root task's chain limit is its own bot's `maxDepth` | `depth_exceeded` |
| 5 | *C* is not *B* and not on *T*'s chain (the visited set of the trace) | `cycle` |
| 6 | *T* has asked fewer children than `maxChildren`, where set, and the grant below leaves at least one model call, one tool call and 30 seconds wherever *T* is limited | `budget_exhausted` ("maxChildren reached" when it is the count) |

**The grant.** *T*'s pool is its own grant (a root task's is its bot's budgets). For each limit *T* has, the child gets a share of the pool by *B*'s `split`, never more than *C*'s own budget; where *T* has no limit, the child has *C*'s.

- `geometric` (the default): `floor(share × (pool − already granted to T's other children))`. The first child gets the most and each later one less.
- `even`: `floor(share × pool / maxChildren)`. Every child gets the same, and the ask after `maxChildren` children is refused `budget_exhausted` with the message "maxChildren reached".

With `budgetShare: 0.5`, a pool of 20 model calls and children whose own budget is 10:

| Child | `geometric`: pool left → grant | `even`, `maxChildren: 4` |
|---|---|---|
| 1st | 20 → min(10, 10) = 10 | min(10, floor(0.5 × 20 / 4)) = 2 |
| 2nd | 10 → 5 | 2 |
| 3rd | 5 → 2 | 2 |
| 4th | 3 → 1 | 2 |
| 5th | 2 → 1 | refused: maxChildren reached |

Order decides the geometric grants, and the parent does not choose that order on purpose; the even grants depend only on the definition. To give *N* children their whole budget *c*, a pool needs `(N − 1 + 1/share) × c` under `geometric` and `N × c / share` under `even`: the same at `budgetShare: 1`, more for `even` below it. The port fleet's dispatcher (`budgetShare: 0.5`, 4 pilots of 10 model calls and 6 tool calls) gets 10 and 6 for every pilot from a pool of 80 and 48 under `even`.

The child's deadline is `now + share × (T's deadline − now)` under either split, never later than *C*'s own timeout allows: children run in parallel, so they share the parent's deadline rather than split it. *T*'s deadline is when its execution started plus its timeout, or its own grant's deadline when earlier. *T*'s pool, its count of children and what it has granted belong to the task, not to one execution, so an execution that goes on after a person's decision asks within the same limits. So a child always ends before its parent, the children of one task together get at most that task's pool (under `even`, `maxChildren × floor(share × pool / maxChildren) ≤ share × pool`), and every hop shrinks what is left: a loop that escaped the depth and the visited set would still run out of budget. The parent also spends its own pool, so a whole tree is bounded by the root's budgets times its depth plus one, not by the root's budgets alone. The control plane does not know how much the parent has spent so far (the node reports usage at the end, and that number is the node's), so "what is left" is what the control plane granted minus what it has already handed down, never a figure from the node.

The child's executions run with its grant: the requests and tool calls go to AXLR in `--budget`, and its timeout is the time left to its deadline when it is assigned.

A cycle A→B→A is cut twice: with A's `maxDepth: 1`, the second hop is at depth 2 (`depth_exceeded`); with a deeper limit, A is on the chain (`cycle`). Every refusal is a `bot.delegation.refused` event and an audit entry, and the model reads the reason as the tool's error result; the parent's task goes on.

**Idempotency.** The child's key is `delegation:<parent task>:<hash of bot and objective>`: the same ask twice from one task (a retry, or a second call to wait for it) returns the same child, is granted once and counts once toward `maxChildren`, even when the task is at its `maxChildren`.

### 4. How the result returns

- `wait: false` answers at once with the child's task id and status.
- `wait: true` (the default) blocks the tool call while the agent long-polls the child's state (20 s at a time) until the child is Completed, Failed, Expired or Cancelled, or until the parent's deadline less 10 seconds; it then answers with the child's status, its answer, its failure reason and its task id. A child still running at that point is not cancelled: the answer says so, and the parent ends at its own deadline.
- A child that stops on a person's approval waits there; the parent's wait ends at its deadline as above.
- **A child's deadline holds wherever it is.** A child still Pending when its deadline passes (its bot's node is offline), or waiting for a person past it (an approval's `ttl` is longer than the grant), or whose decision would be assigned after it, is not run: it fails with the reason `deadline` (`bot.task.failed`, `"reason": "deadline"`), at assignment, at the decision, or when the reconciler finds it. One that was waiting for a person, or whose last wait had already expired, ends `Expired` instead (`bot.task.expired`): nobody decided its call in time.

### 5. The node keeps running while a parent waits

A node runs one execution at a time. A parent blocked in `wait: true` on a child placed on the same node would otherwise wait for its own deadline. While an execution waits on its child, the agent counts it as idle and takes the next assignment; when the wait ends, the parent goes on even if another execution started meanwhile (waiting for a free slot there could deadlock a parent behind its own child's sibling), so for a moment two may work. A chain of depth *d* on one node is then *d* + 1 `axlr-tui` processes, only one of them working. A wait ends too when its relay's connection closes (the parent was stopped by its timeout or its budget), so a killed parent never keeps a lent slot. When the agent stops, it waits for every execution in flight to be reported as interrupted, as it did when it ran one at a time.

### 6. What the control plane shows

- `bot.task.delegated` on the delegator's stream (its task, the child's task and bot, depth, wait, grant) and `bot.task.submitted` on the target's (trigger `delegation`); `bot.delegation.refused` with `target`, `reason`, `message`, `depth`.
- Audit: `task.delegate`, `ok` with the child or `denied` with the reason.
- `GET /v1/tasks/{id}`: `delegation` (the lineage and the grant) on a child, `children` on a parent. The child shares the parent's trace id, so `GET /v1/tasks?…` and the events of one trace show the whole tree.

## Consequences

- One more node route pair on the existing mTLS channel; old nodes ignore the new `delegation` and `budgets` fields of an assignment (the node decodes leniently) and simply never offer the tool; **upgrade the control plane first**, as for every new field.
- The node now carries the bot's request and tool-call budgets to AXLR (`--budget` had only the seconds on a node before).
- A delegating bot's run is `--approve all` under its enforced policy; its level and allow list remain the whole of what it may do.

## Left out

- Delegation from the control plane's in-process runs (single-process mode): `SerialRunner` holds its lock for the whole run, so a waiting parent would block its own child; those runs are not offered the tool.
- Cancelling a parent does not cancel its children; each child ends at its own deadline.
- The socket and the relay keep the tool to the node's user, as the node's key is: a bot with `local_exec` on the same node could reach the socket of another execution there and delegate within that execution's grants. Per-execution sandboxes (vision §9.2) close it.
- A grant from the parent's live spend, and a share of the cost in dollars: AXLR's `usd` budget is not passed yet.
