package filestore

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"sync"
	"time"
)

// statusStamp is the status a record had when it was read from this
// file (its inode, modification time and size), and when that was.
type statusStamp struct {
	file   fs.FileInfo
	status string
	read   time.Time
}

// stampTrust is how long a stamp spares reading its file. Timestamps are
// as coarse as the kernel's tick (a few milliseconds), so two writes of
// a record within one tick, of the same size, onto an inode the kernel
// gave back at once would look like no write: past this a file is read
// again all the same, so such a miss lasts seconds, not for ever.
const stampTrust = 10 * time.Second

// statusStamps remembers the status of each record of a collection with
// the stamp of the file it was read from, so a listing by status reads
// only the files that changed since and those in the status asked for:
// the other files are skipped on their stamp alone. A record is written
// by a rename of a new file over the old one, so a write changes the
// inode, and mostly the modification time and the size too. Copies of a
// repository share it.
type statusStamps struct {
	mu      sync.Mutex
	records map[string]statusStamp
}

func newStatusStamps() *statusStamps {
	return &statusStamps{records: map[string]statusStamp{}}
}

// stampedEntry is one record file as the directory lists it.
type stampedEntry struct {
	id   string
	info fs.FileInfo
}

// entries lists the collection's records with their files' stamps, in
// file-name order; a file removed while listing is left out.
func (c collection) entries() ([]stampedEntry, error) {
	listed, err := os.ReadDir(c.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]stampedEntry, 0, len(listed))
	for _, e := range listed {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".json") {
			continue
		}
		info, err := e.Info()
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, stampedEntry{id: strings.TrimSuffix(name, ".json"), info: info})
	}
	return out, nil
}

// skip reports whether the record's file is the one last read and its
// status then was not the one wanted.
func (s *statusStamps) skip(entry stampedEntry, wanted string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	stamp, ok := s.records[entry.id]
	return ok && stamp.status != wanted && time.Since(stamp.read) < stampTrust && os.SameFile(stamp.file, entry.info) &&
		stamp.file.Size() == entry.info.Size() && stamp.file.ModTime().Equal(entry.info.ModTime())
}

// remember keeps the status read from the record's file. The file may
// have been replaced after it was listed: the content read is then newer
// than the stamp, and the next listing sees another stamp and reads it
// again.
func (s *statusStamps) remember(entry stampedEntry, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[entry.id] = statusStamp{file: entry.info, status: status, read: time.Now()}
}
