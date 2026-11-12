use super::{Bot, Version};

/// A newer version of a bot's definition, staged on the control plane
/// and ready to apply: from the version the bot has (none when its
/// definition names none) to the staged one. A version the panel cannot
/// read on either side says nothing, so no update shows.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BotUpdate {
    pub from: Option<Version>,
    pub to: Version,
}

impl BotUpdate {
    /// The update the bot can take, when its staged version is newer
    /// than its own, or it has none.
    pub fn of(bot: &Bot) -> Option<Self> {
        let to = Version::parse(&bot.staged.as_ref()?.version)?;
        if bot.version.trim().is_empty() {
            return Some(Self { from: None, to });
        }
        let from = Version::parse(&bot.version)?;
        (to > from).then_some(Self { from: Some(from), to })
    }

    /// What the mark says on hover: `v1.2.0 → v1.3.0 ready`.
    pub fn tip(&self) -> String {
        match &self.from {
            Some(from) => format!("{} → {} ready", from.label(), self.to.label()),
            None => format!("{} ready", self.to.label()),
        }
    }

    /// The words on the button that applies it: `Update to v1.3.0`.
    pub fn act(&self) -> String {
        format!("Update to {}", self.to.label())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::domain::StagedDefinition;

    fn bot(version: &str, staged: Option<&str>) -> Bot {
        let mut bot: Bot = serde_json::from_str(
            r#"{"id":"bot-a","name":"bot-a","mission":"Do it.","status":"Enabled",
            "createdAt":"2026-10-24T22:30:00Z","updatedAt":"2026-10-24T22:31:00Z"}"#,
        )
        .unwrap();
        bot.version = version.into();
        bot.staged = staged.map(|v| StagedDefinition {
            version: v.into(),
            staged_at: "2026-11-12T21:00:00Z".parse().unwrap(),
        });
        bot
    }

    #[test]
    fn shows_only_a_newer_staged_version() {
        let table = [
            ("1.2.0", Some("1.3.0"), Some("v1.2.0 → v1.3.0 ready")),
            ("", Some("1.3.0"), Some("v1.3.0 ready")),
            ("v1.2.0", Some("1.2.1"), Some("v1.2.0 → v1.2.1 ready")),
            ("1.3.0-rc.1", Some("1.3.0"), Some("v1.3.0-rc.1 → v1.3.0 ready")),
            ("1.3.0", Some("1.3.0"), None),
            ("1.3.0", Some("1.2.0"), None),
            ("1.3.0", None, None),
            ("", None, None),
            ("nightly", Some("1.3.0"), None),
            ("1.2.0", Some("next"), None),
        ];
        for (version, staged, want) in table {
            let update = BotUpdate::of(&bot(version, staged));
            assert_eq!(
                update.as_ref().map(BotUpdate::tip).as_deref(),
                want,
                "{version} → {staged:?}"
            );
        }
        let update = BotUpdate::of(&bot("1.2.0", Some("1.3.0"))).unwrap();
        assert_eq!(update.act(), "Update to v1.3.0");
    }
}
