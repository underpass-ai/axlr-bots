use super::{NewBotDefaults, PanelConfig, PanelError, Theme};

/// Everything the panel keeps for itself between runs, in sections as
/// the settings file names them: `controlPlane.*` (where the operator
/// API is, absent until a Connect succeeded), `look.*` and `newBot.*`.
/// A bot's own configuration is never here: it lives in its definition.
#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub struct PanelSettings {
    pub control_plane: Option<PanelConfig>,
    pub theme: Theme,
    pub new_bot: NewBotDefaults,
}

impl PanelSettings {
    /// The settings as the file holds them: flat keys, dotted by section,
    /// every key present so a reader sees them all.
    pub fn to_json(&self) -> String {
        let document = super::SettingsDocument::from(self);
        let mut text = serde_json::to_string_pretty(&document).expect("settings serialize");
        text.push('\n');
        text
    }

    /// Settings read from the file's text: an unknown key, a wrong type
    /// or broken JSON is a settings error that says where.
    pub fn from_json(text: &str) -> Result<Self, PanelError> {
        let document: super::SettingsDocument = serde_json::from_str(text)
            .map_err(|e| PanelError::Config(format!("settings line {}, column {}: {e}", e.line(), e.column())))?;
        let settings = Self::try_from(document)?;
        if let Some(control_plane) = &settings.control_plane {
            control_plane.validate()?;
        }
        Ok(settings)
    }

    /// The control plane section, or the install's defaults before a
    /// first Connect.
    pub fn control_plane_or(&self, defaults: &PanelConfig) -> PanelConfig {
        self.control_plane.clone().unwrap_or_else(|| defaults.clone())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn settings() -> PanelSettings {
        PanelSettings {
            control_plane: Some(PanelConfig {
                api_url: "https://127.0.0.1:7879".into(),
                ca_path: "/s/ca.crt".into(),
                token_file: "/s/api.token".into(),
                actor: None,
            }),
            theme: Theme::Dark,
            new_bot: NewBotDefaults {
                model: "codex/gpt-6-luna".into(),
                models: vec!["codex/gpt-6-luna".into(), "anthropic/claude-haiku-5.5".into()],
            },
        }
    }

    #[test]
    fn round_trips_through_flat_dotted_keys() {
        let text = settings().to_json();
        assert!(text.contains("\"controlPlane.apiUrl\": \"https://127.0.0.1:7879\""));
        assert!(text.contains("\"look.theme\": \"dark\""));
        assert!(text.contains("\"newBot.defaultModel\": \"codex/gpt-6-luna\""));
        assert!(text.ends_with("}\n"));
        assert_eq!(PanelSettings::from_json(&text).unwrap(), settings());
        let first_run = PanelSettings::default().to_json();
        assert!(first_run.contains("\"controlPlane.apiUrl\": null"));
        assert_eq!(PanelSettings::from_json(&first_run).unwrap().control_plane, None);
        assert_eq!(PanelSettings::from_json("{}").unwrap(), PanelSettings::default());
    }

    #[test]
    fn a_bad_file_says_where_and_why() {
        let broken = PanelSettings::from_json("{\n  \"look.theme\": \"blue\"\n}").unwrap_err();
        assert!(
            matches!(&broken, PanelError::Config(m) if m.contains("line 2") && m.contains("blue")),
            "{broken:?}"
        );
        let unknown = PanelSettings::from_json("{\"bots.model\": \"x\"}").unwrap_err();
        assert!(
            matches!(&unknown, PanelError::Config(m) if m.contains("bots.model")),
            "{unknown:?}"
        );
        let half = PanelSettings::from_json("{\"controlPlane.apiUrl\": \"https://h\"}").unwrap_err();
        assert!(
            matches!(&half, PanelError::Config(m) if m.contains("controlPlane")),
            "{half:?}"
        );
        let http = PanelSettings::from_json(
            "{\"controlPlane.apiUrl\": \"http://h\", \"controlPlane.caFile\": \"/c\", \"controlPlane.tokenFile\": \"/t\"}",
        )
        .unwrap_err();
        assert!(matches!(&http, PanelError::Config(m) if m.contains("https://")));
        let lone_actor = PanelSettings::from_json("{\"controlPlane.actor\": \"alice\"}").unwrap_err();
        assert!(matches!(&lone_actor, PanelError::Config(m) if m.contains("controlPlane.actor")));
        let spaced = PanelSettings::from_json(
            "{\"controlPlane.apiUrl\": \"https://h\", \"controlPlane.caFile\": \"/c\", \"controlPlane.tokenFile\": \"/t\", \"controlPlane.actor\": \"two words\"}",
        )
        .unwrap_err();
        assert!(matches!(&spaced, PanelError::Config(m) if m.contains("actor")));
        let trailing = PanelSettings::from_json("{\"look.theme\": \"dark\",}").unwrap_err();
        assert!(matches!(&trailing, PanelError::Config(m) if m.contains("line 1")));
    }

    #[test]
    fn keeps_who_the_panel_decides_as() {
        let mut with = settings();
        with.control_plane.as_mut().unwrap().actor = Some("alice".into());
        let text = with.to_json();
        assert!(text.contains("\"controlPlane.actor\": \"alice\""));
        assert_eq!(PanelSettings::from_json(&text).unwrap(), with);
        let blank = text.replace("\"alice\"", "\" \"");
        assert_eq!(
            PanelSettings::from_json(&blank).unwrap(),
            settings(),
            "a blank actor is none"
        );
        assert!(settings().to_json().contains("\"controlPlane.actor\": null"));
    }

    #[test]
    fn falls_back_to_the_install_before_a_first_connect() {
        let defaults = settings().control_plane.unwrap();
        assert_eq!(PanelSettings::default().control_plane_or(&defaults), defaults);
    }
}
