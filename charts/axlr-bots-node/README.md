# axlr-bots-node

The [AXLR Bots](https://github.com/underpass-ai/axlr-bots) node agent as a sidecar beside a service in a pod: `axlr-node` with AXLR's `axlr-tui`, enrolled with a one-time token from a Secret on its first start, its key and certificate on a volume per replica, reading the service's files read-only as a bot's workspace, under a NetworkPolicy. No CRDs.

- `demo.enabled: true` renders a StatefulSet: a service that writes a log, and the sidecar reading it.
- As a dependency, with the demo off, it renders nothing: include `axlr-bots-node.inject.container`, `.volumes`, `.volumeClaimTemplate` and `.egress` in your own pod and policy.

The procedure, the values and the trade-offs are in the [sidecar runbook](https://github.com/underpass-ai/axlr-bots/blob/main/docs/runbooks/sidecar.md).
