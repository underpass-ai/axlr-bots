use std::fmt;

use serde::{Deserialize, Serialize};

use super::DomainError;
use super::identifier::validate_identifier;

/// The identifier of a node in the control plane: lowercase letters,
/// digits, '.', '_' and '-', at most 64 characters.
#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord, Hash, Serialize, Deserialize)]
#[serde(try_from = "String", into = "String")]
pub struct NodeId(String);

impl NodeId {
    pub fn new(raw: &str) -> Result<Self, DomainError> {
        validate_identifier("node id", raw).map(Self)
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

impl TryFrom<String> for NodeId {
    type Error = DomainError;

    fn try_from(raw: String) -> Result<Self, Self::Error> {
        Self::new(&raw)
    }
}

impl From<NodeId> for String {
    fn from(id: NodeId) -> Self {
        id.0
    }
}

impl fmt::Display for NodeId {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&self.0)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn validates_and_round_trips_through_json() {
        let id = NodeId::new(" sample-1 ").unwrap();
        assert_eq!(id.as_str(), "sample-1");
        assert_eq!(id.to_string(), "sample-1");
        let json = serde_json::to_string(&id).unwrap();
        assert_eq!(json, "\"sample-1\"");
        assert_eq!(serde_json::from_str::<NodeId>(&json).unwrap(), id);
        assert!(serde_json::from_str::<NodeId>("\"Not Valid\"").is_err());
        assert!(NodeId::new("").is_err());
    }
}
