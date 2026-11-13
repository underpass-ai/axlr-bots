use std::path::PathBuf;

use crate::application::{NewBotDefaults, PanelConfig, PanelSettings, Theme};

/// The settings screen's fields, as typed.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct SettingsForm {
    pub api_url: String,
    pub ca_path: String,
    pub token_file: String,
    /// Who the panel decides as (controlPlane.actor); empty is nobody
    /// named, as before approvers existed.
    pub actor: String,
    /// The New bot defaults: the model proposed and the models offered,
    /// comma-separated.
    pub default_model: String,
    pub models: String,
    pub busy: bool,
    /// Whether the control plane settings were saved before: false on
    /// the first run.
    pub saved: bool,
    /// The control plane values being tried.
    pub trying: Option<PanelConfig>,
    /// The control plane values the panel is connected with.
    pub connected: Option<PanelConfig>,
}

impl SettingsForm {
    pub fn from_settings(settings: &PanelSettings, saved: bool) -> Self {
        let config = settings.control_plane.as_ref();
        Self {
            api_url: config.map_or(String::new(), |c| c.api_url.clone()),
            ca_path: config.map_or(String::new(), |c| c.ca_path.display().to_string()),
            token_file: config.map_or(String::new(), |c| c.token_file.display().to_string()),
            actor: config.and_then(|c| c.actor.clone()).unwrap_or_default(),
            default_model: settings.new_bot.model.clone(),
            models: settings.new_bot.models.join(", "),
            busy: false,
            saved,
            trying: None,
            connected: None,
        }
    }

    /// Fills the fields from settings, keeping what the connection knows;
    /// settings without a control plane section leave those three alone.
    pub fn take(&mut self, settings: &PanelSettings) {
        let filled = Self::from_settings(settings, self.saved);
        if settings.control_plane.is_some() {
            self.api_url = filled.api_url;
            self.ca_path = filled.ca_path;
            self.token_file = filled.token_file;
            self.actor = filled.actor;
        }
        self.default_model = filled.default_model;
        self.models = filled.models;
    }

    /// The control plane section, as typed.
    pub fn config(&self) -> PanelConfig {
        PanelConfig {
            api_url: self.api_url.trim().to_string(),
            ca_path: PathBuf::from(self.ca_path.trim()),
            token_file: PathBuf::from(self.token_file.trim()),
            actor: Some(self.actor.trim().to_string()).filter(|a| !a.is_empty()),
        }
    }

    /// The New bot defaults, as typed.
    pub fn new_bot(&self) -> NewBotDefaults {
        NewBotDefaults {
            model: self.default_model.clone(),
            models: self.models.split(',').map(str::to_string).collect(),
        }
        .tidy()
    }

    /// Everything on the screen as settings, with the look given.
    pub fn settings(&self, theme: Theme) -> PanelSettings {
        PanelSettings {
            control_plane: Some(self.config()),
            theme,
            new_bot: self.new_bot(),
        }
    }

    /// Whether Connect has something to do: not busy, and not connected
    /// with these very values.
    pub fn can_connect(&self) -> bool {
        !self.busy && self.connected.as_ref() != Some(&self.config())
    }

    /// The control plane values differ from the ones connected.
    pub fn changed(&self) -> bool {
        self.connected.as_ref().is_some_and(|c| *c != self.config())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn settings() -> PanelSettings {
        PanelSettings {
            control_plane: Some(PanelConfig {
                api_url: "https://h:1".into(),
                ca_path: "/ca".into(),
                token_file: "/t".into(),
                actor: None,
            }),
            theme: Theme::Dark,
            new_bot: NewBotDefaults {
                model: "a/b".into(),
                models: vec!["a/b".into(), "c/d".into()],
            },
        }
    }

    #[test]
    fn round_trips_the_settings() {
        let mut form = SettingsForm::from_settings(&settings(), true);
        assert!(form.saved);
        assert_eq!(form.models, "a/b, c/d");
        form.api_url = " https://h:1 ".into();
        form.models = "a/b,c/d , ".into();
        assert_eq!(form.settings(Theme::Dark), settings());
        let mut other = SettingsForm::from_settings(&PanelSettings::default(), false);
        assert_eq!(other.api_url, "");
        other.take(&settings());
        assert_eq!((other.api_url.as_str(), other.saved), ("https://h:1", false));
        other.take(&PanelSettings::default());
        assert_eq!(other.api_url, "https://h:1", "no control plane section: the three stay");
        assert_eq!(other.models, "anthropic/claude-haiku-5.5");
        form.actor = " alice ".into();
        assert_eq!(form.config().actor.as_deref(), Some("alice"));
        let mut named = settings();
        named.control_plane.as_mut().unwrap().actor = Some("alice".into());
        assert_eq!(SettingsForm::from_settings(&named, true).actor, "alice");
        other.take(&named);
        assert_eq!(other.actor, "alice");
    }

    #[test]
    fn connect_has_nothing_to_do_while_connected_with_the_same_values() {
        let mut form = SettingsForm::from_settings(&settings(), true);
        assert!(form.can_connect() && !form.changed());
        form.connected = Some(form.config());
        assert!(!form.can_connect() && !form.changed());
        form.api_url = "https://h:2".into();
        assert!(form.can_connect() && form.changed());
        form.busy = true;
        assert!(!form.can_connect());
    }
}
