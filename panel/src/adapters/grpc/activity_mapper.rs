use serde_json::{Value, json};

use super::event_mapper::EventMapper;
use super::execution_mapper::ExecutionMapper;
use super::proto;
use super::proto::watch_task_activity_response::Update;
use super::task_mapper::TaskMapper;
use super::wire::Wire;
use crate::application::PanelError;
use crate::domain::{
    ActivityBatch, ActivityUpdate, CutReason, ExecutionActivity, ProgressCut, ProgressItem, TaskActivity, TaskId,
};

/// A task's activity tree and the updates that follow it, into the
/// panel's.
pub(super) struct ActivityMapper;

impl ActivityMapper {
    pub fn activity(message: Option<&proto::TaskActivity>) -> Result<TaskActivity, PanelError> {
        Self::tree(message.ok_or_else(|| PanelError::Unreadable("the answer has no activity".into()))?)
    }

    fn tree(message: &proto::TaskActivity) -> Result<TaskActivity, PanelError> {
        let task = message
            .task
            .as_ref()
            .ok_or_else(|| PanelError::Unreadable("an activity has no task".into()))?;
        Ok(TaskActivity {
            task: TaskMapper::task(task)?,
            executions: message
                .executions
                .iter()
                .map(Self::execution)
                .collect::<Result<_, _>>()?,
            events: EventMapper::events(&message.events)?,
            children: message.children.iter().map(Self::tree).collect::<Result<_, _>>()?,
            subtree_usage: ExecutionMapper::usage(message.subtree_usage.as_ref())?,
        })
    }

    fn execution(message: &proto::ExecutionActivity) -> Result<ExecutionActivity, PanelError> {
        let execution = message
            .execution
            .as_ref()
            .ok_or_else(|| PanelError::Unreadable("an execution of an activity is empty".into()))?;
        Ok(ExecutionActivity {
            execution: ExecutionMapper::execution(execution)?,
            progress_cut: message.progress_cut.as_ref().map(Self::cut),
            completed_items: Self::items(&message.completed_items)?,
        })
    }

    fn cut(cut: &proto::ProgressCut) -> ProgressCut {
        ProgressCut {
            reason: match proto::ProgressCutReason::try_from(cut.reason) {
                Ok(proto::ProgressCutReason::EventLimit) => CutReason::EventLimit,
                Ok(proto::ProgressCutReason::ByteLimit) => CutReason::ByteLimit,
                Ok(proto::ProgressCutReason::BatchTruncated) => CutReason::BatchTruncated,
                _ => CutReason::Other,
            },
            events_kept: u32::try_from(cut.events_kept).unwrap_or(0),
            bytes_kept: u64::try_from(cut.bytes_kept).unwrap_or(0),
            cut_at: Wire::instant(cut.cut_at.as_ref()),
            dropped: u32::try_from(cut.dropped).unwrap_or(0),
            from_transcript: cut.completion == proto::CompletionSource::Transcript as i32,
            transcript_sessions: cut.transcript_sessions.clone(),
            completion_bounded: cut.completion_bounded,
        }
    }

    fn items(items: &[proto::ProgressItem]) -> Result<Vec<ProgressItem>, PanelError> {
        items.iter().map(Self::item).collect()
    }

    /// A step. The contract spells the kinds with an underscore
    /// (`tool_started`); the nodes' events spell them with a dot
    /// (`tool.started`), and the panel folds the dotted ones.
    fn item(item: &proto::ProgressItem) -> Result<ProgressItem, PanelError> {
        let kind = match item.kind.as_str() {
            "tool_started" => "tool.started",
            "tool_ended" => "tool.ended",
            "decision_required" => "decision.required",
            "text_truncated" => "text.truncated",
            "thinking_truncated" => "thinking.truncated",
            other => other,
        };
        let at: Value = Wire::time(item.at.as_ref());
        Wire::read(
            json!({
                "kind": kind,
                "at": at,
                "tool": item.tool,
                "mcpTool": item.mcp_tool,
                "callId": item.call_id,
                "summary": item.summary,
                "status": item.status,
                "error": item.error,
                "durationMs": item.duration.as_ref().map(|d| u64::try_from(d.seconds.max(0)).unwrap_or(0) * 1000
                    + u64::try_from(d.nanos.max(0)).unwrap_or(0) / 1_000_000),
                "bot": item.bot,
                "taskId": item.task_id,
                "reason": item.reason,
                "text": item.text,
            }),
            "progress item",
        )
    }

