use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

use super::{BotId, IncidentSeverity, IncidentStatus, TaskId};

/// One grouped fault, as the control plane reports it: what its alerts
/// say, the bot that triages it, the tasks that worked on it and where
/// it stands.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Incident {
    pub id: String,
    pub tenant_id: String,
    pub bot_id: BotId,
    pub group_key: String,
    #[serde(default)]
    pub source: String,
    pub title: String,
    #[serde(default)]
    pub severity: IncidentSeverity,
    pub status: IncidentStatus,
    /// The alerts seen, oldest first.
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub fingerprints: Vec<String>,
    /// The alerts the source said went green.
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub resolved_fingerprints: Vec<String>,
    #[serde(default)]
    pub cycle: u32,
    /// The triage tasks, one per cycle, oldest first.
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub task_ids: Vec<TaskId>,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub diagnosis: String,
    /// Why it was escalated or resolved.
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub reason: String,
    #[serde(default, skip_serializing_if = "is_zero")]
    pub repeats: u32,
    #[serde(default, skip_serializing_if = "is_zero")]
    pub retries: u32,
    #[serde(default, rename = "costUsd", skip_serializing_if = "is_free")]
    pub cost_usd: f64,
    pub opened_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
    pub last_seen_at: DateTime<Utc>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub resolved_at: Option<DateTime<Utc>>,
}

fn is_zero(n: &u32) -> bool {
    *n == 0
}

fn is_free(usd: &f64) -> bool {
    *usd == 0.0
}

impl Incident {
    /// The alerts still firing: those seen that the source has not said
    /// went green, oldest first.
    pub fn firing(&self) -> Vec<&str> {
        self.fingerprints
            .iter()
            .filter(|f| !self.resolved_fingerprints.contains(f))
            .map(String::as_str)
            .collect()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// An incident as `GET /v1/incidents/{id}` answers it.
    const FULL: &str = r#"{"id":"inc-one","tenantId":"acme","botId":"bot-triage","groupKey":"{}:{alertname=\"HighLatency\"}",
        "source":"alertmanager","title":"HighLatency on checkout","severity":"sev2","status":"Escalated",
        "fingerprints":["fp-a","fp-b"],"resolvedFingerprints":["fp-a"],"cycle":2,"taskIds":["task-1","task-2"],
        "diagnosis":"The cache is cold.","reason":"No fix in two tries.","repeats":4,"retries":1,"costUsd":0.25,
        "openedAt":"2026-11-01T10:00:00Z","updatedAt":"2026-11-01T10:20:00Z","lastSeenAt":"2026-11-01T10:15:00Z"}"#;

    #[test]
    fn reads_the_api_json_and_writes_it_back() {
        let incident: Incident = serde_json::from_str(FULL).unwrap();
        assert_eq!(incident.bot_id.as_str(), "bot-triage");
        assert_eq!(incident.severity, IncidentSeverity::Sev2);
        assert_eq!(incident.status, IncidentStatus::Escalated);
        assert_eq!(incident.task_ids.len(), 2);
        assert_eq!(incident.firing(), vec!["fp-b"]);
        assert_eq!((incident.repeats, incident.retries, incident.cycle), (4, 1, 2));
        assert!(incident.resolved_at.is_none());
        let again: Incident = serde_json::from_str(&serde_json::to_string(&incident).unwrap()).unwrap();
        assert_eq!(again, incident);
    }

    #[test]
    fn reads_a_minimal_one_and_values_it_does_not_know() {
        let incident: Incident = serde_json::from_str(
            r#"{"id":"inc-two","tenantId":"t","botId":"bot","groupKey":"g","title":"Disk full",
            "status":"Muted","severity":"sev9","openedAt":"2026-11-01T10:00:00Z",
            "updatedAt":"2026-11-01T10:00:00Z","lastSeenAt":"2026-11-01T10:00:00Z"}"#,
        )
        .unwrap();
        assert_eq!(incident.status, IncidentStatus::Unknown);
        assert_eq!(incident.severity, IncidentSeverity::Unknown);
        assert!(incident.fingerprints.is_empty() && incident.task_ids.is_empty());
        assert!(incident.firing().is_empty());
        let absent: Incident = serde_json::from_str(
            r#"{"id":"inc-3","tenantId":"t","botId":"bot","groupKey":"g","source":"manual","title":"x",
            "status":"Open","cycle":1,"openedAt":"2026-11-01T10:00:00Z","updatedAt":"2026-11-01T10:00:00Z",
            "lastSeenAt":"2026-11-01T10:00:00Z"}"#,
        )
        .unwrap();
        assert_eq!(absent.severity, IncidentSeverity::Sev3);
        assert!(serde_json::from_str::<Incident>(r#"{"id":"inc"}"#).is_err());
    }
}
