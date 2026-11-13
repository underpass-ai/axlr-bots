//! The panel against the operator API's contract, `api/proto`: a server
//! answers every call the panel reads with the least the contract
//! requires, and again with every status, kind and type a newer control
//! plane might add. Both must read without an error: a value the panel
//! does not know yet is shown as unknown, never as "Bad answer".

mod support;

use axlr_bots_panel::adapters::grpc::{GrpcControlPlane, proto};
use axlr_bots_panel::application::ControlPlane;
use axlr_bots_panel::domain::{
    AutonomyLevel, BotId, BotStatus, ExecutionKind, ExecutionOutcome, NodeId, NodeStatus, TaskId, TaskStatus,
    TriggerType,
};
use axum::Router;
use axum::body::Body;
use axum::extract::State;
use axum::http::Request;
use axum::response::Response;
use prost_types::Timestamp;
use support::{Recorder, refused, serve, unary};
use tonic::Status;

const FUTURE: &str = "FromANewerControlPlane";

fn stamp() -> Option<Timestamp> {
    Some(Timestamp {
        seconds: 1_793_000_000,
        nanos: 0,
    })
}

/// Every string a newer control plane could change, changed; otherwise the
/// least the contract needs.
#[derive(Clone, Copy)]
struct Shape {
    future: bool,
}

impl Shape {
    fn word(self, known: &str) -> String {
        if self.future { FUTURE.into() } else { known.into() }
    }

    fn node(self) -> proto::Node {
        proto::Node {
            id: "wrx80".into(),
            host_name: "host".into(),
            status: self.word("Online"),
            registered_at: stamp(),
            last_seen_at: stamp(),
            ..Default::default()
        }
    }

    fn bot(self) -> proto::Bot {
        proto::Bot {
            id: "bot-a".into(),
            name: "bot-a".into(),
            mission: "Do it.".into(),
            level: self.word("observe"),
            status: self.word("Enabled"),
            triggers: vec![proto::Trigger {
                r#type: self.word("manual"),
                ..Default::default()
            }],
            created_at: stamp(),
            updated_at: stamp(),
            ..Default::default()
        }
    }

    fn task(self) -> proto::Task {
        proto::Task {
            id: "task-1".into(),
            bot_id: "bot-a".into(),
            objective: "Count".into(),
            status: self.word("Pending"),
            trigger: Some(proto::TriggerRef {
                r#type: self.word("manual"),
                ..Default::default()
            }),
            created_at: stamp(),
            updated_at: stamp(),
            ..Default::default()
        }
    }

    fn execution(self) -> proto::Execution {
        proto::Execution {
            id: "exec-1".into(),
            task_id: "task-1".into(),
            bot_id: "bot-a".into(),
            started_at: stamp(),
            outcome: self.word("Completed"),
            kind: self.word("run"),
            ..Default::default()
        }
    }
}

