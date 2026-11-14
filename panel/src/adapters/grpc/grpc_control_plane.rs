use std::time::Duration;

use chrono::{DateTime, Utc};
use tonic::{Request, Status, Streaming};

use super::activity_mapper::ActivityMapper;
use super::bot_mapper::BotMapper;
use super::definition_mapper::DefinitionMapper;
use super::event_mapper::EventMapper;
use super::execution_mapper::ExecutionMapper;
use super::group_mapper::GroupMapper;
use super::node_mapper::NodeMapper;
use super::operator_channel::{Authed, OperatorChannel};
use super::overview_mapper::OverviewMapper;
use super::patch_mapper::PatchMapper;
use super::proto;
use super::proto::activity_service_client::ActivityServiceClient;
use super::proto::bots_service_client::BotsServiceClient;
use super::proto::executions_service_client::ExecutionsServiceClient;
use super::proto::groups_service_client::GroupsServiceClient;
use super::proto::nodes_service_client::NodesServiceClient;
use super::proto::overview_service_client::OverviewServiceClient;
use super::proto::tasks_service_client::TasksServiceClient;
use super::status_error::StatusError;
use super::task_mapper::TaskMapper;
use super::wire::Wire;
use crate::application::{ControlPlane, LIVE_FEED_SIZE, PanelConfig, PanelError, PortFuture};
use crate::domain::{
    ActivityBatch, Bot, BotDefinition, BotId, BotPatch, BuildStatus, CancelledTask, DecidedTask, Decision, DeletedBot,
    EnrollmentRequest, EnrollmentToken, Event, Execution, ExecutionId, Node, NodeId, Overview, Publication, Published,
    RevokedNode, SentTaskMessage, StoppedBotAnswer, SubmittedTask, Task, TaskActivity, TaskFilter, TaskId,
    TaskSubmission, Topic,
};

/// How long a read may take.
const READ_TIMEOUT: Duration = Duration::from_secs(15);
/// How long an act may take: a decision hands the run to a node first.
const ACT_TIMEOUT: Duration = Duration::from_secs(60);
/// How long an artifact's bytes may take to come: one may be large.
const DOWNLOAD_TIMEOUT: Duration = Duration::from_secs(120);
/// How long a team's timeline is read at most per call.
const TEAM_PAGE: i32 = 200;
/// After the first message of a watch, how long the rest is waited for: what
/// came together comes within this.
const SETTLE: Duration = Duration::from_millis(150);
/// A snapshot may take a while to build.
const SNAPSHOT_WAIT: Duration = Duration::from_secs(30);
/// What the activity snapshot may weigh: gRPC's default is 4 MiB.
const ACTIVITY_LIMIT: usize = 256 * 1024 * 1024;
/// The most messages a long poll gathers.
const POLL_MESSAGES: usize = 1000;

/// The operator API over gRPC: TLS trusting the fleet CA alone, the bearer
/// token read from its file when the client is built, and the actor in
/// the metadata of every call.
pub struct GrpcControlPlane {
    channel: Authed,
}

fn fail(status: Status) -> PanelError {
    StatusError::classify(&status)
}

fn read<T>(message: T) -> Request<T> {
    let mut request = Request::new(message);
    request.set_timeout(READ_TIMEOUT);
    request
}

fn act<T>(message: T) -> Request<T> {
    let mut request = Request::new(message);
    request.set_timeout(ACT_TIMEOUT);
    request
}

fn count(limit: u32) -> i32 {
    i32::try_from(limit).unwrap_or(i32::MAX)
}

/// What a watch brings: the first message within `first`, then whatever
/// comes together with it. A stream that ends or goes quiet ends the poll.
async fn gather<T>(mut stream: Streaming<T>, first: Duration) -> Result<Vec<T>, PanelError> {
    let mut messages = Vec::new();
    let mut wait = first;
    while messages.len() < POLL_MESSAGES {
        match tokio::time::timeout(wait, stream.message()).await {
            Ok(Ok(Some(message))) => messages.push(message),
            Ok(Ok(None)) | Err(_) => break,
            Ok(Err(status)) => return Err(fail(status)),
        }
        wait = SETTLE;
    }
    Ok(messages)
}

impl GrpcControlPlane {
    /// A client for the settings: reads the CA and the token file now, so
    /// a missing or unreadable file is a settings error before any call.
    /// Needs a Tokio runtime: the channel connects on its first call.
    pub fn new(config: &PanelConfig) -> Result<Self, PanelError> {
        Ok(Self {
            channel: OperatorChannel::open(config)?,
        })
    }

