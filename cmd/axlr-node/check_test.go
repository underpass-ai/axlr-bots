package main

import (
	"bytes"
	"context"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckAnswersWithTheFleetCA(t *testing.T) {
	operator, url, ca := tlsControlPlane(t)
	caFile := filepath.Join(t.TempDir(), "ca.crt")
	_ = os.WriteFile(caFile, ca.CertificatePEM(), 0o644)
	tokenFile, _ := mintToFile(t, operator, "probe-0")
	state := filepath.Join(t.TempDir(), "node")
	if _, err := (firstStart{Control: url, TokenFile: tokenFile, StateDir: state}).ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	for name, args := range map[string][]string{
		"ca file":    {"--ca-file", caFile},
		"token file": {"--token-file", tokenFile},
		"state":      {"--state", state},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(append([]string{"check", "--control", url}, args...), &stdout, &stderr); code != exitOK || !strings.Contains(stdout.String(), `"status": "ok"`) {
			t.Errorf("%s: %d %s %s", name, code, stdout.String(), stderr.String())
		}
	}
	// What does not hold is an error.
	junk := filepath.Join(t.TempDir(), "junk")
	_ = os.WriteFile(junk, []byte("nope"), 0o600)
	wrong := filepath.Join(t.TempDir(), "wrong.crt")
	_ = os.WriteFile(wrong, []byte("not a certificate"), 0o644)
	for name, tc := range map[string]struct {
		args []string
		code int
		text string
	}{
		"no CA":              {[]string{"check", "--control", url, "--state", t.TempDir()}, exitError, "no CA"},
		"junk token":         {[]string{"check", "--control", url, "--token-file", junk}, exitError, "enrollment token"},
		"missing token":      {[]string{"check", "--control", url, "--token-file", filepath.Join(t.TempDir(), "none")}, exitError, "no such file"},
		"missing CA file":    {[]string{"check", "--control", url, "--ca-file", filepath.Join(t.TempDir(), "none")}, exitError, "no such file"},
		"not a CA":           {[]string{"check", "--control", url, "--ca-file", wrong}, exitError, "no certificate"},
		"nobody there":       {[]string{"check", "--control", "https://127.0.0.1:1", "--ca-file", caFile}, exitError, "connection refused"},
		"not https":          {[]string{"check", "--control", "unix:///x.sock"}, exitUsage, "https://"},
		"arguments":          {[]string{"check", "extra"}, exitUsage, "flags only"},
		"unknown flag":       {[]string{"check", "--bogus"}, exitUsage, "flag provided"},
		"help is not wrong":  {[]string{"check", "--help"}, exitOK, ""},
		"bad control (URL)":  {[]string{"check", "--control", "https://\x7f", "--ca-file", caFile}, exitError, "invalid"},
		"untrusted (no SAN)": {[]string{"check", "--control", strings.Replace(url, "127.0.0.1", "localhost", 1), "--ca-file", caFile}, exitError, "certificate"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(tc.args, &stdout, &stderr); code != tc.code || !strings.Contains(stderr.String(), tc.text) {
			t.Errorf("%s: %d %q", name, code, stderr.String())
		}
	}
}

func TestCheckHealthReadsTheAnswer(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		body   string
		text   string
	}{
		"not ok":   {http.StatusServiceUnavailable, `{"status":"down"}`, "503"},
		"not json": {http.StatusOK, `<html>`, "/healthz"},
	} {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		}))
		caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
		if _, err := checkHealth(context.Background(), server.URL, caPEM); err == nil || !strings.Contains(err.Error(), tc.text) {
			t.Errorf("%s: %v", name, err)
		}
		server.Close()
	}
}
