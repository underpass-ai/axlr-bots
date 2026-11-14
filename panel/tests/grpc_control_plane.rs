//! The gRPC adapter against a local TLS server that plays the operator
//! API (see `support`): the calls it makes, the credentials every one
//! carries, what it reads back and how it sorts the failures.

mod support;

use std::sync::Arc;

use axlr_bots_panel::adapters::grpc::proto::{self, watch_task_activity_response::Update};
use axlr_bots_panel::adapters::grpc::{GrpcConnector, GrpcControlPlane, StatusError};
use axlr_bots_panel::application::{Connector, ControlPlane, PanelConfig, PanelError};
use axlr_bots_panel::domain::{
    ActivityUpdate, BotDefinition, BotForm, BotId, BotPatch, Decision, EnrollmentRequest, ExecutionId, IdempotencyKey,
    NodeId, Objective, TaskFilter, TaskId, TaskStatus, TaskSubmission,
};
use axum::Router;
use axum::body::Body;
use axum::extract::State;
use axum::http::Request;
use axum::response::Response;
use prost_types::Timestamp;
use rcgen::{BasicConstraints, CertificateParams, IsCa, KeyPair};
use support::{Recorder, TOKEN, TestServer, refused, serve, streaming, unary};
use tonic::{Code, Status};

fn at(text: &str) -> Option<Timestamp> {
    let moment: chrono::DateTime<chrono::Utc> = text.parse().unwrap();
    Some(Timestamp {
        seconds: moment.timestamp(),
        nanos: 0,
    })
}

fn node(status: &str) -> proto::Node {
    proto::Node {
        id: "wrx80".into(),
        host_name: "wrx80-node1".into(),
        capabilities: Some(proto::NodeCapabilities {
            node_version: "0.4.0".into(),
            node_revision: "4924ad73".into(),
            policy_supported: true,
            ..Default::default()
        }),
        status: status.into(),
        registered_at: at("2026-10-24T22:32:07Z"),
        last_seen_at: at("2026-10-24T22:41:08Z"),
        certificate_serial: "cef8".into(),
        certificate_not_after: at("2027-10-24T22:32:07Z"),
        ..Default::default()
    }
}

fn bot(status: &str, level: &str) -> proto::Bot {
    proto::Bot {
        id: "bot-wrx80".into(),
        name: "bot-wrx80".into(),
        mission: "Do it.".into(),
        level: level.into(),
        status: status.into(),
        placement: Some(proto::Placement {
            nodes: vec!["wrx80".into()],
            ..Default::default()
        }),
        triggers: vec![proto::Trigger {
            r#type: "manual".into(),
            ..Default::default()
        }],
        created_at: at("2026-10-24T22:30:00Z"),
        updated_at: at("2026-10-24T22:31:00Z"),
        ..Default::default()
    }
}

fn task(status: &str) -> proto::Task {
    proto::Task {
        id: "task-1".into(),
        bot_id: "bot-wrx80".into(),
        objective: "Count".into(),
        status: status.into(),
        execution_ids: vec!["exec-1".into()],
        created_at: at("2026-10-24T22:32:26Z"),
        updated_at: at("2026-10-24T22:32:33Z"),
        ..Default::default()
    }
}

fn execution(outcome: &str) -> proto::Execution {
    proto::Execution {
        id: "exec-1".into(),
        task_id: "task-1".into(),
        bot_id: "bot-wrx80".into(),
        started_at: at("2026-10-24T22:32:26Z"),
        outcome: outcome.into(),
        result: Some(proto::Result {
            pending_decision: Some(proto::PendingDecision {
                tool: "local_write".into(),
                call_id: "c1".into(),
                arguments: br#"{"path":"a.txt"}"#.to_vec(),
                ..Default::default()
            }),
            ..Default::default()
        }),
        usage: Some(proto::Usage {
            requests: 2,
            cost_usd: 0.001,
            ..Default::default()
        }),
        node_id: "wrx80".into(),
        ..Default::default()
    }
}

fn event(id: &str) -> proto::Event {
    proto::Event {
        event_id: id.into(),
        r#type: "bot.task.submitted".into(),
        bot_id: "bot-wrx80".into(),
        task_id: "task-1".into(),
        created_at: at("2026-10-24T22:32:26Z"),
        ..Default::default()
    }
}

