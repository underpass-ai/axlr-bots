package grpcapi

import (
	"context"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

func registerNode(t *testing.T, f *fixture, id string) {
	t.Helper()
	registration := application.NodeRegistration{ID: id, HostName: id, Labels: map[string]string{"zone": "a"}, Capabilities: domain.NodeCapabilities{PolicySupported: true, Runtimes: []string{"axlr"}}}
	if _, err := application.NewRegisterNode(f.deps).Execute(context.Background(), registration); err != nil {
		t.Fatal(err)
	}
}

func TestNodesServiceReadsMintsAndRevokes(t *testing.T) {
	f := newFixture(t, "completed")
	ctx := context.Background()
	nodes := operatorv1.NewNodesServiceClient(f.authed)
	registerNode(t, f, "web-1")

	list, err := nodes.ListNodes(ctx, &operatorv1.ListNodesRequest{})
	if err != nil || len(list.GetNodes()) != 1 || list.GetNodes()[0].GetId() != "web-1" {
		t.Fatalf("list: %v %v", list, err)
	}
	node, err := nodes.GetNode(ctx, &operatorv1.GetNodeRequest{NodeId: "web-1"})
	if err != nil || node.GetNode().GetLabels()["zone"] != "a" || !node.GetNode().GetCapabilities().GetPolicySupported() ||
		node.GetNode().GetCapabilities().GetRuntimes()[0] != "axlr" || node.GetNode().GetRegisteredAt() == nil {
		t.Fatalf("get: %v %v", node, err)
	}
	_, err = nodes.GetNode(ctx, &operatorv1.GetNodeRequest{NodeId: "nobody"})
	if k, c := kind(t, err); k != "not_found" || c != "NotFound" {
		t.Errorf("get unknown: %s %s", k, c)
	}

	token, err := nodes.CreateEnrollment(ctx, &operatorv1.CreateEnrollmentRequest{NodeId: "db-1", Labels: map[string]string{"zone": "b"}, Ttl: durationpb.New(2 * time.Hour)})
	if err != nil || token.GetToken() == "" || token.GetNodeId() != "db-1" || token.GetLabels()["zone"] != "b" || token.GetExpiresAt() == nil {
		t.Fatalf("enrollment: %v %v", token, err)
	}
	if _, err := nodes.CreateEnrollment(ctx, &operatorv1.CreateEnrollmentRequest{NodeId: "Bad Node!"}); err == nil {
		t.Error("a bad node id was taken")
	}

	revoked, err := nodes.RevokeNode(ctx, &operatorv1.RevokeNodeRequest{NodeId: "web-1"})
	if err != nil || revoked.GetNode().GetStatus() != "Revoked" || revoked.GetNode().GetRevokedAt() == nil {
		t.Fatalf("revoke: %v %v", revoked, err)
	}
	if _, err := nodes.RevokeNode(ctx, &operatorv1.RevokeNodeRequest{NodeId: "nobody"}); err == nil {
		t.Error("revoked an unknown node")
	}
}

func TestOverviewServiceCountsAndNamesTheBuild(t *testing.T) {
	f := newFixtureWith(t, "completed", func(d *application.Dependencies) {
		d.MinimumAXLR, _ = domain.ParseSemanticVersion("min", "0.20.0")
	})
	ctx := context.Background()
	overview := operatorv1.NewOverviewServiceClient(f.authed)
	registerNode(t, f, "web-1")
	registerAndEnable(t, f, "bot-a", "observe")
	tasks := operatorv1.NewTasksServiceClient(f.authed)
	submitted, _ := tasks.SubmitTask(ctx, &operatorv1.SubmitTaskRequest{BotId: "bot-a", Objective: "say hi", IdempotencyKey: "k"})
	if _, err := tasks.RunTask(ctx, &operatorv1.RunTaskRequest{TaskId: submitted.GetTask().GetId()}); err != nil {
		t.Fatal(err)
	}

	got, err := overview.GetOverview(ctx, &operatorv1.GetOverviewRequest{})
	if err != nil || got.GetBots()["Enabled"] != 1 || got.GetTasks()["Completed"] != 1 || got.GetNodes()["Online"] != 1 || got.GetExecutionsSince() != 1 || got.GetSince() == nil {
		t.Fatalf("overview: %v %v", got, err)
	}
	later, err := overview.GetOverview(ctx, &operatorv1.GetOverviewRequest{Since: timestamp(time.Now().Add(time.Hour))})
	if err != nil || later.GetExecutionsSince() != 0 {
		t.Errorf("since the future: %v %v", later, err)
	}

	build, err := overview.GetBuild(ctx, &operatorv1.GetBuildRequest{})
	if err != nil || build.GetRunning() == nil || build.GetInstalled() == nil {
		t.Fatalf("build: %v %v", build, err)
	}
	if build.GetMinimumAxlr() != "0.20.0" {
		t.Errorf("minimum axlr: %q", build.GetMinimumAxlr())
	}
}
