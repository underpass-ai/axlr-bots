package nodeagent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/underpass-ai/axlr-bots/application"
)

// delegateTool describes axlr_bots_delegate for one execution: the bots
// its bot may ask are the schema's enum and the description's list. The
// control plane checks every call again; the description only helps the
// model ask what will be allowed.
func delegateTool(offer application.DelegationOffer) mcpTool {
	targets := offer.Targets
	description := fmt.Sprintf("Ask another bot of this fleet to do a task. The control plane creates it as a task of that bot, run under that bot's own mission and policy, and checks every ask. The bots you may ask: %s. This task is at delegation depth %d; this chain allows %d. The other bot sees only the objective you write and its own mission, nothing of this conversation: state in full what you need back. With wait true (the default) the call returns the other bot's answer when its task ends, or its state at this task's deadline; with wait false it returns the task id at once, and the same ask again waits for that task. A refused ask says which limit refused it: do not repeat it unchanged.", strings.Join(targets, ", "), offer.Depth, offer.MaxDepth)
	enum, _ := json.Marshal(targets)
	schema := `{"type":"object","properties":{` +
		`"bot":{"type":"string","enum":` + string(enum) + `,"description":"the id of the bot to ask; only these are allowed"},` +
		`"objective":{"type":"string","minLength":1,"maxLength":4000,"description":"what the other bot should do, in full: it sees this objective and its own mission, nothing of this conversation"},` +
		`"wait":{"type":"boolean","default":true,"description":"true: wait for its answer, until it ends or this task's deadline; false: return its task id at once"}` +
		`},"required":["bot","objective"],"additionalProperties":false}`
	return mcpTool{Name: application.DelegateToolName, Description: description, InputSchema: json.RawMessage(schema)}
}