fn activity() -> proto::TaskActivity {
    proto::TaskActivity {
        task: Some(task("Running")),
        executions: vec![proto::ExecutionActivity {
            execution: Some(execution("Running")),
            progress_cut: Some(proto::ProgressCut {
                reason: proto::ProgressCutReason::EventLimit as i32,
                completion: proto::CompletionSource::Transcript as i32,
                ..Default::default()
            }),
            completed_items: Vec::new(),
        }],
        subtree_usage: Some(proto::Usage {
            requests: 2,
            ..Default::default()
        }),
        ..Default::default()
    }
}

async fn route(State(recorder): State<Recorder>, request: Request<Body>) -> Response {
    let method = request.uri().path().rsplit('/').next().unwrap_or_default().to_string();
    let path = request.uri().path().to_string();
    let headers = request.headers().clone();
    let authorized = headers.get("authorization").and_then(|v| v.to_str().ok()) == Some(&format!("Bearer {TOKEN}"));
    if !authorized {
        recorder.record(&path, &headers, String::new());
        return refused(StatusError::refuse(
            Code::Unauthenticated,
            "unauthorized",
            "the bearer token is missing or wrong",
        ));
    }
    macro_rules! call {
        ($request:ty, |$message:ident| $answer:expr) => {{
            let recorder = recorder.clone();
            unary(request, move |$message: $request| {
                recorder.record(&path, &headers, format!("{:?}", $message));
                $answer
            })
            .await
        }};
    }
    macro_rules! watch {
        ($request:ty, $response:ty, $messages:expr, $quiet:expr) => {{
            recorder.record(&path, &headers, String::new());
            streaming::<$request, $response>(request, $messages, $quiet).await
        }};
    }
    match method.as_str() {
        "GetOverview" => call!(proto::GetOverviewRequest, |m| Ok(proto::GetOverviewResponse {
            since: m.since,
            tasks: [("Completed".to_string(), 5)].into_iter().collect(),
            executions_since: 1,
            cost_usd_since: 0.5,
            ..Default::default()
        })),
        "GetBuild" => call!(proto::GetBuildRequest, |_m| Ok(proto::GetBuildResponse {
            running: Some(proto::BuildIdentity {
                version: "0.4.0".into(),
                ..Default::default()
            }),
            installed: Some(proto::BuildIdentity {
                version: "0.4.0".into(),
                ..Default::default()
            }),
            minimum_axlr: "0.14.0".into(),
            ..Default::default()
        })),
        "ListNodes" => call!(proto::ListNodesRequest, |_m| Ok(proto::ListNodesResponse {
            nodes: vec![node("Online")]
        })),
        "GetNode" => call!(proto::GetNodeRequest, |m| if m.node_id == "ghost" {
            Err(StatusError::refuse(Code::NotFound, "not_found", "node ghost not found"))
        } else {
            Ok(proto::GetNodeResponse {
                node: Some(node("Online")),
            })
        }),
        "CreateEnrollment" => call!(proto::CreateEnrollmentRequest, |_m| Ok(
            proto::CreateEnrollmentResponse {
                token: "axlr1.secret".into(),
                id: "enr-1".into(),
                node_id: "web-2".into(),
                expires_at: at("2026-10-25T01:00:00Z"),
                ..Default::default()
            }
        )),
        "RevokeNode" => call!(proto::RevokeNodeRequest, |_m| Ok(proto::RevokeNodeResponse {
            node: Some(node("Revoked")),
            failed_tasks: vec!["task-9".into()],
        })),
        "ListBots" => call!(proto::ListBotsRequest, |_m| Ok(proto::ListBotsResponse {
            bots: vec![bot("Enabled", "observe")]
        })),
        "GetBot" => call!(proto::GetBotRequest, |_m| Ok(proto::GetBotResponse {
            bot: Some(bot("Enabled", "observe"))
        })),
        "RegisterBot" => call!(proto::RegisterBotRequest, |_m| Ok(proto::RegisterBotResponse {
            bot: Some(bot("Registered", "observe"))
        })),
        "UpdateBot" => call!(proto::UpdateBotRequest, |_m| Ok(proto::UpdateBotResponse {
            bot: Some(bot("Enabled", "propose"))
        })),
        "DeleteBot" => call!(proto::DeleteBotRequest, |m| if m.bot_id == "ghost" {
            Err(StatusError::refuse(Code::NotFound, "not_found", "bot ghost not found"))
        } else {
            Ok(proto::DeleteBotResponse {
                bot: Some(bot("Enabled", "observe")),
                cancelled: vec!["task-1".into()],
                timers_removed: 1,
                named_by: vec![proto::BotReference {
                    bot_id: "watcher".into(),
                    how: "delegation".into(),
                    topic: String::new(),
                }],
                ..Default::default()
            })
        }),
        "EnableBot" => call!(proto::EnableBotRequest, |_m| Ok(proto::EnableBotResponse {
            bot: Some(bot("Enabled", "observe"))
        })),
        "PauseBot" => call!(proto::PauseBotRequest, |m| if m.bot_id == "paused" {
            Err(StatusError::refuse(
                Code::FailedPrecondition,
                "state",
                "bot paused is Paused: cannot pause",
            ))
        } else {
            Ok(proto::PauseBotResponse {
                bot: Some(bot("Paused", "observe")),
            })
        }),
        "ApplyStagedBot" => call!(proto::ApplyStagedBotRequest, |m| if m.bot_id == "bare" {
            Err(StatusError::refuse(
                Code::FailedPrecondition,
                "state",
                "bot bare is without a staged change: cannot apply a staged change",
            ))
        } else {
            let mut applied = bot("Enabled", "observe");
            applied.version = "1.3.0".into();
            Ok(proto::ApplyStagedBotResponse { bot: Some(applied) })
        }),
        "UnstageBot" => call!(proto::UnstageBotRequest, |m| if m.bot_id == "bare" {
            Err(StatusError::refuse(Code::FailedPrecondition, "state", "nothing staged"))
        } else {
            Ok(proto::UnstageBotResponse {
                bot: Some(bot("Enabled", "observe")),
            })
        }),
        "ListBotEvents" => call!(proto::ListBotEventsRequest, |_m| Ok(proto::ListBotEventsResponse {
            events: vec![event("evt-1")]
        })),
        "ListGroupEvents" => call!(proto::ListGroupEventsRequest, |_m| Ok(proto::ListGroupEventsResponse {
            events: vec![event("evt-1")]
        })),
        "WatchGroupEvents" => watch!(
            proto::WatchGroupEventsRequest,
            proto::WatchGroupEventsResponse,
            vec![Ok(proto::WatchGroupEventsResponse {
                event: Some(event("evt-2"))
            })],
            true
        ),
        "ListGroupTopics" => call!(proto::ListGroupTopicsRequest, |_m| Ok(proto::ListGroupTopicsResponse {
            topics: vec![proto::GroupTopic {
                name: "info.turno".into(),
                publishers: vec!["watcher".into()],
                count: 1,
                nobody_listens: true,
                ..Default::default()
            }],
        })),
        "PublishGroupEvent" => call!(proto::PublishGroupEventRequest, |_m| Ok(
            proto::PublishGroupEventResponse {
                event_id: "evt-7".into(),
                name: "stop.all".into(),
                severity: "critical".into(),
                deliveries: vec![proto::EventDelivery {
                    bot_id: "worker-1".into(),
                    mode: "task".into(),
                    created: true,
                    ..Default::default()
                }],
                created: true,
                ..Default::default()
            }
        )),
        "ListTasks" => call!(proto::ListTasksRequest, |_m| Ok(proto::ListTasksResponse {
            tasks: vec![task("WaitingApproval")]
        })),
        "GetTask" => call!(proto::GetTaskRequest, |m| match m.task_id.as_str() {
            "broken" => Err(StatusError::refuse(Code::Internal, "internal", "disk full")),
            "undated" => Ok(proto::GetTaskResponse {
                task: Some(proto::Task {
                    created_at: None,
                    ..task("Pending")
                })
            }),
            _ => Ok(proto::GetTaskResponse {
                task: Some(task("WaitingApproval"))
            }),
        }),
        "SubmitTask" => call!(proto::SubmitTaskRequest, |_m| Ok(proto::SubmitTaskResponse {
            created: true,
            task: Some(task("Pending")),
        })),
        "CancelTask" => call!(proto::CancelTaskRequest, |_m| Ok(proto::CancelTaskResponse {
            task: Some(task("Cancelled")),
            execution: None,
            changed: true,
        })),
        "DecideTask" => call!(proto::DecideTaskRequest, |_m| {
            let mut decided = execution("Running");
            decided.kind = "decide".into();
            Ok(proto::DecideTaskResponse {
                task: Some(task("Running")),
                execution: Some(decided),
            })
        }),
        "ListTaskExecutions" => call!(proto::ListTaskExecutionsRequest, |_m| Ok(
            proto::ListTaskExecutionsResponse {
                executions: vec![execution("DecisionRequired")]
            }
        )),
        "ListTaskEvents" => call!(proto::ListTaskEventsRequest, |_m| Ok(proto::ListTaskEventsResponse {
            events: vec![event("evt-1")]
        })),
        "WatchTaskEvents" => watch!(
            proto::WatchTaskEventsRequest,
            proto::WatchTaskEventsResponse,
            vec![
                Ok(proto::WatchTaskEventsResponse {
                    event: Some(event("evt-2"))
                }),
                Ok(proto::WatchTaskEventsResponse {
                    event: Some(event("evt-3"))
                }),
            ],
            true
        ),
        "GetArtifact" => {
            use proto::get_artifact_response::Content;
            watch!(
                proto::GetArtifactRequest,
                proto::GetArtifactResponse,
                vec![
                    Ok(proto::GetArtifactResponse {
                        content: Some(Content::Artifact(proto::Artifact {
                            name: "weekly report.md".into(),
                            size: 10,
                            ..Default::default()
                        }))
                    }),
                    Ok(proto::GetArtifactResponse {
                        content: Some(Content::Chunk(b"# Week".to_vec()))
                    }),
                    Ok(proto::GetArtifactResponse {
                        content: Some(Content::Chunk(b" 44\n".to_vec()))
                    }),
                ],
                false
            )
        }
        "GetTaskActivity" => call!(proto::GetTaskActivityRequest, |_m| Ok(proto::GetTaskActivityResponse {
            activity: Some(activity())
        })),
        "WatchTaskActivity" => watch!(
            proto::WatchTaskActivityRequest,
            proto::WatchTaskActivityResponse,
            vec![
                Ok(proto::WatchTaskActivityResponse {
                    cursor: "c1".into(),
                    update: Some(Update::Snapshot(activity())),
                }),
                Ok(proto::WatchTaskActivityResponse {
                    cursor: "c2".into(),
                    update: Some(Update::TaskChanged(proto::TaskChanged {
                        task: Some(task("Completed"))
                    })),
                }),
            ],
            true
        ),
        _ => refused(Status::unimplemented("no such call")),
    }
}

