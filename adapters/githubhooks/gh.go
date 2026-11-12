package githubhooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// GH is HookAPI over the gh CLI (`gh api`), signed in as the operator.
type GH struct {
	// Bin is the gh binary; empty is "gh" on the PATH.
	Bin string
}

// Hooks is GET repos/{repo}/hooks, every page.
func (g GH) Hooks(ctx context.Context, repo string) ([]Hook, error) {
	var hooks []Hook
	return hooks, g.list(ctx, "repos/"+repo+"/hooks?per_page=100", &hooks)
}

// Deliveries is GET repos/{repo}/hooks/{hook}/deliveries, every page.
func (g GH) Deliveries(ctx context.Context, repo string, hook int) ([]HookDelivery, error) {
	var deliveries []HookDelivery
	return deliveries, g.list(ctx, fmt.Sprintf("repos/%s/hooks/%d/deliveries?per_page=100", repo, hook), &deliveries)
}

// Redeliver is POST repos/{repo}/hooks/{hook}/deliveries/{delivery}/attempts.
func (g GH) Redeliver(ctx context.Context, repo string, hook, delivery int) error {
	_, err := g.run(ctx, "api", "--method", "POST", fmt.Sprintf("repos/%s/hooks/%d/deliveries/%d/attempts", repo, hook, delivery), "--silent")
	return err
}

// list reads a paginated array: gh writes the pages one after another,
// each a JSON array.
func (g GH) list(ctx context.Context, path string, into any) error {
	out, err := g.run(ctx, "api", "--paginate", path)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	var all []json.RawMessage
	for {
		var page []json.RawMessage
		if err := dec.Decode(&page); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return fmt.Errorf("gh api %s: %w", path, err)
		}
		all = append(all, page...)
	}
	raw, _ := json.Marshal(all)
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("gh api %s: %w", path, err)
	}
	return nil
}

func (g GH) run(ctx context.Context, args ...string) ([]byte, error) {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, g.bin(), args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// bin is the binary run: Bin, or gh on the PATH.
func (g GH) bin() string {
	if g.Bin == "" {
		return "gh"
	}
	return g.Bin
}
