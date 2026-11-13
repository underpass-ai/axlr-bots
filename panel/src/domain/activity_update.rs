use super::{Event, Execution, ProgressCut, ProgressItem, Task, TaskActivity, TaskId};

/// One change of a task's activity tree, as the control plane streams it.
#[derive(Debug, Clone, PartialEq)]
pub enum ActivityUpdate {
    /// The whole tree: first on a stream, and again when a resume could
    /// not be honoured.
    Snapshot(Box<TaskActivity>),
    /// Replaces a task of the tree.
    TaskChanged(Box<Task>),
    /// Replaces an execution, or adds it under its task.
    ExecutionChanged(Box<Execution>),
    /// Adds an event to a task's timeline.
    EventAppended { task_id: TaskId, event: Box<Event> },
    /// Adds a delegated task under its parent.
    ChildAttached { parent: TaskId, child: Box<TaskActivity> },
    /// Where an execution's progress was cut, with the steps completed.
    ProgressCompleted {
        task_id: TaskId,
        execution_id: String,
        cut: Option<ProgressCut>,
        items: Vec<ProgressItem>,
    },
    /// Nothing changed.
    Heartbeat,
}
