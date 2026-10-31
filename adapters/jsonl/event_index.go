package jsonl

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"slices"

	"github.com/underpass-ai/axlr-bots/domain"
)

// eventLine is where one event sits in the log: its first byte and its
// length without the newline.
type eventLine struct {
	at  int64
	len int32
}

// teamKey names a team of a tenant: the events about the team itself
// (the operator published on it) name it and no bot.
type teamKey struct {
	tenant domain.TenantID
	team   string
}

// eventKeys are the fields an event is found by: its bot, its task and,
// for an event about a team, the team.
type eventKeys struct {
	TenantID  domain.TenantID `json:"tenantId"`
	BotID     domain.BotID    `json:"botId"`
	GroupName string          `json:"groupName"`
	TaskID    domain.TaskID   `json:"taskId"`
}

// eventIndex knows where each bot's, task's and team's events are in the
// log, in the log's order, so a read decodes only the lines it answers
// with instead of the whole log. It is built by one pass over the log
// and extended with the bytes appended since, by this process or by
// another one; a log that shrank (replaced, truncated) is indexed again
// from its start. Only lines ending in a newline are indexed: a torn
// last line is read once it is whole.
type eventIndex struct {
	size   int64
	lines  []eventLine
	byBot  map[domain.BotID][]int32
	byTask map[domain.TaskID][]int32
	byTeam map[teamKey][]int32
}

// refresh brings the index up to the log's current end.
func (x *eventIndex) refresh(f *os.File) error {
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size() < x.size || x.byBot == nil {
		*x = eventIndex{byBot: map[domain.BotID][]int32{}, byTask: map[domain.TaskID][]int32{}, byTeam: map[teamKey][]int32{}}
	}
	if info.Size() == x.size {
		return nil
	}
	reader := bufio.NewReaderSize(io.NewSectionReader(f, x.size, info.Size()-x.size), 256*1024)
	at := x.size
	for {
		line, err := reader.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			// A line longer than the buffer: gather the rest of it.
			long := append([]byte(nil), line...)
			for errors.Is(err, bufio.ErrBufferFull) {
				line, err = reader.ReadSlice('\n')
				long = append(long, line...)
			}
			line = long
		}
		if errors.Is(err, io.EOF) {
			// A last line without its newline is not whole yet.
			break
		}
		if err != nil {
			return err
		}
		length := len(line) - 1
		x.add(at, line[:length])
		at += int64(len(line))
	}
	x.size = at
	return nil
}

// add indexes one line; a line that is not an event, or is longer than
// a read takes, is skipped as a read skips it.
func (x *eventIndex) add(at int64, line []byte) {
	if len(line) == 0 || len(line) > maxEventLineBytes {
		return
	}
	var keys eventKeys
	if err := json.Unmarshal(line, &keys); err != nil {
		return
	}
	n := int32(len(x.lines))
	x.lines = append(x.lines, eventLine{at: at, len: int32(len(line))})
	if keys.BotID != "" {
		x.byBot[keys.BotID] = append(x.byBot[keys.BotID], n)
	} else if keys.GroupName != "" {
		key := teamKey{tenant: keys.TenantID, team: keys.GroupName}
		x.byTeam[key] = append(x.byTeam[key], n)
	}
	if keys.TaskID != "" {
		x.byTask[keys.TaskID] = append(x.byTask[keys.TaskID], n)
	}
}

// merged is the union of the lists, in the log's order.
func merged(lists ...[]int32) []int32 {
	switch len(lists) {
	case 0:
		return nil
	case 1:
		return lists[0]
	}
	var all []int32
	for _, list := range lists {
		all = append(all, list...)
	}
	slices.Sort(all)
	return slices.Compact(all)
}
