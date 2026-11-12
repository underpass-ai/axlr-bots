use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

/// The change a bot holds staged, as the bot answers it: which version
/// of its definition waits to be applied and since when. The change
/// itself stays on the control plane.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct StagedDefinition {
    #[serde(default)]
    pub version: String,
    pub staged_at: DateTime<Utc>,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn reads_the_bot_answer() {
        let staged: StagedDefinition =
            serde_json::from_str(r#"{"version":"1.3.0","stagedAt":"2026-11-12T21:00:00Z"}"#).unwrap();
        assert_eq!(staged.version, "1.3.0");
        assert_eq!(staged.staged_at.to_rfc3339(), "2026-11-12T21:00:00+00:00");
    }
}
