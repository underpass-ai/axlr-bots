use crate::application::{ActivityBranch, ActivityOutline};
use crate::domain::{ActivityBatch, TaskActivity, TaskId};

/// The activity screen's state: the task's tree as the control plane keeps
/// it, kept up to date from the updates it streams, and the layout the
/// screen draws.
#[derive(Debug, Clone, PartialEq)]
pub struct ActivityScreen {
    pub id: TaskId,
    pub tree: Option<TaskActivity>,
    pub outline: Vec<ActivityBranch>,
    /// Where to resume the stream from.
    pub cursor: Option<String>,
    /// A read of the stream is on its way.
    pub following: bool,
    /// A read of the whole tree is on its way.
    pub loading: bool,
}

impl ActivityScreen {
    pub fn new(id: TaskId) -> Self {
        Self {
            id,
            tree: None,
            outline: Vec::new(),
            cursor: None,
            following: false,
            loading: false,
        }
    }

    /// The tree as read now.
    pub fn loaded(&mut self, tree: TaskActivity) {
        self.outline = ActivityOutline::of(&tree);
        self.tree = Some(tree);
    }

    /// What the stream brought: each update applied, the cursor kept.
    pub fn followed(&mut self, batch: ActivityBatch) {
        if let Some(cursor) = batch.cursor {
            self.cursor = Some(cursor);
        }
        if batch.updates.is_empty() {
            return;
        }
        if let Some(tree) = self.tree.as_mut() {
            for update in batch.updates {
                tree.apply(update);
            }
            self.outline = ActivityOutline::of(tree);
        } else if let Some(crate::domain::ActivityUpdate::Snapshot(tree)) = batch.updates.into_iter().next() {
            self.loaded(*tree);
        }
    }

    /// Whether the whole tree ended: nothing more to follow.
    pub fn finished(&self) -> bool {
        self.tree.as_ref().is_some_and(TaskActivity::finished)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::domain::ActivityUpdate;

    fn id() -> TaskId {
        TaskId::new("task-root").unwrap()
    }

    #[test]
    fn follows_a_tree_until_every_task_ended() {
        let mut screen = ActivityScreen::new(id());
        assert!(!screen.finished());
        let tree = crate::domain::sample_task_activity();
        screen.loaded(tree.clone());
        assert_eq!(screen.outline.len(), 2);
        assert!(!screen.finished());
        let done = |task: &str| {
            let mut t = tree.task.clone();
            t.id = TaskId::new(task).unwrap();
            t.status = crate::domain::TaskStatus::Completed;
            ActivityUpdate::TaskChanged(Box::new(t))
        };
        screen.followed(ActivityBatch {
            cursor: Some("c1".into()),
            updates: vec![done("task-root"), done("task-child")],
        });
        assert_eq!(screen.cursor.as_deref(), Some("c1"));
        assert!(screen.finished());
        assert!(screen.outline.iter().all(|b| b.task.status.is_terminal()));
        screen.followed(ActivityBatch::default());
        assert_eq!(screen.cursor.as_deref(), Some("c1"));
    }

    #[test]
    fn a_stream_that_starts_before_the_read_gives_the_tree() {
        let mut screen = ActivityScreen::new(id());
        screen.followed(ActivityBatch {
            cursor: Some("c0".into()),
            updates: vec![
                ActivityUpdate::Heartbeat,
                ActivityUpdate::Snapshot(Box::new(crate::domain::sample_task_activity())),
            ],
        });
        // The first update is not a snapshot: nothing to apply it to yet.
        assert!(screen.tree.is_none());
        screen.followed(ActivityBatch {
            cursor: None,
            updates: vec![ActivityUpdate::Snapshot(
                Box::new(crate::domain::sample_task_activity()),
            )],
        });
        assert_eq!(screen.outline.len(), 2);
    }
}
