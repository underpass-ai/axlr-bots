package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBotRegisterRejectsStringModelWithGuidance(t *testing.T) {
	file := filepath.Join(t.TempDir(), "bot.json")
	if err := os.WriteFile(file, []byte(`{"id":"example","name":"Example","owner":"operator","mission":"test","model":"qa/fake"}`), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := botRegister(context.Background(), nil, []string{"--file", file}, &stdout, &stderr); code == 0 {
		t.Fatal("invalid model was accepted")
	}
	message := stderr.String()
	if !strings.Contains(message, "model must be an object") || !strings.Contains(message, `"model": {"model": "provider/model"}`) {
		t.Fatalf("missing model guidance: %s", message)
	}
	if strings.Contains(message, "BotDefinition") || strings.Contains(message, "ModelPolicyDefinition") {
		t.Fatalf("internal types leaked: %s", message)
	}
}
