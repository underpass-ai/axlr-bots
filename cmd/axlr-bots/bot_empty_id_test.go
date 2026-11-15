package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
)

func TestBotCommandsRejectEmptyID(t *testing.T) {
	commands := map[string]func(context.Context, unixapi.Service, []string, io.Writer, io.Writer) int{
		"pause":  botPause,
		"enable": botEnable,
	}
	for name, command := range commands {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := command(context.Background(), nil, []string{""}, &stdout, &stderr)
			if code != exitUsage {
				t.Errorf("exit code = %d, want %d", code, exitUsage)
			}
			if !strings.Contains(stderr.String(), "bot id must not be empty") {
				t.Errorf("stderr = %q, want an empty bot ID diagnostic", stderr.String())
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty", stdout.String())
			}
		})
	}
}
