use chrono::{DateTime, Utc};

use super::ChatTurn;
use crate::domain::{Event, TaskId};

/// How many lines a bot's chat shows.
pub const CHAT_LINES: usize = 12;

/// One line of a bot's chat: the person's words or the bot's, when, and
/// the task they belong to.
#[derive(Debug, Clone, PartialEq)]
pub struct ChatLine {
    pub mine: bool,
    pub text: String,
    pub at: DateTime<Utc>,
    pub task: TaskId,
}

impl ChatLine {
    /// The chat in time order: each chat task's message and its reply,
    /// and what the person wrote to a running task and what its run said
    /// back (bot.task.message and bot.task.said in the bot's events);
    /// the newest CHAT_LINES.
    pub fn log(turns: &[ChatTurn], events: &[Event]) -> Vec<Self> {
        let mut lines = Vec::new();
        for turn in turns {
            lines.push(Self {
                mine: true,
                text: turn.message().to_string(),
                at: turn.task.created_at,
                task: turn.task.id.clone(),
            });
            lines.push(Self {
                mine: false,
                text: turn.reply(),
                at: turn.task.updated_at.max(turn.task.created_at),
                task: turn.task.id.clone(),
            });
        }
        for event in events {
            let mine = match event.event_type.as_str() {
                "bot.task.message" => true,
                "bot.task.said" => false,
                _ => continue,
            };
            let (Some(text), Ok(task)) = (event.payload_text("text"), TaskId::new(&event.task_id)) else {
                continue;
            };
            lines.push(Self {
                mine,
                text,
                at: event.created_at,
                task,
            });
        }
        lines.sort_by_key(|line| line.at);
        let skip = lines.len().saturating_sub(CHAT_LINES);
        lines.into_iter().skip(skip).collect()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::application::test_support::{fleet, run};
    use crate::application::{ControlPlane, LoadChat};
    use crate::domain::{TaskFilter, TaskStatus};

    #[test]
    fn the_chat_is_in_time_order_with_live_lines() {
        let (control_plane, _) = fleet();
        let tasks = run(control_plane.list_tasks(TaskFilter::default())).unwrap();
        let running = tasks.iter().find(|t| t.status == TaskStatus::Running).unwrap().clone();
        run(control_plane.send_task_message(running.id.clone(), "check row F".into())).unwrap();
        control_plane.say(&running.id, "OK, row F next.");
        let events = run(control_plane.bot_events(running.bot_id.clone(), 100)).unwrap();
        let turns = run(LoadChat::new(control_plane.clone()).execute(running.bot_id.clone())).unwrap();
        let lines = ChatLine::log(&turns, &events);
        let live: Vec<_> = lines.iter().map(|l| (l.mine, l.text.as_str())).collect();
        assert_eq!(live, vec![(true, "check row F"), (false, "OK, row F next.")]);
        assert!(lines.iter().all(|l| l.task == running.id));

        // Many lines: the newest CHAT_LINES.
        for i in 0..CHAT_LINES {
            control_plane.say(&running.id, &format!("line {i}"));
        }
        let events = run(control_plane.bot_events(running.bot_id.clone(), 100)).unwrap();
        let lines = ChatLine::log(&turns, &events);
        assert_eq!(lines.len(), CHAT_LINES);
        assert_eq!(lines.last().unwrap().text, format!("line {}", CHAT_LINES - 1));
    }
}
