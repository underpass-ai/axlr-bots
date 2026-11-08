use crate::domain::Incident;

/// The panel's watch over the incidents: the list as last read, the one
/// on screen, the reads on their way, and the Resolve form.
#[derive(Debug, Clone, Default, PartialEq)]
pub struct IncidentWatch {
    /// The incidents, the last seen first; `None` before the first read.
    pub list: Option<Vec<Incident>>,
    pub list_loading: bool,
    /// The incident on its page.
    pub detail: Option<Incident>,
    pub detail_loading: bool,
    pub resolve: ResolveForm,
    /// A mark of the incident's diagnosis is on its way.
    pub marking: bool,
}

/// Closing an incident by hand: a reason, optional, then Resolve.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct ResolveForm {
    pub open: bool,
    pub reason: String,
    pub busy: bool,
}

impl ResolveForm {
    pub fn opened() -> Self {
        Self {
            open: true,
            ..Self::default()
        }
    }
}