async fn server() -> TestServer {
    let recorder = Recorder::default();
    serve(Router::new().fallback(route).with_state(recorder.clone()), recorder).await
}

fn api(server: &TestServer) -> GrpcControlPlane {
    GrpcControlPlane::new(&server.config()).unwrap()
}

#[tokio::test]
async fn reads_every_call_with_the_token_over_tls() {
    let server = server().await;
    let api = api(&server);
    let since = "2026-10-24T22:00:00Z".parse().unwrap();

    let overview = api.overview(since).await.unwrap();
    assert_eq!((overview.executions_since, overview.since), (1, Some(since)));
    assert_eq!(server.last().path, "GetOverview");
    assert_eq!(server.last().authorization, format!("Bearer {TOKEN}"));

    let nodes = api.list_nodes().await.unwrap();
    assert_eq!(nodes[0].certificate_serial.as_deref(), Some("cef8"));
    assert_eq!(nodes[0].capabilities.node_revision, "4924ad73");
    assert_eq!(
        api.get_node(NodeId::new("wrx80").unwrap()).await.unwrap().host_name,
        "wrx80-node1"
    );
    assert_eq!(api.list_bots().await.unwrap()[0].placement.describe(), "wrx80");
    let bot_id = BotId::new("bot-wrx80").unwrap();
    assert_eq!(api.get_bot(bot_id.clone()).await.unwrap().name, "bot-wrx80");
    assert_eq!(
        api.bot_events(bot_id.clone(), 50).await.unwrap()[0].event_type,
        "bot.task.submitted"
    );
    assert!(server.last().message.contains("limit: 50"), "{}", server.last().message);

    let filter = TaskFilter {
        bot: Some(bot_id),
        status: Some(TaskStatus::WaitingApproval),
        since: Some(since),
        limit: 20,
    };
    assert_eq!(
        api.list_tasks(filter).await.unwrap()[0].status,
        TaskStatus::WaitingApproval
    );
    let message = server.last().message;
    assert!(
        message.contains(r#"bot_id: "bot-wrx80""#)
            && message.contains(r#"status: "WaitingApproval""#)
            && message.contains("limit: 20"),
        "{message}"
    );
    let task_id = TaskId::new("task-1").unwrap();
    assert_eq!(api.get_task(task_id.clone()).await.unwrap().execution_ids.len(), 1);
    let executions = api.task_executions(task_id.clone()).await.unwrap();
    assert_eq!(
        executions[0].result.pending_decision.as_ref().unwrap().tool,
        "local_write"
    );
    assert_eq!(api.task_events(task_id.clone(), 200).await.unwrap().len(), 1);
    let topics = api.group_topics("ops".into()).await.unwrap();
    assert!(topics[0].nobody_listens && topics[0].publishers == vec!["watcher"]);
    let build = api.build().await.unwrap();
    assert_eq!(build.minimum_axlr().unwrap().label(), "v0.14.0");
}

#[tokio::test]
async fn a_long_poll_is_a_watch_that_ends_when_the_events_stop_coming() {
    let server = server().await;
    let api = api(&server);
    let task_id = TaskId::new("task-1").unwrap();
    let started = std::time::Instant::now();
    let live = api
        .task_events_after(task_id.clone(), Some("evt-1".into()), 20)
        .await
        .unwrap();
    assert_eq!(
        live.iter().map(|e| e.event_id.as_str()).collect::<Vec<_>>(),
        ["evt-2", "evt-3"]
    );
    assert!(
        started.elapsed() < std::time::Duration::from_secs(5),
        "waited for the full 20 s"
    );
    assert_eq!(server.last().path, "WatchTaskEvents");
    // Without a wait it is a plain read after the cursor.
    api.task_events_after(task_id, None, 0).await.unwrap();
    assert_eq!(server.last().path, "ListTaskEvents");
    assert!(server.last().message.contains("limit: 1000"));
    let team = api.group_events("ops".into(), Some("evt-1".into()), 20).await.unwrap();
    assert_eq!(team[0].event_id, "evt-2");
    assert_eq!(server.last().path, "WatchGroupEvents");
    api.group_events("ops".into(), None, 0).await.unwrap();
    assert_eq!(server.last().path, "ListGroupEvents");
    assert!(server.last().message.contains("limit: 200"));
}

#[tokio::test]
async fn sends_the_acts_with_their_messages() {
    let server = server().await;
    let api: Arc<dyn ControlPlane> = GrpcConnector.connect(&server.config()).unwrap();
    let bot_id = BotId::new("bot-wrx80").unwrap();
    let task_id = TaskId::new("task-1").unwrap();

    let token = api
        .mint_enrollment_token(EnrollmentRequest::parse("web-2", "zone=eu", "1h").unwrap())
        .await
        .unwrap();
    assert_eq!(token.token.expose(), "axlr1.secret");
    let sent = server.last().message;
    assert!(
        sent.contains(r#"node_id: "web-2""#) && sent.contains("seconds: 3600") && sent.contains(r#""zone""#),
        "{sent}"
    );
    let revoked = api.revoke_node(NodeId::new("wrx80").unwrap()).await.unwrap();
    assert_eq!(revoked.failed_tasks[0].as_str(), "task-9");

    let form = BotForm {
        name: "NanoBot secured on ESP32".into(),
        team: "NANOTEAM".into(),
        mission: "Do it.".into(),
        ..Default::default()
    };
    let registered = api
        .register_bot(BotDefinition::from_form(&form).unwrap())
        .await
        .unwrap();
    assert_eq!(registered.status.name(), "Registered");
    let sent = server.last().message;
    assert!(sent.contains(r#"id: "nanobot-secured-on-esp32""#), "{sent}");
    assert!(sent.contains(r#"name: "NanoBot secured on ESP32""#), "{sent}");
    assert!(sent.contains(r#"groups: ["nanoteam"]"#), "{sent}");

    api.patch_bot(
        bot_id.clone(),
        BotPatch {
            mission: Some("New".into()),
            groups: Some(Vec::new()),
            ..Default::default()
        },
    )
    .await
    .unwrap();
    let sent = server.last().message;
    assert!(
        sent.contains(r#"mission: Some("New")"#) && sent.contains("groups: Some(StringList { values: [] })"),
        "{sent}"
    );
    let deleted = api.delete_bot(bot_id.clone()).await.unwrap();
    assert_eq!(deleted.summary(), "1 task cancelled");
    assert_eq!(deleted.named_by[0].describe(), "watcher may ask it for a task");
    assert!(matches!(
        api.delete_bot(BotId::new("ghost").unwrap()).await.unwrap_err(),
        PanelError::NotFound(_)
    ));
    assert_eq!(api.enable_bot(bot_id.clone()).await.unwrap().status.name(), "Enabled");
    assert_eq!(api.pause_bot(bot_id.clone()).await.unwrap().status.name(), "Paused");
    let refused = api.pause_bot(BotId::new("paused").unwrap()).await.unwrap_err();
    assert!(matches!(refused, PanelError::State(ref m) if m.contains("cannot pause")));

    let submission = TaskSubmission {
        bot_id: bot_id.clone(),
        objective: Objective::new("Count the files").unwrap(),
        key: IdempotencyKey::new("panel-123").unwrap(),
        urgent: false,
    };
    assert!(api.submit_task(submission.clone()).await.unwrap().created);
    let sent = server.last().message;
    assert!(
        sent.contains(r#"idempotency_key: "panel-123""#) && sent.contains(r#"priority: """#),
        "{sent}"
    );
    api.submit_task(TaskSubmission {
        urgent: true,
        ..submission
    })
    .await
    .unwrap();
    assert!(server.last().message.contains(r#"priority: "urgent""#));

    assert!(api.cancel_task(task_id.clone()).await.unwrap().changed);
    let decided = api
        .decide_task(task_id.clone(), Decision::Approve, Some("call-1".into()))
        .await
        .unwrap();
    assert_eq!(decided.execution.kind.name(), "decide");
    let sent = server.last().message;
    assert!(
        sent.contains(r#"decision: "approve""#)
            && sent.contains(r#"call_id: "call-1""#)
            && sent.contains(r#"decided_by: """#),
        "{sent}"
    );
    assert_eq!(server.last().actor, "", "no actor set: none sent");

    let publication = axlr_bots_panel::domain::Publication {
        name: "stop.all".into(),
        message: "Everyone stop".into(),
        data: Some(serde_json::json!({"bpm": 108})),
        to: Vec::new(),
        idempotency_key: "k-1".into(),
    };
    let published = api.publish_event("ops".into(), publication).await.unwrap();
    assert!(published.created && published.deliveries.len() == 1);
    assert!(server.last().message.contains(r#"event_name: "stop.all""#));

    // The actor of the settings goes in the metadata of every call.
    let mut config = server.config();
    config.actor = Some("alice".into());
    let named = GrpcControlPlane::new(&config).unwrap();
    named.decide_task(task_id, Decision::Approve, None).await.unwrap();
    assert_eq!(server.last().actor, "alice");
}

#[tokio::test]
async fn fetches_an_artifact_from_its_chunks() {
    let server = server().await;
    let api = api(&server);
    let bytes = api
        .artifact(ExecutionId::new("exec-1").unwrap(), "weekly report.md".into())
        .await
        .unwrap();
    assert_eq!(bytes, b"# Week 44\n");
    assert_eq!(server.last().authorization, format!("Bearer {TOKEN}"));
}

#[tokio::test]
async fn reads_versions_and_applies_or_drops_a_staged_change() {
    let server = server().await;
    let api = api(&server);
    let bot_id = BotId::new("bot-wrx80").unwrap();
    let applied = api.apply_staged(bot_id.clone()).await.unwrap();
    assert_eq!((applied.version.as_str(), applied.staged.is_none()), ("1.3.0", true));
    assert!(api.unstage(bot_id).await.unwrap().staged.is_none());
    let bare = BotId::new("bare").unwrap();
    let nothing = api.apply_staged(bare.clone()).await.unwrap_err();
    assert!(
        matches!(nothing, PanelError::State(ref m) if m.contains("without a staged change")),
        "{nothing:?}"
    );
    assert!(matches!(api.unstage(bare).await.unwrap_err(), PanelError::State(_)));
}

#[tokio::test]
async fn reads_the_activity_tree_and_follows_it() {
    let server = server().await;
    let api = api(&server);
    let id = TaskId::new("task-1").unwrap();
    let tree = api.task_activity(id.clone()).await.unwrap();
    assert!(tree.executions[0].progress_cut.is_some());
    assert_eq!(tree.subtree_usage.requests, 2);
    assert_eq!(server.last().path, "GetTaskActivity");

    let batch = api.watch_task_activity(id.clone(), None, 5).await.unwrap();
    assert_eq!(batch.cursor.as_deref(), Some("c2"));
    let mut mirror = match &batch.updates[0] {
        ActivityUpdate::Snapshot(tree) => (**tree).clone(),
        other => panic!("{other:?}"),
    };
    mirror.apply(batch.updates[1].clone());
    assert!(mirror.finished());
    assert_eq!(server.last().path, "WatchTaskActivity");
}

#[tokio::test]
async fn sorts_the_failures() {
    let server = server().await;
    let api = api(&server);
    assert!(matches!(
        api.get_node(NodeId::new("ghost").unwrap()).await,
        Err(PanelError::NotFound(_))
    ));
    assert!(matches!(
        api.get_task(TaskId::new("undated").unwrap()).await,
        Err(PanelError::Unreadable(_))
    ));
    assert!(
        matches!(api.get_task(TaskId::new("broken").unwrap()).await, Err(PanelError::Server(ref m)) if m == "disk full")
    );

    std::fs::write(server.dir.path().join("api.token"), "wrong-token").unwrap();
    let wrong = GrpcControlPlane::new(&server.config()).unwrap();
    assert!(matches!(wrong.list_nodes().await, Err(PanelError::Unauthorized(_))));

    let mut closed = server.config();
    closed.api_url = "https://127.0.0.1:9/".into();
    let unreachable = GrpcControlPlane::new(&closed).unwrap();
    assert!(matches!(
        unreachable.list_nodes().await,
        Err(PanelError::Unreachable(_))
    ));
}

#[tokio::test]
async fn a_control_plane_without_the_activity_service_answers_not_found() {
    // The route answers UNIMPLEMENTED to a call it does not know.
    let recorder = Recorder::default();
    let app = Router::new().fallback(|| async { refused(Status::unimplemented("no such call")) });
    let server = serve(app, recorder).await;
    let error = api(&server)
        .task_activity(TaskId::new("task-1").unwrap())
        .await
        .unwrap_err();
    assert!(matches!(error, PanelError::NotFound(_)), "{error:?}");
}

#[tokio::test]
async fn a_control_plane_that_only_speaks_http_is_told_apart() {
    // What a control plane without --grpc-listen says to a gRPC call: an
    // HTTP 404 with no gRPC status, from a server that does speak HTTP/2.
    let recorder = Recorder::default();
    let app = Router::new().fallback(|| async { (axum::http::StatusCode::NOT_FOUND, "404 page not found") });
    let server = serve(app, recorder).await;
    let error = api(&server).list_nodes().await.unwrap_err();
    assert!(matches!(error, PanelError::NotFound(_)), "{error:?}");
    let connect = axlr_bots_panel::application::Connect::new(
        Arc::new(GrpcConnector),
        Arc::new(axlr_bots_panel::adapters::fake::MemoryConfigStore::new(
            None,
            server.config(),
        )),
        Arc::new(axlr_bots_panel::adapters::fake::FixedClock::new(
            "2026-10-25T10:00:00Z".parse().unwrap(),
        )),
    );
    let told = connect.try_config(&server.config()).await.err().unwrap();
    assert!(
        matches!(&told, PanelError::Config(m) if m.contains("gRPC") && m.contains("--grpc-listen")),
        "{told:?}"
    );
}

#[tokio::test]
async fn trusts_the_fleet_ca_alone() {
    let server = server().await;
    // Another CA: the server's certificate does not chain to it.
    let other_key = KeyPair::generate().unwrap();
    let mut other = CertificateParams::new(Vec::<String>::new()).unwrap();
    other.is_ca = IsCa::Ca(BasicConstraints::Unconstrained);
    let other_ca = other.self_signed(&other_key).unwrap();
    std::fs::write(server.dir.path().join("ca.crt"), other_ca.pem()).unwrap();
    let refused = api(&server).list_nodes().await.unwrap_err();
    assert!(matches!(refused, PanelError::Unreachable(_)), "{refused:?}");
    assert!(server.calls().is_empty(), "the call must not reach the server");
}

#[tokio::test]
async fn refuses_settings_it_cannot_use() {
    let dir = tempfile::tempdir().unwrap();
    let config = |ca: &str, token: &str| PanelConfig {
        api_url: "https://127.0.0.1:7879".into(),
        ca_path: dir.path().join(ca),
        token_file: dir.path().join(token),
        actor: None,
        cert_path: None,
        key_path: None,
    };
    std::fs::write(dir.path().join("bad.crt"), "not a certificate").unwrap();
    std::fs::write(dir.path().join("empty.token"), "\n").unwrap();
    let check = |c: PanelConfig| GrpcControlPlane::new(&c).err().unwrap();
    assert!(matches!(check(config("missing.crt", "t")), PanelError::Config(ref m) if m.contains("cannot read the CA")));
    assert!(matches!(check(config("bad.crt", "t")), PanelError::Config(_)));
    let key = KeyPair::generate().unwrap();
    let mut params = CertificateParams::new(Vec::<String>::new()).unwrap();
    params.is_ca = IsCa::Ca(BasicConstraints::Unconstrained);
    std::fs::write(dir.path().join("ca.crt"), params.self_signed(&key).unwrap().pem()).unwrap();
    assert!(matches!(check(config("ca.crt", "missing.token")), PanelError::Config(ref m) if m.contains("token file")));
    assert!(matches!(check(config("ca.crt", "empty.token")), PanelError::Config(ref m) if m.contains("empty")));
    let mut plain = config("ca.crt", "empty.token");
    plain.api_url = "http://127.0.0.1:7879".into();
    assert!(matches!(check(plain), PanelError::Config(ref m) if m.contains("https")));
    std::fs::write(dir.path().join("bad.token"), "to\nken").unwrap();
    assert!(matches!(check(config("ca.crt", "bad.token")), PanelError::Config(ref m) if m.contains("cannot carry")));
}

#[tokio::test]
async fn presents_the_operators_certificate_and_says_what_is_missing() {
    let server = server().await;
    assert!(api(&server).list_nodes().await.is_ok());

    // A certificate of another CA: the handshake is refused before any call.
    let mut stranger = server.config();
    stranger.cert_path = Some(server.dir.path().join("stranger.crt"));
    stranger.key_path = Some(server.dir.path().join("stranger.key"));
    let calls = server.calls().len();
    let refused = GrpcControlPlane::new(&stranger)
        .unwrap()
        .list_nodes()
        .await
        .unwrap_err();
    assert!(
        matches!(&refused, PanelError::Forbidden(m) if m.contains("ca operator issue")),
        "{refused:?}"
    );
    assert_eq!(server.calls().len(), calls, "the call must not reach the handlers");

    // No file where the settings say, or where `ca operator issue` leaves them.
    let mut missing = server.config();
    missing.cert_path = Some(server.dir.path().join("none.crt"));
    let error = GrpcControlPlane::new(&missing).err().unwrap();
    assert!(
        matches!(&error, PanelError::Config(m) if m.contains("ca operator issue") && m.contains("none.crt")),
        "{error:?}"
    );
    let mut not_pem = server.config();
    std::fs::write(server.dir.path().join("bad.crt"), "nope").unwrap();
    not_pem.cert_path = Some(server.dir.path().join("bad.crt"));
    assert!(
        matches!(GrpcControlPlane::new(&not_pem).err().unwrap(), PanelError::Config(ref m) if m.contains("PEM certificate"))
    );
    let mut not_key = server.config();
    std::fs::write(server.dir.path().join("bad.key"), "nope").unwrap();
    not_key.key_path = Some(server.dir.path().join("bad.key"));
    assert!(
        matches!(GrpcControlPlane::new(&not_key).err().unwrap(), PanelError::Config(ref m) if m.contains("PEM private key"))
    );
}
