# Runbook: turn on one link of the team channel

How an operator turns on exactly **one** live link of the [team channel](../architecture/team-channel.md), QA asks and the developers answer (qa→dev), on the gx10 host, and how to turn it all off again. These are instructions for the operator; nothing here runs by itself. Turn a link on only with the product owner's go-ahead.

```text
                                           gx10 (one user, one $HOME)
 control plane  axlr-bots.service          ~/.config/axlr-bots/env  ← read by ALL four node units
 nodes          axlr-node        gx10-ff95  QA's bots, qa-vigia     ← switch ON  (drop-in)
                axlr-node-dev    gx10-dev   dev-go answers          ← switch ON  (drop-in)
                axlr-node-sre-ro            sre, read-only          ← off
                axlr-node-sre-rw            sre, writes             ← off
 bots           bots-work-teams installers: TEAM_CHANNEL_ASK / TEAM_CHANNEL_ANSWER
```

A link needs both halves: the bots declare it (the teams' installers write `ask.teams` and the `question` trigger) and the nodes that run them have the switch on (`axlr-node --team-channel`, `AXLR_NODE_TEAM_CHANNEL=true`, [off by default](../architecture/team-channel.md#the-switch)). Either half alone does nothing.

## Before you start

| Check | How |
|---|---|
| Versions | the control plane first, then the nodes, from a build that has the switch (the `teamChannel` capability). An older control plane ignores the switch |
| One tenant | QA's and dev's bots are both tenant `default`; the control plane picks who answers only in the asker's tenant |
| Memory | `AXLR_NODE_MEMORY_TEAMS=*` is already in `~/.config/axlr-bots/env`, so `gx10-dev` shares dev's store. Leave that file as it is |
| The switch is not in the shared file | `grep -n TEAM_CHANNEL ~/.config/axlr-bots/env` prints nothing. That file is read by all four node units, so the switch there would turn sre-ro and sre-rw on too; and a value there would override the drop-ins below |
| The drill | dev's responder playbook was tried with injected questions **first**, with everything still off: [`teams/dev/docs/simulacro-canal.md`](https://github.com/underpass-ai/bots-work-teams/blob/main/teams/dev/docs/simulacro-canal.md) in bots-work-teams, every case passed. A failure stops here |
| Idle nodes | no task Running on `gx10-ff95` or `gx10-dev` (`axlr-bots task list --status Running`): a restart interrupts a run in flight |

## 1. Switch the two nodes on

One drop-in per unit, never the shared environment file:

```bash
for unit in axlr-node axlr-node-dev; do
  mkdir -p ~/.config/systemd/user/$unit.service.d
  printf '[Service]\nEnvironment=AXLR_NODE_TEAM_CHANNEL=true\n' > ~/.config/systemd/user/$unit.service.d/team-channel.conf
done
systemctl --user daemon-reload
systemctl --user restart axlr-node-dev.service axlr-node.service
```

Check:

```bash
journalctl --user -u axlr-node -u axlr-node-dev --since -2min | grep 'team channel on'
axlr-bots node get gx10-ff95 | jq .capabilities.teamChannel   # true
axlr-bots node get gx10-dev  | jq .capabilities.teamChannel   # true
axlr-bots node list | jq '.[] | {id, teamChannel: .capabilities.teamChannel}'   # sre's nodes: null
```

The channel's memory (phase 2) is a second switch, `AXLR_NODE_TEAM_CHANNEL_MEMORY=true`, in the same drop-ins when the product owner wants it; it needs `kmp-mcp` on the unit's `PATH` or `AXLR_NODE_KMP_MCP` naming it, or the node does not start. Without it each bot records the exchange in its own about.

## 2. Declare the link: dev answers QA

In `~/.config/bots-work-teams/dev.env`: `TEAM_CHANNEL_ANSWER=qa` and `TEAM_CHANNEL_ASK=` (empty: dev asks nobody).

```bash
teams/dev/ops/install.sh --dry-run --bots dev-go,dev-jefe   # read what it would register
teams/dev/ops/install.sh --bots dev-go,dev-jefe --enable dev-go
axlr-bots bot get dev-go | jq '.triggers[] | select(.type=="question")'   # {"type":"question","from":["qa"]}
axlr-bots bot get dev-go | jq .ask                                         # null
```

dev-jefe stays not enabled: dev-go answers.

## 3. Declare the link: QA asks dev

In `~/.config/bots-work-teams/qa.env`: `TEAM_CHANNEL_ASK=dev` and `TEAM_CHANNEL_ANSWER=` (empty: QA answers nobody, so dev→qa stays off).

```bash
teams/qa/ops/install.sh --dry-run --bots qa-jefe,qa-triaje,qa-vigia
teams/qa/ops/install.sh --bots qa-jefe,qa-triaje,qa-vigia --enable qa-jefe,qa-triaje
axlr-bots bot get qa-jefe | jq .ask                                  # {"teams":["dev"],…}
axlr-bots bot get qa-jefe | jq '{level, auto: .approvals.autoApprove}'
axlr-bots group links qa                                             # one link: qa → dev
```

**A5**: asking needs no approval. A bot at `act-with-approval` must have `axlr_bots_ask_team` and `axlr_bots_question_status` in `approvals.autoApprove`; the installer adds them. On 11 November 2026 no asker of QA was at that level.

## 4. The watcher, last

qa-vigia (a Jev bot, a round every 5 minutes) gives QA's stuck questions up. It is registered paused. Enable it only when both hold:

```bash
axlr-bots node get gx10-ff95 | jq '.capabilities | {teamChannel, runtimes}'   # true, ["jev"]
```

`runtimes` names `jev` only when `TYPESAFE_API_KEY` is in the node's environment. Then `axlr-bots bot enable qa-vigia`. Without the `jev` runtime its round is assigned to no node and its tasks pile up; on a node with the switch off its round is refused (`403`, audit `question.watch`) every 5 minutes.

## 5. The first question

```bash
axlr-bots task submit qa-jefe --objective "Ask dev, with axlr_bots_ask_team, whether it knows finding <id>; go on with the report." --key canal-qa-dev-1
```

Watch `bot.question.asked` on QA's timeline and `bot.task.submitted` (trigger `question`) then `bot.question.answered` on dev's. The answer comes back to qa-jefe as a message or as a `question_answer` task.

## Turn it off

Fast stop, the switch alone: remove the two drop-ins and restart. From then no run on those nodes is offered the ask tool, no bot there is picked to answer, and the watcher is refused.

```bash
rm ~/.config/systemd/user/axlr-node.service.d/team-channel.conf ~/.config/systemd/user/axlr-node-dev.service.d/team-channel.conf
systemctl --user daemon-reload
systemctl --user restart axlr-node.service axlr-node-dev.service
axlr-bots node get gx10-dev | jq .capabilities.teamChannel   # null
```

Then take the declarations back, so the definitions are as before the channel:

1. `axlr-bots bot pause qa-vigia`.
2. `TEAM_CHANNEL_ASK=` and `TEAM_CHANNEL_ANSWER=` in both `qa.env` and `dev.env`, and each installer again with the same `--bots` (it sends `ask: {teams: []}` to a live bot that had one, since a patch without `ask` keeps it).
3. `axlr-bots group links qa` lists no link.

A question still open waits Pending until its two hours run out, then the asker hears `answered: false`; `axlr-bots task cancel <question task id>` ends it at once.
