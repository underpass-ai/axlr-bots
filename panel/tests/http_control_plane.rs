//! The HTTPS adapter against a local TLS server that plays the operator
//! API: a CA made for the test signs the server's certificate, and the
//! adapter trusts that CA alone.

use std::net::SocketAddr;
use std::sync::{Arc, Mutex};

use axlr_bots_panel::adapters::http::{HttpConnector, HttpControlPlane};
use axlr_bots_panel::application::{Connector, ControlPlane, PanelConfig, PanelError};
use axlr_bots_panel::domain::{
    BotDefinition, BotForm, BotId, BotPatch, Decision, EnrollmentRequest, IdempotencyKey, NodeId, Objective,
    TaskFilter, TaskId, TaskStatus, TaskSubmission,
};
use axum::Router;
use axum::body::Bytes;
use axum::extract::{OriginalUri, State};
use axum::http::{HeaderMap, Method, StatusCode};
use axum::response::{IntoResponse, Response};
use axum_server::Handle;
use axum_server::tls_rustls::RustlsConfig;
use rcgen::{BasicConstraints, CertificateParams, IsCa, Issuer, KeyPair};
use rustls::pki_types::{CertificateDer, PrivateKeyDer, PrivatePkcs8KeyDer};

const TOKEN: &str = "operator-token-for-tests";

#[derive(Debug, Clone)]
struct Recorded {
    method: Method,
    path: String,
    authorization: String,
    idempotency_key: String,
    body: String,
}

#[derive(Clone, Default)]
struct Recorder {
    requests: Arc<Mutex<Vec<Recorded>>>,
}

const NODE: &str = r#"{"id":"wrx80","hostName":"wrx80-node1","labels":{"gpu":"rtx3090"},"capabilities":{"policySupported":true,"correlationSupported":true},"status":"Online","registeredAt":"2026-10-24T22:32:07Z","lastSeenAt":"2026-10-24T22:41:08Z","certificateSerial":"cef8","certificateNotAfter":"2027-10-24T22:32:07Z"}"#;
const BOT: &str = r#"{"id":"bot-wrx80","tenantId":"default","name":"bot-wrx80","owner":"operator","mission":"Do it.","allowedTools":["local_read"],"model":{"model":"anthropic/claude-haiku-5.5","routingPolicy":"cost-optimized","allowRemoteProviders":true,"maxContextTokens":32000},"budgets":{"maxModelCallsPerTask":10,"maxToolCallsPerTask":20,"maxDelegationDepth":0,"taskTimeoutSeconds":300},"level":"observe","toolRules":{},"triggers":[{"type":"manual"}],"placement":{"nodes":["wrx80"]},"status":"Enabled","createdAt":"2026-10-24T22:30:00Z","updatedAt":"2026-10-24T22:31:00Z"}"#;
const TASK: &str = r#"{"id":"task-1","botId":"bot-wrx80","objective":"Count","status":"WaitingApproval","idempotencyKey":"k","correlationId":"trace-1","trigger":{"type":"manual","name":"manual"},"executionIds":["exec-1"],"createdAt":"2026-10-24T22:32:26Z","updatedAt":"2026-10-24T22:32:33Z"}"#;
const EXECUTION: &str = r#"{"id":"exec-1","taskId":"task-1","botId":"bot-wrx80","startedAt":"2026-10-24T22:32:26Z","endedAt":"2026-10-24T22:32:33Z","outcome":"DecisionRequired","result":{"answer":"","pendingDecision":{"tool":"local_write","callId":"c1","sessionId":"s1","arguments":{"path":"a.txt"}}},"usage":{"requests":2,"costUsd":0.001},"kind":"run","nodeId":"wrx80"}"#;
const EVENT: &str = r#"{"eventId":"evt-1","schemaVersion":"underpass.ai/events/v1","type":"bot.task.submitted","tenantId":"default","botId":"bot-wrx80","taskId":"task-1","createdAt":"2026-10-24T22:32:26Z"}"#;
const OVERVIEW: &str = r#"{"since":"2026-10-24T00:00:00Z","nodes":{"Online":2},"bots":{"Enabled":3},"tasks":{"Completed":5},"tasksSince":{"Completed":1},"executionsSince":1,"costUsdSince":0.5}"#;

