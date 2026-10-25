package main

import (
	"bufio"
	"bytes"
	"net"
	"os"
	"strings"
	"testing"
)

func TestMCPRelayPipesStdioToTheSocket(t *testing.T) {
	dir, err := os.MkdirTemp("", "axr")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := dir + "/x.sock"
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		scanner := bufio.NewScanner(conn)
		for scanner.Scan() {
			_, _ = conn.Write([]byte(strings.ToUpper(scanner.Text()) + "\n"))
		}
	}()
	var stdout, stderr bytes.Buffer
	if code := mcpRelay([]string{"--socket", socket}, strings.NewReader("ping\npong\n"), &stdout, &stderr); code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if stdout.String() != "PING\nPONG\n" {
		t.Errorf("stdout = %q", stdout.String())
	}
	if code := run([]string{"mcp-relay", "--socket", dir + "/missing.sock"}, &stdout, &stderr); code != exitError || !strings.Contains(stderr.String(), "mcp-relay") {
		t.Errorf("missing socket: %d %s", code, stderr.String())
	}
	for _, args := range [][]string{{}, {"--socket", socket, "extra"}, {"--nope"}} {
		if code := mcpRelay(args, strings.NewReader(""), &stdout, &stderr); code != exitUsage {
			t.Errorf("%v: exit %d", args, code)
		}
	}
	if code := mcpRelay([]string{"-h"}, strings.NewReader(""), &stdout, &stderr); code != exitOK {
		t.Errorf("help: exit %d", code)
	}
}
