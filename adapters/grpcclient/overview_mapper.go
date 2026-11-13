package grpcclient

import (
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/application"
)

func overviewOf(o *operatorv1.GetOverviewResponse) application.Overview {
	return application.Overview{
		Since: timeOf(o.GetSince()), Nodes: counts(o.GetNodes()), Bots: counts(o.GetBots()), Tasks: counts(o.GetTasks()),
		TasksSince: counts(o.GetTasksSince()), ExecutionsSince: int(o.GetExecutionsSince()), CostUSDSince: o.GetCostUsdSince(),
		CostUnknownSince: int(o.GetCostUnknownSince()),
	}
}

// counts is a map of counts by status; none is {}, not null.
func counts(in map[string]int32) map[string]int {
	out := make(map[string]int, len(in))
	for key, value := range in {
		out[key] = int(value)
	}
	return out
}
