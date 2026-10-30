package nats

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// entry is one push in the outbox: the node, the bytes to publish and
// the file that keeps them until the broker took them. Its name is the
// publish's id: a republish after a lost answer is dropped by the broker
// within the duplicates window.
type entry struct {
	id      string
	node    domain.NodeID
	payload []byte
	path    string
}

// outbox keeps pushes as files, <dir>/<node>/<time>-<seq>.json, written
// before the publisher tries the broker and removed once it answered:
// a restart publishes what was left.
type outbox struct {
	dir string
}

type outboxRecord struct {
	Node    string          `json:"node"`
	Payload json.RawMessage `json:"payload"`
}

// add writes the push and answers its entry.
func (o outbox) add(node domain.NodeID, seq uint64, payload []byte) (entry, error) {
	dir := filepath.Join(o.dir, token(node))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return entry{}, err
	}
	id := fmt.Sprintf("%020d-%012d", time.Now().UnixNano(), seq)
	path := filepath.Join(dir, id+".json")
	raw, err := json.Marshal(outboxRecord{Node: node.String(), Payload: payload})
	if err != nil {
		return entry{}, err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return entry{}, err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return entry{}, err
	}
	return entry{id: id, node: node, payload: payload, path: path}, nil
}

// remove forgets a push the broker took.
func (o outbox) remove(e entry) { _ = os.Remove(e.path) }

// sweep reads every push left, oldest first.
func (o outbox) sweep() []entry {
	var out []entry
	dirs, _ := os.ReadDir(o.dir)
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		files, _ := os.ReadDir(filepath.Join(o.dir, d.Name()))
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
				continue
			}
			path := filepath.Join(o.dir, d.Name(), f.Name())
			raw, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			var record outboxRecord
			if json.Unmarshal(raw, &record) != nil {
				continue
			}
			node, err := domain.NewNodeID(record.Node)
			if err != nil {
				continue
			}
			out = append(out, entry{id: strings.TrimSuffix(f.Name(), ".json"), node: node, payload: record.Payload, path: path})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out
}
