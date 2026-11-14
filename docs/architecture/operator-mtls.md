# Operator mTLS

Decision of 14 November 2026. The gRPC operator address (`serve --grpc-listen`) takes only operators that present a client certificate of the fleet CA marked as an operator's, and the bearer token on top of it.

## Why

Until now the address was TLS with the fleet CA on the server side only and the bearer token as the sole credential (`VerifyClientCertIfGiven`, and the panel trusted the CA without presenting anything). A copied token was enough, and the actor a decision is recorded under was whatever the caller wrote in `x-axlr-actor`, so a bot's `approvers` ([approvers](approvers.md), D1) were a word, not a fact.

## Decisions

| | |
|:--|:--|
| M1 | **Mutual TLS, same PKI.** The gRPC TLS address requires a client certificate that chains to the fleet CA (`tls.RequireAndVerifyClientCert`), TLS 1.3. The server certificate is the one the node and HTTP addresses already use. The configuration is a clone of the daemon's, so the node address (`--listen`) and the HTTP operator address (`--api-listen`) are untouched |
| M2 | **An operator's certificate cannot be mistaken for another kind.** `ca operator issue <name>` signs a certificate with organisation `axlr-bots operators`, a URI SAN `axlr-bots:operator:<name>` and common name `<name>`. A node's has organisation `axlr-bots nodes` and no URI (set by the CA, whatever the request says); a service's (`ca issue-client`) has `axlr-bots clients`. The gRPC interceptor accepts a certificate only when all three operator marks agree, so neither a node's nor a service's passes as an operator's, and an operator's is no node's (`IsNodeCertificate` looks at the organisation). The marks are the CA's to set: no request carries them |
| M3 | **The token stays.** The bearer token is the second factor (`authorization: Bearer`): a stolen key without the token, or a stolen token without the key, opens nothing. The certificate is checked first |
| M4 | **The actor is the certificate's.** The operator's name is the actor of every call. `x-axlr-actor` is optional; if sent it must equal the name, or the call is `PERMISSION_DENIED`. The alternative, letting the header override the name, would keep decisions as unproven as before. The Unix socket has no certificate and keeps the asserted actor. `DecideTask`'s `decided_by` is compared with the actor as before |
| M5 | **Revocation reuses the denylist.** `ca operator revoke` (by certificate or serial) writes the serial to `<state>/pki/revoked.json`; the interceptor reads it on every call, as node authentication does, so a revocation takes effect on the next call without a restart. Serials are 128 random bits, so operator and node serials cannot collide. The entry's node field reads `operator:<name>` |
| M6 | **Short lives, no CRL.** An operator certificate lasts 90 days by default (a node's: 365), as the key sits on a person's machine. There is no CRL or OCSP: the denylist is checked by this server only, which is the only party that needs it |
| M7 | **Both clients present it.** The CLI (`--cert-file`, `--key-file`; `AXLR_BOTS_CERT_FILE`, `AXLR_BOTS_KEY_FILE`) and the panel (`controlPlane.certFile`, `controlPlane.keyFile`) take the files, and by default look where `ca operator issue` leaves them for the local operator: `$AXLR_BOTS_OPERATOR_DIR` or `<user config>/axlr-bots/operator/operator.{crt,key}`. With no file, each says how to issue one |

## The layers

```text
cmd/axlr-bots       ca operator issue|revoke        remote_control (CLI client certificate)
adapters/pki        IssueOperator, OperatorName     OperatorCertificates (marks + denylist)
adapters/grpcapi    ServeTLS (RequireAndVerify)     certificateInterceptor → actorInterceptor
adapters/daemon     wires the policy with the denylist the nodes use
panel/adapters/grpc ClientTlsConfig::identity       StatusError (refused certificate)
```

`grpcapi` knows a port (`OperatorCertificates`), not the PKI; `ServeTLS` refuses to serve without it, so the address cannot be left open by omission. The interceptor order is: metrics, errors, certificate, token, actor.

## What is left out

- The HTTP operator address (`--api-listen`) and the node protocol address are unchanged: bearer token and optional client certificate as before.
- No per-operator roles. Every operator may do what the token allowed; the certificate says who, not what.
- No automatic renewal of operator certificates; issue a new one before the 90 days end.
- The chart changes nothing: the address is the same port, now mutual TLS (documented in its README). Issue certificates with `kubectl exec` into the pod or from a copy of the CA.
