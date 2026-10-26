use std::fmt;

use serde::{Deserialize, Serialize};

use super::DomainError;
use super::identifier::validate_identifier;

/// The identifier of a bot in the control plane: lowercase letters,
/// digits, '.', '_' and '-', at most 64 characters.
#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord, Hash, Serialize, Deserialize)]
#[serde(try_from = "String", into = "String")]
pub struct BotId(String);

impl BotId {
    pub fn new(raw: &str) -> Result<Self, DomainError> {
        validate_identifier("bot id", raw).map(Self)
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

impl TryFrom<String> for BotId {
    type Error = DomainError;

    fn try_from(raw: String) -> Result<Self, Self::Error> {
        Self::new(&raw)
    }
}

impl From<BotId> for String {
    fn from(id: BotId) -> Self {
        id.0
    }
}

impl fmt::Display for BotId {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&self.0)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn validates_and_round_trips_through_json() {
        let id = BotId::new(" sample-1 ").unwrap();
        assert_eq!(id.as_str(), "sample-1");
        assert_eq!(id.to_string(), "sample-1");
        let json = serde_json::to_string(&id).unwrap();
        assert_eq!(json, "\"sample-1\"");
        assert_eq!(serde_json::from_str::<BotId>(&json).unwrap(), id);
        assert!(serde_json::from_str::<BotId>("\"Not Valid\"").is_err());
        assert!(BotId::new("").is_err());
    }
}
