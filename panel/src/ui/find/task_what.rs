use crate::domain::Task;

/// What a task is about, as its row says it: the message of the event
/// that started it, when one did (`input.message`), and what it was
/// asked. A task an event made reads by its message first, since its
/// objective is the same for every event of its kind; a queued task
/// says what it waits for after it.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TaskWhat<'a> {
    /// The event's own message, whole; none for a task no event made,
    /// or when the message is empty.
    pub message: Option<&'a str>,
    /// What it was asked, in its first line.
    pub objective: &'a str,
    /// What a queued task waits for, when it says.
    pub waits: Option<String>,
}

impl<'a> TaskWhat<'a> {
    pub fn of(task: &'a Task) -> Self {
        Self {
            message: task.event_message(),
            objective: task.ask(),
            waits: task.waits_for(),
        }
    }

    /// The words the row leads with: the message's first line, or the
    /// objective.
    pub fn title(&self) -> &'a str {
        match self.message {
            Some(message) => message.lines().next().unwrap_or(message),
            None => self.objective,
        }
    }

    /// The muted words after the title: what a queued task waits for,
    /// else the objective of a task that leads with a message.
    pub fn rest(&self) -> Option<&str> {
        self.waits.as_deref().or(self.message.map(|_| self.objective))
    }
}

#[cfg(test)]
mod tests {
    use super::super::test_rows::task;
    use super::*;

    #[test]
    fn a_task_an_event_made_leads_with_its_message() {
        let made = task(
            "task-1",
            "ops-watcher",
            "Completed",
            "event:stock.low",
            Some("  3 left of A-12 "),
        );
        let what = TaskWhat::of(&made);
        assert_eq!(what.message, Some("3 left of A-12"));
        assert_eq!(what.title(), "3 left of A-12");
        assert_eq!(what.rest(), Some("Do the work of ops-watcher."));
    }

    #[test]
    fn a_message_of_several_lines_leads_with_its_first() {
        let made = task(
            "task-1",
            "ops-watcher",
            "Completed",
            "event:stock.low",
            Some("3 left of A-12\nreorder before Friday"),
        );
        let what = TaskWhat::of(&made);
        assert_eq!(what.title(), "3 left of A-12");
        assert_eq!(what.message, Some("3 left of A-12\nreorder before Friday"));
    }

    #[test]
    fn any_other_task_leads_with_what_it_was_asked() {
        let by_hand = task("task-1", "ops-watcher", "Completed", "manual", None);
        let what = TaskWhat::of(&by_hand);
        assert_eq!(what.message, None);
        assert_eq!(what.title(), "Do the work of ops-watcher.");
        assert_eq!(what.rest(), None);
        let mut odd = by_hand.clone();
        for input in [
            serde_json::json!({"message": "   "}),
            serde_json::json!({"message": 7}),
            serde_json::json!({"notes": []}),
            serde_json::json!("words"),
        ] {
            odd.input = Some(input);
            assert_eq!(TaskWhat::of(&odd).message, None);
        }
    }

    #[test]
    fn a_queued_task_says_what_it_waits_for() {
        let mut queued = task("task-1", "ops-watcher", "Pending", "manual", None);
        queued.waiting = "another task of the bot runs".into();
        let what = TaskWhat::of(&queued);
        assert_eq!(what.title(), "Do the work of ops-watcher.");
        assert_eq!(what.rest(), Some("Another task of the bot runs"));
        let mut made = task("task-2", "ops-fixer", "Pending", "event:stock.low", Some("3 left"));
        made.waiting = "a free node".into();
        assert_eq!(TaskWhat::of(&made).rest(), Some("A free node"), "the wait comes first");
        made.status = crate::domain::TaskStatus::Running;
        assert_eq!(TaskWhat::of(&made).rest(), Some("Do the work of ops-fixer."));
    }
}
