use serde::Deserialize;

use super::{DomainError, validate_topic};

/// One button on a team's page that publishes a set event in one press:
/// a notice, a halt, a stop. The operator writes them in the panel's
/// `shortcuts.toml`; each press is a new event with its own key.
#[derive(Debug, Clone, PartialEq, Eq, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct Shortcut {
    pub team: String,
    pub label: String,
    pub name: String,
    pub message: String,
    #[serde(default)]
    pub data: Option<serde_json::Value>,
}

/// The file: `[[shortcut]]` tables.
#[derive(Debug, Default, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct ShortcutFile {
    #[serde(default, rename = "shortcut")]
    pub shortcuts: Vec<Shortcut>,
}

impl Shortcut {
    /// Checked as the control plane would: one name, not a pattern, not
    /// the control plane's own kinds, a message, and facts as an object.
    pub fn validate(&self) -> Result<(), DomainError> {
        if self.team.trim().is_empty() {
            return Err(DomainError::new("team", "must not be empty"));
        }
        if self.label.trim().is_empty() || self.label.chars().count() > 24 {
            return Err(DomainError::new("label", "must be 1 to 24 characters"));
        }
        validate_topic(&self.name)?;
        if self.name.contains('*') {
            return Err(DomainError::new("name", "is one name, not a pattern"));
        }
        if self.name.starts_with("error.") || self.name.starts_with("done.") {
            return Err(DomainError::new(
                "name",
                "error.* and done.* are the control plane's own",
            ));
        }
        if self.message.trim().is_empty() {
            return Err(DomainError::new("message", "must not be empty"));
        }
        if matches!(&self.data, Some(d) if !d.is_object()) {
            return Err(DomainError::new("data", "must be a table of facts"));
        }
        Ok(())
    }

    /// Whether pressing it stops bots: the button is drawn as a danger.
    pub fn stops(&self) -> bool {
        self.name.starts_with("stop.") || self.name.starts_with("danger.")
    }
}

impl ShortcutFile {
    /// The shortcuts of a TOML text, every one checked; the first bad one
    /// names its place.
    pub fn parse(text: &str) -> Result<Vec<Shortcut>, DomainError> {
        let file: ShortcutFile =
            toml::from_str(text).map_err(|e| DomainError::new("shortcuts", e.message().to_string()))?;
        for (i, shortcut) in file.shortcuts.iter().enumerate() {
            shortcut
                .validate()
                .map_err(|e| DomainError::new("shortcuts", format!("#{}: {e}", i + 1)))?;
        }
        Ok(file.shortcuts)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    const BUTTONS: &str = r#"
[[shortcut]]
team = "ops"
label = "Notice"
name = "info.notice"
message = "A deploy is starting."
data = { service = "service-1" }

[[shortcut]]
team = "ops"
label = "Stop all"
name = "stop.all"
message = "Stop."
"#;

    #[test]
    fn reads_an_operators_buttons() {
        let list = ShortcutFile::parse(BUTTONS).unwrap();
        assert_eq!(list.len(), 2);
        assert_eq!(list[0].data, Some(serde_json::json!({"service": "service-1"})));
        assert!(!list[0].stops() && list[1].stops());
        assert_eq!(list[1].data, None);
        assert!(ShortcutFile::parse("").unwrap().is_empty());
    }

    #[test]
    fn refuses_what_the_control_plane_would() {
        let one = |extra: &str| {
            ShortcutFile::parse(&format!(
                "[[shortcut]]\nteam = \"t\"\nlabel = \"L\"\nmessage = \"m\"\n{extra}"
            ))
        };
        assert!(one("name = \"info.x\"").is_ok());
        assert!(one("name = \"info.*\"").is_err());
        assert!(one("name = \"error.bot\"").is_err());
        assert!(one("name = \"Bad\"").is_err());
        assert!(one("name = \"info.x\"\ndata = 3").is_err());
        assert!(one("name = \"info.x\"\ncolour = \"red\"").is_err());
        let err = ShortcutFile::parse("[[shortcut]]\nteam = \"t\"\nlabel = \"\"\nname = \"info.x\"\nmessage = \"m\"")
            .unwrap_err();
        assert!(err.reason.starts_with("#1: label"));
    }
}
