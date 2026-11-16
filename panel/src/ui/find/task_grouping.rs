use super::{Collapsed, GroupKey, TaskGroup, TaskLine};
use crate::domain::Task;

/// How the task list is grouped: not at all, or by what started each
/// task, by its bot or by where it stands. The list's Group button goes
/// round them.
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Default)]
pub enum TaskGrouping {
    #[default]
    None,
    Trigger,
    Bot,
    Status,
}

impl TaskGrouping {
    /// The next one a press on the button gives: none, trigger, bot,
    /// status, and round again.
    pub fn next(self) -> Self {
        match self {
            Self::None => Self::Trigger,
            Self::Trigger => Self::Bot,
            Self::Bot => Self::Status,
            Self::Status => Self::None,
        }
    }

    /// The word on the button: `Group: trigger`.
    pub fn label(self) -> &'static str {
        match self {
            Self::None => "none",
            Self::Trigger => "trigger",
            Self::Bot => "bot",
            Self::Status => "status",
        }
    }

    /// Whether the list has group headers.
    pub fn is_grouped(self) -> bool {
        self != Self::None
    }

    /// The group a task falls in, as its header reads: `timer clock`,
    /// its bot's id, `Failed`. Ungrouped, every task is in the one group
    /// without a name.
    pub fn group_of(self, task: &Task) -> String {
        match self {
            Self::None => String::new(),
            Self::Trigger => task.trigger.describe(),
            Self::Bot => task.bot_id.to_string(),
            Self::Status => task.status.label().to_string(),
        }
    }

    /// The lines in their groups, the group of most lines first and two
    /// as large in the order they first come; each closed when the
    /// person closed it. A line goes where the task it shows goes.
    /// Ungrouped, the lines are one open group without a header; no
    /// lines are no groups.
    pub fn groups<'a>(self, lines: Vec<TaskLine<'a>>, collapsed: &Collapsed) -> Vec<TaskGroup<'a>> {
        let mut groups: Vec<TaskGroup<'a>> = Vec::new();
        for line in lines {
            let label = self.group_of(line.task);
            match groups.iter_mut().find(|group| group.label == label) {
                Some(group) => group.lines.push(line),
                None => {
                    let key = GroupKey::Tasks(self, label.clone());
                    groups.push(TaskGroup {
                        open: !(self.is_grouped() && collapsed.is_closed(&key, false)),
                        key,
                        label,
                        lines: vec![line],
                    });
                }
            }
        }
        groups.sort_by_key(|group| std::cmp::Reverse(group.count()));
        groups
    }
}

#[cfg(test)]
mod tests {
    use super::super::FoldRepeats;
    use super::super::test_rows::task;
    use super::*;

    fn tasks() -> Vec<Task> {
        vec![
            task("task-6", "lab-reader", "Failed", "manual", None),
            task("task-5", "ops-watcher", "Completed", "timer:clock", None),
            task("task-4", "ops-watcher", "Failed", "timer:clock", None),
            task(
                "task-3",
                "ops-fixer",
                "Completed",
                "event:stock.low",
                Some("3 left of A-12"),
            ),
            task("task-2", "ops-watcher", "Completed", "timer:clock", None),
            task(
                "task-1",
                "ops-fixer",
                "WaitingApproval",
                "event:stock.low",
                Some("1 left of B-2"),
            ),
        ]
    }

    fn shape(groups: &[TaskGroup<'_>]) -> Vec<(String, usize)> {
        groups.iter().map(|g| (g.label.clone(), g.count())).collect()
    }

    fn group(label: &str, count: usize) -> (String, usize) {
        (label.to_string(), count)
    }

    #[test]
    fn the_button_goes_round_the_groupings() {
        let mut grouping = TaskGrouping::default();
        let mut words = Vec::new();
        for _ in 0..5 {
            words.push(grouping.label());
            grouping = grouping.next();
        }
        assert_eq!(words, vec!["none", "trigger", "bot", "status", "none"]);
        assert!(!TaskGrouping::None.is_grouped() && TaskGrouping::Bot.is_grouped());
    }

    #[test]
    fn groups_by_trigger_bot_or_status_the_largest_first() {
        let tasks = tasks();
        let lines = || FoldRepeats::off().lines(&tasks);
        let open = Collapsed::default();
        assert_eq!(
            shape(&TaskGrouping::Trigger.groups(lines(), &open)),
            vec![group("timer clock", 3), group("event stock.low", 2), group("manual", 1)]
        );
        assert_eq!(
            shape(&TaskGrouping::Bot.groups(lines(), &open)),
            vec![group("ops-watcher", 3), group("ops-fixer", 2), group("lab-reader", 1)]
        );
        assert_eq!(
            shape(&TaskGrouping::Status.groups(lines(), &open)),
            vec![group("Done", 3), group("Failed", 2), group("Needs OK", 1)]
        );
    }

    #[test]
    fn two_groups_as_large_keep_the_order_they_first_come() {
        let tasks = [
            task("task-4", "lab-reader", "Completed", "manual", None),
            task("task-3", "ops-watcher", "Completed", "manual", None),
            task("task-2", "ops-watcher", "Completed", "manual", None),
            task("task-1", "lab-reader", "Completed", "manual", None),
        ];
        let groups = TaskGrouping::Bot.groups(FoldRepeats::off().lines(&tasks), &Collapsed::default());
        assert_eq!(shape(&groups), vec![group("lab-reader", 2), group("ops-watcher", 2)]);
        let ids: Vec<&str> = groups[0].lines.iter().map(|l| l.task.id.as_str()).collect();
        assert_eq!(
            ids,
            vec!["task-4", "task-1"],
            "inside a group the lines keep the list's order"
        );
    }

    #[test]
    fn counts_lines_so_folded_repeats_count_once() {
        let tasks = tasks();
        let groups = TaskGrouping::Bot.groups(FoldRepeats::on().lines(&tasks), &Collapsed::default());
        assert_eq!(
            shape(&groups),
            vec![group("ops-watcher", 2), group("ops-fixer", 2), group("lab-reader", 1)]
        );
        assert_eq!(groups[0].tasks(), 3);
    }

    #[test]
    fn a_group_the_person_closed_stays_closed_under_its_grouping_only() {
        let tasks = tasks();
        let mut collapsed = Collapsed::default();
        collapsed.toggle(GroupKey::Tasks(TaskGrouping::Bot, "ops-watcher".into()), false);
        let by_bot = TaskGrouping::Bot.groups(FoldRepeats::off().lines(&tasks), &collapsed);
        assert_eq!(
            by_bot.iter().map(|g| g.open).collect::<Vec<_>>(),
            vec![false, true, true]
        );
        assert_eq!(by_bot[0].key, GroupKey::Tasks(TaskGrouping::Bot, "ops-watcher".into()));
        let by_status = TaskGrouping::Status.groups(FoldRepeats::off().lines(&tasks), &collapsed);
        assert!(by_status.iter().all(|g| g.open));
    }

    #[test]
    fn ungrouped_the_lines_are_one_open_group_without_a_header() {
        let tasks = tasks();
        let mut collapsed = Collapsed::default();
        collapsed.set(GroupKey::Tasks(TaskGrouping::None, String::new()), true);
        let groups = TaskGrouping::None.groups(FoldRepeats::off().lines(&tasks), &collapsed);
        assert_eq!(shape(&groups), vec![group("", 6)]);
        assert!(groups[0].open, "a list without headers has nothing to close");
        for grouping in [TaskGrouping::None, TaskGrouping::Trigger] {
            assert!(grouping.groups(Vec::new(), &collapsed).is_empty());
        }
    }
}
