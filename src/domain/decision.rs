use serde::Serialize;

/// What a person says about the call a task stopped on.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize)]
#[serde(rename_all = "lowercase")]
pub enum Decision {
    Approve,
    Deny,
}

impl Decision {
    pub fn name(self) -> &'static str {
        match self {
            Self::Approve => "approve",
            Self::Deny => "deny",
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn serializes_as_the_control_plane_word() {
        assert_eq!(
            serde_json::to_string(&Decision::Approve).unwrap(),
            "\"approve\""
        );
        assert_eq!(Decision::Deny.name(), "deny");
    }
}
