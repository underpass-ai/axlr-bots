# ADR: signed catalog skills, pinned by version, for a bot

Status: accepted, 9 November 2026. axlr-sre design §8.2 (runbooks as signed skills). AXLR's side is its [catalog skills](https://github.com/underpass-ai/AXLR/blob/main/docs/console.md#catalog-skills) and [skills per execution](https://github.com/underpass-ai/AXLR/blob/main/docs/runtime-contract.md#skills-per-execution); this repository gives them to a bot.

## Context

A bot could not be told which procedures it follows. `axlr-tui` already installs a signed skill and runs an execution on exactly the versions it is given. Nothing asked it to: no bot field, no install on the node, no `--skills`. The sre team's runbooks need that path before they can become skills.

## Decisions

| # | Decision |
|:--|:--|
| 1 | **A bot definition gains `skills: [{name, version, catalog}]`**, validated at register and update and on every read of a stored bot. The version is one exact `major.minor.patch`: no range, no `latest`. The catalog is named, never inferred. The shapes are AXLR's own (`domain/skill_pin.go`), at most 32, one version of a skill per catalog. A stored bot without the field reads and writes back without it |
| 2 | **Pinned at submission.** `SubmitTask` copies the bot's skills onto the task, as #133 pins a phase's policy. It is the one place a task is created, so triggers, incidents, delegations and events are all covered. A bot updated later never changes what a submitted task reads |
| 3 | **The node installs what is missing, and only that.** Before a run, `adapters/axlr` looks for each pin. When one is missing it first runs `axlr-tui skills list --refresh`, then `axlr-tui skills install <name>@<version> --from <catalog> --yes` for each one the host lacks; AXLR fetches it and verifies it against the catalog's signed index. The refresh is there because `skills install` reads a cached index however old it is, so a version published after the node's first install would never be found. A pin already installed asks nothing of the network. A refresh that fails keeps the cached index, and the install answers from it or fails with its reason. It never runs `skills update` |
| 4 | **The install lands where `--exec` reads.** `--exec` takes no `--state`: it reads `$XDG_STATE_HOME/axlr`, or `$HOME/.local/state/axlr`. So the node passes no `--state` either. A node-private directory would look installed and be invisible to the run (verified below) |
| 5 | **A skill that cannot be had fails the task, with the reason, and the run does not start.** A refused signature, an unknown version, an unknown catalog, an install that hangs past 3 minutes or the run's timeout, a binary without `--skills`. `--exec` and `--decide` are the last check: they refuse a pin they cannot serve. In every one of these the report sets `result.notStarted`, so a ladder knows nothing was written (decision 10). A refusal by the runtime counts only when it wrote no event before it; a run that started is never `notStarted` |
| 6 | **`--yes` is not an approval.** The person who wrote the definition chose this version; that act, audited as `bot.register` or `bot.update`, is what the install carries out. The install is not a card, a task never waits on one, and its exit 3 is a failure like any other, never `WaitingApproval`. The node does not read the card it prints, so a skill's text never reaches a failure reason |
| 7 | **A task with skills goes only to a node that can read them.** `capabilities.skillsSupported`, read from `--help`, gates the assignment. An older node ignores an unknown field and would run without the skill, which is the one thing this may not do; here the usual "an older node ignores it" does not hold. The gate reads the task's pins, not the bot's, since those are what the run asks for: a task keeps its gate if its bot drops its skills, and a task with none can still go to an older node if its bot gains some. A placed task waits, and says so (`has an axlr-tui that takes no pinned skills`); an unplaced one runs in process |
| 8 | **A bot without skills gets an empty `--skills` list**, when the binary takes the flag. Without it AXLR serves every installed skill at its newest version, so one bot's install would appear in every other bot's prompt on that node. An operator's own `/skills install` stops reaching bots too; that is the same rule |
| 9 | **`axlr_skill` joins the allow list of an execution with skills.** An allow list forbids what it does not name, and the model could not open what it was given. It is a read, in AXLR's own list. Of the catalog skills it serves only the pinned versions; it also reads the skill files of any plugin installed on the host, which pins do not cover. It is added on the node, also when a phase narrows the tools, and it shows in the execution's `policy` record |
| 10 | **A run that never started costs a ladder nothing.** Without `notStarted`, a failure with no session reads as "may have written" and lowers the runbook two steps (ladder decision 7). A registry outage is no reason to lose what was earned. The entry ends as it does for any failure that wrote nothing |
| 11 | **A decision goes on with the pins its session resolved.** `--decide` gets no `--skills` and the node installs nothing for it: passing the pins again would re-resolve them, and a bundle reinstalled at that version while the approval waited would be accepted. Without the flag AXLR compares the bundle with the one the session took, and refuses a change or a removal. The decision still carries the pins, because `--decide` renews the policy and `axlr_skill` must stay on it |
| 12 | **A Jev bot takes no skills**: it has no model to read one. Refused at register and update |

