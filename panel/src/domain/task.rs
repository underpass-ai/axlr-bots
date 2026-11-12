use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

use super::{BotId, ExecutionId, TaskId, TaskStatus, TriggerRef};

/// A task as the control plane reports it: what a bot was asked, where
/// it stands and the executions that ran it.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Task {
    pub id: TaskId,
    pub bot_id: BotId,
    pub objective: String,
    pub status: TaskStatus,
    #[serde(default)]
    pub idempotency_key: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub correlation_id: String,
    #[serde(default)]
    pub trigger: TriggerRef,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub input: Option<serde_json::Value>,
    #[serde(default)]
    pub execution_ids: Vec<ExecutionId>,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub failure_reason: String,
    /// `urgent` for a task that went first; empty for normal.
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub priority: String,
    /// The incident a triage task works on; empty for any other task.
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub incident_id: String,
    /// Why a Pending task of a placed bot has not started, as the
    /// control plane says it: `waiting for node web-1, which is
    /// Offline`; empty for any other task.
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub waiting: String,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

impl Task {
    /// Whether it went first and stopped the bot's work in progress.
    pub fn is_urgent(&self) -> bool {
        self.priority == "urgent"
    }

    /// What it waits for, while it is Pending and its bot's node is
    /// away: `Waiting for node web-1, which is Offline`.
    pub fn waits_for(&self) -> Option<String> {
        if self.status != TaskStatus::Pending || self.waiting.trim().is_empty() {
            return None;
        }
        let mut chars = self.waiting.trim().chars();
        chars.next().map(|first| first.to_uppercase().chain(chars).collect())
    }

    /// Who said what, for a task an event made: `watcher · stop.halt: a
    /// deploy is starting`.
    pub fn told_by(&self) -> Option<String> {
        let input = self.input.as_ref()?;
        let from = input.get("from")?.as_str()?;
        let message = input.get("message")?.as_str()?;
        match input.get("name").and_then(|n| n.as_str()) {
            Some(name) => Some(format!("{from} · {name}: {message}")),
            None => Some(format!("{from}: {message}")),
        }
    }

    /// What its node read of the team's memory before the run
    /// (`input.earlier`), one line per key: `ws:s1 (team qa): a · b`, or
    /// the line that says why nothing was read.
    pub fn earlier(&self) -> Vec<String> {
        let Some(reads) = self
            .input
            .as_ref()
            .and_then(|i| i.get("earlier"))
            .and_then(|e| e.as_array())
        else {
            return Vec::new();
        };
        reads
            .iter()
            .filter_map(|read| {
                let key = read.get("key")?.as_str()?;
                let team = read.get("team")?.as_str()?;
                let lines: Vec<&str> = read
                    .get("read")
                    .and_then(|r| r.as_array())
                    .map(|r| r.iter().filter_map(|l| l.as_str()).collect())
                    .unwrap_or_default();
                let what = if lines.is_empty() {
                    read.get("line").and_then(|l| l.as_str()).unwrap_or("").to_string()
                } else {
                    lines.join(" · ")
                };
                Some(format!("{key} (team {team}): {what}"))
            })
            .collect()
    }

    /// What the task knew when it started: the notes of the events its
    /// bot heard, oldest first, as `recepcion · info.turno: shift at six`.
    pub fn knew(&self) -> Vec<String> {
        let Some(notes) = self
            .input
            .as_ref()
            .and_then(|i| i.get("notes"))
            .and_then(|n| n.as_array())
        else {
            return Vec::new();
        };
        notes
            .iter()
            .filter_map(|note| {
                let from = note.get("from")?.as_str()?;
                let name = note.get("name")?.as_str()?;
                let message = note.get("message")?.as_str()?;
                Some(format!("{from} · {name}: {message}"))
            })
            .collect()
    }

    /// What it was asked, in its first line: for a triage task, without
    /// the incident's own name its objective starts with (`Incident
    /// inc-… (sev1): `), which the incident's page already shows.
    pub fn ask(&self) -> &str {
        let first = self.objective.lines().next().unwrap_or_default();
        if self.incident_id.is_empty() {
            return first;
        }
        first
            .strip_prefix("Incident ")
            .and_then(|rest| rest.strip_prefix(self.incident_id.as_str()))
            .and_then(|rest| rest.split_once("): "))
            .map_or(first, |(_, ask)| ask)
    }

