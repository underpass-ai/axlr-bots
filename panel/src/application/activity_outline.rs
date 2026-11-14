use super::{Activity, ActivityBranch, ActivityEntry, ActivityRun};
use crate::domain::{Event, ExecutionActivity, ProgressItem, TaskActivity};

/// A task's activity tree laid out for the screen: its tasks in reading
/// order, each with its executions and their tools.
pub struct ActivityOutline;

impl ActivityOutline {
    pub fn of(tree: &TaskActivity) -> Vec<ActivityBranch> {
        tree.flatten()
            .into_iter()
            .map(|(depth, node)| {
                let live = !node.task.status.is_terminal();
                ActivityBranch {
                    depth,
                    task: node.task.clone(),
                    runs: node
                        .executions
                        .iter()
                        .map(|run| Self::run(run, &node.events, live))
                        .collect(),
                    own: node.own_usage(),
                    subtree: node.subtree_usage,
                }
            })
            .collect()
    }

    /// The tools an execution called: its progress events, then the steps
    /// completed from the transcript after the cut, folded together by the
    /// same reading the live Activity uses.
    fn run(run: &ExecutionActivity, events: &[Event], live: bool) -> ActivityRun {
        let id = run.execution.id.as_str();
        let mut own: Vec<Event> = events.iter().filter(|e| e.execution_id == id).cloned().collect();
        if !run.completed_items.is_empty() {
            own.push(Self::completed(run));
        }
        let tools = Activity::of(&own, live)
            .entries
            .into_iter()
            .filter_map(|entry| match entry {
                ActivityEntry::Tool(call) => Some(call),
                _ => None,
            })
            .collect();
        ActivityRun {
            execution: run.execution.clone(),
            tools,
            cut: run.progress_cut.clone(),
            from_transcript: run.from_transcript(),
        }
    }

    /// The completed steps as the progress event the nodes would have sent.
    fn completed(run: &ExecutionActivity) -> Event {
        let items: Vec<&ProgressItem> = run.completed_items.iter().collect();
        let at = run
            .progress_cut
            .as_ref()
            .and_then(|cut| cut.cut_at)
            .unwrap_or(run.execution.started_at);
        Event {
            event_id: format!("completed-{}", run.execution.id),
            event_type: "bot.task.progress".into(),
            tenant_id: String::new(),
            bot_id: run.execution.bot_id.to_string(),
            node_id: String::new(),
            group_name: String::new(),
            task_id: run.execution.task_id.to_string(),
            execution_id: run.execution.id.to_string(),
            correlation_id: String::new(),
            created_at: at,
            payload: serde_json::json!({ "items": items }).as_object().cloned(),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::application::ToolState;
    use crate::domain::{CutReason, ProgressCut};

    fn tree() -> TaskActivity {
        let mut tree = crate::domain::sample_task_activity();
        let run = &mut tree.executions[0];
        let kept: Event = serde_json::from_value(serde_json::json!({
            "eventId": "e1", "type": "bot.task.progress", "executionId": "exec-r",
            "createdAt": "2026-11-02T10:00:01Z", "payload": {"items": [
                {"kind": "tool.started", "tool": "local_read", "callId": "c1", "at": "2026-11-02T10:00:01Z"},
                {"kind": "tool.ended", "tool": "local_read", "callId": "c1", "status": "ok", "durationMs": 120}
            ]}
        }))
        .unwrap();
        run.progress_cut = Some(ProgressCut {
            reason: CutReason::EventLimit,
            events_kept: 400,
            bytes_kept: 1,
            cut_at: None,
            dropped: 0,
            from_transcript: true,
            transcript_sessions: vec!["s".into()],
            completion_bounded: false,
        });
        run.completed_items = serde_json::from_value(serde_json::json!([
            {"kind": "tool.started", "tool": "local_write", "callId": "c2", "at": "2026-11-02T10:00:02Z"},
            {"kind": "tool.ended", "tool": "local_write", "callId": "c2", "status": "failed", "error": "denied", "durationMs": 2400}
        ]))
        .unwrap();
        tree.events.push(kept);
        tree
    }

    #[test]
    fn lays_out_the_tree_with_its_tools_and_the_completed_steps() {
        let branches = ActivityOutline::of(&tree());
        assert_eq!(branches.iter().map(|b| b.depth).collect::<Vec<_>>(), [0, 1]);
        let root = &branches[0];
        assert_eq!(root.runs.len(), 1);
        let run = &root.runs[0];
        assert!(run.from_transcript);
        let tools: Vec<(&str, ToolState, Option<u64>)> = run
            .tools
            .iter()
            .map(|t| (t.tool.as_str(), t.state, t.duration_ms))
            .collect();
        assert_eq!(
            tools,
            [
                ("local_read", ToolState::Ok, Some(120)),
                ("local_write", ToolState::Failed, Some(2400))
            ]
        );
        assert_eq!(root.own.requests, 3);
        assert_eq!(root.subtree.requests, 5);
        assert!(!branches[1].runs[0].from_transcript);
        assert!(branches[1].runs[0].tools.is_empty());
    }
}
