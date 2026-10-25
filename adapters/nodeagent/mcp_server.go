package nodeagent

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"strings"
	"sync"
)

// mcpServer serves one tool to one execution over a Unix socket, as an
// MCP server speaks over stdio: one JSON-RPC message per line. AXLR
// reaches it through `axlr-node mcp-relay`, a pipe between the relay's
// stdin and stdout and the socket, so the socket stands for the
// execution and needs no credential of its own.
type mcpServer struct {
	path     string
	listener net.Listener
	tool     mcpTool
	call     func(ctx context.Context, arguments json.RawMessage) mcpToolResult
	version  string
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

// serveMCP listens on path (a stale socket there is replaced) and
// serves every connection until Close.
func serveMCP(ctx context.Context, path string, tool mcpTool, version string, call func(context.Context, json.RawMessage) mcpToolResult) (*mcpServer, error) {
	if strings.Contains(path, "..") || !strings.HasSuffix(path, ".sock") {
		return nil, errors.New("a socket path ends in .sock and holds no ..")
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = listener.Close()
		return nil, err
	}
	serverCtx, cancel := context.WithCancel(ctx)
	s := &mcpServer{path: path, listener: listener, tool: tool, call: call, version: version, ctx: serverCtx, cancel: cancel}
	s.wg.Add(1)
	go s.accept()
	return s, nil
}

func (s *mcpServer) accept() {
	defer s.wg.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			c := &mcpConnection{server: s, conn: conn, inFlight: map[string]context.CancelFunc{}}
			c.serve(s.ctx)
		}()
	}
}

// Close stops listening, ends every call in flight and removes the
// socket.
func (s *mcpServer) Close() {
	s.cancel()
	_ = s.listener.Close()
	s.wg.Wait()
	_ = os.Remove(s.path)
}