async fn route(State(shape): State<Shape>, request: Request<Body>) -> Response {
    let method = request.uri().path().rsplit('/').next().unwrap_or_default().to_string();
    match method.as_str() {
        "GetOverview" => {
            unary(request, |_: proto::GetOverviewRequest| {
                Ok(proto::GetOverviewResponse::default())
            })
            .await
        }
        "GetBuild" => {
            unary(request, |_: proto::GetBuildRequest| {
                Ok(proto::GetBuildResponse::default())
            })
            .await
        }
        "ListNodes" => {
            unary(request, move |_: proto::ListNodesRequest| {
                Ok(proto::ListNodesResponse {
                    nodes: vec![shape.node()],
                })
            })
            .await
        }
        "GetNode" => {
            unary(request, move |_: proto::GetNodeRequest| {
                Ok(proto::GetNodeResponse {
                    node: Some(shape.node()),
                })
            })
            .await
        }
        "ListBots" => {
            unary(request, move |_: proto::ListBotsRequest| {
                Ok(proto::ListBotsResponse {
                    bots: vec![shape.bot()],
                })
            })
            .await
        }
        "GetBot" => {
            unary(request, move |_: proto::GetBotRequest| {
                Ok(proto::GetBotResponse { bot: Some(shape.bot()) })
            })
            .await
        }
        "ListTasks" => {
            unary(request, move |_: proto::ListTasksRequest| {
                Ok(proto::ListTasksResponse {
                    tasks: vec![shape.task()],
                })
            })
            .await
        }
        "GetTask" => {
            unary(request, move |_: proto::GetTaskRequest| {
                Ok(proto::GetTaskResponse {
                    task: Some(shape.task()),
                })
            })
            .await
        }
        "ListTaskExecutions" => {
            unary(request, move |_: proto::ListTaskExecutionsRequest| {
                Ok(proto::ListTaskExecutionsResponse {
                    executions: vec![shape.execution()],
                })
            })
            .await
        }
        "ListTaskEvents" => {
            unary(request, move |_: proto::ListTaskEventsRequest| {
                Ok(proto::ListTaskEventsResponse {
                    events: vec![proto::Event {
                        event_id: "evt-1".into(),
                        r#type: shape.word("bot.task.submitted"),
                        created_at: stamp(),
                        ..Default::default()
                    }],
                })
            })
            .await
        }
        "GetTaskActivity" => {
            unary(request, move |_: proto::GetTaskActivityRequest| {
                Ok(proto::GetTaskActivityResponse {
                    activity: Some(proto::TaskActivity {
                        task: Some(shape.task()),
                        executions: vec![proto::ExecutionActivity {
                            execution: Some(shape.execution()),
                            progress_cut: Some(proto::ProgressCut {
                                // A reason this panel has never heard of.
                                reason: 99,
                                completion: 99,
                                ..Default::default()
                            }),
                            completed_items: vec![proto::ProgressItem {
                                kind: shape.word("tool_started"),
                                ..Default::default()
                            }],
                        }],
                        children: vec![proto::TaskActivity {
                            task: Some(shape.task()),
                            ..Default::default()
                        }],
                        ..Default::default()
                    }),
                })
            })
            .await
        }
        _ => refused(Status::unimplemented("no such call")),
    }
}

async fn read_everything(future: bool) {
    let shape = Shape { future };
    let server = serve(Router::new().fallback(route).with_state(shape), Recorder::default()).await;
    let api = GrpcControlPlane::new(&server.config()).unwrap();
    let since = "2026-10-26T00:00:00Z".parse().unwrap();
    api.overview(since).await.unwrap();
    api.build().await.unwrap();
    let node = api.list_nodes().await.unwrap().remove(0);
    api.get_node(NodeId::new("wrx80").unwrap()).await.unwrap();
    let bot = api.list_bots().await.unwrap().remove(0);
    api.get_bot(BotId::new("bot-a").unwrap()).await.unwrap();
    let task = api.list_tasks(Default::default()).await.unwrap().remove(0);
    let id = TaskId::new("task-1").unwrap();
    api.get_task(id.clone()).await.unwrap();
    let execution = api.task_executions(id.clone()).await.unwrap().remove(0);
    api.task_events(id.clone(), 10).await.unwrap();
    let tree = api.task_activity(id).await.unwrap();
    assert_eq!(tree.children.len(), 1);
    assert!(!tree.executions[0].from_transcript(), "completion 99 is no transcript");
    if future {
        assert_eq!(node.status, NodeStatus::Unknown);
        assert_eq!((bot.status, bot.level), (BotStatus::Unknown, AutonomyLevel::Unknown));
        assert_eq!(bot.triggers[0].trigger_type, TriggerType::Unknown);
        assert_eq!(task.status, TaskStatus::Unknown);
        assert_eq!(execution.outcome, ExecutionOutcome::Unknown);
        assert_eq!(execution.kind, ExecutionKind::Unknown);
    } else {
        assert_eq!((node.status, task.status), (NodeStatus::Online, TaskStatus::Pending));
    }
}

#[tokio::test]
async fn reads_the_least_the_contract_requires() {
    read_everything(false).await;
}

#[tokio::test]
async fn reads_every_value_a_newer_control_plane_may_add() {
    read_everything(true).await;
}
