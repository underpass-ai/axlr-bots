use std::collections::BTreeMap;

use chrono::{DateTime, Utc};
use serde::Deserialize;

use super::{NodeId, Secret};

/// A one-time token a new host enrolls with. The control plane shows it
/// once; the panel never logs it.
#[derive(Debug, Clone, PartialEq, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct EnrollmentToken {
    #[serde(deserialize_with = "secret")]
    pub token: Secret,
    pub id: String,
    pub node_id: NodeId,
    #[serde(default)]
    pub labels: BTreeMap<String, String>,
    pub expires_at: DateTime<Utc>,
}

fn secret<'de, D: serde::Deserializer<'de>>(deserializer: D) -> Result<Secret, D::Error> {
    String::deserialize(deserializer).map(Secret::new)
}

impl EnrollmentToken {
    /// The command to run on the new host.
    pub fn enroll_command(&self, control_url: &str) -> String {
        format!(
            "axlr-node enroll --control {control_url} --token '{}' --state ~/.local/state/axlr-node",
            self.token.expose()
        )
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn keeps_the_token_secret() {
        let token: EnrollmentToken = serde_json::from_str(
            r#"{"token":"axlr1.abc","id":"enr-1","nodeId":"web-2","labels":{"zone":"eu"},"expiresAt":"2026-10-25T01:00:00Z"}"#,
        )
        .unwrap();
        assert_eq!(token.token.expose(), "axlr1.abc");
        assert!(!format!("{token:?}").contains("axlr1.abc"));
        assert_eq!(
            token.enroll_command("https://cp:7878"),
            "axlr-node enroll --control https://cp:7878 --token 'axlr1.abc' --state ~/.local/state/axlr-node"
        );
    }
}
