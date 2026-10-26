# Contributing

- **Layout**: hexagonal. `domain` (aggregates, value objects, events) depends on nothing; `application` holds the ports and the use cases; `adapters/*` implement the ports; `compose` wires them; `cmd/*` are the two commands. Dependencies point inward only.
- **One file per type**: a package has one Go file per type, named after it (`bot_repository.go`, `submit_task.go`); tests sit beside the code as `*_test.go`.
- **Coverage**: every package keeps at least 80 % statement coverage, and CI gates the total at 80 %. Tests run against the stand-ins (`internal/fakeaxlr`, the helper process) by default; the live tests need `AXLR_LIVE=1` or `AXLR_BOTS_LIVE=1` and a real `axlr-tui`.
- **Before you push**: `gofmt -l .` prints nothing, `go vet ./...` is clean, `go build ./... && go test -race -count=1 ./...` is green. On a small machine use `GOFLAGS=-p=2` and run one `go test` at a time.
- **The panel** (`panel/`, Rust): `make check` and `make deny` in `panel/` are green before you push; its CI is the Panel workflow, which runs when `panel/` or the operator API changes.
- **Commits**: one imperative sentence that says what the change does, no prefix or ticket tag (`Serve the operator routes on an optional TLS address`). Keep each commit buildable.
- **Docs**: a change in behaviour updates the page under `docs/` that describes it; the README stays short and points there.
- **Licence**: contributions are accepted under the Apache License 2.0 of this repository.
- **Pull requests**: open them against `main`; CI (format, vet, build, race tests, coverage gate) must be green before the merge.
