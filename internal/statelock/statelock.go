// Package statelock keeps two processes from writing one state directory
// at once: the daemon holds an exclusive lock on <state>/.lock for its
// life, and a command that would work on the files directly takes it for
// its own run. A second daemon, or a command while the daemon runs, is
// refused instead of racing it on the same files.
package statelock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// FileName is the lock file under the state directory. It starts with a
// dot, so no listing of the state takes it for a record.
const FileName = ".lock"

// ErrLocked is what Acquire answers, wrapped in a LockedError naming the
// directory, when another process holds the lock.
var ErrLocked = errors.New("statelock: the state directory is locked")

// LockedError says which state directory is in use, and what to do.
type LockedError struct {
	Dir  string
	Path string
}

func (e LockedError) Error() string {
	return fmt.Sprintf("the state at %s is in use by another process (a daemon holds %s); use the socket or stop it", e.Dir, e.Path)
}

// Is makes errors.Is(err, ErrLocked) true for a LockedError.
func (e LockedError) Is(target error) bool { return target == ErrLocked }

// Acquire takes the exclusive lock on <stateDir>/.lock without waiting.
// The lock is flock(2): it belongs to the open file, so the kernel drops
// it when the process ends however it ends, and two opens of the file
// conflict even inside one process. release closes the file and leaves
// it in place: removing it would let a third process lock a new file
// while a second still holds the old one.
func Acquire(stateDir string) (release func(), err error) {
	if err := os.MkdirAll(stateDir, 0o750); err != nil {
		return nil, fmt.Errorf("statelock: %w", err)
	}
	path := filepath.Join(stateDir, FileName)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("statelock: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, LockedError{Dir: stateDir, Path: path}
		}
		return nil, fmt.Errorf("statelock: lock %s: %w", path, err)
	}
	return func() { _ = file.Close() }, nil
}
