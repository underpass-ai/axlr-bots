package kmpchannel

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// protocolVersion is the MCP revision the session opens; kmp-mcp 0.26.0
// answers it (checked on 10 November 2026, the cut's step 0).
const protocolVersion = "2025-06-18"

// maxLine is the longest line the session reads from the server: a write's
// answer is a few KiB.
const maxLine = 4 << 20

// mcpSession is one MCP conversation over a server's stdin and stdout:
// newline-delimited JSON-RPC 2.0, one request at a time, as kmp-mcp
// serves it. It opens with initialize, then calls tools.
type mcpSession struct {
	in   io.Writer
	out  *bufio.Reader
	next int
}

func newMCPSession(in io.Writer, out io.Reader) *mcpSession {
	return &mcpSession{in: in, out: bufio.NewReaderSize(out, 64<<10)}
}

// open initializes the session and says so.
func (s *mcpSession) open(client, version string) error {
	params := map[string]any{"protocolVersion": protocolVersion, "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": client, "version": version}}
	if _, err := s.request("initialize", params); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	return s.send(rpcMessage{JSONRPC: "2.0", Method: "notifications/initialized"})
}

// callTool calls one tool and returns its text; a tool's error comes back
// as an error with the tool's text.
func (s *mcpSession) callTool(name string, arguments map[string]any) (string, error) {
	raw, err := s.request("tools/call", map[string]any{"name": name, "arguments": arguments})
	if err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	var result toolResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("%s: unreadable result: %w", name, err)
	}
	var texts []string
	for _, item := range result.Content {
		texts = append(texts, item.Text)
	}
	text := strings.Join(texts, "\n")
	if result.IsError {
		return "", fmt.Errorf("%s refused: %s", name, firstLine(text))
	}
	return text, nil
}

// request sends one request and reads until its answer, skipping the
// server's notifications and requests.
func (s *mcpSession) request(method string, params any) (json.RawMessage, error) {
	s.next++
	id := s.next
	if err := s.send(rpcMessage{JSONRPC: "2.0", ID: &id, Method: method, Params: params}); err != nil {
		return nil, err
	}
	for {
		line, err := s.out.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			line, err = s.readLong(line)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, errors.New("the server closed before answering")
			}
			return nil, err
		}
		var message rpcMessage
		if json.Unmarshal(line, &message) != nil || message.ID == nil || *message.ID != id || message.Method != "" {
			continue
		}
		if message.Error != nil {
			return nil, fmt.Errorf("rpc error %d: %s", message.Error.Code, message.Error.Message)
		}
		return message.Result, nil
	}
}

// readLong reads the rest of a line longer than the buffer, up to maxLine.
func (s *mcpSession) readLong(start []byte) ([]byte, error) {
	line := append([]byte(nil), start...)
	for {
		chunk, err := s.out.ReadSlice('\n')
		line = append(line, chunk...)
		if len(line) > maxLine {
			return nil, errors.New("the server's answer is too long")
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			return line, err
		}
	}
}

// send writes one message as one line.
func (s *mcpSession) send(message rpcMessage) error {
	raw, err := json.Marshal(message)
	if err != nil {
		return err
	}
	_, err = s.in.Write(append(raw, '\n'))
	return err
}

// firstLine is a tool's text cut to its first line and 300 characters,
// enough for a log line.
func firstLine(text string) string {
	text, _, _ = strings.Cut(strings.TrimSpace(text), "\n")
	if runes := []rune(text); len(runes) > 300 {
		text = string(runes[:300]) + "…"
	}
	return text
}
