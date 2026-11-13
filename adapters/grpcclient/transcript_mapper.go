package grpcclient

import (
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

func transcriptRecordOf(r *operatorv1.TranscriptRecord) application.TranscriptRecord {
	return application.TranscriptRecord{
		Seq: r.GetSeq(), Execution: domain.ExecutionID(r.GetExecutionId()), Task: domain.TaskID(r.GetTaskId()), Bot: domain.BotID(r.GetBotId()),
		Session: r.GetSession(), Size: r.GetSize(), SHA256: r.GetSha256(), Filtered: r.GetFiltered(), SentSHA256: r.GetSentSha256(),
		ReceivedAt: timeOf(r.GetReceivedAt()),
	}
}
