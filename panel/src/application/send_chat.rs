use std::sync::Arc;

use super::{CHAT_CONTEXT_MARK, CHAT_KEY_PREFIX, ChatSent, ChatTurn, ControlPlane, KeySource, PanelError};
use crate::domain::{BotId, IdempotencyKey, Objective, TaskId, TaskSubmission};

/// How many earlier turns a chat message carries, and how much of each
/// answer.
const CONTEXT_TURNS: usize = 3;
const CONTEXT_ANSWER: usize = 600;

/// Sends a person's message to a bot. While the bot runs a task, the
/// message goes to that task, and its node hands it to the run between
/// model steps. Otherwise, or when the control plane cannot take it there
/// (the task just ended, or a control plane older than the route), it
/// goes as a new task, with the last turns of the chat after it so the
/// bot keeps the thread; the task's key marks it as a chat turn.
pub struct SendChat {
    control_plane: Arc<dyn ControlPlane>,
    keys: Arc<dyn KeySource>,
}

impl SendChat {
    pub fn new(control_plane: Arc<dyn ControlPlane>, keys: Arc<dyn KeySource>) -> Self {
        Self { control_plane, keys }
    }

    pub async fn execute(
        &self,
        bot: BotId,
        message: &str,
        earlier: &[ChatTurn],
        running: Option<TaskId>,
    ) -> Result<ChatSent, PanelError> {
        if let Some(task) = running {
            match self
                .control_plane
                .send_task_message(task, message.trim().to_string())
                .await
            {
                Ok(sent) => return Ok(ChatSent::ToRun(sent)),
                Err(PanelError::State(_) | PanelError::NotFound(_)) => {}
                Err(error) => return Err(error),
            }
        }
        let objective = Objective::new(&objective(message, earlier))?;
        let key = IdempotencyKey::new(&format!("{CHAT_KEY_PREFIX}{}", self.keys.next_key().as_str()))?;
        self.control_plane
            .submit_task(TaskSubmission {
                bot_id: bot,
                objective,
                key,
                urgent: false,
            })
            .await
            .map(|task| ChatSent::AsTask(Box::new(task)))
    }
}

