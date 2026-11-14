use super::{ActivityUpdate, Event, ExecutionActivity, Task, TaskId, Usage};

/// A task with everything under it: its executions with usage and cost,
/// its whole timeline, and the tasks it delegated, recursively.
#[derive(Debug, Clone, PartialEq)]
pub struct TaskActivity {
    pub task: Task,
    pub executions: Vec<ExecutionActivity>,
    pub events: Vec<Event>,
    pub children: Vec<TaskActivity>,
    /// This task's usage and every descendant's.
    pub subtree_usage: Usage,
}

impl TaskActivity {
    /// The usage of this task's own executions.
    pub fn own_usage(&self) -> Usage {
        self.executions
            .iter()
            .fold(Usage::default(), |sum, e| sum.plus(e.execution.usage))
    }

    /// The tasks of the tree, this one first, each with its depth.
    pub fn flatten(&self) -> Vec<(usize, &TaskActivity)> {
        let mut rows = Vec::new();
        self.collect(0, &mut rows);
        rows
    }

    fn collect<'a>(&'a self, depth: usize, rows: &mut Vec<(usize, &'a TaskActivity)>) {
        rows.push((depth, self));
        for child in &self.children {
            child.collect(depth + 1, rows);
        }
    }

    /// Whether every task of the tree has ended.
    pub fn finished(&self) -> bool {
        self.flatten().iter().all(|(_, t)| t.task.status.is_terminal())
    }

    fn find_mut(&mut self, id: &TaskId) -> Option<&mut TaskActivity> {
        if &self.task.id == id {
            return Some(self);
        }
        self.children.iter_mut().find_map(|c| c.find_mut(id))
    }

    fn recount(&mut self) -> Usage {
        let mut sum = self.own_usage();
        for child in &mut self.children {
            sum = sum.plus(child.recount());
        }
        self.subtree_usage = sum;
        sum
    }

    /// Applies one update the control plane streamed. An update about a
    /// task the tree does not hold is ignored: the snapshot that comes
    /// when a stream is resumed puts it right.
    pub fn apply(&mut self, update: ActivityUpdate) {
        match update {
            ActivityUpdate::Snapshot(snapshot) => *self = *snapshot,
            ActivityUpdate::TaskChanged(task) => {
                if let Some(node) = self.find_mut(&task.id) {
                    node.task = *task;
                }
            }
            ActivityUpdate::ExecutionChanged(execution) => {
                if let Some(node) = self.find_mut(&execution.task_id.clone()) {
                    match node.executions.iter_mut().find(|e| e.execution.id == execution.id) {
                        Some(known) => known.execution = *execution,
                        None => node.executions.push(ExecutionActivity::bare(*execution)),
                    }
                }
            }
            ActivityUpdate::EventAppended { task_id, event } => {
                if let Some(node) = self.find_mut(&task_id)
                    && !node.events.iter().any(|e| e.event_id == event.event_id)
                {
                    node.events.push(*event);
                }
            }
            ActivityUpdate::ChildAttached { parent, child } => {
                if let Some(node) = self.find_mut(&parent) {
                    match node.children.iter_mut().find(|c| c.task.id == child.task.id) {
                        Some(known) => *known = *child,
                        None => node.children.push(*child),
                    }
                }
            }
            ActivityUpdate::ProgressCompleted {
                task_id,
                execution_id,
                cut,
                items,
            } => {
                if let Some(node) = self.find_mut(&task_id)
                    && let Some(run) = node
                        .executions
                        .iter_mut()
                        .find(|e| e.execution.id.as_str() == execution_id)
                {
                    run.progress_cut = cut;
                    run.completed_items = items;
                }
            }
            ActivityUpdate::Heartbeat => {}
        }
        self.recount();
    }
}

#[cfg(test)]
pub(crate) mod tests {
    use super::*;
    use crate::domain::{CutReason, Execution, ProgressCut};

    pub(crate) fn task(id: &str, status: &str) -> Task {
        serde_json::from_value(serde_json::json!({
            "id": id, "botId": "bot-a", "objective": "Do it.", "status": status,
            "createdAt": "2026-11-02T10:00:00Z", "updatedAt": "2026-11-02T10:00:05Z"
        }))
        .unwrap()
    }

    pub(crate) fn execution(id: &str, task: &str, requests: u32, cost: f64) -> Execution {
        serde_json::from_value(serde_json::json!({
            "id": id, "taskId": task, "botId": "bot-a", "startedAt": "2026-11-02T10:00:00Z",
            "endedAt": "2026-11-02T10:00:04Z", "outcome": "Completed",
            "usage": {"requests": requests, "costUsd": cost}
        }))
        .unwrap()
    }