    /// What one message of a watch brings.
    pub fn update(message: &proto::WatchTaskActivityResponse) -> Result<Option<ActivityUpdate>, PanelError> {
        let Some(update) = &message.update else {
            return Ok(None);
        };
        let task_id = |raw: &str| {
            TaskId::new(raw).map_err(|e| PanelError::Unreadable(format!("an activity update names a bad task: {e}")))
        };
        Ok(Some(match update {
            Update::Snapshot(tree) => ActivityUpdate::Snapshot(Box::new(Self::tree(tree)?)),
            Update::TaskChanged(change) => {
                let task = change
                    .task
                    .as_ref()
                    .ok_or_else(|| PanelError::Unreadable("a task change has no task".into()))?;
                ActivityUpdate::TaskChanged(Box::new(TaskMapper::task(task)?))
            }
            Update::ExecutionChanged(change) => {
                let execution = change
                    .execution
                    .as_ref()
                    .ok_or_else(|| PanelError::Unreadable("an execution change has no execution".into()))?;
                ActivityUpdate::ExecutionChanged(Box::new(ExecutionMapper::execution(execution)?))
            }
            Update::EventAppended(appended) => {
                let event = appended
                    .event
                    .as_ref()
                    .ok_or_else(|| PanelError::Unreadable("an appended event is empty".into()))?;
                ActivityUpdate::EventAppended {
                    task_id: task_id(&appended.task_id)?,
                    event: Box::new(EventMapper::event(event)?),
                }
            }
            Update::ChildAttached(attached) => {
                let child = attached
                    .child
                    .as_ref()
                    .ok_or_else(|| PanelError::Unreadable("an attached child is empty".into()))?;
                ActivityUpdate::ChildAttached {
                    parent: task_id(&attached.parent_task_id)?,
                    child: Box::new(Self::tree(child)?),
                }
            }
            Update::ProgressCompleted(done) => ActivityUpdate::ProgressCompleted {
                task_id: task_id(&done.task_id)?,
                execution_id: done.execution_id.clone(),
                cut: done.progress_cut.as_ref().map(Self::cut),
                items: Self::items(&done.items)?,
            },
            Update::Heartbeat(_) => ActivityUpdate::Heartbeat,
        }))
    }