    fn overview_client(&self) -> OverviewServiceClient<Authed> {
        OverviewServiceClient::new(self.channel.clone())
    }

    fn nodes_client(&self) -> NodesServiceClient<Authed> {
        NodesServiceClient::new(self.channel.clone())
    }

    fn bots_client(&self) -> BotsServiceClient<Authed> {
        BotsServiceClient::new(self.channel.clone())
    }

    fn groups_client(&self) -> GroupsServiceClient<Authed> {
        GroupsServiceClient::new(self.channel.clone())
    }

    fn tasks_client(&self) -> TasksServiceClient<Authed> {
        TasksServiceClient::new(self.channel.clone())
    }

    fn executions_client(&self) -> ExecutionsServiceClient<Authed> {
        ExecutionsServiceClient::new(self.channel.clone())
    }

    fn activity_client(&self) -> ActivityServiceClient<Authed> {
        ActivityServiceClient::new(self.channel.clone()).max_decoding_message_size(ACTIVITY_LIMIT)
    }

    fn bot_call<F, Fut>(&self, call: F) -> PortFuture<'_, Bot>
    where
        F: FnOnce(BotsServiceClient<Authed>) -> Fut + Send + 'static,
        Fut: std::future::Future<Output = Result<Option<proto::Bot>, Status>> + Send + 'static,
    {
        let client = self.bots_client();
        Box::pin(async move { BotMapper::bot(call(client).await.map_err(fail)?) })
    }
}

