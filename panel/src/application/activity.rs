use std::collections::BTreeMap;

use super::{ActivityEntry, DelegationActivity, ToolActivity, ToolState};
use crate::domain::{Event, TaskId};

/// What a task's executions did, folded from its timeline: each tool call
/// with its state and how long it took, the bots it asked, the calls that
/// wait for a person, what the person wrote to it and what it said back,
/// and the model's words and reasoning as they came.
#[derive(Debug, Clone, Default, PartialEq)]
pub struct Activity {
    pub entries: Vec<ActivityEntry>,
    /// The model's words, in the order they came.
    pub text: String,
    /// The node stopped sending words at the run's cap.
    pub text_cut: bool,
    /// The model's reasoning, in the order it came, when the run tells it.
    pub thinking: String,
    /// The node stopped sending reasoning at the run's cap.
    pub thinking_cut: bool,
    /// Steps the node dropped on the way, and whether the control plane
    /// stopped keeping them at the execution's bound.
    pub dropped: u64,
    pub truncated: bool,
}

impl Activity {
    /// Folds the timeline, oldest first. `live` is whether the task may
    /// still run: once it ended, a call without an end is unfinished.
    pub fn of(events: &[Event], live: bool) -> Self {
        let mut activity = Self::default();
        let mut calls: BTreeMap<(String, String), usize> = BTreeMap::new();
        // Whether the last item was reasoning: the reasoning of the next
        // step starts a paragraph of its own.
        let mut thinking = false;
        for event in events {
            match event.event_type.as_str() {
                "bot.task.progress" => {
                    activity.dropped += event
                        .payload
                        .as_ref()
                        .and_then(|p| p.get("dropped"))
                        .and_then(|v| v.as_u64())
                        .unwrap_or(0);
                    activity.truncated |= event.payload_text("truncated").as_deref() == Some("true");
                    for item in event.progress() {
                        let at = item.at.unwrap_or(event.created_at);
                        let was_thinking = std::mem::replace(&mut thinking, item.kind == "thinking");
                        let key = (event.execution_id.clone(), item.call_id.clone());
                        match item.kind.as_str() {
                            "tool.started" => {
                                calls.insert(key, activity.entries.len());
                                activity.entries.push(ActivityEntry::Tool(ToolActivity {
                                    call_id: item.call_id,
                                    tool: item.tool,
                                    mcp_tool: item.mcp_tool,
                                    summary: item.summary,
                                    state: ToolState::Running,
                                    error: String::new(),
                                    started_at: at,
                                    duration_ms: None,
                                }));
                            }
                            "tool.ended" => match activity.call(&calls, &key) {
                                Some(call) => {
                                    call.state = ToolState::ended(&item.status);
                                    call.error = item.error;
                                    call.duration_ms = item.duration_ms;
                                }
                                None => activity.entries.push(ActivityEntry::Tool(ToolActivity {
                                    call_id: item.call_id,
                                    tool: item.tool,
                                    mcp_tool: String::new(),
                                    summary: String::new(),
                                    state: ToolState::ended(&item.status),
                                    error: item.error,
                                    started_at: at,
                                    duration_ms: item.duration_ms,
                                })),
                            },
                            "decision.required" => {
                                if let Some(call) = activity.call(&calls, &key) {
                                    call.state = ToolState::Waiting;
                                }
                                activity.entries.push(ActivityEntry::Waiting {
                                    tool: item.tool,
                                    call_id: item.call_id,
                                    at,
                                    decision: String::new(),
                                });
                            }
                            "delegation" => activity.asked(DelegationActivity {
                                bot: item.bot,
                                task: TaskId::new(&item.task_id).ok(),
                                refused: item.reason,
                                at,
                            }),
                            "text" => activity.text.push_str(&item.text),
                            "text.truncated" => activity.text_cut = true,
                            "thinking" => {
                                if !was_thinking && !activity.thinking.is_empty() {
                                    activity.thinking = format!("{}\n\n", activity.thinking.trim_end());
                                }
                                activity.thinking.push_str(&item.text);
                            }
                            "thinking.truncated" => activity.thinking_cut = true,
                            _ => {}
                        }
                    }
                }
                "bot.task.delegated" => activity.asked(DelegationActivity {
                    bot: event.payload_text("childBotId").unwrap_or_default(),
                    task: event.payload_text("childTaskId").and_then(|t| TaskId::new(&t).ok()),
                    refused: String::new(),
                    at: event.created_at,
                }),
                "bot.delegation.refused" => activity.asked(DelegationActivity {
                    bot: event.payload_text("target").unwrap_or_default(),
                    task: None,
                    refused: event.payload_text("reason").unwrap_or_default(),
                    at: event.created_at,
                }),
                "bot.task.decided" => activity.decided(
                    &event.payload_text("callId").unwrap_or_default(),
                    &event.payload_text("decision").unwrap_or_default(),
                ),
                "bot.task.message" => activity.entries.push(ActivityEntry::YouWrote {
                    text: event.payload_text("text").unwrap_or_default(),
                    at: event.created_at,
                }),
                "bot.task.said" => activity.entries.push(ActivityEntry::Said {
                    text: event.payload_text("text").unwrap_or_default(),
                    at: event.created_at,
                }),
                _ => {}
            }
        }
        if !live {
            for entry in &mut activity.entries {
                if let ActivityEntry::Tool(call) = entry
                    && matches!(call.state, ToolState::Running | ToolState::Waiting)
                {
                    call.state = ToolState::Unfinished;
                }
            }
        }
        activity
    }

