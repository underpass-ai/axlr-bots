use std::cmp::Ordering;

use super::SortColumn;
use crate::domain::{Task, TaskStatus};

/// The Tasks table's columns a person can sort by.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum TaskColumn {
    Status,
    Bot,
    Task,
    From,
    /// Ascending, the youngest first.
    Age,
}

impl SortColumn for TaskColumn {
    type Row = Task;

    fn label(self) -> &'static str {
        match self {
            Self::Status => "Status",
            Self::Bot => "Bot",
            Self::Task => "Task",
            Self::From => "From",
            Self::Age => "Age",
        }
    }

    fn compare(self, a: &Task, b: &Task) -> Ordering {
        match self {
            Self::Status => rank(a.status).cmp(&rank(b.status)),
            Self::Bot => a.bot_id.cmp(&b.bot_id),
            Self::Task => a.objective.to_lowercase().cmp(&b.objective.to_lowercase()),
            Self::From => a.trigger.describe().cmp(&b.trigger.describe()),
            Self::Age => b.created_at.cmp(&a.created_at),
        }
    }
}

/// The order of the status chips: what needs a person first, what ended last.
fn rank(status: TaskStatus) -> u8 {
    match status {
        TaskStatus::WaitingApproval => 0,
        TaskStatus::Running => 1,
        TaskStatus::Pending => 2,
        TaskStatus::Failed => 3,
        TaskStatus::Expired => 4,
        TaskStatus::Completed => 5,
        TaskStatus::Cancelled => 6,
        TaskStatus::Unknown => 7,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::application::ControlPlane;
    use crate::application::test_support::{fleet, run};
    use crate::domain::TaskFilter;
    use crate::ui::table::{SortDirection, SortSpec};

    fn tasks() -> Vec<Task> {
        let (cp, _) = fleet();
        run(cp.list_tasks(TaskFilter::with_limit(50))).unwrap()
    }

    fn statuses(spec: SortSpec<TaskColumn>, tasks: &[Task]) -> Vec<TaskStatus> {
        spec.order(tasks).into_iter().map(|t| t.status).collect()
    }

    #[test]
    fn sorts_tasks_by_status_then_age() {
        let tasks = tasks();
        let by_status = statuses(SortSpec::by(TaskColumn::Status, SortDirection::Ascending), &tasks);
        assert_eq!(by_status[0], TaskStatus::WaitingApproval);
        assert_eq!(by_status[1], TaskStatus::Running);
        assert_eq!(*by_status.last().unwrap(), TaskStatus::Completed);
        let youngest = SortSpec::by(TaskColumn::Age, SortDirection::Ascending);
        let ordered = youngest.order(&tasks);
        assert!(ordered.windows(2).all(|w| w[0].created_at >= w[1].created_at));
        let by_bot =
            SortSpec::by(TaskColumn::Bot, SortDirection::Ascending).then(TaskColumn::Age, SortDirection::Descending);
        let ordered = by_bot.order(&tasks);
        assert!(ordered.windows(2).all(|w| w[0].bot_id <= w[1].bot_id));
        let bot_tasks: Vec<_> = ordered.iter().filter(|t| t.bot_id.as_str() == "bot").collect();
        assert!(bot_tasks.windows(2).all(|w| w[0].created_at <= w[1].created_at));
    }

    #[test]
    fn sorts_by_words_and_origin() {
        let tasks = tasks();
        let by_words = SortSpec::by(TaskColumn::Task, SortDirection::Ascending).order(&tasks);
        assert_eq!(by_words[0].objective, "List the files in your workspace");
        let by_origin = SortSpec::by(TaskColumn::From, SortDirection::Ascending).order(&tasks);
        assert_eq!(by_origin.len(), tasks.len());
        assert_eq!(TaskColumn::From.label(), "From");
        let all = [
            TaskStatus::WaitingApproval,
            TaskStatus::Running,
            TaskStatus::Pending,
            TaskStatus::Failed,
            TaskStatus::Expired,
            TaskStatus::Completed,
            TaskStatus::Cancelled,
            TaskStatus::Unknown,
        ];
        assert!(all.windows(2).all(|w| rank(w[0]) < rank(w[1])));
        for column in [TaskColumn::Status, TaskColumn::Bot, TaskColumn::Task, TaskColumn::Age] {
            assert!(!column.label().is_empty());
        }
    }
}
