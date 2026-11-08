use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

use super::{
    BotId, Cost, IncidentApprovals, IncidentCurrentTask, IncidentPhase, IncidentSeverity, IncidentStatus, IncidentTask,
    TaskId,
};

/// One grouped fault, as the control plane reports it: what its alerts
/// say, the bot that triages it, the tasks of each phase that worked on
/// it and where it stands.
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
    /// The tasks that worked on it, oldest first.
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub task_ids: Vec<TaskId>,
    /// The same tasks with their bot, phase and cycle; empty from a
    /// control plane older than the fix phase.
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub tasks: Vec<IncidentTask>,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub diagnosis: String,
    /// What the fix task proposed: the change and its rollback, applied
    /// by nobody.
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub proposal: String,
    /// Why it was escalated or resolved.
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub reason: String,
    #[serde(default, skip_serializing_if = "is_zero")]
    pub repeats: u32,
    #[serde(default, skip_serializing_if = "is_zero")]
    pub retries: u32,
    /// The times it went back to Open from a proposal (its apply denied,
    /// expired, failed or cancelled, its verify red) since it was last
    /// reopened; the second leaves it to a person.
    #[serde(default, skip_serializing_if = "is_zero")]
    pub send_backs: u32,
    #[serde(default, rename = "costUsd", skip_serializing_if = "is_free")]
    pub cost_usd: f64,
    /// One of its runs stopped before it said what it cost: `cost_usd`
    /// is at least what it says.
    #[serde(default, skip_serializing_if = "std::ops::Not::not")]
    pub cost_unknown: bool,
    pub opened_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
    pub last_seen_at: DateTime<Utc>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub resolved_at: Option<DateTime<Utc>>,
    /// Its current task waits for a person's approval where the incident
    /// stands: Approve or Deny moves it on.
    #[serde(default, skip_serializing_if = "std::ops::Not::not")]
    pub needs_you: bool,
    /// The task its next move waits on; only when read on its own.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub current_task: Option<IncidentCurrentTask>,
    /// The approvals its tasks waited for; only when read on its own.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub approvals: Option<IncidentApprovals>,
}

fn is_zero(n: &u32) -> bool {
    *n == 0
}

fn is_free(usd: &f64) -> bool {
    *usd == 0.0
}

impl Incident {
    /// What its runs cost: a floor when one of them did not tell.
    pub fn cost(&self) -> Cost {
        Cost::new(self.cost_usd, u32::from(self.cost_unknown))
    }

    /// The phase a task of the incident worked on, when the control plane
    /// said.
    pub fn phase_of(&self, task: &TaskId) -> Option<IncidentPhase> {
        self.tasks.iter().find(|t| &t.task_id == task).map(|t| t.phase)
    }

    /// The task a person decides now, with the call it waits on: only
    /// while the incident needs you.
    pub fn decision(&self) -> Option<(&TaskId, &str)> {
        if !self.needs_you {
            return None;
        }
        let current = self.current_task.as_ref()?;
        let call = current.waiting_call().map_or("", |w| w.call_id.as_str());
        Some((&current.task_id, call))
    }

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
    fn reads_a_proposal_and_the_phase_of_each_task() {
        let incident: Incident = serde_json::from_str(
            r#"{"id":"inc-three","tenantId":"t","botId":"triage","groupKey":"g","title":"Drift in shop",
            "status":"Proposed","cycle":1,"taskIds":["task-1","task-2"],
            "tasks":[{"taskId":"task-1","botId":"triage","phase":"triage","cycle":1},
                     {"taskId":"task-2","botId":"fix","phase":"fix","cycle":1}],
            "diagnosis":"replicas 1, desired 3","proposal":"PROPOSAL: kubectl apply -f desired/web.yaml",
            "openedAt":"2026-11-01T10:00:00Z","updatedAt":"2026-11-01T10:00:00Z","lastSeenAt":"2026-11-01T10:00:00Z"}"#,
        )
        .unwrap();
        assert_eq!(incident.status, IncidentStatus::Proposed);
        assert!(incident.proposal.starts_with("PROPOSAL"));
        assert_eq!(incident.tasks[1].bot_id.as_str(), "fix");
        let fix = TaskId::new("task-2").unwrap();
        assert_eq!(incident.phase_of(&fix), Some(IncidentPhase::Fix));
        assert_eq!(incident.phase_of(&TaskId::new("task-9").unwrap()), None);
        let again: Incident = serde_json::from_str(&serde_json::to_string(&incident).unwrap()).unwrap();
        assert_eq!(again, incident);
    }

    #[test]
    fn reads_what_it_waits_for_and_offers_the_decision_only_when_it_needs_you() {
        let raw = r#"{"id":"inc-4","tenantId":"t","botId":"triage","groupKey":"g","title":"Drift in shop",
            "status":"Proposed","cycle":1,"proposal":"PROPOSAL: apply","sendBacks":1,"needsYou":true,
            "currentTask":{"taskId":"task-9","botId":"fix","phase":"apply","status":"WaitingApproval",
              "approval":{"callId":"c1","tool":"apply","since":"2026-11-08T10:00:00Z","deadline":"2026-11-08T10:02:00Z","onExpire":"deny"}},
            "approvals":{"approved":0,"denied":1,"expired":0,"waiting":1},
            "openedAt":"2026-11-08T10:00:00Z","updatedAt":"2026-11-08T10:00:00Z","lastSeenAt":"2026-11-08T10:00:00Z"}"#;
        let mut incident: Incident = serde_json::from_str(raw).unwrap();
        assert_eq!(incident.send_backs, 1);
        assert_eq!(incident.approvals.unwrap().summary(), "1 denied · 1 waiting");
        let (task, call) = incident.decision().unwrap();
        assert_eq!((task.as_str(), call), ("task-9", "c1"));
        assert_eq!(incident.current_task.as_ref().unwrap().phase, IncidentPhase::Apply);
        incident.needs_you = false;
        assert!(incident.decision().is_none());
        assert!(!serde_json::to_string(&incident).unwrap().contains("needsYou"));
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
        assert!(!incident.cost().is_floor());
        let mut unknown = incident.clone();
        unknown.cost_unknown = true;
        assert!(unknown.cost().is_floor());
        assert!(
            serde_json::to_string(&unknown)
                .unwrap()
                .contains("\"costUnknown\":true")
        );
        assert!(!serde_json::to_string(&incident).unwrap().contains("costUnknown"));
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
