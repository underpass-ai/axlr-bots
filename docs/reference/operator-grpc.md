# Operator API over gRPC

The operator API of the control plane as gRPC services. The contract is [`api/proto/axlrbots/operator/v1`](../../api/proto/axlrbots/operator/v1); the Go code generated from it is in `api/gen/axlrbots/operator/v1` (package `operatorv1`). The HTTP routes of the [operator API](operator-api.md) keep working beside it.

## Services

| Service | Does |
|---|---|
| `BotsService` | register, read, update, stage, enable, pause, stop and delete bots; their timeline (`WatchBotEvents` streams it); submit a task on a bot |
| `TasksService` | list, read, submit, run, approve, deny, decide, cancel and message tasks; their timeline (`WatchTaskEvents` streams it), executions and report |
| `GroupsService` | teams, their topics and timeline (`WatchGroupEvents` streams it); publish an operator event |
| `ExecutionsService` | read an execution; download an artifact or a transcript in chunks of at most 64 KiB |
| `NodesService` | read nodes, mint an enrollment token, revoke a node |
| `OverviewService` | the dashboard counts; which build runs |
| `ActivityService` | a task's whole activity tree, once (`GetTaskActivity`) or followed live (`WatchTaskActivity`); see [task activity](../architecture/task-activity.md). The snapshot is one message, so the server's send limit is 64 MiB instead of gRPC's 4 MiB default: a client sets its receive limit to match, or follows the watch, which sends small updates |

Every RPC calls the same use case as its HTTP route, so the rules are the HTTP ones: the same validation, idempotency keys, redaction of trigger secrets, the approvers check and the list limits (zero is 100, at most 1000).

## Listeners

| Listener | Flag | Credential |
|---|---|---|
| Unix socket `<state>/axlr-bots-grpc.sock` (mode 0660; `serve --grpc-socket`, `AXLR_BOTS_GRPC_SOCKET`) | always | the socket's permissions; no token |
| Mutual TLS 1.3 address | `serve --grpc-listen <addr>` (`AXLR_BOTS_GRPC_LISTEN`) | an operator's client certificate **and** `authorization: Bearer <token>` |

The certificate of the TLS address is issued by the fleet CA (`ca init`; clients check it with `<state>/ca/ca.crt`). The token is the operator token of `--api-listen`: `<state>/api.token` (`--api-token-file`), generated at the first start when either flag is used. `--grpc-listen` needs no `--api-listen` and the other way round. The HTTP operator address (`--api-listen`) is unchanged: bearer token only.

### Operator certificates

The address is mutual TLS ([ADR](../architecture/operator-mtls.md)). A client without a certificate of the fleet CA, or with one of another CA, fails the handshake. A certificate of the fleet CA that is not an operator's is refused on the first call with `PERMISSION_DENIED` (`kind: forbidden`): a node's, and the service certificates of `ca issue-client`. The token is the second factor: the certificate alone is `UNAUTHENTICATED` (`unauthorized`).

An operator's certificate is the one `axlr-bots ca operator issue <name>` writes. It carries three marks that no other certificate of the CA has, and all three must agree:

| Mark | Value |
|---|---|
| organisation | `axlr-bots operators` (nodes: `axlr-bots nodes`; services: `axlr-bots clients`) |
| URI SAN | `axlr-bots:operator:<name>` |
| common name | `<name>` |

```sh
# on the control plane's host: writes operator.key (0600), operator.crt and ca.crt
axlr-bots ca operator issue alice --out /tmp/alice        # --lifetime 720h; default 90 days, without --out ~/.config/axlr-bots/operator
# revoke it, by file or by the serial `issue` printed (the daemon sees it on the next call)
axlr-bots ca operator revoke --cert-file /tmp/alice/operator.crt
axlr-bots ca operator revoke --serial 5f3a… --name alice
```

Revocation writes the serial to `<state>/pki/revoked.json`, the denylist node revocations use; it is read on each call and a revoked certificate gets `PERMISSION_DENIED` with `revoked: true` in the `ErrorDetail`. The names are `[A-Za-z0-9][A-Za-z0-9._-]{0,62}`; issue another certificate for the same name after a revocation (a leaked key is revoked by serial, the name stays usable).

