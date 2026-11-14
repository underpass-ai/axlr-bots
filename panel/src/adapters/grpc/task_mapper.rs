use serde_json::{Value, json};

use super::execution_mapper::ExecutionMapper;
use super::proto;
use super::wire::Wire;
use crate::application::PanelError;
use crate::domain::{CancelledTask, DecidedTask, SentTaskMessage, SubmittedTask, Task, TaskFilter};

/// Tasks and what the calls on them answer, into the panel's.
pub(super) struct TaskMapper;

impl TaskMapper {
    pub fn task(message: &proto::Task) -> Result<Task, PanelError> {
        Wire::read(Self::shape(message)?, "task")
    }

    pub fn tasks(messages: &[proto::Task]) -> Result<Vec<Task>, PanelError> {
        messages.iter().map(Self::task).collect()
    }

    fn required_task(message: Option<&proto::Task>) -> Result<Task, PanelError> {
        Self::task(message.ok_or_else(|| PanelError::Unreadable("the answer has no task".into()))?)
    }

    pub fn submitted(answer: &proto::SubmitTaskResponse) -> Result<SubmittedTask, PanelError> {
        Ok(SubmittedTask {
            created: answer.created,
            task: Self::required_task(answer.task.as_ref())?,
        })
    }

    pub fn decided(
        task: Option<&proto::Task>,
        execution: Option<&proto::Execution>,
    ) -> Result<DecidedTask, PanelError> {
        Ok(DecidedTask {
            task: Self::required_task(task)?,
            execution: ExecutionMapper::execution(
                execution.ok_or_else(|| PanelError::Unreadable("the answer has no execution".into()))?,
            )?,
        })
    }

    pub fn cancelled(answer: &proto::CancelTaskResponse) -> Result<CancelledTask, PanelError> {
        Ok(CancelledTask {
            task: Self::required_task(answer.task.as_ref())?,
            execution: answer.execution.as_ref().map(ExecutionMapper::execution).transpose()?,
            changed: answer.changed,
        })
    }

    pub fn message_sent(answer: &proto::SendTaskMessageResponse) -> Result<SentTaskMessage, PanelError> {
        Wire::read(
            json!({
                "taskId": answer.task_id,
                "executionId": answer.execution_id,
                "createdAt": Wire::time(answer.created_at.as_ref()),
            }),
            "message receipt",
        )
    }

    pub fn list_request(filter: &TaskFilter) -> proto::ListTasksRequest {
        proto::ListTasksRequest {
            bot_id: filter.bot.as_ref().map(ToString::to_string).unwrap_or_default(),
            status: filter.status.map(|s| s.name().to_string()).unwrap_or_default(),
            since: filter.since.map(Wire::timestamp),
            limit: i32::try_from(filter.limit).unwrap_or(i32::MAX),
        }
    }

    fn shape(task: &proto::Task) -> Result<Value, PanelError> {
        let trigger = task.trigger.clone().unwrap_or_default();
        Ok(json!({
            "id": task.id,
            "botId": task.bot_id,
            "objective": task.objective,
            "status": task.status,
            "idempotencyKey": task.idempotency_key,
            "correlationId": task.correlation_id,
            "trigger": {"type": trigger.r#type, "name": trigger.name},
            "input": Wire::document(&task.input)?,
            "executionIds": task.execution_ids,
            "failureReason": task.failure_reason,
            "priority": task.priority,
            "waiting": task.waiting,
            "createdAt": Wire::time(task.created_at.as_ref()),
            "updatedAt": Wire::time(task.updated_at.as_ref()),
        }))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::domain::{BotId, TaskStatus};

    pub(crate) fn message() -> proto::Task {
        proto::Task {
            id: "task-68ed".into(),
            bot_id: "bot-wrx80".into(),
            objective: "List the files.\nThen stop.".into(),
            status: "Pending".into(),
            trigger: Some(proto::TriggerRef {
                r#type: "manual".into(),
                name: "manual".into(),
            }),
            input: br#"{"from":"watcher","message":"a deploy is starting"}"#.to_vec(),
            execution_ids: vec!["exec-4a92".into()],
            priority: "urgent".into(),
            waiting: "a node".into(),
            created_at: Some(Wire::timestamp("2026-10-24T22:32:26Z".parse().unwrap())),
            updated_at: Some(Wire::timestamp("2026-10-24T22:32:33Z".parse().unwrap())),
            ..Default::default()
        }
    }

    #[test]
    fn reads_a_task_with_its_input() {
        let task = TaskMapper::task(&message()).unwrap();
        assert!(task.is_urgent());
        assert_eq!(task.told_by().as_deref(), Some("watcher: a deploy is starting"));
        assert_eq!(task.waits_for().as_deref(), Some("A node"));
        assert_eq!(task.execution_ids.len(), 1);
    }

    #[test]
    fn a_task_of_a_newer_status_is_unknown_and_one_without_an_id_is_unreadable() {
        let mut newer = message();
        newer.status = "Sleeping".into();
        assert_eq!(TaskMapper::task(&newer).unwrap().status, TaskStatus::Unknown);
        let mut broken = message();
        broken.id.clear();
        assert!(matches!(TaskMapper::task(&broken), Err(PanelError::Unreadable(_))));
    }

    #[test]
    fn asks_for_tasks_by_the_filter() {
        let filter = TaskFilter {
            bot: Some(BotId::new("bot-a").unwrap()),
            status: Some(TaskStatus::Failed),
            since: None,
            limit: 50,
        };
        let request = TaskMapper::list_request(&filter);
        assert_eq!(
            (request.bot_id.as_str(), request.status.as_str(), request.limit),
            ("bot-a", "Failed", 50)
        );
        assert!(request.since.is_none());
    }

    #[test]
    fn answers_without_their_task_are_unreadable() {
        assert!(TaskMapper::decided(None, None).is_err());
        assert!(TaskMapper::cancelled(&proto::CancelTaskResponse::default()).is_err());
        let cancelled = TaskMapper::cancelled(&proto::CancelTaskResponse {
            task: Some(message()),
            changed: true,
            ..Default::default()
        })
        .unwrap();
        assert!(cancelled.changed && cancelled.execution.is_none());
    }
}
