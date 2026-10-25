package axlr

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/underpass-ai/axlr-bots/domain"
)

// OpenDelivered opens a file an execution delivered, by the reference
// AXLR's result gives it: a path relative to the bot's workspace, with
// `/` separators. The reference is not trusted: one that is absolute,
// holds ".." once cleaned (even inside a name), or reaches outside the
// workspace once links are resolved is refused, and so is anything but
// a regular file of at most domain.MaxArtifactBytes.
func (e *Executor) OpenDelivered(bot domain.BotID, reference string) (io.ReadCloser, error) {
	if bot == "" || strings.ContainsAny(bot.String(), `/\`) || strings.Contains(bot.String(), "..") {
		return nil, fmt.Errorf("axlr: bot id %q is not a workspace", bot)
	}
	if reference == "" || strings.HasPrefix(reference, "/") || strings.Contains(reference, `\`) || strings.ContainsRune(reference, 0) {
		return nil, fmt.Errorf("axlr: reference %q is not a path inside the workspace", reference)
	}
	relative := filepath.Clean(filepath.FromSlash(reference))
	if relative == "." || strings.Contains(relative, "..") || filepath.IsAbs(relative) {
		return nil, fmt.Errorf("axlr: reference %q leaves the workspace, or may", reference)
	}
	workspace, err := filepath.EvalSymlinks(filepath.Join(e.StateDir, "workspaces", bot.String()))
	if err != nil {
		return nil, fmt.Errorf("axlr: workspace of %s: %w", bot, err)
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(workspace, relative))
	if err != nil {
		return nil, fmt.Errorf("axlr: delivered file %s: %w", reference, err)
	}
	inside, err := filepath.Rel(workspace, resolved)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("axlr: delivered file %s resolves outside the workspace", reference)
	}
	file, err := os.Open(resolved)
	if err != nil {
		return nil, fmt.Errorf("axlr: %w", err)
	}
	info, err := file.Stat()
	if err == nil && !info.Mode().IsRegular() {
		err = errors.New("is not a regular file")
	} else if err == nil && info.Size() > domain.MaxArtifactBytes {
		err = errors.New("is larger than 32 MiB")
	}
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("axlr: delivered file %s %w", reference, err)
	}
	return file, nil
}