    pub(crate) fn tree() -> TaskActivity {
        let child = TaskActivity {
            task: task("task-child", "Running"),
            executions: vec![ExecutionActivity::bare(execution("exec-c", "task-child", 2, 0.5))],
            events: Vec::new(),
            children: Vec::new(),
            subtree_usage: Usage::default(),
        };
        let mut root = TaskActivity {
            task: task("task-root", "Running"),
            executions: vec![ExecutionActivity::bare(execution("exec-r", "task-root", 3, 0.25))],
            events: Vec::new(),
            children: vec![child],
            subtree_usage: Usage::default(),
        };
        root.recount();
        root
    }

    fn id(raw: &str) -> TaskId {
        TaskId::new(raw).unwrap()
    }

    #[test]
    fn adds_the_usage_of_the_whole_tree() {
        let root = tree();
        assert_eq!(root.own_usage().requests, 3);
        assert_eq!(root.subtree_usage.requests, 5);
        assert!((root.subtree_usage.cost_usd - 0.75).abs() < 1e-9);
        let depths: Vec<usize> = root.flatten().iter().map(|(d, _)| *d).collect();
        assert_eq!(depths, [0, 1]);
        assert!(!root.finished());
    }

    #[test]
    fn follows_the_updates_the_stream_brings() {
        let mut root = tree();
        root.apply(ActivityUpdate::TaskChanged(Box::new(task("task-child", "Completed"))));
        root.apply(ActivityUpdate::TaskChanged(Box::new(task("task-root", "Completed"))));
        assert!(root.finished());
        root.apply(ActivityUpdate::ExecutionChanged(Box::new(execution(
            "exec-c2",
            "task-child",
            4,
            1.0,
        ))));
        assert_eq!(root.children[0].executions.len(), 2);
        assert_eq!(root.subtree_usage.requests, 9);
        root.apply(ActivityUpdate::ExecutionChanged(Box::new(execution(
            "exec-c2",
            "task-child",
            6,
            1.0,
        ))));
        assert_eq!(
            (root.children[0].executions.len(), root.subtree_usage.requests),
            (2, 11)
        );
        let event: Event = serde_json::from_value(serde_json::json!({
            "eventId": "e1", "type": "bot.task.said", "createdAt": "2026-11-02T10:00:01Z"
        }))
        .unwrap();
        for _ in 0..2 {
            root.apply(ActivityUpdate::EventAppended {
                task_id: id("task-child"),
                event: Box::new(event.clone()),
            });
        }
        assert_eq!(root.children[0].events.len(), 1);
        let grandchild = TaskActivity {
            task: task("task-grand", "Pending"),
            executions: Vec::new(),
            events: Vec::new(),
            children: Vec::new(),
            subtree_usage: Usage::default(),
        };
        root.apply(ActivityUpdate::ChildAttached {
            parent: id("task-child"),
            child: Box::new(grandchild),
        });
        assert_eq!(root.flatten().len(), 3);
        assert!(!root.finished());
        root.apply(ActivityUpdate::Heartbeat);
        // An update about a task the tree does not hold changes nothing.
        let before = root.clone();
        root.apply(ActivityUpdate::TaskChanged(Box::new(task("task-lost", "Failed"))));
        assert_eq!(root, before);
    }

    #[test]
    fn marks_the_completion_from_the_transcript() {
        let mut root = tree();
        let cut = ProgressCut {
            reason: CutReason::ByteLimit,
            events_kept: 10,
            bytes_kept: 100,
            cut_at: None,
            dropped: 0,
            from_transcript: true,
            transcript_sessions: vec!["s1".into()],
            completion_bounded: false,
        };
        root.apply(ActivityUpdate::ProgressCompleted {
            task_id: id("task-root"),
            execution_id: "exec-r".into(),
            cut: Some(cut),
            items: vec![crate::domain::ProgressItem {
                kind: "text".into(),
                text: "More.".into(),
                ..Default::default()
            }],
        });
        assert!(root.executions[0].from_transcript());
        assert_eq!(root.executions[0].completed_items.len(), 1);
        assert!(!root.children[0].executions[0].from_transcript());
        let snapshot = tree();
        root.apply(ActivityUpdate::Snapshot(Box::new(snapshot.clone())));
        assert_eq!(root, snapshot);
    }
}
