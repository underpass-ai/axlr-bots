use serde::{Deserialize, Serialize};

/// How an execution ended, or that it still runs.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub enum ExecutionOutcome {
    Running,
    Completed,
    Failed,
    DecisionRequired,
    Cancelled,
    /// A value this panel does not know: the control plane is newer. Read,
    /// shown as unknown, never refused.
    #[serde(other)]
    Unknown,
}

impl ExecutionOutcome {
    pub fn name(self) -> &'static str {
        match self {
            Self::Running => "Running",
            Self::Completed => "Completed",
            Self::Failed => "Failed",
            Self::DecisionRequired => "DecisionRequired",
            Self::Cancelled => "Cancelled",
            Self::Unknown => "Unknown",
        }
    }

    /// The short word shown in a pill.
    pub fn label(self) -> &'static str {
        match self {
            Self::DecisionRequired => "Needs OK",
            other => other.name(),
        }
    }

    pub fn is_final(self) -> bool {
        self != Self::Running
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn labels_and_finality() {
        assert_eq!(ExecutionOutcome::DecisionRequired.label(), "Needs OK");
        assert_eq!(ExecutionOutcome::Failed.label(), "Failed");
        assert!(!ExecutionOutcome::Running.is_final());
        assert!(ExecutionOutcome::Cancelled.is_final());
        let parsed: ExecutionOutcome = serde_json::from_str("\"DecisionRequired\"").unwrap();
        assert_eq!(parsed.name(), "DecisionRequired");
    }
}
