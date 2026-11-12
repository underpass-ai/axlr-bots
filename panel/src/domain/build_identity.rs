use serde::Deserialize;

/// The source a control plane binary was built from: its commit, the
/// commit's time and whether the tree had changes.
#[derive(Debug, Clone, Default, PartialEq, Eq, Deserialize)]
pub struct BuildIdentity {
    #[serde(default)]
    pub version: String,
    #[serde(default)]
    pub revision: String,
    #[serde(default)]
    pub time: String,
    #[serde(default)]
    pub modified: bool,
}

impl BuildIdentity {
    /// The commit, short, with its time and changes, for a message.
    pub fn describe(&self) -> String {
        if self.revision.is_empty() {
            return "a build without a commit".into();
        }
        let mut text: String = self.revision.chars().take(12).collect();
        if !self.time.is_empty() {
            text.push_str(&format!(" ({})", self.time));
        }
        if self.modified {
            text.push_str(" with changes");
        }
        text
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn describes_the_short_commit_its_time_and_changes() {
        let build = BuildIdentity {
            revision: "a136d7b1ab8799d047e796ee80f0ce5f3e97d158".into(),
            time: "2026-11-12T12:56:26Z".into(),
            modified: true,
            ..Default::default()
        };
        assert_eq!(build.describe(), "a136d7b1ab87 (2026-11-12T12:56:26Z) with changes");
        assert_eq!(BuildIdentity::default().describe(), "a build without a commit");
    }
}