    /// The call the key names, to change it: in its execution, or, for a
    /// call decided by a person and ended by the execution that took the
    /// decision, the call with its id still waiting or running.
    fn call(&mut self, calls: &BTreeMap<(String, String), usize>, key: &(String, String)) -> Option<&mut ToolActivity> {
        if key.1.is_empty() {
            return None;
        }
        let index = match calls.get(key) {
            Some(index) => *index,
            None => self.entries.iter().position(|entry| {
                matches!(entry, ActivityEntry::Tool(call) if call.call_id == key.1 && matches!(call.state, ToolState::Running | ToolState::Waiting))
            })?,
        };
        match self.entries.get_mut(index) {
            Some(ActivityEntry::Tool(call)) => Some(call),
            _ => None,
        }
    }

    /// A person decided the call that waited: it no longer waits, it runs
    /// when approved and was denied otherwise.
    fn decided(&mut self, call_id: &str, decision: &str) {
        let decision = if decision == "deny" { "deny" } else { "approve" };
        for entry in &mut self.entries {
            match entry {
                ActivityEntry::Tool(call)
                    if call.state == ToolState::Waiting && (call_id.is_empty() || call.call_id == call_id) =>
                {
                    call.state = if decision == "deny" {
                        ToolState::Denied
                    } else {
                        ToolState::Running
                    };
                }
                ActivityEntry::Waiting {
                    call_id: waited,
                    decision: said,
                    ..
                } if said.is_empty() && (call_id.is_empty() || waited == call_id) => *said = decision.to_string(),
                _ => {}
            }
        }
    }

    /// Adds a bot asked, once: the node and the control plane both tell
    /// it, by the child task, or by the bot and the refusal.
    fn asked(&mut self, asked: DelegationActivity) {
        if asked.bot.is_empty() {
            return;
        }
        let known = self.entries.iter().any(|entry| match entry {
            ActivityEntry::Delegation(seen) => match (&seen.task, &asked.task) {
                (Some(a), Some(b)) => a == b,
                (None, None) => seen.bot == asked.bot && seen.refused == asked.refused,
                _ => false,
            },
            _ => false,
        });
        if !known {
            self.entries.push(ActivityEntry::Delegation(asked));
        }
    }

    /// Whether there is anything to show.
    pub fn is_empty(&self) -> bool {
        self.entries.is_empty() && self.text.is_empty() && self.thinking.is_empty()
    }

