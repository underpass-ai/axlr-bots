// Package grpcapi serves the operator API (api/proto/axlrbots/operator/v1)
// over gRPC: one service per file, each calling the same use cases, through
// unixapi.Local, as the HTTP routes of unixapi do. Messages of the contract
// never carry domain types; the mappers in this package are the only place
// that knows both sides.
package grpcapi
