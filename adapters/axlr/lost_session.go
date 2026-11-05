package axlr

import (
	"strings"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// LostSessionReason is a decision's failure when AXLR no longer has the
// session it continues: the node's state went with a replaced pod (the
// sidecar keeps AXLR's sessions on tmpfs) or a wiped host. Nothing on the
// node can bring the session back, so the task has to run again.
const LostSessionReason = "the run's session was lost when the node restarted; run the task again"

// explainLostSession replaces the raw reason of a decision that failed
// because AXLR could not open its session file
// (`open …/sessions/<id>.json: no such file or directory`) with
// LostSessionReason. Any other failure is left as AXLR said it.
func explainLostSession(report *application.ExecutionReport, session string) {
	if report.Outcome != domain.ExecutionFailed || session == "" {
		return
	}
	reason := report.FailureReason
	if strings.Contains(reason, "sessions/"+session+".json") && strings.Contains(reason, "no such file or directory") {
		report.FailureReason = LostSessionReason
	}
}
