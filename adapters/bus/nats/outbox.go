package nats

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
	"github.com/underpass-ai/axlr-bots/internal/atomicfile"
)

// entry is one message in the outbox: a push's node, or a topic's
// subject and message id, the bytes to publish and the file that keeps
// them until the broker took them. A push's file name is the publish's
// id; a topic's id is its event's and team's. Either way a republish
// after a lost answer is dropped by the broker within the duplicates
// window.
type entry struct {
	id      string
	node    domain.NodeID
	subject string
	msgID   string
	payload []byte
	path    string
}

// target is the subject the entry is published to.
func (e entry) target() string {
	if e.subject != "" {
		return e.subject
	}
	return subject(e.node)
}

// publishID is the id the broker drops a repeat of the entry by.
func (e entry) publishID() string {
	if e.msgID != "" {
		return e.msgID
	}
	return e.id
}

// olderThan reports whether the push was made more than age before
// now; its id starts with the time it was made, in nanoseconds. An id
// that does not is taken as new.
func (e entry) olderThan(age time.Duration, now time.Time) bool {
	stamp, _, _ := strings.Cut(e.id, "-")
	nanos, err := strconv.ParseInt(stamp, 10, 64)
	if err != nil {
		return false
	}
	return now.Sub(time.Unix(0, nanos)) > age
}

// outbox keeps pushes as files, <dir>/<node>/<time>-<seq>.json, written
// before the publisher tries the broker and removed once it answered:
// a restart publishes what was left.
type outbox struct {
	dir string
}

type outboxRecord struct {
	Node    string          `json:"node,omitempty"`
	Subject string          `json:"subject,omitempty"`
	MsgID   string          `json:"msgId,omitempty"`
	Payload json.RawMessage `json:"payload"`
}

// add writes the push and answers its entry.
func (o outbox) add(node domain.NodeID, seq uint64, payload []byte) (entry, error) {
	return o.write(token(node), entry{node: node, payload: payload}, seq)
}

// addMessage writes a message for the subject, dropped as a repeat by
// msgID, in the outbox's folder named folder.
func (o outbox) addMessage(folder, subject, msgID string, seq uint64, payload []byte) (entry, error) {
	return o.write(folder, entry{subject: subject, msgID: msgID, payload: payload}, seq)
}

func (o outbox) write(folder string, e entry, seq uint64) (entry, error) {
	dir := filepath.Join(o.dir, folder)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return entry{}, err
	}
	e.id = fmt.Sprintf("%020d-%012d", time.Now().UnixNano(), seq)
	e.path = filepath.Join(dir, e.id+".json")
	raw, err := json.Marshal(outboxRecord{Node: e.node.String(), Subject: e.subject, MsgID: e.msgID, Payload: e.payload})
	if err != nil {
		return entry{}, err
	}
	// Written whole and fsynced: a message the broker never got must still
	// be there, readable, after a crash.
	if err := atomicfile.Write(e.path, raw, 0o600); err != nil {
		return entry{}, err
	}
	return e, nil
}

// remove forgets a push the broker took.
func (o outbox) remove(e entry) { _ = os.Remove(e.path) }

// sweep reads every message left, oldest first.
func (o outbox) sweep() []entry {
	var out []entry
	dirs, _ := os.ReadDir(o.dir)
	for _, d := range dirs {
		if d.IsDir() {
			out = append(out, o.read(d.Name())...)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out
}

// sweepFolder reads the messages left in one folder, oldest first.
func (o outbox) sweepFolder(folder string) []entry {
	out := o.read(folder)
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out
}

// read reads the messages of one folder of the outbox.
func (o outbox) read(folder string) []entry {
	var out []entry
	files, _ := os.ReadDir(filepath.Join(o.dir, folder))
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		path := filepath.Join(o.dir, folder, f.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var record outboxRecord
		if json.Unmarshal(raw, &record) != nil {
			continue
		}
		e := entry{id: strings.TrimSuffix(f.Name(), ".json"), subject: record.Subject, msgID: record.MsgID, payload: record.Payload, path: path}
		if e.subject == "" {
			node, err := domain.NewNodeID(record.Node)
			if err != nil {
				continue
			}
			e.node = node
		}
		out = append(out, e)
	}
	return out
}
