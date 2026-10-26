use serde::{Deserialize, Serialize};

/// Where a task stands, from accepted to its end.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub enum TaskStatus {
    Pending,
    Running,
    WaitingApproval,
    Completed,
    Failed,
    /// Nobody decided the call it waited on in time; the call never ran.
    Expired,
    Cancelled,
}

impl TaskStatus {
    pub const ALL: [TaskStatus; 7] = [
        Self::Pending,
        Self::Running,
        Self::WaitingApproval,
        Self::Completed,
        Self::Failed,
        Self::Expired,
        Self::Cancelled,
    ];

    /// The word the control plane uses.
    pub fn name(self) -> &'static str {
        match self {
            Self::Pending => "Pending",
            Self::Running => "Running",
            Self::WaitingApproval => "WaitingApproval",
            Self::Completed => "Completed",
            Self::Failed => "Failed",
            Self::Expired => "Expired",
            Self::Cancelled => "Cancelled",
        }
    }

    /// The short word shown in a pill.
    pub fn label(self) -> &'static str {
        match self {
            Self::WaitingApproval => "Waiting",
            other => other.name(),
        }
    }

    pub fn parse(raw: &str) -> Option<Self> {
        Self::ALL.into_iter().find(|s| s.name() == raw)
    }

    /// A Completed, Failed, Expired or Cancelled task has ended.
    pub fn is_terminal(self) -> bool {
        matches!(self, Self::Completed | Self::Failed | Self::Expired | Self::Cancelled)
    }

    /// A task that has not ended can be cancelled.
    pub fn can_cancel(self) -> bool {
        !self.is_terminal()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn names_labels_and_ends() {
        for status in TaskStatus::ALL {
            assert_eq!(TaskStatus::parse(status.name()), Some(status));
            assert_eq!(
                serde_json::to_string(&status).unwrap(),
                format!("\"{}\"", status.name())
            );
        }
        assert_eq!(TaskStatus::parse("Sleeping"), None);
        assert_eq!(TaskStatus::WaitingApproval.label(), "Waiting");
        assert_eq!(TaskStatus::Running.label(), "Running");
        assert!(TaskStatus::Failed.is_terminal());
        assert!(TaskStatus::Expired.is_terminal());
        assert!(!TaskStatus::Expired.can_cancel());
        assert_eq!(TaskStatus::parse("Expired"), Some(TaskStatus::Expired));
        assert!(TaskStatus::WaitingApproval.can_cancel());
        assert!(!TaskStatus::Completed.can_cancel());
    }
}
