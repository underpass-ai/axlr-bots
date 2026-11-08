use std::sync::Arc;

use super::{ControlPlane, PanelError};
use crate::domain::Incident;

/// How many incidents the list reads: the control plane's own default.
pub const INCIDENTS: u32 = 100;

/// The incidents of every tenant, in any status, the last seen first.
pub struct ListIncidents {
    control_plane: Arc<dyn ControlPlane>,
}

impl ListIncidents {
    pub fn new(control_plane: Arc<dyn ControlPlane>) -> Self {
        Self { control_plane }
    }

    pub async fn execute(&self) -> Result<Vec<Incident>, PanelError> {
        self.control_plane.list_incidents(None, None, INCIDENTS).await
    }
}

/// One incident, as the control plane has it now.
pub struct ShowIncident {
    control_plane: Arc<dyn ControlPlane>,
}

impl ShowIncident {
    pub fn new(control_plane: Arc<dyn ControlPlane>) -> Self {
        Self { control_plane }
    }

    pub async fn execute(&self, id: String) -> Result<Incident, PanelError> {
        self.control_plane.get_incident(id).await
    }
}

/// Closes an incident by hand, with the operator's reason; words that are
/// only spaces are no reason.
pub struct ResolveIncident {
    control_plane: Arc<dyn ControlPlane>,
}

impl ResolveIncident {
    pub fn new(control_plane: Arc<dyn ControlPlane>) -> Self {
        Self { control_plane }
    }

    pub async fn execute(&self, id: String, reason: &str) -> Result<Incident, PanelError> {
        let reason = reason.trim();
        let reason = (!reason.is_empty()).then(|| reason.to_string());
        self.control_plane.resolve_incident(id, reason).await
    }
}

/// Approves an incident's postmortem, as who the panel decides as.
pub struct ApprovePostmortem {
    control_plane: Arc<dyn ControlPlane>,
}

impl ApprovePostmortem {
    pub fn new(control_plane: Arc<dyn ControlPlane>) -> Self {
        Self { control_plane }
    }

    pub async fn execute(&self, id: String) -> Result<Incident, PanelError> {
        self.control_plane.approve_postmortem(id).await
    }
}

/// Sends an incident's postmortem back; a rewrite needs words to go on,
/// and the panel says so before asking the control plane.
pub struct SendBackPostmortem {
    control_plane: Arc<dyn ControlPlane>,
}

impl SendBackPostmortem {
    pub fn new(control_plane: Arc<dyn ControlPlane>) -> Self {
        Self { control_plane }
    }

    pub async fn execute(&self, id: String, note: &str) -> Result<Incident, PanelError> {
        let note = note.trim();
        if note.is_empty() {
            return Err(PanelError::Invalid("say what the rewrite should change".into()));
        }
        self.control_plane.send_back_postmortem(id, note.to_string()).await
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::application::test_support::{fleet, run};
    use crate::domain::{IncidentStatus, PostmortemState};

    #[test]
    fn approves_a_postmortem_and_sends_one_back() {
        let (control_plane, _) = fleet();
        assert!(matches!(
            run(SendBackPostmortem::new(control_plane.clone()).execute("inc-cert".into(), "  ")),
            Err(PanelError::Invalid(_))
        ));
        let back = run(SendBackPostmortem::new(control_plane.clone()).execute("inc-cert".into(), " add the owner "))
            .unwrap()
            .postmortem
            .unwrap();
        assert_eq!(
            (back.state, back.note.as_str()),
            (PostmortemState::Writing, "add the owner")
        );
        assert!(matches!(
            run(ApprovePostmortem::new(control_plane.clone()).execute("inc-cert".into())),
            Err(PanelError::State(_))
        ));
        assert!(matches!(
            run(ApprovePostmortem::new(control_plane.clone()).execute("inc-none".into())),
            Err(PanelError::NotFound(_))
        ));
    }

    #[test]
    fn lists_shows_and_resolves_incidents() {
        let (control_plane, _) = fleet();
        let list = run(ListIncidents::new(control_plane.clone()).execute()).unwrap();
        assert_eq!(
            list.iter().map(|i| i.id.as_str()).collect::<Vec<_>>(),
            vec!["inc-latency", "inc-disk", "inc-cert"]
        );
        let open = run(ShowIncident::new(control_plane.clone()).execute("inc-latency".into())).unwrap();
        assert_eq!(open.status, IncidentStatus::Open);
        assert_eq!(open.task_ids.len(), 1);
        let resolved =
            run(ResolveIncident::new(control_plane.clone()).execute("inc-latency".into(), "  Fixed the pool. "))
                .unwrap();
        assert_eq!(resolved.status, IncidentStatus::Resolved);
        assert_eq!(resolved.reason, "Fixed the pool.");
        assert!(resolved.resolved_at.is_some());
        assert!(matches!(
            run(ResolveIncident::new(control_plane.clone()).execute("inc-latency".into(), "")),
            Err(PanelError::State(_))
        ));
        let disk = run(ResolveIncident::new(control_plane.clone()).execute("inc-disk".into(), "   ")).unwrap();
        assert!(disk.reason.is_empty());
        assert!(matches!(
            run(ShowIncident::new(control_plane.clone()).execute("inc-none".into())),
            Err(PanelError::NotFound(_))
        ));
        control_plane.fail_with(Some(PanelError::Server("boom".into())));
        assert!(run(ListIncidents::new(control_plane).execute()).is_err());
    }
}