/// The message, then the newest earlier turns, oldest first.
pub fn objective(message: &str, earlier: &[ChatTurn]) -> String {
    let message = message.trim();
    let turns: Vec<_> = earlier.iter().rev().take(CONTEXT_TURNS).rev().collect();
    if turns.is_empty() {
        return message.to_string();
    }
    let mut text = format!("{message}{CHAT_CONTEXT_MARK} (oldest first):");
    for turn in turns {
        let reply: String = turn.reply().chars().take(CONTEXT_ANSWER).collect();
        text.push_str(&format!("\nThe person: {}\nYou: {reply}", turn.message()));
    }
    text
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::adapters::fake::CountingKeys;
    use crate::application::LoadChat;
    use crate::application::test_support::{fleet, run};
    use crate::domain::{TaskFilter, TaskStatus};

    fn as_task(sent: ChatSent) -> crate::domain::SubmittedTask {
        match sent {
            ChatSent::AsTask(task) => *task,
            other => panic!("{other:?}"),
        }
    }

    #[test]
    fn a_message_goes_to_the_running_task_or_else_as_a_new_one() {
        let (control_plane, _) = fleet();
        let tasks = run(control_plane.list_tasks(TaskFilter::default())).unwrap();
        let running = tasks.iter().find(|t| t.status == TaskStatus::Running).unwrap().clone();
        let send = SendChat::new(control_plane.clone(), Arc::new(CountingKeys::default()));
        let sent = run(send.execute(running.bot_id.clone(), " check row F ", &[], Some(running.id.clone()))).unwrap();
        assert!(matches!(sent, ChatSent::ToRun(ref m) if m.task_id == running.id));
        let events = run(control_plane.task_events(running.id.clone(), 50)).unwrap();
        assert!(events.iter().any(|e| e.event_type == "bot.task.message" && e.payload_text("text").as_deref() == Some("check row F")));

        // The task ended meanwhile: the message goes as a new task.
        control_plane.finish_task(&running.id, TaskStatus::Completed);
        let fallback = run(send.execute(running.bot_id.clone(), "and row G", &[], Some(running.id.clone()))).unwrap();
        assert!(matches!(fallback, ChatSent::AsTask(_)));
        // So does it on a control plane older than the route.
        control_plane.without_newer_routes();
        let older = run(send.execute(running.bot_id.clone(), "hello", &[], Some(running.id.clone()))).unwrap();
        assert!(matches!(older, ChatSent::AsTask(_)));
        // Any other error is passed on.
        control_plane.fail_with(Some(PanelError::Unreachable("down".into())));
        assert!(run(send.execute(running.bot_id, "x", &[], Some(running.id))).is_err());
    }

    #[test]
    fn a_message_goes_as_a_chat_task_with_the_thread_after_it() {
        let (control_plane, _) = fleet();
        let bot = BotId::new("bot-wrx80").unwrap();
        let send = SendChat::new(control_plane.clone(), Arc::new(CountingKeys::default()));
        let first = as_task(run(send.execute(bot.clone(), "  How many files are there?  ", &[], None)).unwrap());
        assert_eq!(first.task.objective, "How many files are there?");
        assert!(first.task.idempotency_key.starts_with("chat-"));

        let chat = run(LoadChat::new(control_plane.clone()).execute(bot.clone())).unwrap();
        assert_eq!(chat.len(), 1);
        assert_eq!(chat[0].message(), "How many files are there?");
        assert_eq!(chat[0].reply(), "Got it. It starts as soon as a node is free.");

        let second = as_task(run(send.execute(bot.clone(), "List them", &chat, None)).unwrap());
        assert!(
            second
                .task
                .objective
                .starts_with("List them\n\n---\nEarlier in this chat")
        );
        assert!(second.task.objective.contains("The person: How many files are there?"));
        let chat = run(LoadChat::new(control_plane.clone()).execute(bot.clone())).unwrap();
        assert_eq!(
            chat.iter().map(ChatTurn::message).collect::<Vec<_>>(),
            vec!["How many files are there?", "List them"]
        );
        // Tasks not sent from the chat stay out of it.
        let all = run(control_plane.list_tasks(TaskFilter::default())).unwrap();
        assert!(all.iter().any(|t| t.bot_id == bot && !ChatTurn::is_chat(t)));
        assert!(
            run(send.execute(bot, "  ", &[], None)).is_err(),
            "an empty message is refused"
        );
    }

    #[test]
    fn a_reply_is_the_answer_or_where_the_task_stands() {
        let (control_plane, _) = fleet();
        let tasks = run(control_plane.list_tasks(TaskFilter::default())).unwrap();
        let mut turn = ChatTurn {
            task: tasks[0].clone(),
            answer: Some(" Three. ".into()),
        };
        assert_eq!(turn.reply(), "Three.");
        turn.answer = None;
        for (status, words) in [
            (TaskStatus::Running, "Working on it…"),
            (TaskStatus::WaitingApproval, "It waits for your OK before it goes on."),
            (TaskStatus::Completed, "Done, with nothing to say."),
            (TaskStatus::Expired, "Nobody answered its question in time."),
            (TaskStatus::Cancelled, "Stopped."),
        ] {
            turn.task.status = status;
            assert_eq!(turn.reply(), words);
        }
        turn.task.status = TaskStatus::Failed;
        turn.task.failure_reason = "no node".into();
        assert_eq!(turn.reply(), "It failed: no node");
        turn.task.failure_reason.clear();
        assert_eq!(turn.reply(), TaskStatus::Failed.label());
        let long: Vec<_> = (0..5)
            .map(|i| ChatTurn {
                task: crate::domain::Task {
                    objective: format!("m{i}"),
                    ..tasks[0].clone()
                },
                answer: Some(format!("a{i}")),
            })
            .collect();
        let text = objective("now", &long);
        assert!(
            !text.contains("m1") && text.contains("m2") && text.contains("m4"),
            "{text}"
        );
    }
}