    /// The updates of a batch of messages and the cursor of the last.
    pub fn batch(messages: &[proto::WatchTaskActivityResponse]) -> Result<ActivityBatch, PanelError> {
        let mut batch = ActivityBatch::default();
        for message in messages {
            if !message.cursor.is_empty() {
                batch.cursor = Some(message.cursor.clone());
            }
            if let Some(update) = Self::update(message)? {
                batch.updates.push(update);
            }
        }
        Ok(batch)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn stamp(text: &str) -> prost_types::Timestamp {
        Wire::timestamp(text.parse().unwrap())
    }

    fn task(id: &str, status: &str) -> proto::Task {
        proto::Task {
            id: id.into(),
            bot_id: "bot-a".into(),
            objective: "Do it.".into(),
            status: status.into(),
            created_at: Some(stamp("2026-11-02T10:00:00Z")),
            updated_at: Some(stamp("2026-11-02T10:00:05Z")),
            ..Default::default()
        }
    }

    fn run(id: &str, task_id: &str) -> proto::Execution {
        proto::Execution {
            id: id.into(),
            task_id: task_id.into(),
            bot_id: "bot-a".into(),
            started_at: Some(stamp("2026-11-02T10:00:00Z")),
            ended_at: Some(stamp("2026-11-02T10:00:04Z")),
            outcome: "Completed".into(),
            usage: Some(proto::Usage {
                requests: 2,
                cost_usd: 0.5,
                ..Default::default()
            }),
            ..Default::default()
        }
    }

    fn tree() -> proto::TaskActivity {
        proto::TaskActivity {
            task: Some(task("task-root", "Completed")),
            executions: vec![proto::ExecutionActivity {
                execution: Some(run("exec-r", "task-root")),
                progress_cut: Some(proto::ProgressCut {
                    reason: proto::ProgressCutReason::EventLimit as i32,
                    events_kept: 400,
                    completion: proto::CompletionSource::Transcript as i32,
                    transcript_sessions: vec!["s1".into()],
                    ..Default::default()
                }),
                completed_items: vec![proto::ProgressItem {
                    kind: "tool_ended".into(),
                    tool: "local_read".into(),
                    call_id: "c1".into(),
                    duration: Some(prost_types::Duration {
                        seconds: 1,
                        nanos: 500_000_000,
                    }),
                    ..Default::default()
                }],
            }],
            children: vec![proto::TaskActivity {
                task: Some(task("task-child", "Failed")),
                ..Default::default()
            }],
            subtree_usage: Some(proto::Usage {
                requests: 2,
                cost_usd: 0.5,
                cost_unknown: true,
                ..Default::default()
            }),
            ..Default::default()
        }
    }

    #[test]
    fn reads_the_tree_with_its_cut_and_its_transcript_steps() {
        let activity = ActivityMapper::activity(Some(&tree())).unwrap();
        assert_eq!(activity.children[0].task.id.as_str(), "task-child");
        let run = &activity.executions[0];
        assert!(run.from_transcript());
        assert_eq!(run.progress_cut.as_ref().unwrap().reason, CutReason::EventLimit);
        assert_eq!(run.completed_items[0].kind, "tool.ended");
        assert_eq!(run.completed_items[0].duration_ms, Some(1500));
        assert!(activity.subtree_usage.cost_unknown);
        assert!(ActivityMapper::activity(None).is_err());
        assert!(ActivityMapper::activity(Some(&proto::TaskActivity::default())).is_err());
    }

    #[test]
    fn reads_every_kind_of_update_and_the_last_cursor() {
        let messages = vec![
            proto::WatchTaskActivityResponse {
                cursor: "c1".into(),
                update: Some(Update::Snapshot(tree())),
            },
            proto::WatchTaskActivityResponse {
                cursor: "c2".into(),
                update: Some(Update::TaskChanged(proto::TaskChanged {
                    task: Some(task("task-child", "Completed")),
                })),
            },
            proto::WatchTaskActivityResponse {
                cursor: "c3".into(),
                update: Some(Update::ExecutionChanged(proto::ExecutionChanged {
                    execution: Some(run("exec-c", "task-child")),
                })),
            },
            proto::WatchTaskActivityResponse {
                cursor: "c4".into(),
                update: Some(Update::EventAppended(proto::EventAppended {
                    task_id: "task-root".into(),
                    event: Some(proto::Event {
                        event_id: "e1".into(),
                        r#type: "bot.task.said".into(),
                        created_at: Some(stamp("2026-11-02T10:00:06Z")),
                        ..Default::default()
                    }),
                })),
            },
            proto::WatchTaskActivityResponse {
                cursor: "c5".into(),
                update: Some(Update::ChildAttached(proto::ChildAttached {
                    parent_task_id: "task-root".into(),
                    child: Some(proto::TaskActivity {
                        task: Some(task("task-new", "Pending")),
                        ..Default::default()
                    }),
                })),
            },
            proto::WatchTaskActivityResponse {
                cursor: "c6".into(),
                update: Some(Update::ProgressCompleted(proto::ProgressCompleted {
                    task_id: "task-root".into(),
                    execution_id: "exec-r".into(),
                    ..Default::default()
                })),
            },
            proto::WatchTaskActivityResponse {
                cursor: "c7".into(),
                update: Some(Update::Heartbeat(proto::Heartbeat {})),
            },
        ];
        let batch = ActivityMapper::batch(&messages).unwrap();
        assert_eq!(batch.cursor.as_deref(), Some("c7"));
        assert_eq!(batch.updates.len(), 7);
        let mut mirror = match &batch.updates[0] {
            ActivityUpdate::Snapshot(tree) => (**tree).clone(),
            other => panic!("{other:?}"),
        };
        for update in batch.updates.into_iter().skip(1) {
            mirror.apply(update);
        }
        assert_eq!(mirror.flatten().len(), 3);
        assert!(
            ActivityMapper::batch(&[proto::WatchTaskActivityResponse::default()])
                .unwrap()
                .updates
                .is_empty()
        );
        let bad = proto::WatchTaskActivityResponse {
            cursor: String::new(),
            update: Some(Update::EventAppended(proto::EventAppended {
                task_id: "Bad Id".into(),
                event: None,
            })),
        };
        assert!(ActivityMapper::update(&bad).is_err());
    }
}
