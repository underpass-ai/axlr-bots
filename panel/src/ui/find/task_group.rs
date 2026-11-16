use super::{GroupKey, TaskLine};
use crate::domain::TaskId;

/// A group of the task list under its header: the lines of one trigger,
/// bot or status, how many, and whether the person left it open.
#[derive(Debug, Clone, PartialEq)]
pub struct TaskGroup<'a> {
    /// What a press on its header names.
    pub key: GroupKey,
    /// What its header reads; empty for the one group of an ungrouped
    /// list.
    pub label: String,
    /// Its lines show; closed, only its header does.
    pub open: bool,
    pub lines: Vec<TaskLine<'a>>,
}

impl<'a> TaskGroup<'a> {
    /// How many lines it has: the number its header carries.
    pub fn count(&self) -> usize {
        self.lines.len()
    }

    /// How many tasks its lines stand for, the folded ones too.
    pub fn tasks(&self) -> usize {
        self.lines.iter().map(TaskLine::count).sum()
    }

    /// The lines on show, group after group: the ones of the open
    /// groups.
    pub fn shown<'g>(groups: &'g [TaskGroup<'a>]) -> Vec<&'g TaskLine<'a>> {
        groups
            .iter()
            .filter(|group| group.open)
            .flat_map(|group| &group.lines)
            .collect()
    }

    /// The lines on show by the task that stands for each: what the row
    /// cursor moves over. The open task stands for the line that holds
    /// it.
    pub fn keys(groups: &[TaskGroup<'a>], open: Option<&TaskId>) -> Vec<TaskId> {
        Self::shown(groups).into_iter().map(|line| line.key(open)).collect()
    }
}

#[cfg(test)]
mod tests {
    use super::super::test_rows::task;
    use super::super::{Collapsed, FoldRepeats, TaskGrouping};
    use super::*;

    #[test]
    fn shows_the_lines_of_the_open_groups_in_order() {
        let tasks = [
            task("task-5", "ops-watcher", "Completed", "timer:clock", None),
            task("task-4", "lab-reader", "Failed", "manual", None),
            task("task-3", "ops-watcher", "Completed", "timer:clock", None),
            task("task-2", "ops-fixer", "Completed", "manual", None),
            task("task-1", "ops-watcher", "Failed", "timer:clock", None),
        ];
        let mut collapsed = Collapsed::default();
        let groups = TaskGrouping::Bot.groups(FoldRepeats::on().lines(&tasks), &collapsed);
        assert_eq!((groups[0].count(), groups[0].tasks()), (2, 3));
        let ids = |groups: &[TaskGroup<'_>], open: Option<&TaskId>| -> Vec<String> {
            TaskGroup::keys(groups, open).iter().map(|id| id.to_string()).collect()
        };
        assert_eq!(ids(&groups, None), vec!["task-5", "task-1", "task-4", "task-2"]);
        assert_eq!(
            ids(&groups, Some(&tasks[2].id)),
            vec!["task-3", "task-1", "task-4", "task-2"],
            "the open task, folded, stands for its line"
        );
        collapsed.toggle(groups[0].key.clone(), false);
        let groups = TaskGrouping::Bot.groups(FoldRepeats::on().lines(&tasks), &collapsed);
        assert_eq!(ids(&groups, None), vec!["task-4", "task-2"]);
        assert_eq!(TaskGroup::shown(&groups).len(), 2);
        assert!(TaskGroup::shown(&[]).is_empty() && TaskGroup::keys(&[], None).is_empty());
    }
}
