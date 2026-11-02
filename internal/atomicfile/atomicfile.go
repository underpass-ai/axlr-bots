// Package atomicfile writes a file so that a crash leaves either the old
// content or the new one, never a torn one.
package atomicfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Write writes data to path through a temporary file in the same
// directory, gives it perm, fsyncs it and renames it over the target,
// then fsyncs the directory so the rename itself survives a crash. The
// directory must exist: each caller makes its own, with its own mode.
func Write(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	err = fill(tmp, data, perm)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		if err = os.Rename(tmp.Name(), path); err != nil {
			err = fmt.Errorf("atomicfile: rename %s: %w", path, err)
		}
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// fill writes the temporary's bytes, gives it its mode and fsyncs it.
// CreateTemp makes the file 0600; the mode is set before the rename so
// the target never shows another one, not even for an instant.
func fill(f *os.File, data []byte, perm os.FileMode) error {
	_, err := f.Write(data)
	if err != nil {
		return err
	}
	return errors.Join(f.Chmod(perm), f.Sync())
}
