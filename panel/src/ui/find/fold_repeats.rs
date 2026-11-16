use std::collections::BTreeMap;

use super::{TaskLine, TaskWhat};
use crate::domain::Task;

/// Whether the task list folds its repeats: the tasks of one bot that
/// the same trigger started and that stand the same fold into the
/// newest of them, so a timer's two hundred runs take one line. A task
/// an event made with a message of its own never folds: its message is
/// what tells it apart. On until a person turns it off.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct FoldRepeats {
    on: bool,
}

impl Default for FoldRepeats {
    fn default() -> Self {
        Self { on: true }
    }
}

impl FoldRepeats {
    pub fn on() -> Self {
        Self { on: true }
    }

    pub fn off() -> Self {
        Self { on: false }
    }

    pub fn is_on(self) -> bool {
        self.on
    }

    pub fn toggle(&mut self) {
        self.on = !self.on;
    }

    /// The list's lines, the tasks in the order they came (newest
    /// first): each line where its first task was, the later tasks alike
    /// folded into it wherever they come. Off, every task is a line.
    pub fn lines<'a>(self, tasks: impl IntoIterator<Item = &'a Task>) -> Vec<TaskLine<'a>> {
        let mut lines: Vec<TaskLine<'a>> = Vec::new();
        let mut seen: BTreeMap<(&str, &str, &str, &str), usize> = BTreeMap::new();
        for task in tasks {
            if !self.on || TaskWhat::of(task).message.is_some() {
                lines.push(TaskLine::alone(task));
                continue;
            }
            let alike = (
                task.bot_id.as_str(),
                task.trigger.trigger_type.as_str(),
                task.trigger.name.as_str(),
                task.status.name(),
            );
            match seen.get(&alike) {
                Some(&line) => lines[line].alike.push(task),
                None => {
                    seen.insert(alike, lines.len());
                    lines.push(TaskLine::alone(task));
                }
            }
        }
        lines
    }

    /// How many tasks the lines hide under another: `131` of 200 tasks
    /// on 69 lines.
    pub fn folded(lines: &[TaskLine<'_>]) -> usize {
        lines.iter().map(|line| line.count() - 1).sum()
    }

    /// The list's note: how many tasks the panel read and how many of
    /// the ones shown are folded, `Newest 200 tasks · 131 repeats
    /// folded`.
    pub fn note(lines: &[TaskLine<'_>], newest: usize) -> String {
        match Self::folded(lines) {
            0 => format!("Newest {newest} tasks"),
            1 => format!("Newest {newest} tasks · 1 repeat folded"),
            folded => format!("Newest {newest} tasks · {folded} repeats folded"),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::super::test_rows::task;
    use super::*;

    fn shape(lines: &[TaskLine<'_>]) -> Vec<(String, usize)> {
        lines.iter().map(|l| (l.task.id.to_string(), l.count())).collect()
    }

    fn line(id: &str, count: usize) -> (String, usize) {
        (id.to_string(), count)
    }

    #[test]
    fn folds_repeats_that_come_between_other_tasks_into_their_newest() {
        let tasks = [
            task("task-7", "ops-watcher", "Completed", "timer:clock", None),
            task("task-6", "lab-reader", "Completed", "timer:clock", None),
            task("task-5", "ops-watcher", "Completed", "timer:clock", None),
            task("task-4", "ops-watcher", "Failed", "timer:clock", None),
            task("task-3", "lab-reader", "Completed", "timer:clock", None),
            task("task-2", "ops-watcher", "Completed", "timer:clock", None),
            task("task-1", "ops-watcher", "Completed", "manual", None),
        ];
        let lines = FoldRepeats::on().lines(&tasks);
        assert_eq!(
            shape(&lines),
            vec![
                line("task-7", 3),
                line("task-6", 2),
                line("task-4", 1),
                line("task-1", 1)
            ]
        );
        let folded: Vec<&str> = lines[0].alike.iter().map(|t| t.id.as_str()).collect();
        assert_eq!(folded, vec!["task-7", "task-5", "task-2"]);
        assert_eq!(FoldRepeats::folded(&lines), 3);
        assert_eq!(FoldRepeats::note(&lines, 200), "Newest 200 tasks · 3 repeats folded");
    }

    #[test]
    fn tells_tasks_apart_by_bot_trigger_type_trigger_name_and_status() {
        let tasks = [
            task("task-6", "ops-watcher", "Completed", "timer:clock", None),
            task("task-5", "ops-fixer", "Completed", "timer:clock", None),
            task("task-4", "ops-watcher", "Completed", "timer:nightly", None),
            task("task-3", "ops-watcher", "Completed", "webhook:clock", None),
            task("task-2", "ops-watcher", "Running", "timer:clock", None),
            task("task-1", "ops-watcher", "Completed", "timer:clock", None),
        ];
        let lines = FoldRepeats::on().lines(&tasks);
        assert_eq!(lines.len(), 5);
        assert_eq!(lines[0].badge().as_deref(), Some("×2"));
        assert!(lines[1..].iter().all(|l| l.count() == 1));
    }

    #[test]
    fn a_task_with_an_event_message_never_folds_nor_takes_others() {
        let tasks = [
            task(
                "task-5",
                "ops-fixer",
                "Completed",
                "event:stock.low",
                Some("3 left of A-12"),
            ),
            task("task-4", "ops-fixer", "Completed", "event:stock.low", None),
            task(
                "task-3",
                "ops-fixer",
                "Completed",
                "event:stock.low",
                Some("3 left of A-12"),
            ),
            task("task-2", "ops-fixer", "Completed", "event:stock.low", None),
            task("task-1", "ops-fixer", "Completed", "event:stock.low", Some(" ")),
        ];
        let lines = FoldRepeats::on().lines(&tasks);
        assert_eq!(
            shape(&lines),
            vec![line("task-5", 1), line("task-4", 3), line("task-3", 1)]
        );
        assert!(
            lines[1].holds(&tasks[4].id),
            "an empty message is no message: the task folds"
        );
    }

    #[test]
    fn off_every_task_is_a_line() {
        let tasks = [
            task("task-2", "ops-watcher", "Completed", "timer:clock", None),
            task("task-1", "ops-watcher", "Completed", "timer:clock", None),
        ];
        let mut fold = FoldRepeats::default();
        assert!(fold.is_on());
        fold.toggle();
        assert_eq!(fold, FoldRepeats::off());
        let lines = fold.lines(&tasks);
        assert_eq!(shape(&lines), vec![line("task-2", 1), line("task-1", 1)]);
        assert_eq!(FoldRepeats::note(&lines, 200), "Newest 200 tasks");
        fold.toggle();
        assert!(fold.is_on());
        let lines = fold.lines(&tasks);
        assert_eq!(FoldRepeats::note(&lines, 2), "Newest 2 tasks · 1 repeat folded");
    }

    #[test]
    fn no_tasks_are_no_lines() {
        let none: [Task; 0] = [];
        assert!(FoldRepeats::on().lines(&none).is_empty());
        assert_eq!(FoldRepeats::folded(&[]), 0);
        assert_eq!(FoldRepeats::note(&[], 200), "Newest 200 tasks");
    }

    #[test]
    fn folds_what_a_filter_left() {
        let tasks = [
            task("task-3", "ops-watcher", "Completed", "timer:clock", None),
            task("task-2", "lab-reader", "Completed", "timer:clock", None),
            task("task-1", "ops-watcher", "Completed", "timer:clock", None),
        ];
        let kept = super::super::Query::new("ops").filter(&tasks);
        let lines = FoldRepeats::on().lines(kept);
        assert_eq!(shape(&lines), vec![line("task-3", 2)]);
    }
}
