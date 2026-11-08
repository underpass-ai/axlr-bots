use serde::{Deserialize, Serialize};

/// The approvals an incident's tasks waited for, by how each ended. The
/// product's rule is one approval per incident: shown, not enforced.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct IncidentApprovals {
    #[serde(default)]
    pub approved: u32,
    #[serde(default)]
    pub denied: u32,
    #[serde(default)]
    pub expired: u32,
    #[serde(default)]
    pub waiting: u32,
}

impl IncidentApprovals {
    /// Every approval its tasks waited for.
    pub fn total(&self) -> u32 {
        self.approved + self.denied + self.expired + self.waiting
    }

    /// `1 approved · 1 denied`, the counts that are not zero; `none` when
    /// every one is.
    pub fn summary(&self) -> String {
        let parts: Vec<String> = [
            (self.approved, "approved"),
            (self.denied, "denied"),
            (self.expired, "expired"),
            (self.waiting, "waiting"),
        ]
        .iter()
        .filter(|(n, _)| *n > 0)
        .map(|(n, word)| format!("{n} {word}"))
        .collect();
        if parts.is_empty() {
            "none".into()
        } else {
            parts.join(" · ")
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn says_the_counts_that_are_not_zero() {
        assert_eq!(IncidentApprovals::default().summary(), "none");
        let some = IncidentApprovals {
            approved: 1,
            denied: 1,
            ..Default::default()
        };
        assert_eq!(some.summary(), "1 approved · 1 denied");
        assert_eq!(some.total(), 2);
    }
}