    /// The calls still going.
    pub fn running(&self) -> usize {
        self.entries.iter().filter(|e| e.is_running()).count()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn event(id: &str, kind: &str, execution: &str, payload: serde_json::Value) -> Event {
        serde_json::from_value(serde_json::json!({
            "eventId": id, "type": kind, "tenantId": "t", "botId": "watcher", "taskId": "task-1",
            "executionId": execution, "createdAt": "2026-11-02T10:00:00Z", "payload": payload
        }))
        .unwrap()
    }

    fn progress(id: &str, items: serde_json::Value) -> Event {
        event(id, "bot.task.progress", "exec-1", serde_json::json!({ "items": items }))
    }

    fn timeline() -> Vec<Event> {
        vec![
            event("e0", "bot.task.started", "exec-1", serde_json::json!({})),
            progress(
                "e1",
                serde_json::json!([
                    {"kind": "thinking", "text": "The pods first, "},
                    {"kind": "text", "text": "Looking at the pods. "},
                    {"kind": "tool.started", "tool": "axlr_call_tool", "mcpTool": "list_pods", "callId": "c1",
                     "summary": "{\"namespace\":\"web\"}", "at": "2026-11-02T10:00:01Z"},
                    {"kind": "tool.ended", "tool": "axlr_call_tool", "callId": "c1", "status": "ok", "durationMs": 120},
                    {"kind": "tool.started", "tool": "local_read", "callId": "c2"},
                    {"kind": "tool.ended", "tool": "local_grep", "callId": "c9", "status": "failed", "error": "no such file"},
                    {"kind": "delegation", "bot": "worker", "taskId": "task-child"},
                    {"kind": "delegation", "bot": "stranger", "reason": "not_allowed"},
                    {"kind": "something new"}
                ]),
            ),
            event(
                "e2",
                "bot.task.delegated",
                "exec-1",
                serde_json::json!({"childBotId": "worker", "childTaskId": "task-child"}),
            ),
            event(
                "e3",
                "bot.delegation.refused",
                "exec-1",
                serde_json::json!({"target": "stranger", "reason": "not_allowed"}),
            ),
            event(
                "e4",
                "bot.task.delegated",
                "exec-1",
                serde_json::json!({"childBotId": "helper", "childTaskId": "task-other"}),
            ),
            event(
                "e5",
                "bot.task.message",
                "exec-1",
                serde_json::json!({"text": "check row F"}),
            ),
            event(
                "e6",
                "bot.task.said",
                "exec-1",
                serde_json::json!({"text": "Row F next."}),
            ),
            event(
                "e7",
                "bot.task.progress",
                "exec-1",
                serde_json::json!({"dropped": 2, "truncated": true, "items": [
                    {"kind": "text", "text": "All pods run."},
                    {"kind": "tool.started", "tool": "service_restart", "callId": "c3"},
                    {"kind": "decision.required", "tool": "service_restart", "callId": "c3"},
                    {"kind": "text.truncated"},
                    {"kind": "thinking", "text": "then the log."},
                    {"kind": "thinking.truncated"},
                    {"kind": "tool.ended", "tool": "x"}
                ]}),
            ),
        ]
    }

    #[test]
    fn folds_the_runs_steps_in_order() {
        let activity = Activity::of(&timeline(), true);
        let lines: Vec<String> = activity.entries.iter().map(ActivityEntry::headline).collect();
        assert_eq!(
            lines,
            [
                "list_pods · ok · 120 ms",
                "local_read · running",
                "local_grep · failed",
                "Asked worker · task task-child",
                "Asked stranger · refused: not_allowed",
                "Asked helper · task task-other",
                "You wrote: check row F",
                "It answered: Row F next.",
                "service_restart · waits for you",
                "Waits for your OK to call service_restart",
                "x · ok",
            ]
        );
        assert_eq!(activity.text, "Looking at the pods. All pods run.");
        assert!(activity.text_cut && activity.truncated);
        // A step came between: the next reasoning is a paragraph of its own.
        assert_eq!(activity.thinking, "The pods first,\n\nthen the log.");
        assert!(activity.thinking_cut);
        assert_eq!(activity.dropped, 2);
        assert_eq!(activity.running(), 1);
        assert!(!activity.is_empty());
        assert_eq!(
            activity.entries[0].at(),
            "2026-11-02T10:00:01Z".parse::<chrono::DateTime<chrono::Utc>>().unwrap()
        );
    }

    #[test]
    fn a_task_that_ended_leaves_no_call_running() {
        let activity = Activity::of(&timeline(), false);
        assert_eq!(activity.running(), 0);
        assert_eq!(activity.entries[1].headline(), "local_read · no end seen");
        assert!(
            activity.entries[2]
                .details()
                .contains(&("Error", "no such file".to_string()))
        );
        assert_eq!(activity.entries[8].headline(), "service_restart · no end seen");
        assert!(Activity::of(&[], false).is_empty());
        let thought = Activity::of(
            &[progress("e1", serde_json::json!([{"kind": "thinking", "text": "Hm."}]))],
            true,
        );
        assert!(!thought.is_empty() && thought.text.is_empty());
    }

    #[test]
    fn each_step_tells_its_details() {
        let activity = Activity::of(&timeline(), true);
        assert_eq!(
            activity.entries[0].details(),
            [
                ("Tool", "axlr_call_tool".to_string()),
                ("MCP tool", "list_pods".to_string()),
                ("State", "ok".to_string()),
                ("Took", "120 ms".to_string()),
                ("Arguments", "{\"namespace\":\"web\"}".to_string()),
                ("Call id", "c1".to_string()),
            ]
        );
        assert_eq!(
            activity.entries[3].details_text(),
            "Asked worker · task task-child\nBot: worker\nTask: task-child"
        );
        assert_eq!(
            activity.entries[4].details(),
            [("Bot", "stranger".to_string()), ("Refused", "not_allowed".to_string())]
        );
        assert_eq!(activity.entries[6].details(), [("Words", "check row F".to_string())]);
        assert_eq!(
            activity.entries[9].details(),
            [("Tool", "service_restart".to_string()), ("Call id", "c3".to_string())]
        );
        let unnamed = Activity::of(
            &[progress(
                "e1",
                serde_json::json!([{"kind": "delegation", "bot": "", "taskId": "x"}]),
            )],
            true,
        );
        assert!(unnamed.is_empty());
        let asked = Activity::of(
            &[progress(
                "e1",
                serde_json::json!([{"kind": "delegation", "bot": "worker"}]),
            )],
            true,
        );
        assert_eq!(asked.entries[0].headline(), "Asked worker");
    }

    #[test]
    fn a_decided_call_no_longer_waits_and_its_end_in_the_next_run_closes_it() {
        let mut events = timeline();
        events.push(event(
            "e8",
            "bot.task.decided",
            "exec-2",
            serde_json::json!({"decision": "approve", "callId": "c3"}),
        ));
        let decided = Activity::of(&events, true);
        assert_eq!(decided.entries[8].headline(), "service_restart · running");
        assert_eq!(
            decided.entries[9].headline(),
            "You approved the call to service_restart"
        );
        events.push(event(
            "e9",
            "bot.task.progress",
            "exec-2",
            serde_json::json!({"items": [{"kind": "tool.ended", "tool": "service_restart", "callId": "c3", "status": "ok"}]}),
        ));
        let ended = Activity::of(&events, true);
        assert_eq!(ended.entries[8].headline(), "service_restart · ok");
        assert_eq!(ended.entries.len(), decided.entries.len());

        let mut denied = timeline();
        denied.push(event(
            "e8",
            "bot.task.decided",
            "exec-2",
            serde_json::json!({"decision": "deny", "callId": "c3"}),
        ));
        let denied = Activity::of(&denied, true);
        assert_eq!(denied.entries[8].headline(), "service_restart · denied");
        assert_eq!(denied.entries[9].headline(), "You denied the call to service_restart");
    }
}
