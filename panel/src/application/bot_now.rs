use chrono::{DateTime, Utc};

use crate::domain::{Bot, BotStatus, Event, Task, TaskStatus};

/// What a bot is doing right now, from the newest tasks and its events:
/// the first thing a person wants to know about it.
#[derive(Debug, Clone, PartialEq)]
pub enum BotNow {
    /// A call waits for the person.
    Waiting { task: Task },
    /// A task runs, since when, and its newest step.
    Working {
        task: Task,
        since: DateTime<Utc>,
        last: Option<Box<Event>>,
    },
    /// A task waits for a node.
    Queued { task: Task },
    /// Paused, or not switched on yet: it takes no task.
    Off(BotStatus),
    /// On, with nothing to do.
    Idle,
}

impl BotNow {
    /// The bot's state from `tasks` (newest first, any bot) and `events`
    /// (oldest first, its own feed or none). A waiting call comes first,
    /// then a running task, then a pending one.
    pub fn of(bot: &Bot, tasks: &[Task], events: &[Event]) -> Self {
        let mine = |status: TaskStatus| tasks.iter().find(|t| t.bot_id == bot.id && t.status == status).cloned();
        if let Some(task) = mine(TaskStatus::WaitingApproval) {
            return Self::Waiting { task };
        }
        if let Some(task) = mine(TaskStatus::Running) {
            let of_task = |e: &&Event| e.task_id == task.id.as_str();
            let since = events
                .iter()
                .filter(of_task)
                .filter(|e| e.event_type == "bot.task.started")
                .map(|e| e.created_at)
                .next_back()
                .unwrap_or(task.updated_at);
            let last = events.iter().rfind(of_task).cloned().map(Box::new);
            return Self::Working { task, since, last };
        }
        if let Some(task) = mine(TaskStatus::Pending) {
            return Self::Queued { task };
        }
        if bot.status.accepts_tasks() {
            Self::Idle
        } else {
            Self::Off(bot.status)
        }
    }

    /// The state in one or two words, for a list.
    pub fn word(&self) -> &'static str {
        match self {
            Self::Waiting { .. } => "Waits for you",
            Self::Working { .. } => "Working",
            Self::Queued { .. } => "Queued",
            Self::Off(BotStatus::Paused) => "Paused",
            Self::Off(_) => "Off",
            Self::Idle => "Idle",
        }
    }

    /// The task it is on, if any.
    pub fn task(&self) -> Option<&Task> {
        match self {
            Self::Waiting { task } | Self::Working { task, .. } | Self::Queued { task } => Some(task),
            Self::Off(_) | Self::Idle => None,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::application::test_support::{fleet, run};
    use crate::application::{ControlPlane, RefreshFleet};
    use crate::domain::BotId;

    fn event(kind: &str, task: &str, at: &str) -> Event {
        serde_json::from_str(&format!(
            r#"{{"eventId":"e","type":"{kind}","taskId":"{task}","createdAt":"{at}","payload":{{"tool":"local_read"}}}}"#
        ))
        .unwrap()
    }

    #[test]
    fn a_waiting_call_comes_before_work_and_work_before_a_queue() {
        let (control_plane, clock) = fleet();
        let fleet = run(RefreshFleet::new(control_plane.clone(), clock).execute()).unwrap();
        let bot = |id: &str| fleet.bots.iter().find(|b| b.id.as_str() == id).unwrap().clone();

        let asking = BotNow::of(&bot("bot-act-with-approval"), &fleet.tasks, &[]);
        assert_eq!(asking.word(), "Waits for you");
        assert!(asking.task().is_some());

        let worker = bot("bot");
        let running = fleet.tasks.iter().find(|t| t.status == TaskStatus::Running).unwrap();
        let id = running.id.as_str();
        let events = vec![
            event("bot.task.started", id, "2026-10-25T09:59:40Z"),
            event("execution.reported", id, "2026-10-25T09:59:55Z"),
            event("bot.task.started", "other", "2026-10-25T09:59:58Z"),
        ];
        let BotNow::Working { task, since, last } = BotNow::of(&worker, &fleet.tasks, &events) else {
            panic!("the bot runs a task");
        };
        assert_eq!(task.id, running.id);
        assert_eq!(since.to_rfc3339(), "2026-10-25T09:59:40+00:00");
        assert_eq!(last.unwrap().event_type, "execution.reported");
        let without_events = BotNow::of(&worker, &fleet.tasks, &[]);
        assert!(matches!(without_events, BotNow::Working { since, last: None, .. } if since == running.updated_at));
        assert_eq!(without_events.word(), "Working");

        let mut queued = fleet.tasks.clone();
        queued
            .iter_mut()
            .filter(|t| t.status == TaskStatus::Running)
            .for_each(|t| t.status = TaskStatus::Pending);
        let now = BotNow::of(&worker, &queued, &[]);
        assert_eq!((now.word(), now.task().is_some()), ("Queued", true));

        let idle = BotNow::of(&bot("bot-wrx80"), &fleet.tasks, &[]);
        assert_eq!((idle.word(), idle.task()), ("Idle", None));

        run(control_plane.pause_bot(BotId::new("bot-wrx80").unwrap())).unwrap();
        let paused = run(control_plane.get_bot(BotId::new("bot-wrx80").unwrap())).unwrap();
        assert_eq!(BotNow::of(&paused, &fleet.tasks, &[]).word(), "Paused");
        let mut draft = paused;
        draft.status = BotStatus::Draft;
        assert_eq!(BotNow::of(&draft, &fleet.tasks, &[]), BotNow::Off(BotStatus::Draft));
        assert_eq!(BotNow::Off(BotStatus::Draft).word(), "Off");
    }
}
