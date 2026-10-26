use std::path::PathBuf;

use crate::application::PanelConfig;

/// The settings screen's fields, as typed.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct SettingsForm {
    pub api_url: String,
    pub ca_path: String,
    pub token_file: String,
    pub busy: bool,
    /// Whether settings were saved before: false on the first run.
    pub saved: bool,
}

impl SettingsForm {
    pub fn from_config(config: &PanelConfig, saved: bool) -> Self {
        Self {
            api_url: config.api_url.clone(),
            ca_path: config.ca_path.display().to_string(),
            token_file: config.token_file.display().to_string(),
            busy: false,
            saved,
        }
    }

    pub fn config(&self) -> PanelConfig {
        PanelConfig {
            api_url: self.api_url.trim().to_string(),
            ca_path: PathBuf::from(self.ca_path.trim()),
            token_file: PathBuf::from(self.token_file.trim()),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn round_trips_the_settings() {
        let config = PanelConfig {
            api_url: "https://h:1".into(),
            ca_path: "/ca".into(),
            token_file: "/t".into(),
        };
        let mut form = SettingsForm::from_config(&config, true);
        assert!(form.saved);
        form.api_url = " https://h:1 ".into();
        assert_eq!(form.config(), config);
    }
}