async fn route(
    State(recorder): State<Recorder>,
    method: Method,
    OriginalUri(uri): OriginalUri,
    headers: HeaderMap,
    body: Bytes,
) -> Response {
    let header = |name: &str| {
        headers
            .get(name)
            .and_then(|v| v.to_str().ok())
            .unwrap_or_default()
            .to_string()
    };
    let path = uri.path_and_query().map(|p| p.to_string()).unwrap_or_default();
    recorder.requests.lock().unwrap().push(Recorded {
        method: method.clone(),
        path: path.clone(),
        authorization: header("authorization"),
        idempotency_key: header("idempotency-key"),
        body: String::from_utf8_lossy(&body).to_string(),
    });
    if header("authorization") != format!("Bearer {TOKEN}") {
        return json(
            StatusCode::UNAUTHORIZED,
            r#"{"kind":"unauthorized","error":"the bearer token is missing or wrong"}"#,
        );
    }
    let plain = uri.path();
    let ok = |body: String| json(StatusCode::OK, &body);
    match (method.as_str(), plain) {
        ("GET", "/v1/overview") => ok(OVERVIEW.into()),
        ("GET", "/v1/nodes") => ok(format!("[{NODE}]")),
        ("GET", "/v1/nodes/wrx80") => ok(NODE.into()),
        ("GET", "/v1/nodes/ghost") => json(
            StatusCode::NOT_FOUND,
            r#"{"kind":"not_found","error":"node ghost not found"}"#,
        ),
        ("POST", "/v1/enrollments") => json(
            StatusCode::CREATED,
            r#"{"token":"axlr1.secret","id":"enr-1","nodeId":"web-2","labels":{"zone":"eu"},"expiresAt":"2026-10-25T01:00:00Z"}"#,
        ),
        ("POST", "/v1/nodes/wrx80/revoke") => ok(format!(
            r#"{{"node":{},"failedTasks":["task-9"]}}"#,
            NODE.replace("Online", "Revoked")
        )),
        ("GET", "/v1/bots") => ok(format!("[{BOT}]")),
        ("GET", "/v1/bots/bot-wrx80") => ok(BOT.into()),
        ("POST", "/v1/bots") => json(StatusCode::CREATED, &BOT.replace("Enabled", "Registered")),
        ("PATCH", "/v1/bots/bot-wrx80") => ok(BOT.replace("\"observe\"", "\"propose\"")),
        ("POST", "/v1/bots/bot-wrx80/enable") => ok(BOT.into()),
        ("POST", "/v1/bots/bot-wrx80/pause") => ok(BOT.replace("Enabled", "Paused")),
        ("POST", "/v1/bots/paused/pause") => json(
            StatusCode::CONFLICT,
            r#"{"kind":"state","error":"bot paused is Paused: cannot pause"}"#,
        ),
        ("GET", "/v1/bots/bot-wrx80/events") => ok(format!("[{EVENT}]")),
        ("GET", "/v1/groups/port/events") => ok(format!("[{EVENT}]")),
        ("GET", "/v1/tasks") => ok(format!("[{TASK}]")),
        ("GET", "/v1/tasks/task-1") => ok(TASK.into()),
        ("POST", "/v1/bots/bot-wrx80/tasks") => json(
            StatusCode::CREATED,
            &format!(
                r#"{{"created":true,"task":{}}}"#,
                TASK.replace("WaitingApproval", "Pending")
            ),
        ),
        ("POST", "/v1/tasks/task-1/cancel") => ok(format!(
            r#"{{"task":{},"changed":true}}"#,
            TASK.replace("WaitingApproval", "Cancelled")
        )),
        ("POST", "/v1/tasks/task-1/decide") => ok(format!(
            r#"{{"task":{},"execution":{}}}"#,
            TASK.replace("WaitingApproval", "Running"),
            EXECUTION
                .replace("DecisionRequired", "Running")
                .replace("\"run\"", "\"decide\"")
        )),
        ("GET", "/v1/tasks/task-1/executions") => ok(format!("[{EXECUTION}]")),
        ("GET", "/v1/tasks/task-1/events") => ok(format!("[{EVENT}]")),
        ("GET", "/v1/tasks/garbled") => ok("{not json".into()),
        ("GET", "/v1/tasks/broken") => json(
            StatusCode::INTERNAL_SERVER_ERROR,
            r#"{"kind":"internal","error":"disk full"}"#,
        ),
        _ => json(StatusCode::NOT_FOUND, r#"{"kind":"not_found","error":"no route"}"#),
    }
}

fn json(status: StatusCode, body: &str) -> Response {
    (status, [("content-type", "application/json")], body.to_string()).into_response()
}

struct TestServer {
    address: SocketAddr,
    recorder: Recorder,
    dir: tempfile::TempDir,
    handle: Handle,
}

impl TestServer {
    fn config(&self) -> PanelConfig {
        PanelConfig {
            api_url: format!("https://127.0.0.1:{}/", self.address.port()),
            ca_path: self.dir.path().join("ca.crt"),
            token_file: self.dir.path().join("api.token"),
        }
    }

    fn requests(&self) -> Vec<Recorded> {
        self.recorder.requests.lock().unwrap().clone()
    }

    fn last(&self) -> Recorded {
        self.requests().last().cloned().unwrap()
    }
}

impl Drop for TestServer {
    fn drop(&mut self) {
        self.handle.shutdown();
    }
}

/// A fleet CA, a server certificate for 127.0.0.1 it signs, the CA and
/// the token written where the panel reads them, and the server running.
async fn serve() -> TestServer {
    let ca_key = KeyPair::generate().unwrap();
    let mut ca_params = CertificateParams::new(Vec::<String>::new()).unwrap();
    ca_params.is_ca = IsCa::Ca(BasicConstraints::Unconstrained);
    ca_params
        .distinguished_name
        .push(rcgen::DnType::CommonName, "test fleet CA");
    let ca_cert = ca_params.self_signed(&ca_key).unwrap();
    let issuer = Issuer::new(ca_params, &ca_key);
    let server_key = KeyPair::generate().unwrap();
    let server_params = CertificateParams::new(vec!["127.0.0.1".to_string(), "localhost".to_string()]).unwrap();
    let server_cert = server_params.signed_by(&server_key, &issuer).unwrap();

    let dir = tempfile::tempdir().unwrap();
    std::fs::write(dir.path().join("ca.crt"), ca_cert.pem()).unwrap();
    std::fs::write(dir.path().join("api.token"), format!("{TOKEN}\n")).unwrap();

    let provider = Arc::new(rustls::crypto::ring::default_provider());
    let tls = rustls::ServerConfig::builder_with_provider(provider)
        .with_safe_default_protocol_versions()
        .unwrap()
        .with_no_client_auth()
        .with_single_cert(
            vec![CertificateDer::from(server_cert.der().to_vec())],
            PrivateKeyDer::Pkcs8(PrivatePkcs8KeyDer::from(server_key.serialize_der())),
        )
        .unwrap();
    let recorder = Recorder::default();
    let app = Router::new().fallback(route).with_state(recorder.clone());
    let handle = Handle::new();
    let server = axum_server::bind_rustls("127.0.0.1:0".parse().unwrap(), RustlsConfig::from_config(Arc::new(tls)))
        .handle(handle.clone());
    tokio::spawn(server.serve(app.into_make_service()));
    let address = handle.listening().await.unwrap();
    TestServer {
        address,
        recorder,
        dir,
        handle,
    }
}

#[tokio::test]
async fn reads_every_route_with_the_token_over_tls() {
    let server = serve().await;
    let api = HttpControlPlane::new(&server.config()).unwrap();
    let since = "2026-10-24T22:00:00Z".parse().unwrap();

    let overview = api.overview(since).await.unwrap();
    assert_eq!(overview.executions_since, 1);
    assert_eq!(server.last().path, "/v1/overview?since=2026-10-24T22%3A00%3A00Z");
    assert_eq!(server.last().authorization, format!("Bearer {TOKEN}"));

    let nodes = api.list_nodes().await.unwrap();
    assert_eq!(nodes[0].certificate_serial.as_deref(), Some("cef8"));
    assert_eq!(
        api.get_node(NodeId::new("wrx80").unwrap()).await.unwrap().host_name,
        "wrx80-node1"
    );
    let bots = api.list_bots().await.unwrap();
    assert_eq!(bots[0].placement.describe(), "wrx80");
    let bot_id = BotId::new("bot-wrx80").unwrap();
    assert_eq!(api.get_bot(bot_id.clone()).await.unwrap().name, "bot-wrx80");
    let events = api.bot_events(bot_id.clone(), 50).await.unwrap();
    assert_eq!(events[0].event_type, "bot.task.submitted");
    assert_eq!(server.last().path, "/v1/bots/bot-wrx80/events?limit=50");

    let filter = TaskFilter {
        bot: Some(bot_id.clone()),
        status: Some(TaskStatus::WaitingApproval),
        since: Some(since),
        limit: 20,
    };
    let tasks = api.list_tasks(filter).await.unwrap();
    assert_eq!(tasks[0].status, TaskStatus::WaitingApproval);
    assert_eq!(
        server.last().path,
        "/v1/tasks?limit=20&bot=bot-wrx80&status=WaitingApproval&since=2026-10-24T22%3A00%3A00Z"
    );
    let task_id = TaskId::new("task-1").unwrap();
    assert_eq!(api.get_task(task_id.clone()).await.unwrap().execution_ids.len(), 1);
    let executions = api.task_executions(task_id.clone()).await.unwrap();
    assert_eq!(
        executions[0].result.pending_decision.as_ref().unwrap().tool,
        "local_write"
    );
    assert_eq!(api.task_events(task_id.clone(), 200).await.unwrap().len(), 1);
    assert_eq!(server.last().path, "/v1/tasks/task-1/events?limit=200");
    let team = api.group_events("port".into(), Some("evt-9".into()), 20).await.unwrap();
    assert_eq!(team[0].event_type, "bot.task.submitted");
    assert_eq!(
        server.last().path,
        "/v1/groups/port/events?limit=200&after=evt-9&wait=20s"
    );
    api.group_events("port".into(), None, 0).await.unwrap();
    assert_eq!(server.last().path, "/v1/groups/port/events?limit=200");
}

#[tokio::test]
async fn sends_the_acts_with_their_bodies() {
    let server = serve().await;
    let api: Arc<dyn ControlPlane> = HttpConnector.connect(&server.config()).unwrap();
    let bot_id = BotId::new("bot-wrx80").unwrap();
    let task_id = TaskId::new("task-1").unwrap();

    let token = api
        .mint_enrollment_token(EnrollmentRequest::parse("web-2", "zone=eu", "1h").unwrap())
        .await
        .unwrap();
    assert_eq!(token.token.expose(), "axlr1.secret");
    assert_eq!(
        server.last().body,
        r#"{"nodeId":"web-2","labels":{"zone":"eu"},"ttl":"1h"}"#
    );

    let revoked = api.revoke_node(NodeId::new("wrx80").unwrap()).await.unwrap();
    assert_eq!(revoked.failed_tasks[0].as_str(), "task-9");
    assert_eq!(
        (server.last().method, server.last().body),
        (Method::POST, String::new())
    );

    let form = BotForm {
        name: "bot-wrx80".into(),
        mission: "Do it.".into(),
        ..Default::default()
    };
    let registered = api
        .register_bot(BotDefinition::from_form(&form).unwrap())
        .await
        .unwrap();
    assert_eq!(registered.status.name(), "Registered");
    assert!(server.last().body.contains(r#""mission":"Do it.""#));

    let patch = BotPatch {
        mission: Some("New".into()),
        ..Default::default()
    };
    api.patch_bot(bot_id.clone(), patch).await.unwrap();
    assert_eq!(
        (server.last().method, server.last().body),
        (Method::PATCH, r#"{"mission":"New"}"#.to_string())
    );
    let teams = BotPatch {
        groups: Some(vec!["port".into()]),
        ..Default::default()
    };
    api.patch_bot(bot_id.clone(), teams).await.unwrap();
    assert_eq!(server.last().body, r#"{"groups":["port"]}"#);
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
    let submitted = api.submit_task(submission.clone()).await.unwrap();
    assert!(submitted.created);
    assert_eq!(server.last().idempotency_key, "panel-123");
    assert_eq!(server.last().body, r#"{"objective":"Count the files"}"#);
    let urgent = TaskSubmission {
        urgent: true,
        ..submission
    };
    api.submit_task(urgent).await.unwrap();
    assert_eq!(
        server.last().body,
        r#"{"objective":"Count the files","priority":"urgent"}"#
    );

    assert!(api.cancel_task(task_id.clone()).await.unwrap().changed);
    let decided = api.decide_task(task_id, Decision::Approve).await.unwrap();
    assert_eq!(decided.execution.kind.name(), "decide");
    assert_eq!(server.last().body, r#"{"decision":"approve"}"#);
}

#[tokio::test]
async fn sorts_the_failures() {
    let server = serve().await;
    let api = HttpControlPlane::new(&server.config()).unwrap();
    assert!(matches!(
        api.get_node(NodeId::new("ghost").unwrap()).await,
        Err(PanelError::NotFound(_))
    ));
    assert!(matches!(
        api.get_task(TaskId::new("garbled").unwrap()).await,
        Err(PanelError::Unreadable(_))
    ));
    assert!(
        matches!(api.get_task(TaskId::new("broken").unwrap()).await, Err(PanelError::Server(ref m)) if m == "disk full")
    );

    std::fs::write(server.dir.path().join("api.token"), "wrong-token").unwrap();
    let wrong = HttpControlPlane::new(&server.config()).unwrap();
    assert!(matches!(wrong.list_nodes().await, Err(PanelError::Unauthorized(_))));

    let mut closed = server.config();
    closed.api_url = "https://127.0.0.1:9/".into();
    let unreachable = HttpControlPlane::new(&closed).unwrap();
    assert!(matches!(
        unreachable.list_nodes().await,
        Err(PanelError::Unreachable(_))
    ));
}

#[tokio::test]
async fn trusts_the_fleet_ca_alone() {
    let server = serve().await;
    // Another CA: the server's certificate does not chain to it.
    let other_key = KeyPair::generate().unwrap();
    let mut other = CertificateParams::new(Vec::<String>::new()).unwrap();
    other.is_ca = IsCa::Ca(BasicConstraints::Unconstrained);
    let other_ca = other.self_signed(&other_key).unwrap();
    std::fs::write(server.dir.path().join("ca.crt"), other_ca.pem()).unwrap();
    let api = HttpControlPlane::new(&server.config()).unwrap();
    let refused = api.list_nodes().await.unwrap_err();
    assert!(
        matches!(refused, PanelError::Unreachable(ref m) if m.to_lowercase().contains("certificate")),
        "{refused:?}"
    );
    assert!(server.requests().is_empty());
}

#[test]
fn refuses_settings_it_cannot_use() {
    let dir = tempfile::tempdir().unwrap();
    let config = |ca: &str, token: &str| PanelConfig {
        api_url: "https://127.0.0.1:7879".into(),
        ca_path: dir.path().join(ca),
        token_file: dir.path().join(token),
    };
    std::fs::write(dir.path().join("bad.crt"), "not a certificate").unwrap();
    std::fs::write(dir.path().join("empty.token"), "\n").unwrap();
    let check = |c: PanelConfig| HttpControlPlane::new(&c).err().unwrap();
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
}
