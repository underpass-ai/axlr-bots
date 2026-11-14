package grpcapi

import (
	"context"

	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// overviewService is OverviewService: the dashboard's counts and the build.
type overviewService struct {
	operatorv1.UnimplementedOverviewServiceServer
	local *application.LocalOperator
}

func (s overviewService) GetOverview(ctx context.Context, request *operatorv1.GetOverviewRequest) (*operatorv1.GetOverviewResponse, error) {
	overview, err := s.local.GetOverview(ctx, instant(request.GetSince()))
	if err != nil {
		return nil, err
	}
	return &operatorv1.GetOverviewResponse{
		Since: timestamp(overview.Since), Nodes: counts(overview.Nodes), Bots: counts(overview.Bots), Tasks: counts(overview.Tasks),
		TasksSince: counts(overview.TasksSince), ExecutionsSince: int32(overview.ExecutionsSince),
		CostUsdSince: overview.CostUSDSince, CostUnknownSince: int32(overview.CostUnknownSince),
	}, nil
}

func (s overviewService) GetBuild(context.Context, *operatorv1.GetBuildRequest) (*operatorv1.GetBuildResponse, error) {
	deps := s.local.Dependencies()
	status := application.CheckBuild(deps.Build)
	return &operatorv1.GetBuildResponse{
		Running: buildToProto(status.Running), Installed: buildToProto(status.Installed), Stale: status.Stale,
		Reason: status.Reason, MinimumAxlr: deps.MinimumAXLR.String(),
	}, nil
}

func buildToProto(b domain.BuildIdentity) *operatorv1.BuildIdentity {
	return &operatorv1.BuildIdentity{Version: b.Version, Revision: b.Revision, Time: b.Time, Modified: b.Modified}
}

func counts(in map[string]int) map[string]int32 {
	out := make(map[string]int32, len(in))
	for key, n := range in {
		out[key] = int32(n)
	}
	return out
}
