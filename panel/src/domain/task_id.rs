use std::fmt;

use serde::{Deserialize, Serialize};

use super::DomainError;
use super::identifier::validate_identifier;

/// The identifier of a task in the control plane: lowercase letters,
/// digits, '.', '_' and '-', at most 64 characters.
#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord, Hash, Serialize, Deserialize)]
#[serde(try_from = "String", into = "String")]
pub struct TaskId(String);

impl TaskId {
    pub fn new(raw: &str) -> Result<Self, DomainError> {
        validate_identifier("task id", raw).map(Self)
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

impl TryFrom<String> for TaskId {
    type Error = DomainError;

    fn try_from(raw: String) -> Result<Self, Self::Error> {
        Self::new(&raw)
    }
}

impl From<TaskId> for String {
    fn from(id: TaskId) -> Self {
        id.0
    }
}

impl fmt::Display for TaskId {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&self.0)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn validates_and_round_trips_through_json() {
        let id = TaskId::new(" sample-1 ").unwrap();
        assert_eq!(id.as_str(), "sample-1");
        assert_eq!(id.to_string(), "sample-1");
        let json = serde_json::to_string(&id).unwrap();
        assert_eq!(json, "\"sample-1\"");
        assert_eq!(serde_json::from_str::<TaskId>(&json).unwrap(), id);
        assert!(serde_json::from_str::<TaskId>("\"Not Valid\"").is_err());
        assert!(TaskId::new("").is_err());
    }
}
