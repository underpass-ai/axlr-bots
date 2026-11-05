use std::cmp::Ordering;

use super::SortColumn;
use crate::domain::{Incident, IncidentSeverity, IncidentStatus};

/// The Incidents table's columns a person can sort by.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum IncidentColumn {
    /// Ascending, sev1 first.
    Severity,
    Status,
    Incident,
    Bot,
    Repeats,
    /// Ascending, the one seen last first.
    LastSeen,
}

impl SortColumn for IncidentColumn {
    type Row = Incident;

    fn label(self) -> &'static str {
        match self {
            Self::Severity => "Severity",
            Self::Status => "Status",
            Self::Incident => "Incident",
            Self::Bot => "Bot",
            Self::Repeats => "Repeats",
            Self::LastSeen => "Last seen",
        }
    }

    fn compare(self, a: &Incident, b: &Incident) -> Ordering {
        match self {
            Self::Severity => severity(a.severity).cmp(&severity(b.severity)),
            Self::Status => rank(a.status).cmp(&rank(b.status)),
            Self::Incident => a.title.to_lowercase().cmp(&b.title.to_lowercase()),
            Self::Bot => a.bot_id.cmp(&b.bot_id),
            Self::Repeats => a.repeats.cmp(&b.repeats),
            Self::LastSeen => b.last_seen_at.cmp(&a.last_seen_at),
        }
    }
}

fn severity(severity: IncidentSeverity) -> u8 {
    match severity {
        IncidentSeverity::Sev1 => 0,
        IncidentSeverity::Sev2 => 1,
        IncidentSeverity::Sev3 => 2,
        IncidentSeverity::Sev4 => 3,
        IncidentSeverity::Unknown => 4,
    }
}

/// Still firing first, closed last.
fn rank(status: IncidentStatus) -> u8 {
    match status {
        IncidentStatus::Open => 0,
        IncidentStatus::Diagnosed => 1,
        IncidentStatus::Escalated => 2,
        IncidentStatus::Resolved => 3,
        IncidentStatus::Unknown => 4,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::application::ControlPlane;
    use crate::application::test_support::{fleet, run};
    use crate::ui::table::{SortDirection, SortSpec};

    fn ids(spec: SortSpec<IncidentColumn>) -> Vec<String> {
        let (cp, _) = fleet();
        let incidents = run(cp.list_incidents(None, None, 50)).unwrap();
        spec.order(&incidents).into_iter().map(|i| i.id.clone()).collect()
    }

    #[test]
    fn sorts_incidents_by_severity_status_and_sight() {
        assert_eq!(
            ids(SortSpec::by(IncidentColumn::LastSeen, SortDirection::Ascending)),
            vec!["inc-latency", "inc-disk", "inc-cert"]
        );
        let by_severity = ids(SortSpec::by(IncidentColumn::Severity, SortDirection::Ascending));
        assert_eq!(by_severity.len(), 3);
        assert_eq!(
            ids(SortSpec::by(IncidentColumn::Repeats, SortDirection::Descending)).len(),
            3
        );
        assert_eq!(
            ids(SortSpec::by(IncidentColumn::Status, SortDirection::Ascending))[0],
            "inc-latency"
        );
        assert_eq!(
            ids(SortSpec::by(IncidentColumn::Incident, SortDirection::Ascending)).len(),
            3
        );
        assert_eq!(
            ids(SortSpec::by(IncidentColumn::Bot, SortDirection::Ascending)).len(),
            3
        );
        let all = [
            IncidentSeverity::Sev1,
            IncidentSeverity::Sev2,
            IncidentSeverity::Sev3,
            IncidentSeverity::Sev4,
            IncidentSeverity::Unknown,
        ];
        assert!(all.windows(2).all(|w| severity(w[0]) < severity(w[1])));
        let statuses = [
            IncidentStatus::Open,
            IncidentStatus::Diagnosed,
            IncidentStatus::Escalated,
            IncidentStatus::Resolved,
            IncidentStatus::Unknown,
        ];
        assert!(statuses.windows(2).all(|w| rank(w[0]) < rank(w[1])));
        for column in [
            IncidentColumn::Severity,
            IncidentColumn::Status,
            IncidentColumn::Incident,
            IncidentColumn::Bot,
            IncidentColumn::Repeats,
            IncidentColumn::LastSeen,
        ] {
            assert!(!column.label().is_empty());
        }
    }
}
