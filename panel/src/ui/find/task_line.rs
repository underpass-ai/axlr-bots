use crate::domain::{Task, TaskId};

/// One line of the task list: a task and the tasks alike folded into
/// it. The line shows its first task, the newest, and counts the rest
/// as `×3`.
#[derive(Debug, Clone, PartialEq)]
pub struct TaskLine<'a> {
    /// The task the line shows.
    pub task: &'a Task,
    /// Every task of the line in list order, the shown one first.
    pub alike: Vec<&'a Task>,
}

impl<'a> TaskLine<'a> {
    /// A line of one task.
    pub fn alone(task: &'a Task) -> Self {
        Self {
            task,
            alike: vec![task],
        }
    }

    /// How many tasks the line stands for.
    pub fn count(&self) -> usize {
        self.alike.len()
    }

    /// The badge of a line that folds others, `×3`; none for a task
    /// alone.
    pub fn badge(&self) -> Option<String> {
        (self.count() > 1).then(|| format!("×{}", self.count()))
    }

    /// Whether the task is the line's, shown or folded.
    pub fn holds(&self, id: &TaskId) -> bool {
        self.alike.iter().any(|task| &task.id == id)
    }

    /// The task that stands for the line under the keyboard: the open
    /// task when the line holds it, even folded, so the line stays the
    /// selected one; else the task it shows.
    pub fn key(&self, open: Option<&TaskId>) -> TaskId {
        match open {
            Some(id) if self.holds(id) => id.clone(),
            _ => self.task.id.clone(),
        }
    }

    /// The lines' keys in order: what the row cursor moves over.
    pub fn keys(lines: &[TaskLine<'a>], open: Option<&TaskId>) -> Vec<TaskId> {
        lines.iter().map(|line| line.key(open)).collect()
    }
}

#[cfg(test)]
mod tests {
    use super::super::test_rows::task;
    use super::*;

    #[test]
    fn a_task_alone_is_a_line_without_a_badge() {
        let one = task("task-1", "ops-watcher", "Completed", "timer:clock", None);
        let line = TaskLine::alone(&one);
        assert_eq!(line.count(), 1);
        assert_eq!(line.badge(), None);
        assert!(line.holds(&one.id));
        assert!(!line.holds(&TaskId::new("task-9").unwrap()));
    }

    #[test]
    fn a_line_counts_the_tasks_folded_into_it() {
        let newest = task("task-3", "ops-watcher", "Completed", "timer:clock", None);
        let older = task("task-2", "ops-watcher", "Completed", "timer:clock", None);
        let line = TaskLine {
            task: &newest,
            alike: vec![&newest, &older],
        };
        assert_eq!(line.badge().as_deref(), Some("×2"));
        assert!(line.holds(&older.id) && line.holds(&newest.id));
    }

    #[test]
    fn the_open_task_stands_for_its_line_even_folded() {
        let newest = task("task-3", "ops-watcher", "Completed", "timer:clock", None);
        let older = task("task-2", "ops-watcher", "Completed", "timer:clock", None);
        let other = task("task-1", "lab-reader", "Failed", "manual", None);
        let lines = [
            TaskLine {
                task: &newest,
                alike: vec![&newest, &older],
            },
            TaskLine::alone(&other),
        ];
        assert_eq!(TaskLine::keys(&lines, None), vec![newest.id.clone(), other.id.clone()]);
        assert_eq!(
            TaskLine::keys(&lines, Some(&older.id)),
            vec![older.id.clone(), other.id.clone()]
        );
        let gone = TaskId::new("task-9").unwrap();
        assert_eq!(
            TaskLine::keys(&lines, Some(&gone)),
            vec![newest.id.clone(), other.id.clone()]
        );
        assert!(TaskLine::keys(&[], Some(&gone)).is_empty());
    }
}
