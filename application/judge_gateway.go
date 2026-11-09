package application

import "context"

// JudgeGateway reaches Jev with the control plane's key.
type JudgeGateway interface {
	Choose(ctx context.Context, question JudgeQuestion) (JudgeVerdict, error)
}
