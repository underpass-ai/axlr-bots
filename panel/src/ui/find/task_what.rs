use crate::domain::Task;

/// What a task is about, as its row says it: the message of the event
/// that started it, when one did (`input.message`), and what it was
/// asked. A task an event made reads by its message first, since its
/// objective is the same for every event of its kind.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct TaskWhat<'a> {
    /// The event's own message; none for a task no event made, or when
    /// the message is empty.
    pub message: Option<&'a str>,
    /// What it was asked, in its first line.
    pub objective: &'a str,
}

impl<'a> TaskWhat<'a> {
    pub fn of(task: &'a Task) -> Self {
        let message = task
            .input
            .as_ref()
            .and_then(|input| input.get("message"))
            .and_then(|message| message.as_str())
            .map(str::trim)
            .filter(|message| !message.is_empty());
        Self {
            message,
            objective: task.ask(),
        }
    }

    /// The words the row leads with: the message, or the objective.
    pub fn title(&self) -> &'a str {
        self.message.unwrap_or(self.objective)
    }

    /// The objective, when the row leads with a message and shows it
    /// after, muted.
    pub fn rest(&self) -> Option<&'a str> {
        self.message.map(|_| self.objective)
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
}
