package filestore

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// collection is one directory of <id>.json files for one aggregate kind.
type collection struct {
	dir  string
	kind string
}

// path is the file of the record id. The ids reaching here are
// validated identifiers; an id that is not a plain file name (a
// separator, a "..") is refused all the same, so no caller can step out
// of the directory.
func (c collection) path(id string) (string, error) {
	if id == "" || strings.ContainsAny(id, `/\`) || strings.Contains(id, "..") {
		return "", fmt.Errorf("filestore: %s id %q is not a file name", c.kind, id)
	}
	return filepath.Join(c.dir, id+".json"), nil
}

func (c collection) write(id string, snapshot any) error {
	path, err := c.path(id)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(path, append(data, '\n'))
}

func (c collection) read(id string, snapshot any) error {
	path, err := c.path(id)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return domain.NotFoundError{Kind: c.kind, ID: id}
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, snapshot); err != nil {
		return invalidRecordError{kind: c.kind, id: id, err: err}
	}
	return nil
}

// quarantineDirName is where a listing sets aside the files it cannot
// parse, beside the records of the kind; ids skips it as a dot entry.
const quarantineDirName = ".quarantine"

// invalidRecordError is a record whose file is not valid JSON: torn by
// hand, by a disk, by a tool that wrote it without this store.
type invalidRecordError struct {
	kind, id string
	err      error
}

func (e invalidRecordError) Error() string {
	return fmt.Sprintf("filestore: %s %s is not valid JSON: %v", e.kind, e.id, e.err)
}

func (e invalidRecordError) Unwrap() error { return e.err }

// skipListed reports whether a listing goes on past the record id whose
// read answered err. A file that is not valid JSON is moved to
// <dir>/.quarantine/<id>.json.<unixnano> and skipped: one bad file must
// not hide every other record of its kind (every listing, every tick of
// the daemon, would fail on it). A get of the id keeps failing loudly
// while the file is there and answers not found once it is set aside.
// A record gone since the directory was read (deleted, or set aside by
// a listing running beside this one) is skipped too. Any other error,
// and a valid file the domain refuses, still fails the listing.
func (c collection) skipListed(id string, err error) bool {
	if domain.IsNotFound(err) {
		return true
	}
	var invalid invalidRecordError
	if !errors.As(err, &invalid) {
		return false
	}
	path, pathErr := c.path(id)
	if pathErr != nil {
		return false
	}
	dir := filepath.Join(c.dir, quarantineDirName)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		log.Printf("%v; cannot set it aside: %v", invalid, err)
		return false
	}
	target := filepath.Join(dir, fmt.Sprintf("%s.json.%d", id, time.Now().UnixNano()))
	if err := os.Rename(path, target); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return true
		}
		log.Printf("%v; cannot set it aside: %v", invalid, err)
		return false
	}
	log.Printf("%v; set aside as %s", invalid, target)
	return true
}

// ids lists the identifiers stored, in file-name order.
func (c collection) ids() ([]string, error) {
	entries, err := os.ReadDir(c.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".json") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(name, ".json"))
	}
	return ids, nil
}
