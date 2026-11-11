package nodeproto

import "github.com/underpass-ai/axlr-bots/domain"

// FromChannelRecord renders a team channel's record for the wire; nil
// stays nil.
func FromChannelRecord(record *domain.ChannelRecord) *domain.ChannelRecordSnapshot {
	if record == nil {
		return nil
	}
	snapshot := record.Snapshot()
	return &snapshot
}

// ToChannelRecord reads a team channel's record from the wire. A record
// that does not validate is dropped, never an error: writing it is
// best-effort, and the run or the report it came with stands.
func ToChannelRecord(snapshot *domain.ChannelRecordSnapshot) *domain.ChannelRecord {
	if snapshot == nil {
		return nil
	}
	record, err := domain.ChannelRecordFromSnapshot(*snapshot)
	if err != nil {
		return nil
	}
	return &record
}