impl ControlPlane for GrpcControlPlane {
    fn overview(&self, since: DateTime<Utc>) -> PortFuture<'_, Overview> {
        let mut client = self.overview_client();
        Box::pin(async move {
            let request = proto::GetOverviewRequest {
                since: Some(Wire::timestamp(since)),
            };
            let answer = client.get_overview(read(request)).await.map_err(fail)?.into_inner();
            OverviewMapper::overview(&answer)
        })
    }

    fn build(&self) -> PortFuture<'_, BuildStatus> {
        let mut client = self.overview_client();
        Box::pin(async move {
            let answer = client
                .get_build(read(proto::GetBuildRequest {}))
                .await
                .map_err(fail)?
                .into_inner();
            OverviewMapper::build(&answer)
        })
    }

    fn list_nodes(&self) -> PortFuture<'_, Vec<Node>> {
        let mut client = self.nodes_client();
        Box::pin(async move {
            let answer = client
                .list_nodes(read(proto::ListNodesRequest {}))
                .await
                .map_err(fail)?
                .into_inner();
            NodeMapper::nodes(&answer.nodes)
        })
    }

    fn get_node(&self, id: NodeId) -> PortFuture<'_, Node> {
        let mut client = self.nodes_client();
        Box::pin(async move {
            let request = proto::GetNodeRequest {
                node_id: id.to_string(),
            };
            let answer = client.get_node(read(request)).await.map_err(fail)?.into_inner();
            NodeMapper::node(
                answer
                    .node
                    .as_ref()
                    .ok_or_else(|| PanelError::Unreadable("the answer has no node".into()))?,
            )
        })
    }

    fn mint_enrollment_token(&self, request: EnrollmentRequest) -> PortFuture<'_, EnrollmentToken> {
        let mut client = self.nodes_client();
        Box::pin(async move {
            let message = NodeMapper::enrollment(&request)?;
            let answer = client.create_enrollment(act(message)).await.map_err(fail)?.into_inner();
            NodeMapper::token(&answer)
        })
    }

    fn revoke_node(&self, id: NodeId) -> PortFuture<'_, RevokedNode> {
        let mut client = self.nodes_client();
        Box::pin(async move {
            let request = proto::RevokeNodeRequest {
                node_id: id.to_string(),
            };
            let answer = client.revoke_node(act(request)).await.map_err(fail)?.into_inner();
            NodeMapper::revoked(&answer)
        })
    }

    fn list_bots(&self) -> PortFuture<'_, Vec<Bot>> {
        let mut client = self.bots_client();
        Box::pin(async move {
            let answer = client
                .list_bots(read(proto::ListBotsRequest {}))
                .await
                .map_err(fail)?
                .into_inner();
            BotMapper::bots(answer.bots)
        })
    }

    fn get_bot(&self, id: BotId) -> PortFuture<'_, Bot> {
        self.bot_call(move |mut client| async move {
            let request = proto::GetBotRequest { bot_id: id.to_string() };
            Ok(client.get_bot(read(request)).await?.into_inner().bot)
        })
    }

    fn register_bot(&self, definition: BotDefinition) -> PortFuture<'_, Bot> {
        let mut client = self.bots_client();
        Box::pin(async move {
            let request = proto::RegisterBotRequest {
                definition: Some(DefinitionMapper::definition(&definition)?),
            };
            let answer = client.register_bot(act(request)).await.map_err(fail)?.into_inner();
            BotMapper::bot(answer.bot)
        })
    }

    fn patch_bot(&self, id: BotId, patch: BotPatch) -> PortFuture<'_, Bot> {
        let mut client = self.bots_client();
        Box::pin(async move {
            let request = proto::UpdateBotRequest {
                bot_id: id.to_string(),
                patch: Some(PatchMapper::patch(&patch)?),
            };
            let answer = client.update_bot(act(request)).await.map_err(fail)?.into_inner();
            BotMapper::bot(answer.bot)
        })
    }

    fn apply_staged(&self, id: BotId) -> PortFuture<'_, Bot> {
        self.bot_call(move |mut client| async move {
            let request = proto::ApplyStagedBotRequest { bot_id: id.to_string() };
            Ok(client.apply_staged_bot(act(request)).await?.into_inner().bot)
        })
    }

    fn unstage(&self, id: BotId) -> PortFuture<'_, Bot> {
        self.bot_call(move |mut client| async move {
            let request = proto::UnstageBotRequest { bot_id: id.to_string() };
            Ok(client.unstage_bot(act(request)).await?.into_inner().bot)
        })
    }

    fn enable_bot(&self, id: BotId) -> PortFuture<'_, Bot> {
        self.bot_call(move |mut client| async move {
            let request = proto::EnableBotRequest { bot_id: id.to_string() };
            Ok(client.enable_bot(act(request)).await?.into_inner().bot)
        })
    }

    fn pause_bot(&self, id: BotId) -> PortFuture<'_, Bot> {
        self.bot_call(move |mut client| async move {
            let request = proto::PauseBotRequest { bot_id: id.to_string() };
            Ok(client.pause_bot(act(request)).await?.into_inner().bot)
        })
    }

    fn stop_bot(&self, id: BotId) -> PortFuture<'_, StoppedBotAnswer> {
        let mut client = self.bots_client();
        Box::pin(async move {
            let request = proto::StopBotRequest { bot_id: id.to_string() };
            let answer = client.stop_bot(act(request)).await.map_err(fail)?.into_inner();
            BotMapper::stopped(answer)
        })
    }

    fn delete_bot(&self, id: BotId) -> PortFuture<'_, DeletedBot> {
        let mut client = self.bots_client();
        Box::pin(async move {
            let request = proto::DeleteBotRequest { bot_id: id.to_string() };
            let answer = client.delete_bot(act(request)).await.map_err(fail)?.into_inner();
            BotMapper::deleted(answer)
        })
    }

    fn bot_events(&self, id: BotId, limit: u32) -> PortFuture<'_, Vec<Event>> {
        let mut client = self.bots_client();
        Box::pin(async move {
            let request = proto::ListBotEventsRequest {
                bot_id: id.to_string(),
                limit: count(limit),
            };
            let answer = client.list_bot_events(read(request)).await.map_err(fail)?.into_inner();
            EventMapper::events(&answer.events)
        })
    }

    fn group_events(&self, name: String, after: Option<String>, wait_seconds: u32) -> PortFuture<'_, Vec<Event>> {
        let mut client = self.groups_client();
        Box::pin(async move {
            let after = after.unwrap_or_default();
            if wait_seconds == 0 {
                let request = proto::ListGroupEventsRequest {
                    name,
                    tenant: String::new(),
                    after,
                    limit: TEAM_PAGE,
                };
                let answer = client
                    .list_group_events(read(request))
                    .await
                    .map_err(fail)?
                    .into_inner();
                return EventMapper::events(&answer.events);
            }
            let request = proto::WatchGroupEventsRequest {
                name,
                tenant: String::new(),
                after,
            };
            let stream = client
                .watch_group_events(Request::new(request))
                .await
                .map_err(fail)?
                .into_inner();
            let messages = gather(stream, Duration::from_secs(u64::from(wait_seconds))).await?;
            messages
                .iter()
                .filter_map(|m| m.event.as_ref())
                .map(EventMapper::event)
                .collect()
        })
    }

    fn group_topics(&self, name: String) -> PortFuture<'_, Vec<Topic>> {
        let mut client = self.groups_client();
        Box::pin(async move {
            let request = proto::ListGroupTopicsRequest {
                name,
                tenant: String::new(),
            };
            let answer = client
                .list_group_topics(read(request))
                .await
                .map_err(fail)?
                .into_inner();
            GroupMapper::topics(&answer.topics)
        })
    }

    fn publish_event(&self, team: String, publication: Publication) -> PortFuture<'_, Published> {
        let mut client = self.groups_client();
        Box::pin(async move {
            let request = GroupMapper::publication(&team, &publication)?;
            let answer = client
                .publish_group_event(act(request))
                .await
                .map_err(fail)?
                .into_inner();
            GroupMapper::published(&answer)
        })
    }

    fn list_tasks(&self, filter: TaskFilter) -> PortFuture<'_, Vec<Task>> {
        let mut client = self.tasks_client();
        Box::pin(async move {
            let answer = client
                .list_tasks(read(TaskMapper::list_request(&filter)))
                .await
                .map_err(fail)?
                .into_inner();
            TaskMapper::tasks(&answer.tasks)
        })
    }

    fn get_task(&self, id: TaskId) -> PortFuture<'_, Task> {
        let mut client = self.tasks_client();
        Box::pin(async move {
            let request = proto::GetTaskRequest {
                task_id: id.to_string(),
            };
            let answer = client.get_task(read(request)).await.map_err(fail)?.into_inner();
            TaskMapper::task(
                answer
                    .task
                    .as_ref()
                    .ok_or_else(|| PanelError::Unreadable("the answer has no task".into()))?,
            )
        })
    }

    fn submit_task(&self, submission: TaskSubmission) -> PortFuture<'_, SubmittedTask> {
        let mut client = self.tasks_client();
        Box::pin(async move {
            let request = proto::SubmitTaskRequest {
                bot_id: submission.bot_id.to_string(),
                objective: submission.objective.as_str().to_string(),
                idempotency_key: submission.key.as_str().to_string(),
                correlation_id: String::new(),
                priority: if submission.urgent {
                    "urgent".into()
                } else {
                    String::new()
                },
            };
            let answer = client.submit_task(act(request)).await.map_err(fail)?.into_inner();
            TaskMapper::submitted(&answer)
        })
    }

    fn cancel_task(&self, id: TaskId) -> PortFuture<'_, CancelledTask> {
        let mut client = self.tasks_client();
        Box::pin(async move {
            let request = proto::CancelTaskRequest {
                task_id: id.to_string(),
            };
            let answer = client.cancel_task(act(request)).await.map_err(fail)?.into_inner();
            TaskMapper::cancelled(&answer)
        })
    }

    fn send_task_message(&self, id: TaskId, text: String) -> PortFuture<'_, SentTaskMessage> {
        let mut client = self.tasks_client();
        Box::pin(async move {
            let request = proto::SendTaskMessageRequest {
                task_id: id.to_string(),
                text,
            };
            let answer = client.send_task_message(act(request)).await.map_err(fail)?.into_inner();
            TaskMapper::message_sent(&answer)
        })
    }

    fn decide_task(&self, id: TaskId, decision: Decision, call_id: Option<String>) -> PortFuture<'_, DecidedTask> {
        let mut client = self.tasks_client();
        Box::pin(async move {
            // The actor travels in the metadata: `decided_by` stays empty.
            let request = proto::DecideTaskRequest {
                task_id: id.to_string(),
                decision: decision.name().to_string(),
                call_id: call_id.unwrap_or_default(),
                decided_by: String::new(),
            };
            let answer = client.decide_task(act(request)).await.map_err(fail)?.into_inner();
            TaskMapper::decided(answer.task.as_ref(), answer.execution.as_ref())
        })
    }

    fn task_executions(&self, id: TaskId) -> PortFuture<'_, Vec<Execution>> {
        let mut client = self.tasks_client();
        Box::pin(async move {
            let request = proto::ListTaskExecutionsRequest {
                task_id: id.to_string(),
            };
            let answer = client
                .list_task_executions(read(request))
                .await
                .map_err(fail)?
                .into_inner();
            ExecutionMapper::executions(&answer.executions)
        })
    }

    fn task_events(&self, id: TaskId, limit: u32) -> PortFuture<'_, Vec<Event>> {
        let mut client = self.tasks_client();
        Box::pin(async move {
            let request = proto::ListTaskEventsRequest {
                task_id: id.to_string(),
                after: String::new(),
                limit: count(limit),
            };
            let answer = client.list_task_events(read(request)).await.map_err(fail)?.into_inner();
            EventMapper::events(&answer.events)
        })
    }

    fn artifact(&self, execution: ExecutionId, name: String) -> PortFuture<'_, Vec<u8>> {
        let mut client = self.executions_client();
        Box::pin(async move {
            let request = proto::GetArtifactRequest {
                execution_id: execution.as_str().to_string(),
                name,
            };
            let mut call = Request::new(request);
            call.set_timeout(DOWNLOAD_TIMEOUT);
            let mut stream = client.get_artifact(call).await.map_err(fail)?.into_inner();
            let mut bytes = Vec::new();
            let mut said = None;
            while let Some(message) = stream.message().await.map_err(fail)? {
                match message.content {
                    Some(proto::get_artifact_response::Content::Artifact(artifact)) => said = Some(artifact.size),
                    Some(proto::get_artifact_response::Content::Chunk(chunk)) => bytes.extend_from_slice(&chunk),
                    None => {}
                }
            }
            match said {
                Some(size) if usize::try_from(size).ok() != Some(bytes.len()) => Err(PanelError::Unreadable(format!(
                    "the artifact came with {} bytes of the {size} it says it has",
                    bytes.len()
                ))),
                _ => Ok(bytes),
            }
        })
    }

    fn task_events_after(&self, id: TaskId, after: Option<String>, wait_seconds: u32) -> PortFuture<'_, Vec<Event>> {
        let mut client = self.tasks_client();
        Box::pin(async move {
            let after = after.unwrap_or_default();
            if wait_seconds == 0 {
                let request = proto::ListTaskEventsRequest {
                    task_id: id.to_string(),
                    after,
                    limit: i32::try_from(LIVE_FEED_SIZE).unwrap_or(i32::MAX),
                };
                let answer = client.list_task_events(read(request)).await.map_err(fail)?.into_inner();
                return EventMapper::events(&answer.events);
            }
            let request = proto::WatchTaskEventsRequest {
                task_id: id.to_string(),
                after,
            };
            let stream = client
                .watch_task_events(Request::new(request))
                .await
                .map_err(fail)?
                .into_inner();
            let messages = gather(stream, Duration::from_secs(u64::from(wait_seconds))).await?;
            messages
                .iter()
                .filter_map(|m| m.event.as_ref())
                .map(EventMapper::event)
                .collect()
        })
    }

    fn task_activity(&self, id: TaskId) -> PortFuture<'_, TaskActivity> {
        let mut client = self.activity_client();
        Box::pin(async move {
            let request = proto::GetTaskActivityRequest {
                task_id: id.to_string(),
                options: None,
            };
            let answer = client
                .get_task_activity(read(request))
                .await
                .map_err(fail)?
                .into_inner();
            ActivityMapper::activity(answer.activity.as_ref())
        })
    }

    fn watch_task_activity(
        &self,
        id: TaskId,
        cursor: Option<String>,
        wait_seconds: u32,
    ) -> PortFuture<'_, ActivityBatch> {
        let mut client = self.activity_client();
        Box::pin(async move {
            // Without a cursor the first message is a snapshot, which may
            // take a while to build.
            let first = match &cursor {
                None => SNAPSHOT_WAIT.max(Duration::from_secs(u64::from(wait_seconds))),
                Some(_) => Duration::from_secs(u64::from(wait_seconds)),
            };
            let request = proto::WatchTaskActivityRequest {
                task_id: id.to_string(),
                options: None,
                resume_cursor: cursor.clone().unwrap_or_default(),
            };
            let stream = client
                .watch_task_activity(Request::new(request))
                .await
                .map_err(fail)?
                .into_inner();
            let messages = gather(stream, first).await?;
            let mut batch = ActivityMapper::batch(&messages)?;
            if batch.cursor.is_none() {
                batch.cursor = cursor;
            }
            Ok(batch)
        })
    }
}
