package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
)

func executionGet(ctx context.Context, service unixapi.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "axlr-bots: execution get needs exactly one <execution-id>")
		return exitUsage
	}
	execution, err := service.GetExecution(ctx, args[0])
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, execution)
	return exitOK
}

// executionArtifact writes an artifact the control plane keeps to stdout
// or to --out, and checks its bytes against the SHA-256 it was kept with.
func executionArtifact(ctx context.Context, service unixapi.Service, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("execution artifact", flag.ContinueOnError)
	flags.SetOutput(stderr)
	out := flags.String("out", "", "write the artifact to this file instead of stdout")
	if err := flags.Parse(args); err != nil {
		return exitUsage
	}
	if flags.NArg() != 2 {
		fmt.Fprintln(stderr, "axlr-bots: execution artifact needs <execution-id> and <name>")
		return exitUsage
	}
	content, err := service.GetArtifact(ctx, flags.Arg(0), flags.Arg(1))
	if err != nil {
		return fail(stderr, err)
	}
	defer content.Body.Close()
	sink := stdout
	if *out != "" {
		file, err := os.Create(*out)
		if err != nil {
			return fail(stderr, err)
		}
		defer file.Close()
		sink = file
	}
	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(sink, hash), content.Body); err != nil {
		return fail(stderr, err)
	}
	if got := hex.EncodeToString(hash.Sum(nil)); content.Artifact.SHA256 != "" && got != content.Artifact.SHA256 {
		return fail(stderr, fmt.Errorf("artifact %s: the bytes hash to %s, it was kept as %s", flags.Arg(1), got, content.Artifact.SHA256))
	}
	if *out != "" {
		fmt.Fprintf(stdout, "%s (%s, sha256 %s)\n", *out, content.Artifact.MediaType, content.Artifact.SHA256)
	}
	return exitOK
}
