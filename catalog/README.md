# Bot catalog

Ready-made bot definitions: each file is one bot `axlr-bots bot register` takes as it is, with a mission, an autonomy level, triggers, tool rules, budgets and a placement chosen for the job. The [bot definition](../docs/reference/bot-definition.md) explains every field; this page says what each bot is for, what it needs on its node and how to install it. `cmd/axlr-bots/catalog_test.go` registers every definition on every CI run, so a file here is one the control plane accepts.

Every bot asks `anthropic/claude-haiku-5.5` through the node's model key, except the two [Jev bots](#senses), which ask no language model. Every bot is in tenant `default`.

## Install a bot

```bash
axlr-bots bot register --file catalog/reader.json      # one file, one bot; the id is the file's name
axlr-bots bot enable reader
axlr-bots task submit reader --objective "List the files in your workspace" --key first-1
```

Before you register, change what is yours:

| In the file | Replace | Why |
|:--|:--|:--|
| `"secret"` or `"signingKey"` of a `webhook` or `alertmanager` trigger | the placeholder `replace-me-with-16-to-256-chars` with your own 16 to 256 characters | the credential the sender presents; the test refuses a catalog file whose credential is not the placeholder, so a real one is never committed |
| `"placement"` | the selector with the labels your nodes carry (`axlr-node --labels hardware=motor`, or the enrollment token's `--labels`), or `{"nodes": ["host-1"]}`, or `{}` for any node | a bot with a selector runs only on a node that matches; one with none runs anywhere |
| `"groups"` | drop it, or rename the team | a team is just a name bots share; a bot alone in a team works as if it had none |
| `"owner"` | who answers for the bot | shown in the panel and the events |

A bot whose tools are not AXLR's own (`local_read`, `local_search`, `local_list`, `local_write`, `local_exec`) calls an **MCP server** the node loads: name the server and its `http(s)` URL in the node's `mcp.json` (`axlr-node --axlr-mcp-config` for an AXLR bot, `--mcp-config` for a Jev bot; [command reference](../docs/reference/cli.md)). The catalog fixes the tools' names and what they take; the server is yours, built as the [input adapters](../docs/architecture/input-adapters.md) ADR describes. Any server that speaks MCP over HTTP will do, whoever wrote it. A Jev bot also needs `TYPESAFE_API_KEY` in its node's environment ([install](../docs/runbooks/install.md)).

A bot at `act-with-approval` waits for a person before every write or command its `autoApprove` does not name; the [approvals runbook](../docs/runbooks/approvals.md) is the procedure. Once a bot has earned it, a patch raises its level: `axlr-bots bot update <bot> --file patch.json` with `{"level": "act-within-policy"}`; the tool rules still bound it.

## The bots

The cost ceiling is the most a task may cost the model: the budget's model calls times the context each may carry, as input tokens. Few tasks reach it; the budget stops one that would. Low is under half a million tokens, medium under two, high above. A Jev bot pays TypeSafe's price per question ([`adapters/jev/judge.go`](../adapters/jev/judge.go)): cents a task.

### Basics

| Bot | For | Trigger | Autonomy | Cost ceiling / task |
|:--|:--|:--|:--|:--|
| [reader](reader.json) | answer questions from the workspace's files | manual, delegation | observe | low (10 × 32k) |
| [writer-with-approval](writer-with-approval.json) | change files in the workspace, each write approved | manual, delegation | act-with-approval | low (10 × 32k) |
| [scheduled-reporter](scheduled-reporter.json) | an hourly report of what changed | timer 1h, manual | observe | low (10 × 32k) |

**reader.** The first bot to register: it reads, searches and lists its workspace and says what it found with file and line. Use it to try the install, and as the target other bots delegate questions to. It changes nothing.

**writer-with-approval.** The same bot one level up, with `local_write`: it changes the files a task names, and each write stops the task until a person approves it in the panel or with `task decide`. Use it for edits you want to see before they land; `.env` files and `.git` are denied.

**scheduled-reporter.** A timer every hour makes a task: what changed in the workspace, what the newest log lines say, what looks wrong, in at most ten lines. Use it to watch a folder a service writes into without a person looking at it; point its workspace at that folder.

### Operations

| Bot | For | Trigger | Autonomy | Cost ceiling / task |
|:--|:--|:--|:--|:--|
| [log-watcher](log-watcher.json) | errors and warnings in the logs every quarter of an hour, told to the team | timer 15m, manual | observe | low (8 × 32k) |
| [incident-triage](incident-triage.json) | find the cause of an alert with evidence; change nothing | alertmanager, incident, manual | observe | medium (16 × 64k) |
| [service-restarter](service-restarter.json) | restart one systemd unit, approved by a person | manual, delegation | act-with-approval | low (8 × 32k) |
| [disk-cleaner](disk-cleaner.json) | free disk space nightly, each removal approved | timer 04:00, manual | act-with-approval | low (12 × 32k) |

**log-watcher.** Reads only `*.log` files and reports what was written since the last run as a table. A service down, a disk or memory exhausted becomes `warn.logs.<service>` on team `ops`, so incident-triage or a person hears it. Use it where a service logs to files and nobody reads them.

**incident-triage.** Point Alertmanager at it ([triggers runbook](../docs/runbooks/triggers.md#point-alertmanager-at-a-bot)) or open an incident by hand: one triage task per alert group, with the journal, the unit's status, the disk and the load as evidence, and the one command a person should run next. It runs nothing but `journalctl`, `systemctl status`, `df` and `uptime`. When the fix is a restart it asks service-restarter, its teammate, where a person still approves.

**service-restarter.** Restarts the one unit a task names, after showing its status and journal, and the restart waits for a person; `axlr-*` units are denied. Use it as the hand incident-triage and a person share: the diagnosis is theirs, the approval is theirs, the typing is the bot's.

**disk-cleaner.** Every night at four: measure (`df`, `du`, `find`), propose, remove only under `/var/tmp` and `/tmp` with one approved `rm` per path, vacuum the journal to two weeks. Above 30 % free it removes nothing. Its approvals wait eight hours, so a morning person decides. `find -delete` and `find -exec` are denied.

### Code and documents

| Bot | For | Trigger | Autonomy | Cost ceiling / task |
|:--|:--|:--|:--|:--|
| [repo-reviewer](repo-reviewer.json) | review the changed files of a repository on each GitHub event | webhook (GitHub signature), manual | observe | medium (20 × 64k) |
| [doc-summariser](doc-summariser.json) | summarise Markdown, text, reStructuredText and AsciiDoc; answer questions from them | manual, delegation | observe | medium (12 × 64k) |
| [changelog-writer](changelog-writer.json) | write the week's `CHANGELOG.md` entry every Monday, approved | timer Monday 09:00, manual | act-with-approval | medium (16 × 64k) |

**repo-reviewer.** Its workspace is a checkout the node keeps up to date; a GitHub webhook signed with its `signingKey` (`X-Hub-Signature-256`) makes a task per event, and the bot reports bugs, missing tests and simplifications with file and line, nothing about style. It posts nowhere: a person posts the review. Use it as the first reader of every pull request.

**doc-summariser.** Reads only documents (`*.md`, `*.txt`, `*.rst`, `*.adoc`) and answers in at most twelve lines with the file and heading each point came from. Use it alone, or as the bot others delegate "what does this document say" to.

**changelog-writer.** Every Monday at nine it reads `commits.log` (the node writes it from the checkout), the changed files and the existing `CHANGELOG.md`, and writes one entry under the date, in a user's words. The single write waits up to four hours for a person. Use it where a changelog is owed and nobody writes it.

### A team

| Bot | For | Trigger | Autonomy | Cost ceiling / task |
|:--|:--|:--|:--|:--|
| [team-supervisor](team-supervisor.json) | give tasks to the workers, retry a failure, halt the team | manual; hears `error.*` as a task | observe | medium (20 × 32k) |
| [team-worker-a](team-worker-a.json) | the writer: changes files, each write approved | manual, from the team | act-with-approval | low (12 × 32k) |
| [team-worker-b](team-worker-b.json) | the researcher: finds facts in the workspace | manual, from the team | observe | low (12 × 32k) |

The three share team `crew`, so each knows the others and may ask them without naming targets ([teams](../docs/reference/bot-definition.md#teams)). Register the three, enable them, and give the supervisor a task: it delegates to the worker that fits, hears the control plane's `error.bot` when a worker's task fails and retries once, and emits `stop.halt` when the same worker fails twice. Use the trio as the template for your own team: swap the workers' missions and tools, keep the supervisor. The [teams and events runbook](../docs/runbooks/teams-and-events.md) shows the panel side.

### Senses

| Bot | For | Trigger | Autonomy | Cost ceiling / task |
|:--|:--|:--|:--|:--|
| [speech-bridge](speech-bridge.json) | speech to text and text to speech in real time | manual, delegation; hears `info.say.*` | act-within-policy | high (60 × 64k) |
| [image-cataloguer](image-cataloguer.json) | file each new image under the category Jev picks | manual, delegation | act-within-policy (Jev) | Jev: 200 questions |
| [emotion-reader](emotion-reader.json) | the emotion in a voice or a face, as a word, in real time | manual, delegation | act-within-policy (Jev) | Jev: 300 questions |

The three share team `senses`. **speech-bridge** calls a speech MCP server (`speech.listen`, `speech.transcribe`, `speech.say`, `speech.stream`, `speech.stop`): a "listen" task transcribes utterance after utterance and tells the team `info.speech.heard`; a teammate's or the operator's `info.say.*` makes it speak. It acts within the policy because a spoken word is not a write; its budget (200 tool calls, 30 minutes) is what ends a listening task. It adapts its tone to what emotion-reader tells it.

**image-cataloguer** and **emotion-reader** are Jev bots ([runtime](../docs/reference/bot-definition.md#runtime)): no language model, a fixed program over one MCP server (`images`, `emotion`), and every choice one question to Jev, answered in about 0.2 s. The server writes the questions (which category, which emotion); the bot carries them. After each decision the bot tells its team (`info.images.labelled`, `info.emotion.read`) with the choice and Jev's probability, so speech-bridge, a display or your own bot acts on it. Their node needs `TYPESAFE_API_KEY` and the server in its `mcp.json`. The samples never leave the node; the word does.

### Edge and robotics

| Bot | For | Trigger | Autonomy | Cost ceiling / task |
|:--|:--|:--|:--|:--|
| [motor-controller](motor-controller.json) | drive motors behind a Pi, an Arduino or an ESP32 | manual, delegation; `stop.*` stops | act-with-approval | low (12 × 32k) |
| [gimbal-controller](gimbal-controller.json) | point a brushless gimbal on an ESP32 | manual, delegation; `stop.*` homes | act-with-approval | low (12 × 32k) |
| [led-matrix-controller](led-matrix-controller.json) | a wall of chained 64×64 LED panels; shows the team's warnings | manual, delegation; hears `warn.*`, `info.show.*` | act-within-policy | low (8 × 32k) |
| [imu-gps-navigator](imu-gps-navigator.json) | position, heading and attitude every five minutes, told to the team | timer 5m, manual, delegation | observe | low (8 × 32k) |
| [ros2-coordinator](ros2-coordinator.json) | look at a ROS 2 graph, change it with approval, ask the hardware bots | manual, delegation; hears `error.*` as a task | act-with-approval | high (40 × 64k) |

The five share team `robot`, and each runs on the node whose label names its hardware (`hardware=motor`, `gimbal`, `led`, `nav`, `ros2`): one Raspberry Pi per device, or one host with all five labels. Each calls an MCP server that owns the device; the catalog fixes the tools (`motor.set_speed`, `gimbal.point`, `led.show_text`, `nav.read_all`, `ros2.echo`, …) and your adapter serves them over the serial, I2C, SPI or ROS link it has.

**motor-controller** and **gimbal-controller** move things, so every move waits for a person: the call names the motor or the angles and why. Their status reads and their stops (`motor.stop`, `gimbal.home`) are approved by policy, and a `stop.*` from the team makes an urgent task that calls them at once. Once the adapter has earned trust, the patch to `act-within-policy` removes the wait.

**led-matrix-controller** only changes pixels, so it acts within the policy. It shows what a task says, and every `warn.*` of the team in red, so the wall is the team's status board.

**imu-gps-navigator** only reads. Its timer tells the team `info.nav.position` every five minutes, so the motor and gimbal bots know where north is; a lost fix or a tilt past 30 degrees is a `warn.nav.*`. Waypoints come from `*.json` files in its workspace. 288 tasks a day: small ones, by design.

**ros2-coordinator** looks at the graph freely (`ros2.nodes`, `topics`, `echo`, `params`, approved by policy) and changes it with approval (`set_param`, `call_service`, `launch`). A move is not its own: it asks motor-controller or gimbal-controller, with an even share of its budget each. `ros2.stop` is approved by policy and it emits `stop.all` when a node it launched dies. Use it as the one bot a person talks to about the robot.

### SRE

| Bot | For | Trigger | Autonomy | Cost ceiling / task |
|:--|:--|:--|:--|:--|
| [sre-aws](sre-aws.json) | the cause of an alert in one AWS account, read-only | alertmanager, incident, manual | observe | medium (24 × 64k) |
| [sre-gcp](sre-gcp.json) | the same for one Google Cloud project | alertmanager, incident, manual | observe | medium (24 × 64k) |
| [sre-azure](sre-azure.json) | the same for one Azure subscription | alertmanager, incident, manual | observe | medium (24 × 64k) |
| [sre-grafana](sre-grafana.json) | the alert's cause in Grafana's metrics and logs; asks the cloud bots | alertmanager, incident, manual | observe | medium (24 × 64k) |
| [network-architect](network-architect.json) | check the network against its design; propose the exact change | manual, delegation | observe | medium (20 × 64k) |
| [nats-operator](nats-operator.json) | a NATS cluster with JetStream: look freely, edit and purge with approval | manual, delegation, incident | act-with-approval | medium (20 × 64k) |

The six share team `sre`. The three cloud bots run `aws`, `gcloud` and `az` on a node already signed in (`cloud=aws`, `gcp`, `azure`), and only their read verbs: `describe`, `list`, `get`, `show`, the logs and the metrics; `delete`, `create`, `update`, `terminate` and their kin are denied twice, by the allow rules and by a deny. Each reports what fails, since when, the evidence, the cause and the exact command a person should run. Their runbooks live in the workspace. Use one per account, under Alertmanager, with sre-grafana in front.

**sre-grafana** reads Grafana through an MCP server (`grafana.query_prometheus`, `grafana.query_loki`, the dashboards, the alert rules) and, when the cause is in a cloud, asks the teammate that owns the account and merges its view. It tells the team whether the alert is real (`warn.observability.*`) or noise, with the rule to fix.

**network-architect** holds the network's design in its workspace (address plan, VLANs, firewall rules, zone files) and measures the host with `ip`, `ss`, `dig`, `nft list`, `traceroute` and `ping -c`; it answers with a text diagram, what differs from the design and the configuration lines to change, which a person applies. `ip add`, `del`, `set` and `flush` are denied.

**nats-operator** runs `nats` against the context the node is pointed at: `server`, `stream` and `consumer` reads are approved by policy; `stream edit`, `consumer edit`, `stream purge` and `consumer rm` wait for a person; `stream rm` and `--force` are denied. Use it for a lagging consumer or a stream near its limit.

### Data

| Bot | For | Trigger | Autonomy | Cost ceiling / task |
|:--|:--|:--|:--|:--|
| [dba-postgres](dba-postgres.json) | PostgreSQL: plans, locks, bloat; indexes and maintenance with approval | manual, delegation, incident | act-with-approval | medium (20 × 64k) |
| [dba-redis](dba-redis.json) | Redis: memory, latency, slow log; config and expiries with approval | manual, delegation, incident | act-with-approval | medium (20 × 64k) |
| [dba-document-db](dba-document-db.json) | MongoDB and its kin through one MCP server; indexes and bulk changes with approval | manual, delegation, incident | act-with-approval | medium (20 × 64k) |
| [dba-sqlite](dba-sqlite.json) | the SQLite files in the workspace; indexes and maintenance with approval | manual, delegation | act-with-approval | medium (20 × 64k) |

The four share team `data`. Each reads freely, approved by policy, and changes only what a task asks, with a person's approval: an index, a `VACUUM`, a `CONFIG SET`, an expiry. Deletes of data, drops and truncates are denied in every one; a person runs those.

**dba-postgres** runs every statement as `psql -X -At -c '<statement>'` with the connection the node's environment gives (never a password in a call): `SELECT`, `EXPLAIN` and `SHOW` by policy; `VACUUM`, `ANALYZE`, `REINDEX`, `CREATE INDEX CONCURRENTLY` and `ALTER` with approval. **dba-redis** runs `redis-cli` with the command first, pointed at the instance by the node's environment: `INFO`, `SLOWLOG`, `MEMORY`, `SCAN` and the rest of the reads by policy, `CONFIG SET`, `EXPIRE`, `DEL` and `CLIENT KILL` with approval, `KEYS` and `FLUSH*` denied. **dba-document-db** speaks to the database through a `docdb` MCP server, so one bot serves MongoDB, DocumentDB, Cosmos DB, CouchDB or Firestore. **dba-sqlite** works on the `*.db` files in its workspace with `sqlite3 -readonly -json` for every read and `sqlite3 -json` for the approved maintenance; `ATTACH` is denied.

## What a bot needs on its node

| Bot | Programs or MCP server on the node | Node labels |
|:--|:--|:--|
| reader, writer-with-approval, scheduled-reporter, doc-summariser, repo-reviewer, changelog-writer, team-* | AXLR's own tools only | any node |
| log-watcher | AXLR's own tools; `*.log` files in the workspace | any node |
| incident-triage, service-restarter, disk-cleaner | `journalctl`, `systemctl`, `df`, `du`, `find`, `rm`, `uptime` | any node |
| speech-bridge | MCP server `speech` | `role=media` |
| image-cataloguer, emotion-reader | MCP servers `images`, `emotion`; `TYPESAFE_API_KEY` | `role=media` |
| motor-controller, gimbal-controller, led-matrix-controller, imu-gps-navigator, ros2-coordinator | MCP servers `motor`, `gimbal`, `led`, `nav`, `ros2` | `hardware=motor`, `gimbal`, `led`, `nav`, `ros2` |
| sre-aws, sre-gcp, sre-azure | `aws`, `gcloud`, `az`, signed in | `cloud=aws`, `gcp`, `azure` |
| sre-grafana | MCP server `grafana` | `role=observability` |
| network-architect | `ip`, `ss`, `resolvectl`, `dig`, `nft`, `traceroute`, `ping` | any node |
| nats-operator | `nats` with its context | `role=messaging` |
| dba-postgres, dba-redis | `psql` with `PG*` and a password file; `redis-cli` with `REDISCLI_AUTH` | `role=db` |
| dba-document-db | MCP server `docdb` | `role=db` |
| dba-sqlite | `sqlite3`; the `*.db` files in the workspace | any node |

A bot that names an MCP server is a contract for an adapter you build: the tool names and what each takes are in the bot's mission. The private repository `axlr-bots-examples` has adapters of this shape (`world-adapter/`, the drone fleet's companion computer, the orchestra's score service).

## Keep it valid

```bash
GOFLAGS=-p=2 go test -count=1 -run TestCatalog ./cmd/axlr-bots/
```

The test registers every file through the use case and through `bot register`, in one control plane, and fails when a definition is refused, a file is not named after its bot, a bot delegates to one outside the catalog or that takes no delegated task, a credential is not the placeholder, a Jev bot is not at `act-within-policy` with its program, or this README lists a file that does not exist or misses one that does. Add a bot by adding its file and its row.
