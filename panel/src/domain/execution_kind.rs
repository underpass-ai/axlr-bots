use serde::{Deserialize, Serialize};

/// Whether an execution ran the task or carried a person's decision.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default, Serialize, Deserialize)]
#[serde(try_from = "String", into = "String")]
pub enum ExecutionKind {
    #[default]
    Run,
    Decide,
    /// A value this panel does not know: the control plane is newer. Read,
    /// shown as unknown, never refused.
    Unknown,
}

impl ExecutionKind {
    pub fn name(self) -> &'static str {
        match self {
            Self::Run => "run",
            Self::Decide => "decide",
            Self::Unknown => "unknown",
        }
    }
}

impl TryFrom<String> for ExecutionKind {
    type Error = super::DomainError;

    fn try_from(raw: String) -> Result<Self, Self::Error> {
        match raw.as_str() {
            "" | "run" => Ok(Self::Run),
            "decide" => Ok(Self::Decide),
            _ => Ok(Self::Unknown),
        }
    }
}

impl From<ExecutionKind> for String {
    fn from(kind: ExecutionKind) -> Self {
        kind.name().to_string()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn empty_is_a_run() {
        assert_eq!(
            serde_json::from_str::<ExecutionKind>("\"\"").unwrap(),
            ExecutionKind::Run
        );
        assert_eq!(
            serde_json::from_str::<ExecutionKind>("\"decide\"").unwrap(),
            ExecutionKind::Decide
        );
        assert_eq!(
            serde_json::from_str::<ExecutionKind>("\"other\"").unwrap(),
            ExecutionKind::Unknown
        );
        assert_eq!(serde_json::to_string(&ExecutionKind::Decide).unwrap(), "\"decide\"");
    }
}
