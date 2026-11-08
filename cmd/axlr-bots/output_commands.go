package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	"github.com/underpass-ai/axlr-bots/application"
)

// stdin is what --url-file - and --secret-file - read; a test replaces it.
var stdin io.Reader = os.Stdin

// maxSecretFileBytes bounds what a --url-file or --secret-file holds.
const maxSecretFileBytes = 8 << 10

// outputAdd keeps a new incident output target. The URL and the secret
// may come from files (or stdin, -) so that they stay out of the shell's
// history and the process list; a command target's program and its
// arguments come after --.
func outputAdd(ctx context.Context, service unixapi.Service, args []string, stdout, stderr io.Writer) int {
	args, program := splitCommand(args)
	flags := flag.NewFlagSet("output add", flag.ContinueOnError)
	flags.SetOutput(stderr)
	file := flags.String("file", "", "the whole definition as JSON (name, type, url, secret, command, events, tenants, severities, panelUrl)")
	kind := flags.String("type", "", "webhook, slack or command")
	targetURL := flags.String("url", "", "where a webhook or slack target posts (a slack URL is its secret: prefer --url-file)")
	urlFile := flags.String("url-file", "", "a file holding the URL, - for stdin")
	secretFile := flags.String("secret-file", "", "a file holding a webhook's signing secret (16 to 256 characters), - for stdin")
	events := flags.String("events", "", "incident events, comma-separated, short (opened) or full (incident.opened); default opened,diagnosed,proposed,applying,verifying,resolved,escalated")
	tenants := flags.String("tenants", "", "only these tenants, comma-separated (default all)")
	severities := flags.String("severities", "", "only these severities, comma-separated: sev1..sev4 (default all)")
	panelURL := flags.String("panel-url", "", "the panel base this target links to, <base>/incidents/<id> (default serve --panel-url)")
	positional, ok := parseAnywhere(flags, args)
	if !ok {
		return exitUsage
	}
	definition := application.OutputDefinition{}
	if *file != "" {
		raw, err := os.ReadFile(*file)
		if err != nil {
			return fail(stderr, err)
		}
		decoder := json.NewDecoder(bytesReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&definition); err != nil {
			return fail(stderr, fmt.Errorf("%s: %w", *file, err))
		}
	}
	if len(positional) > 1 || (len(positional) == 0 && definition.Name == "") {
		fmt.Fprintln(stderr, "axlr-bots: output add needs exactly one <name> (or a name in --file)")
		return exitUsage
	}
	if *urlFile == "-" && *secretFile == "-" {
		fmt.Fprintln(stderr, "axlr-bots: output add reads stdin for one of --url-file and --secret-file, not both")
		return exitUsage
	}
	if len(positional) == 1 {
		definition.Name = positional[0]
	}
	set := func(into *string, value string) {
		if value != "" {
			*into = value
		}
	}
	set(&definition.Type, *kind)
	set(&definition.URL, *targetURL)
	set(&definition.PanelURL, *panelURL)
	for _, read := range []struct {
		path string
		into *string
	}{{*urlFile, &definition.URL}, {*secretFile, &definition.Secret}} {
		if read.path == "" {
			continue
		}
		value, err := readSecretFile(read.path)
		if err != nil {
			return fail(stderr, err)
		}
		*read.into = value
	}
	if list := commaList(*events); len(list) > 0 {
		definition.Events = list
	}
	if list := commaList(*tenants); len(list) > 0 {
		definition.Tenants = list
	}
	if list := commaList(*severities); len(list) > 0 {
		definition.Severities = list
	}
	if len(program) > 0 {
		definition.Command = program
	}
	view, err := service.AddOutput(ctx, definition)
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, view)
	return exitOK
}

// outputList prints the targets with a fingerprint, never a URL, a
// secret or a command.
func outputList(ctx context.Context, service unixapi.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "axlr-bots: output list takes no arguments")
		return exitUsage
	}
	views, err := service.ListOutputs(ctx)
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, views)
	return exitOK
}

// outputRemove drops a target; its pending deliveries are skipped.
func outputRemove(ctx context.Context, service unixapi.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "axlr-bots: output remove needs exactly one <name>")
		return exitUsage
	}
	view, err := service.RemoveOutput(ctx, args[0])
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, view)
	return exitOK
}

// splitCommand cuts the arguments at the first --: what follows is a
// command target's program and its arguments, never flags of ours.
func splitCommand(args []string) ([]string, []string) {
	at := slices.Index(args, "--")
	if at < 0 {
		return args, nil
	}
	return args[:at], args[at+1:]
}

// readSecretFile reads a URL or a secret from a file or stdin (-),
// without its trailing newline.
func readSecretFile(path string) (string, error) {
	var reader io.Reader
	if path == "-" {
		reader = stdin
	} else {
		f, err := os.Open(path)
		if err != nil {
			return "", err
		}
		defer f.Close()
		reader = f
	}
	raw, err := io.ReadAll(io.LimitReader(reader, maxSecretFileBytes+1))
	if err != nil {
		return "", err
	}
	if len(raw) > maxSecretFileBytes {
		return "", errors.New(path + ": more than 8 KiB")
	}
	return strings.TrimSpace(string(raw)), nil
}

// commaList splits a comma-separated flag, dropping empty items.
func commaList(raw string) []string {
	var out []string
	for _, item := range strings.Split(raw, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}
