use std::collections::BTreeMap;

use super::{MessageShape, TaskLine, TaskWhat};
use crate::domain::{Task, TaskStatus};

/// Whether the task list folds its repeats: the tasks of one bot that
/// the same trigger started and that stand the same fold into the
/// newest of them, so a timer's two hundred runs take one line. Tasks
/// fold only when they read alike: the same words in the event's
/// message, or in what they were asked when no event started them, once
/// dates and times are left out ([`MessageShape`]). An hourly event's
/// runs fold; two pieces of work with their own ids, or two different
/// asks sent by hand, do not. A task that waits for a person or is
/// running stays a line of its own: each needs its own look. On until a
/// person turns it off.
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
        let mut seen: BTreeMap<(&str, &str, &str, &str, bool, MessageShape), usize> = BTreeMap::new();
        for task in tasks {
            if !self.on || matches!(task.status, TaskStatus::WaitingApproval | TaskStatus::Running) {
                lines.push(TaskLine::alone(task));
                continue;
            }
            let what = TaskWhat::of(task);
            let alike = (
                task.bot_id.as_str(),
                task.trigger.trigger_type.as_str(),
                task.trigger.name.as_str(),
                task.status.name(),
                what.message.is_some(),
                MessageShape::of(what.message.unwrap_or(&task.objective)),
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
    fn a_task_with_an_event_message_folds_only_with_messages_of_its_shape() {
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
        assert_eq!(shape(&lines), vec![line("task-5", 2), line("task-4", 3)]);
        assert!(
            lines[1].holds(&tasks[4].id),
            "an empty message is no message: the task folds"
        );
    }

    #[test]
    fn a_periodic_event_folds_and_pieces_of_work_with_their_own_ids_do_not() {
        let tasks = [
            task(
                "task-5",
                "qa-runner",
                "Completed",
                "event:qa.sync",
                Some("hourly sync 2026-11-16T09:00:00Z"),
            ),
            task(
                "task-4",
                "qa-runner",
                "Completed",
                "event:qa.item",
                Some("item-4198 · gate failed"),
            ),
            task(
                "task-3",
                "qa-runner",
                "Completed",
                "event:qa.sync",
                Some("hourly sync 2026-11-16T08:00:00Z"),
            ),
            task(
                "task-2",
                "qa-runner",
                "Completed",
                "event:qa.item",
                Some("item-4182 · gate failed"),
            ),
            task(
                "task-1",
                "qa-runner",
                "Completed",
                "event:qa.sync",
                Some("daily sync 2026-11-16T00:00:00Z"),
            ),
        ];
        let lines = FoldRepeats::on().lines(&tasks);
        assert_eq!(
            shape(&lines),
            vec![
                line("task-5", 2),
                line("task-4", 1),
                line("task-2", 1),
                line("task-1", 1)
            ]
        );
    }

    #[test]
    fn different_asks_sent_by_hand_keep_apart_and_timer_runs_fold() {
        let tasks = [
            task("task-4", "ops-watcher", "Completed", "manual", None),
            task("task-3", "ops-watcher", "Completed", "manual", None),
            task("task-2", "ops-watcher", "Completed", "manual", None),
            task("task-1", "ops-watcher", "Completed", "manual", None),
        ];
        let mut tasks = tasks;
        tasks[0].objective = "Check service-1 after its deploy".into();
        tasks[1].objective = "Check service-2 after its deploy".into();
        tasks[2].objective = "Check service-1 after its deploy".into();
        tasks[3].objective = "Round of the queue (timer fired at 2026-11-16T09:00:00Z)".into();
        let lines = FoldRepeats::on().lines(&tasks);
        assert_eq!(
            shape(&lines),
            vec![line("task-4", 2), line("task-3", 1), line("task-1", 1)]
        );
        let mut timer = tasks.clone();
        timer[0].objective = "Round of the queue (timer fired at 2026-11-16T10:00:00Z)".into();
        timer[1].objective = "Round of the queue (timer fired at 2026-11-16T09:30:00Z)".into();
        assert_eq!(FoldRepeats::on().lines(&timer[..2]).len(), 1);
    }

    #[test]
    fn a_task_that_waits_for_a_person_or_runs_never_folds() {
        let tasks = [
            task("task-4", "ops-watcher", "WaitingApproval", "timer:clock", None),
            task("task-3", "ops-watcher", "WaitingApproval", "timer:clock", None),
            task("task-2", "ops-watcher", "Running", "timer:clock", None),
            task("task-1", "ops-watcher", "Running", "timer:clock", None),
        ];
        let lines = FoldRepeats::on().lines(&tasks);
        assert_eq!(lines.len(), 4);
        assert!(lines.iter().all(|l| l.count() == 1));
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