The operator named by the certificate is who acts. `x-axlr-actor` is optional: absent, the actor is the certificate's name; present, it must be that name, and any other value is `PERMISSION_DENIED` (a certificate cannot decide in another person's name, so a bot's `approvers` become a fact for the gRPC address, not only a word). `DecideTask`'s `decided_by` is compared with the actor as before. The Unix socket has no certificate and keeps the asserted actor.

The CLI and the panel present the certificate for you ([CLI](cli.md#the-grpc-connection), [panel](../../panel/README.md)). With grpcurl add `-cert operator.crt -key operator.key`.

## Calling it

With [grpcurl](https://github.com/fullstorydev/grpcurl), from the repository (the server has no reflection, so name the proto):

```sh
TOKEN=$(cat ~/.local/state/axlr-bots/api.token)
CA=~/.local/state/axlr-bots/ca/ca.crt
CERT=~/.config/axlr-bots/operator/operator.crt KEY=~/.config/axlr-bots/operator/operator.key
PROTO="-import-path api/proto -proto axlrbots/operator/v1/bots.proto"

# list the bots
grpcurl -cacert "$CA" -cert "$CERT" -key "$KEY" -H "authorization: Bearer $TOKEN" $PROTO \
  control.example:7880 axlrbots.operator.v1.BotsService/ListBots

# submit a task
grpcurl -cacert "$CA" -cert "$CERT" -key "$KEY" -H "authorization: Bearer $TOKEN" $PROTO \
  -d '{"bot_id":"bot-a","objective":"look at the disks","idempotency_key":"k-1"}' \
  control.example:7880 axlrbots.operator.v1.BotsService/SubmitBotTask

# follow a bot's timeline until Ctrl-C
grpcurl -cacert "$CA" -cert "$CERT" -key "$KEY" -H "authorization: Bearer $TOKEN" $PROTO \
  -d '{"bot_id":"bot-a","replay_limit":20}' \
  control.example:7880 axlrbots.operator.v1.BotsService/WatchBotEvents

# on the same host, through the socket: no TLS, no token
grpcurl -plaintext -unix $PROTO \
  ~/.local/state/axlr-bots/axlr-bots-grpc.sock axlrbots.operator.v1.BotsService/ListBots
```

For the other services pass their file too (`tasks.proto`, `groups.proto`, `executions.proto`, `nodes.proto`, `overview.proto`).

A Go client dials with `credentials.NewTLS` over a pool holding the fleet CA and the operator certificate in `Certificates`, and adds the token to every call's metadata (`metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)`).

## Who decides

`ApproveTask`, `DenyTask` and `DecideTask` read the actor from the metadata key `x-axlr-actor` (`-H "x-axlr-actor: alice"`), which a bot's approvers are compared with. `DecideTask`'s `decided_by` says the same: when both are given and differ the call fails with `INVALID_ARGUMENT`. With neither, the actor is the control plane's own, which a bot that lists approvers refuses (`PERMISSION_DENIED`).

## Errors

Every failure is a gRPC status with an `axlrbots.operator.v1.ErrorDetail` in its details (`kind`, `message`, and `resource` and `id` for a not-found):

| Kind | Code |
|---|---|
| `not_found` | `NOT_FOUND` |
| `validation` | `INVALID_ARGUMENT` |
| `state`, `approval_expired` | `FAILED_PRECONDITION` |
| `conflict` | `ABORTED` |
| `forbidden` | `PERMISSION_DENIED` |
| `unauthorized` | `UNAUTHENTICATED` |
| `too_large` | `RESOURCE_EXHAUSTED` |
| `internal` | `INTERNAL` |

A missing or wrong token is `UNAUTHENTICATED` on every RPC, streams included.

## Streams

`WatchTaskEvents` and `WatchGroupEvents` send the events after `after`, then each new one. `WatchBotEvents` sends the newest `replay_limit` events, then each new one. They wait on the same change signal as the `?wait` long polls of the HTTP API and look again every 5 seconds; they end when the client cancels or the server stops (five seconds' grace). `GetArtifact` and `GetTranscript` send the listing first, then the content in chunks of at most 64 KiB; check `size` and `sha256`.

## Metrics

With `--metrics-listen` the server also counts its calls: `axlr_bots_grpc_requests_total{service,method,code}` and `axlr_bots_grpc_request_duration_seconds{service,method,code}` (`code` is the gRPC code's name, such as `OK` or `NotFound`). Streams are counted when they end and left out of the histogram.
