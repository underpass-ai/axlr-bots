use std::time::Duration;

use chrono::{DateTime, SecondsFormat, Utc};
use reqwest::{Client, Method, RequestBuilder};
use serde::Serialize;
use serde::de::DeserializeOwned;

use super::api_error::ApiError;
use crate::application::{ControlPlane, PanelConfig, PanelError, PortFuture};
use crate::domain::{
    Bot, BotDefinition, BotId, BotPatch, CancelledTask, DecidedTask, Decision, EnrollmentRequest, EnrollmentToken,
    Event, Execution, Node, NodeId, Overview, RevokedNode, Secret, SubmittedTask, Task, TaskFilter, TaskId,
    TaskSubmission,
};

/// How long a read may take.
const READ_TIMEOUT: Duration = Duration::from_secs(15);
/// How long an act may take: a decision hands the run to a node first.
const ACT_TIMEOUT: Duration = Duration::from_secs(60);

/// The operator API over HTTPS: TLS 1.2+ trusting the fleet CA alone, and
/// the bearer token read from its file on every connect.
pub struct HttpControlPlane {
    client: Client,
    base: String,
    token: Secret,
}

impl HttpControlPlane {
    /// A client for the settings: reads the CA and the token file now, so
    /// a missing or unreadable file is a settings error before any call.
    pub fn new(config: &PanelConfig) -> Result<Self, PanelError> {
        config.validate()?;
        let ca = std::fs::read(&config.ca_path)
            .map_err(|e| PanelError::Config(format!("cannot read the CA {}: {e}", config.ca_path.display())))?;
        let certificate = reqwest::Certificate::from_pem(&ca).map_err(|e| {
            PanelError::Config(format!(
                "the CA {} is not a PEM certificate: {e}",
                config.ca_path.display()
            ))
        })?;
        let token = std::fs::read_to_string(&config.token_file).map_err(|e| {
            PanelError::Config(format!(
                "cannot read the token file {}: {e}",
                config.token_file.display()
            ))
        })?;
        let token = token.trim();
        if token.is_empty() {
            return Err(PanelError::Config(format!(
                "the token file {} is empty",
                config.token_file.display()
            )));
        }
        let client = Client::builder()
            .use_rustls_tls()
            .tls_built_in_root_certs(false)
            .add_root_certificate(certificate)
            .https_only(true)
            .connect_timeout(Duration::from_secs(5))
            .build()
            .map_err(|e| PanelError::Config(format!("cannot build the HTTPS client: {e}")))?;
        Ok(Self {
            client,
            base: config.base_url(),
            token: Secret::new(token),
        })
    }

    fn request(&self, method: Method, path: &str, timeout: Duration) -> RequestBuilder {
        self.client
            .request(method, format!("{}{path}", self.base))
            .bearer_auth(self.token.expose())
            .timeout(timeout)
    }

    fn get<T: DeserializeOwned + Send + 'static>(&self, path: String) -> PortFuture<'_, T> {
        let request = self.request(Method::GET, &path, READ_TIMEOUT);
        Box::pin(send(request))
    }

    fn get_query<T: DeserializeOwned + Send + 'static>(
        &self,
        path: String,
        query: Vec<(&'static str, String)>,
    ) -> PortFuture<'_, T> {
        let request = self.request(Method::GET, &path, READ_TIMEOUT).query(&query);
        Box::pin(send(request))
    }

    fn post<T: DeserializeOwned + Send + 'static>(
        &self,
        path: String,
        body: Option<impl Serialize>,
    ) -> PortFuture<'_, T> {
        self.act(Method::POST, path, body)
    }

    fn act<T: DeserializeOwned + Send + 'static>(
        &self,
        method: Method,
        path: String,
        body: Option<impl Serialize>,
    ) -> PortFuture<'_, T> {
        let mut request = self.request(method, &path, ACT_TIMEOUT);
        if let Some(body) = body {
            request = request.json(&body);
        }
        Box::pin(send(request))
    }
}

/// Sends the request and reads the answer: the value on a 2xx, the
/// classified error otherwise.
async fn send<T: DeserializeOwned>(request: RequestBuilder) -> Result<T, PanelError> {
    let response = request.send().await.map_err(transport_error)?;
    let status = response.status();
    let body = response.bytes().await.map_err(transport_error)?;
    if !status.is_success() {
        return Err(ApiError::classify(status.as_u16(), &body));
    }
    serde_json::from_slice(&body).map_err(|e| PanelError::Unreadable(format!("the control plane's answer: {e}")))
}

