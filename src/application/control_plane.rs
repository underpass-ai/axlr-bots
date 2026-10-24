use chrono::{DateTime, Utc};

use super::PortFuture;
use crate::domain::{
    Bot, BotDefinition, BotId, BotPatch, CancelledTask, DecidedTask, Decision, EnrollmentRequest, EnrollmentToken,
    Event, Execution, Node, NodeId, Overview, RevokedNode, SubmittedTask, Task, TaskFilter, TaskId, TaskSubmission,
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
    /// The bot's newest `limit` events, oldest first.
    fn bot_events(&self, id: BotId, limit: u32) -> PortFuture<'_, Vec<Event>>;

    /// Tasks across bots, newest first.
    fn list_tasks(&self, filter: TaskFilter) -> PortFuture<'_, Vec<Task>>;
    fn get_task(&self, id: TaskId) -> PortFuture<'_, Task>;
    fn submit_task(&self, submission: TaskSubmission) -> PortFuture<'_, SubmittedTask>;
    fn cancel_task(&self, id: TaskId) -> PortFuture<'_, CancelledTask>;
    fn decide_task(&self, id: TaskId, decision: Decision) -> PortFuture<'_, DecidedTask>;
    /// The task's executions, oldest first.
    fn task_executions(&self, id: TaskId) -> PortFuture<'_, Vec<Execution>>;
    /// The task's newest `limit` events, oldest first.
    fn task_events(&self, id: TaskId, limit: u32) -> PortFuture<'_, Vec<Event>>;
}