    /// The objective's first line, cut to `max` characters with an ellipsis.
    pub fn headline(&self, max: usize) -> String {
        let first = self.objective.lines().next().unwrap_or_default();
        if first.chars().count() <= max {
            return first.to_string();
        }
        let cut: String = first.chars().take(max.saturating_sub(1)).collect();
        format!("{cut}…")
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    pub(crate) fn sample() -> Task {
        serde_json::from_str(
            r#"{"id":"task-68ed","botId":"bot-wrx80","objective":"List the files in your workspace and say how many there are.\nThen stop.",
            "status":"Completed","idempotencyKey":"wrx80-first-run-1","correlationId":"trace-e470",
            "trigger":{"type":"manual","name":"manual"},"executionIds":["exec-4a92"],
            "createdAt":"2026-10-24T22:32:26Z","updatedAt":"2026-10-24T22:32:33Z"}"#,
        )
        .unwrap()
    }

    #[test]
    fn says_who_told_it_and_whether_it_was_urgent() {
        let mut task = sample();
        assert!(!task.is_urgent() && task.told_by().is_none());
        task.priority = "urgent".into();
        task.input = Some(serde_json::json!({"from": "watcher", "message": "a deploy is starting"}));
        assert!(task.is_urgent());
        assert_eq!(task.told_by().as_deref(), Some("watcher: a deploy is starting"));
        task.input = Some(
            serde_json::json!({"from": "watcher", "name": "stop.halt", "message": "a deploy is starting",
            "notes": [{"from": "recepcion", "name": "info.turno", "message": "shift", "at": "2026-10-28T10:00:00Z"}, {"bad": 1}]}),
        );
        assert_eq!(
            task.told_by().as_deref(),
            Some("watcher · stop.halt: a deploy is starting")
        );
        assert_eq!(task.knew(), vec!["recepcion · info.turno: shift"]);
        assert!(sample().knew().is_empty());
    }

    #[test]
    fn says_what_its_node_read_of_the_teams_memory() {
        let mut task = sample();
        assert!(task.earlier().is_empty());
        task.input = Some(serde_json::json!({"earlier": [
            {"key": "ws:s1", "team": "qa", "read": ["F-12 flaky", "open: F-13"]},
            {"key": "project:dev.x", "team": "dev", "line": "held by team dev"},
            {"bad": 1}
        ]}));
        assert_eq!(
            task.earlier(),
            vec![
                "ws:s1 (team qa): F-12 flaky · open: F-13",
                "project:dev.x (team dev): held by team dev"
            ]
        );
    }

    #[test]
    fn says_what_a_pending_task_waits_for() {
        let mut task = sample();
        task.waiting = "waiting for node web-1, which is Offline".into();
        assert_eq!(task.waits_for(), None, "a Completed task waits for nothing");
        task.status = TaskStatus::Pending;
        assert_eq!(
            task.waits_for().as_deref(),
            Some("Waiting for node web-1, which is Offline")
        );
        task.waiting = " ".into();
        assert_eq!(task.waits_for(), None);
    }

    #[test]
    fn reads_the_api_json() {
        let task = sample();
        assert_eq!(task.status, TaskStatus::Completed);
        assert_eq!(task.execution_ids.len(), 1);
        assert_eq!(task.trigger.describe(), "manual");
        assert!(task.input.is_none());
        assert!(task.failure_reason.is_empty());
    }

    #[test]
    fn a_triage_task_asks_without_its_incident_name() {
        let mut task = sample();
        assert_eq!(
            task.ask(),
            "List the files in your workspace and say how many there are."
        );
        task.incident_id = "inc-12ff".into();
        task.objective = "Incident inc-12ff (sev3): Slow DNS answers. Find the cause.\nMore".into();
        assert_eq!(task.ask(), "Slow DNS answers. Find the cause.");
        task.objective = "Look at inc-12ff".into();
        assert_eq!(task.ask(), "Look at inc-12ff");
    }

    #[test]
    fn cuts_the_headline() {
        let task = sample();
        assert_eq!(
            task.headline(100),
            "List the files in your workspace and say how many there are."
        );
        assert_eq!(task.headline(10), "List the …");
    }
}