fn transport_error(error: reqwest::Error) -> PanelError {
    let mut message = error.to_string();
    let mut source = std::error::Error::source(&error);
    while let Some(cause) = source {
        message = format!("{message}: {cause}");
        source = cause.source();
    }
    if error.is_timeout() {
        PanelError::Unreachable(format!("no answer in time: {message}"))
    } else if error.is_decode() {
        PanelError::Unreadable(message)
    } else {
        PanelError::Unreachable(message)
    }
}

#[derive(Serialize)]
struct ObjectiveBody<'a> {
    objective: &'a str,
}

#[derive(Serialize)]
struct DecisionBody {
    decision: Decision,
}

/// No body: a POST that carries nothing.
const NO_BODY: Option<()> = None;

fn rfc3339(moment: DateTime<Utc>) -> String {
    moment.to_rfc3339_opts(SecondsFormat::Secs, true)
}

impl ControlPlane for HttpControlPlane {
    fn overview(&self, since: DateTime<Utc>) -> PortFuture<'_, Overview> {
        self.get_query("/v1/overview".into(), vec![("since", rfc3339(since))])
    }

    fn list_nodes(&self) -> PortFuture<'_, Vec<Node>> {
        self.get("/v1/nodes".into())
    }

    fn get_node(&self, id: NodeId) -> PortFuture<'_, Node> {
        self.get(format!("/v1/nodes/{id}"))
    }

    fn mint_enrollment_token(&self, request: EnrollmentRequest) -> PortFuture<'_, EnrollmentToken> {
        self.post("/v1/enrollments".into(), Some(request))
    }

    fn revoke_node(&self, id: NodeId) -> PortFuture<'_, RevokedNode> {
        self.post(format!("/v1/nodes/{id}/revoke"), NO_BODY)
    }

    fn list_bots(&self) -> PortFuture<'_, Vec<Bot>> {
        self.get("/v1/bots".into())
    }

    fn get_bot(&self, id: BotId) -> PortFuture<'_, Bot> {
        self.get(format!("/v1/bots/{id}"))
    }

    fn register_bot(&self, definition: BotDefinition) -> PortFuture<'_, Bot> {
        self.post("/v1/bots".into(), Some(definition))
    }

    fn patch_bot(&self, id: BotId, patch: BotPatch) -> PortFuture<'_, Bot> {
        self.act(Method::PATCH, format!("/v1/bots/{id}"), Some(patch))
    }

    fn enable_bot(&self, id: BotId) -> PortFuture<'_, Bot> {
        self.post(format!("/v1/bots/{id}/enable"), NO_BODY)
    }

    fn pause_bot(&self, id: BotId) -> PortFuture<'_, Bot> {
        self.post(format!("/v1/bots/{id}/pause"), NO_BODY)
    }

    fn bot_events(&self, id: BotId, limit: u32) -> PortFuture<'_, Vec<Event>> {
        self.get_query(format!("/v1/bots/{id}/events"), vec![("limit", limit.to_string())])
    }

    fn list_tasks(&self, filter: TaskFilter) -> PortFuture<'_, Vec<Task>> {
        let mut query = vec![("limit", filter.limit.to_string())];
        if let Some(bot) = filter.bot {
            query.push(("bot", bot.to_string()));
        }
        if let Some(status) = filter.status {
            query.push(("status", status.name().to_string()));
        }
        if let Some(since) = filter.since {
            query.push(("since", rfc3339(since)));
        }
        self.get_query("/v1/tasks".into(), query)
    }

    fn get_task(&self, id: TaskId) -> PortFuture<'_, Task> {
        self.get(format!("/v1/tasks/{id}"))
    }

    fn submit_task(&self, submission: TaskSubmission) -> PortFuture<'_, SubmittedTask> {
        let request = self
            .request(
                Method::POST,
                &format!("/v1/bots/{}/tasks", submission.bot_id),
                ACT_TIMEOUT,
            )
            .header("Idempotency-Key", submission.key.as_str())
            .json(&ObjectiveBody {
                objective: submission.objective.as_str(),
            });
        Box::pin(send(request))
    }

    fn cancel_task(&self, id: TaskId) -> PortFuture<'_, CancelledTask> {
        self.post(format!("/v1/tasks/{id}/cancel"), NO_BODY)
    }

    fn decide_task(&self, id: TaskId, decision: Decision) -> PortFuture<'_, DecidedTask> {
        self.post(format!("/v1/tasks/{id}/decide"), Some(DecisionBody { decision }))
    }

    fn task_executions(&self, id: TaskId) -> PortFuture<'_, Vec<Execution>> {
        self.get(format!("/v1/tasks/{id}/executions"))
    }

    fn task_events(&self, id: TaskId, limit: u32) -> PortFuture<'_, Vec<Event>> {
        self.get_query(format!("/v1/tasks/{id}/events"), vec![("limit", limit.to_string())])
    }
}
