// Package proto holds the operator API's contract: the .proto files in
// axlrbots/operator/v1 and the generation that turns them into the Go code
// in api/gen. See README.md. The package has no code of its own; its test
// compiles and exercises the generated stubs.
package proto

//go:generate go run github.com/bufbuild/buf/cmd/buf@v1.75.0 lint
//go:generate go run github.com/bufbuild/buf/cmd/buf@v1.75.0 generate
