use chrono::{DateTime, Utc};

use super::PortFuture;
use crate::domain::{
    ActivityBatch, Bot, BotDefinition, BotId, BotPatch, BuildStatus, CancelledTask, DecidedTask, Decision, DeletedBot,
    EnrollmentRequest, EnrollmentToken, Event, Execution, ExecutionId, Node, NodeId, Overview, Publication, Published,
    RevokedNode, SentTaskMessage, StoppedBotAnswer, SubmittedTask, Task, TaskActivity, TaskFilter, TaskId,
    TaskSubmission, Topic,
};

/// The control plane as the panel needs it: the operator API's calls.
/// The gRPC adapter talks to a running control plane; the fake keeps
/// everything in memory for tests.
pub trait ControlPlane: Send + Sync {
    fn overview(&self, since: DateTime<Utc>) -> PortFuture<'_, Overview>;
    /// The build the control plane runs and whether a newer one is
    /// installed; NotFound from a control plane older than the route.
    fn build(&self) -> PortFuture<'_, BuildStatus>;

    fn list_nodes(&self) -> PortFuture<'_, Vec<Node>>;
    fn get_node(&self, id: NodeId) -> PortFuture<'_, Node>;
    fn mint_enrollment_token(&self, request: EnrollmentRequest) -> PortFuture<'_, EnrollmentToken>;
    fn revoke_node(&self, id: NodeId) -> PortFuture<'_, RevokedNode>;

    fn list_bots(&self) -> PortFuture<'_, Vec<Bot>>;
    fn get_bot(&self, id: BotId) -> PortFuture<'_, Bot>;
    fn register_bot(&self, definition: BotDefinition) -> PortFuture<'_, Bot>;
    fn patch_bot(&self, id: BotId, patch: BotPatch) -> PortFuture<'_, Bot>;
    /// Applies the change the bot holds staged, through the update; a
    /// bot that holds none is a state error, a control plane older than
    /// the route answers not found.
    fn apply_staged(&self, id: BotId) -> PortFuture<'_, Bot>;
    /// Drops the change the bot holds staged, unapplied; a bot that holds
    /// none is a state error.
    fn unstage(&self, id: BotId) -> PortFuture<'_, Bot>;
    fn enable_bot(&self, id: BotId) -> PortFuture<'_, Bot>;
    fn pause_bot(&self, id: BotId) -> PortFuture<'_, Bot>;
    /// Pauses the bot and cancels its pending, running and waiting tasks.
    /// A control plane older than the route answers not found.
    fn stop_bot(&self, id: BotId) -> PortFuture<'_, StoppedBotAnswer>;
    /// Deletes the bot: its unfinished tasks and the children it asked
    /// for are cancelled, its timers removed; the
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

    /// A task's timeline after the event `after` names, oldest first;
    /// with `wait_seconds`, the control plane answers as soon as there is
    /// a new one. A control plane older than `after` answers the newest.
    fn task_events_after(&self, id: TaskId, after: Option<String>, wait_seconds: u32) -> PortFuture<'_, Vec<Event>>;

    /// The task with everything under it, as it is now: its executions
    /// with usage and cost, its whole timeline and the tasks it
    /// delegated, recursively. A control plane older than the call
    /// answers not found.
    fn task_activity(&self, id: TaskId) -> PortFuture<'_, TaskActivity>;

    /// The updates of the task's activity tree after the `cursor` (none:
    /// from a snapshot). With `wait_seconds`, the control plane answers
    /// as soon as there is an update; without, with what is there now.
    fn watch_task_activity(
        &self,
        id: TaskId,
        cursor: Option<String>,
        wait_seconds: u32,
    ) -> PortFuture<'_, ActivityBatch>;
}
