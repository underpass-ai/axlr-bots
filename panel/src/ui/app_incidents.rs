//! Incidents: the list, one incident's page, closing one by hand, and
//! saving a task's artifact.

use std::path::PathBuf;

use super::{App, Banner, Effect, Screen};
use crate::application::PanelError;
use crate::domain::{ExecutionId, Incident};
use crate::ui::incident_watch::{PostmortemForm, ResolveForm};

impl App {
    pub(super) fn incidents_loaded(&mut self, result: Result<Vec<Incident>, PanelError>) -> Vec<Effect> {
        self.incidents.list_loading = false;
        match result {
            Ok(list) => {
                self.incidents.list = Some(list);
                self.drop_gone_picks();
            }
            Err(error) => self.banner = Some(Banner::error(&error, self.now)),
        }
        Vec::new()
    }

    /// An incident's read: kept only while its page is on screen.
    pub(super) fn incident_loaded(&mut self, id: String, result: Result<Incident, PanelError>) -> Vec<Effect> {
        self.incidents.detail_loading = false;
        if self.screen != Screen::Incident(id) {
            return Vec::new();
        }
        match result {
            Ok(incident) => self.incidents.detail = Some(incident),
            Err(error) => self.banner = Some(Banner::error(&error, self.now)),
        }
        Vec::new()
    }

    pub(super) fn resolve_open(&mut self) -> Vec<Effect> {
        self.incidents.resolve = ResolveForm::opened();
        Vec::new()
    }

    /// Sends the Resolve of the incident on screen, once at a time.
    pub(super) fn resolve_send(&mut self) -> Vec<Effect> {
        let Screen::Incident(id) = &self.screen else {
            return Vec::new();
        };
        if self.incidents.resolve.busy {
            return Vec::new();
        }
        self.incidents.resolve.busy = true;
        vec![Effect::ResolveIncident {
            id: id.clone(),
            reason: self.incidents.resolve.reason.clone(),
        }]
    }

    /// The Resolve's answer: the incident closed, or the reason with the
    /// form still open and the words kept.
    pub(super) fn incident_resolved(&mut self, result: Result<Incident, PanelError>) -> Vec<Effect> {
        self.incidents.resolve.busy = false;
        match result {
            Ok(incident) => {
                self.banner = Some(Banner::good(format!("{} resolved", incident.id), "", self.now));
                self.incidents.resolve = ResolveForm::default();
                if let Some(listed) = self.incidents.list.iter_mut().flatten().find(|i| i.id == incident.id) {
                    *listed = incident.clone();
                }
                if self.incidents.detail.as_ref().is_some_and(|d| d.id == incident.id) {
                    self.incidents.detail = Some(incident);
                }
            }
            Err(error) => self.banner = Some(Banner::error(&error, self.now)),
        }
        self.reload_screen()
    }

    /// The task whose text is on screen: what an answer says was read.
    fn author_on_screen(&self, id: &str) -> Option<String> {
        let incident = self.incidents.detail.as_ref().filter(|i| i.id == id)?;
        let author = incident.reviewable_postmortem()?.author_task_id.as_ref()?;
        Some(author.to_string())
    }

    /// Approves the postmortem of the incident on screen, once at a time.
    pub(super) fn postmortem_approve(&mut self) -> Vec<Effect> {
        let Screen::Incident(id) = &self.screen else {
            return Vec::new();
        };
        if self.incidents.review.busy {
            return Vec::new();
        }
        let waits = self.incidents.detail.as_ref().is_some_and(Incident::postmortem_waits);
        let Some(author) = self.author_on_screen(id).filter(|_| waits) else {
            return Vec::new();
        };
        let id = id.clone();
        self.incidents.review.busy = true;
        vec![Effect::ApprovePostmortem { id, author }]
    }

    /// Sends the postmortem back with the note, once at a time.
    pub(super) fn postmortem_back_send(&mut self) -> Vec<Effect> {
        let Screen::Incident(id) = &self.screen else {
            return Vec::new();
        };
        if self.incidents.review.busy {
            return Vec::new();
        }
        let author = self.author_on_screen(id);
        let id = id.clone();
        self.incidents.review.busy = true;
        vec![Effect::SendBackPostmortem {
            id,
            note: self.incidents.review.note.clone(),
            author,
        }]
    }

    /// The answer: the incident as it stands now, or the reason with the
    /// note kept.
    pub(super) fn postmortem_reviewed(&mut self, result: Result<Incident, PanelError>) -> Vec<Effect> {
        self.incidents.review.busy = false;
        match result {
            Ok(incident) => {
                let state = incident.postmortem.as_ref().map_or("", |p| p.state.label());
                self.banner = Some(Banner::good(format!("Postmortem {state}"), "", self.now));
                self.incidents.review = PostmortemForm::default();
                if let Some(listed) = self.incidents.list.iter_mut().flatten().find(|i| i.id == incident.id) {
                    *listed = incident.clone();
                }
                if self.incidents.detail.as_ref().is_some_and(|d| d.id == incident.id) {
                    self.incidents.detail = Some(incident);
                }
            }
            Err(error) => self.banner = Some(Banner::error(&error, self.now)),
        }
        self.reload_screen()
    }

    pub(super) fn download(&mut self, execution: ExecutionId, name: String) -> Vec<Effect> {
        if self.downloading {
            return Vec::new();
        }
        self.downloading = true;
        vec![Effect::Download(execution, name)]
    }

    pub(super) fn downloaded(&mut self, result: Result<Option<PathBuf>, PanelError>) -> Vec<Effect> {
        self.downloading = false;
        match result {
            Ok(Some(path)) => self.banner = Some(Banner::good("Saved", path.display().to_string(), self.now)),
            Ok(None) => {}
            Err(error) => self.banner = Some(Banner::error(&error, self.now)),
        }
        Vec::new()
    }
}
