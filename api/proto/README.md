# Operator API contract

The operator API of AXLR Bots (what the command line, the panel and other
operators use to drive the control plane) is gRPC. The contract is the
`.proto` files in `axlrbots/operator/v1`; the Go code generated from them is
in `../gen/axlrbots/operator/v1` and is committed.

| file | what |
|------|------|
| `common.proto` | auth, errors and conventions (read its header), `Event`, `Usage`, `Artifact` |
| `bot_model.proto`, `task_model.proto` | the bot, task and execution messages |
| `bots.proto` `tasks.proto` `executions.proto` `nodes.proto` `groups.proto` `overview.proto` `activity.proto` | one service each |

Not in this contract, and still HTTP: the node protocol (`/v1/enroll`,
`/v1/nodes/{id}/...`), webhook triggers (`/v1/triggers/...`), `/healthz`,
`/metrics`.

## Regenerate

After any change to a `.proto` file:

    make proto          # or: go generate ./api/proto

It runs `buf lint` and `buf generate`. Both come from the module cache
(`go run ...buf@v1.75.0`, pinned in `doc.go`), and the two plugins
(`protoc-gen-go`, `protoc-gen-go-grpc`) are `tool` directives of `go.mod`,
so nothing has to be installed and the output is the same on every machine.
CI runs the same command and fails if `git status` shows a difference in
`api/`.

Change a plugin or buf version by editing `go.mod` (`go get -tool ...@vX`)
or `doc.go`, then regenerate in the same commit.

## Compatibility

While the API has no clients outside this repository, change `v1` in place.
Once it has, add `buf breaking --against` to the CI step and keep to
additive changes.
