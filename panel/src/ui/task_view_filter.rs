use crate::domain::{BotId, Task, TaskStatus};

/// What the task list shows: one status or all, one bot or all.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct TaskViewFilter {
    pub status: Option<TaskStatus>,
    pub bot: Option<BotId>,
}

impl TaskViewFilter {
    pub fn apply<'a>(&self, tasks: &'a [Task]) -> Vec<&'a Task> {
        tasks
            .iter()
            .filter(|t| self.status.is_none_or(|s| t.status == s))
            .filter(|t| self.bot.as_ref().is_none_or(|b| &t.bot_id == b))
            .collect()
    }
}
