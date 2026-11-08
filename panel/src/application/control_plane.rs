use chrono::{DateTime, Utc};

use super::PortFuture;
use crate::domain::{
    Autonomy, Bot, BotDefinition, BotId, BotPatch, CancelledTask, DecidedTask, Decision, DeletedBot, EnrollmentRequest,
    EnrollmentToken, Event, Execution, ExecutionId, Incident, IncidentStatus, Node, NodeId, Overview, Publication,
    Published, RevokedNode, SentTaskMessage, StoppedBotAnswer, SubmittedTask, Task, TaskFilter, TaskId, TaskSubmission,
    Topic,
};

/// The control plane as the panel needs it: the operator API's routes.
/// The HTTP adapter talks to a running control plane; the fake keeps
/// everything in memory for tests.
pub trait ControlPlane: Send + Sync {
    fn overview(&self, since: DateTime<Utc>) -> PortFuture<'_, Overview>;

    fn list_nodes(&self) -> PortFuture<'_, Vec<Node>>;
    fn get_node(&self, id: NodeId) -> PortFuture<'_, Node>;
    fn mint_enrollment_token(&self, request: EnrollmentRequest) -> PortFuture<'_, EnrollmentToken>;
    fn revoke_node(&self, id: NodeId) -> PortFuture<'_, RevokedNode>;

    fn list_bots(&self) -> PortFuture<'_, Vec<Bot>>;
    fn get_bot(&self, id: BotId) -> PortFuture<'_, Bot>;
    fn register_bot(&self, definition: BotDefinition) -> PortFuture<'_, Bot>;
    fn patch_bot(&self, id: BotId, patch: BotPatch) -> PortFuture<'_, Bot>;
    fn enable_bot(&self, id: BotId) -> PortFuture<'_, Bot>;
    fn pause_bot(&self, id: BotId) -> PortFuture<'_, Bot>;
    /// Pauses the bot and cancels its pending, running and waiting tasks.
    /// A control plane older than the route answers not found.
    fn stop_bot(&self, id: BotId) -> PortFuture<'_, StoppedBotAnswer>;
    /// Deletes the bot: its unfinished tasks and the children it asked
    /// for are cancelled, its incidents resolved, its timers removed; the
    /// answer names the bots that still name it.
    fn delete_bot(&self, id: BotId) -> PortFuture<'_, DeletedBot>;
    /// The bot's newest `limit` events, oldest first.
    fn bot_events(&self, id: BotId, limit: u32) -> PortFuture<'_, Vec<Event>>;
    /// A team's timeline after the event `after` names, oldest first;
    /// with `wait_seconds`, the control plane answers as soon as there is
    /// a new one.
    fn group_events(&self, name: String, after: Option<String>, wait_seconds: u32) -> PortFuture<'_, Vec<Event>>;
    /// A team's topics: who publishes, who listens, nobody listens. A
    /// control plane older than the route answers not found.
    fn group_topics(&self, name: String) -> PortFuture<'_, Vec<Topic>>;
    /// Publishes the operator's event on a team.
    fn publish_event(&self, team: String, publication: Publication) -> PortFuture<'_, Published>;

    /// Tasks across bots, newest first.
    fn list_tasks(&self, filter: TaskFilter) -> PortFuture<'_, Vec<Task>>;
    fn get_task(&self, id: TaskId) -> PortFuture<'_, Task>;
    fn submit_task(&self, submission: TaskSubmission) -> PortFuture<'_, SubmittedTask>;
    fn cancel_task(&self, id: TaskId) -> PortFuture<'_, CancelledTask>;
    /// Writes to a running task: its node hands the text to the run
    /// between model steps. Any other task is a state error.
    fn send_task_message(&self, id: TaskId, text: String) -> PortFuture<'_, SentTaskMessage>;
    /// Decides the call the task waits on. With `call_id`, a task that now
    /// waits on another call is a state error, so a decision made on an old
    /// screen never lands on a newer call.
    fn decide_task(&self, id: TaskId, decision: Decision, call_id: Option<String>) -> PortFuture<'_, DecidedTask>;
    /// The task's executions, oldest first.
    fn task_executions(&self, id: TaskId) -> PortFuture<'_, Vec<Execution>>;
    /// The task's newest `limit` events, oldest first.
    fn task_events(&self, id: TaskId, limit: u32) -> PortFuture<'_, Vec<Event>>;
    /// The bytes of an artifact the control plane keeps for the execution.
    fn artifact(&self, execution: ExecutionId, name: String) -> PortFuture<'_, Vec<u8>>;

    /// Incidents, the last seen first: of one tenant or all, in one status
    /// or any, at most `limit`.
    fn list_incidents(
        &self,
        tenant: Option<String>,
        status: Option<IncidentStatus>,
        limit: u32,
    ) -> PortFuture<'_, Vec<Incident>>;
    fn get_incident(&self, id: String) -> PortFuture<'_, Incident>;
    /// Closes the incident, with the operator's reason if they gave one.
    /// One already resolved is a state error.
    fn resolve_incident(&self, id: String, reason: Option<String>) -> PortFuture<'_, Incident>;
    /// Marks the diagnosis of the incident's current cycle, as the panel's
    /// actor: `correct` or `wrong`.
    fn mark_diagnosis(&self, id: String, verdict: String) -> PortFuture<'_, Autonomy>;
    /// Approves the postmortem text of the `author` task, the one on
    /// screen, as the actor the panel decides as.
    fn approve_postmortem(&self, id: String, author: String) -> PortFuture<'_, Incident>;
    /// Sends back the text of the `author` task with the person's note; no
    /// author when none was read, which asks for one if none started.
    fn send_back_postmortem(&self, id: String, note: String, author: Option<String>) -> PortFuture<'_, Incident>;

    /// A task's timeline after the event `after` names, oldest first;
    /// with `wait_seconds`, the control plane answers as soon as there is
    /// a new one. A control plane older than `after` answers the newest.
    fn task_events_after(&self, id: TaskId, after: Option<String>, wait_seconds: u32) -> PortFuture<'_, Vec<Event>>;
}
