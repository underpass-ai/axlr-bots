package grpcclient

import (
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/domain"
)

func runtimeSnapshot(r *operatorv1.Runtime) *domain.RuntimeSnapshot {
	if r == nil {
		return nil
	}
	snapshot := &domain.RuntimeSnapshot{Kind: r.GetKind()}
	if jev := r.GetJev(); jev != nil {
		program := &domain.JevProgramSnapshot{
			Server: jev.GetServer(), Start: jev.GetStart(), Wait: jev.GetWait(), Stop: jev.GetStop(), Model: jev.GetModel(),
			Act: domain.JevActSnapshot{Tool: jev.GetAct().GetTool(), Choice: jev.GetAct().GetChoice(), Verdict: jev.GetAct().GetVerdict(), Pass: jev.GetAct().GetPass()},
		}
		if tell := jev.GetTell(); tell != nil {
			program.Tell = &domain.JevTellSnapshot{Name: tell.GetName(), Fields: tell.GetFields()}
		}
		snapshot.Jev = program
	}
	return snapshot
}

func runtimeMessage(r *domain.RuntimeSnapshot) *operatorv1.Runtime {
	if r == nil {
		return nil
	}
	message := &operatorv1.Runtime{Kind: r.Kind}
	if j := r.Jev; j != nil {
		program := &operatorv1.JevProgram{
			Server: j.Server, Start: j.Start, Wait: j.Wait, Stop: j.Stop, Model: j.Model,
			Act: &operatorv1.JevAct{Tool: j.Act.Tool, Choice: j.Act.Choice, Verdict: j.Act.Verdict, Pass: j.Act.Pass},
		}
		if j.Tell != nil {
			program.Tell = &operatorv1.JevTell{Name: j.Tell.Name, Fields: j.Tell.Fields}
		}
		message.Jev = program
	}
	return message
}