## Decided in the building (9 November 2026)

| Point | Decision | Why |
|:--|:--|:--|
| How the node knows a pin is there | It looks for `skills/<catalog>/<name>/<version>/skill.json` under AXLR's state, the layout AXLR documents | `skills list` fetches a stale catalog index first, so an incident during a registry outage would pay 30 seconds per run. The look is only there to save the network: a wrong answer means an extra install, or a run `--exec` refuses |
| One install at a time | One slot on the executor, whose wait ends with the execution's context | A lent slot can run two executions of one node at once. An execution whose pins are all here does not wait |
| The install's time | The 3-minute budget starts once the slot is held; what the install took, the wait included, comes out of the run's timeout | A task keeps to the time it was given. A task with nothing to install keeps its whole timeout |
| The pins on the wire | `run.skills` and `decide.skills`, `{name, version, catalog}` | The same shape everywhere; the node re-validates through the domain |
| Bundle digests | Not stored by the control plane | AXLR resolves them at the start and writes them in the execution's receipt, which is kept (`skills.pins[].bundle_sha256`) |
| The panel | Passes `skills` through a definition file, as it does `runtime`; no editor | Without it, a definition loaded in the panel would silently lose its skills |

## How an incident's task picks a runbook: not decided here

Every skill the bot was given is offered to every one of its tasks. The model sees one line each in the skill index and opens the one that fits.

The ladder's runbook id is the alert name (ladder decision 2). Binding it to a skill needs choices that are the owner's, and none is forced by this change:

- Where the pin of an incident's runbook lives: on `ladder.runbooks[]`, on the trigger, or matched by name.
- Whether a new version of the skill keeps what the runbook earned, or restarts it as a changed tool does (ladder decision 15).
- Whether an unattended run's budget of `LadderMaxToolCalls` (4) still holds: `axlr_skill` is one of its calls, in place of the local read of a runbook.

## AXLR's behaviour, verified

Against the installed `axlr-tui` (0.16.0-tools-7e6034a), with a scratch `HOME` and a signed catalog on the loopback:

| What | Result |
|:--|:--|
| `skills install x@1.0.0 --from lab`, no `--yes` | exit 3, nothing installed |
| `--exec --skills` with a pin not installed | exit 1, `the execution's skills are unavailable here: crashloop is not installed` |
| `skills --state <dir> install … --yes`, then `--exec --skills` | **exit 1, not installed**: the run does not read `<dir>` |
| `skills install … --yes` without `--state`, then `--exec --skills` | the run reads it |
| A file that does not match the signed index | exit 1, `does not match the digest the signed index carries; the install is refused` |
| An unknown version, an unknown catalog | exit 1 with the reason |
| `--skills` with `[]` | the run has no catalog skill: `skills pinned: none (no catalog skill)` |
| Version published after the index was cached | `skills install` refuses it until `skills list --refresh` |

## Alternatives

| Option | Why not |
|:--|:--|
| Install from a job on the control plane | The control plane does not hold the node's state, and a node added later would have nothing |
| Pin only at launch of the run | A retry or a long queue would run other versions than the ones submitted |
| Always install, even when present | Every run would need the registry, and an outage would stop the bots that need it most |
| Move AXLR's state under the node's (`XDG_STATE_HOME=<node-state>`) | It would also move the sessions, so a decision pending on a running node would be lost |
| Refuse a bot whose allow list lacks `axlr_skill` | An incident phase can narrow the tools later and would still run blind |

## What this does not do

No selection of one runbook per incident. No retry of a failed install: the task fails, and a person or a trigger submits it again. No editor in the panel. The sidecar's `XDG_STATE_HOME` is a tmpfs, so a container restart installs again. The chart's NetworkPolicy has no rule of its own for a catalog: the default model-provider rule (public addresses on 443) happens to reach a public one such as `registry.underpassai.com`, and a catalog on a private address, or a narrowed `networkPolicy.modelProvider`, needs its own rule.
