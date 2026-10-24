use serde::{Deserialize, Serialize};

/// Whether an execution ran the task or carried a person's decision.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default, Serialize, Deserialize)]
#[serde(try_from = "String", into = "String")]
pub enum ExecutionKind {
    #[default]
    Run,
    Decide,
}

impl ExecutionKind {
    pub fn name(self) -> &'static str {
        match self {
            Self::Run => "run",
            Self::Decide => "decide",
        }
    }
}

impl TryFrom<String> for ExecutionKind {
    type Error = super::DomainError;

    fn try_from(raw: String) -> Result<Self, Self::Error> {
        match raw.as_str() {
            "" | "run" => Ok(Self::Run),
            "decide" => Ok(Self::Decide),
            _ => Err(super::DomainError::new(
                "execution kind",
                "must be run or decide",
            )),
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
        assert!(serde_json::from_str::<ExecutionKind>("\"other\"").is_err());
        assert_eq!(
            serde_json::to_string(&ExecutionKind::Decide).unwrap(),
            "\"decide\""
        );
    }
}
