package transcriptaudit

import (
	"log"
	"time"
)

// AuditorConfig is what an Auditor needs. Publisher nil is a dry run:
// verdicts are recorded, nothing is published. Team and Event are the
// operator's choice (the spec's transcriptFailure); Model names the Jev
// version the judge asks, for the record.
type AuditorConfig struct {
	Source    Source
	Publisher Publisher
	Judge     Judge
	Log       *VerdictLog
	Team      string
	Event     string
	Model     string
	Now       func() time.Time
	Logger    *log.Logger
}
