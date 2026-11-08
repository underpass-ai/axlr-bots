use std::path::PathBuf;

use crate::application::{ConfigStore, NewBotDefaults, PanelConfig, PanelError, PanelSettings};

/// The settings as one JSON file of flat dotted keys,
/// `~/.config/axlr-bots-panel/settings.json`. A `config.toml` an older
/// panel wrote beside it is read once, when there is no JSON yet, and
/// left where it is.
pub struct JsonSettingsStore {
    path: PathBuf,
    legacy: PathBuf,
    defaults: PanelConfig,
    new_bot: NewBotDefaults,
}

impl JsonSettingsStore {
    pub fn new(path: PathBuf, legacy: PathBuf, defaults: PanelConfig, new_bot: NewBotDefaults) -> Self {
        Self {
            path,
            legacy,
            defaults,
            new_bot,
        }
    }

    pub fn path(&self) -> &PathBuf {
        &self.path
    }

    /// The control plane section of an older `config.toml`, if there is one.
    fn legacy(&self) -> Result<Option<PanelConfig>, PanelError> {
        let text = match std::fs::read_to_string(&self.legacy) {
            Ok(text) => text,
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(None),
            Err(e) => return Err(PanelError::Config(format!("{}: {e}", self.legacy.display()))),
        };
        toml::from_str(&text)
            .map(Some)
            .map_err(|e| PanelError::Config(format!("{} is not valid: {e}", self.legacy.display())))
    }
}

impl ConfigStore for JsonSettingsStore {
    fn load(&self) -> Result<Option<PanelSettings>, PanelError> {
        let text = match std::fs::read_to_string(&self.path) {
            Ok(text) => text,
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => {
                return match self.legacy()? {
                    Some(control_plane) => {
                        let settings = PanelSettings {
                            control_plane: Some(control_plane),
                            new_bot: self.new_bot.clone(),
                            ..PanelSettings::default()
                        };
                        self.save(&settings)?;
                        Ok(Some(settings))
                    }
                    None => Ok(None),
                };
            }
            Err(e) => return Err(PanelError::Config(format!("{}: {e}", self.path.display()))),
        };
        PanelSettings::from_json(&text)
            .map(Some)
            .map_err(|e| PanelError::Config(format!("{}: {e}", self.path.display())))
    }

    fn save(&self, settings: &PanelSettings) -> Result<(), PanelError> {
        let shown = self.path.display().to_string();
        if let Some(dir) = self.path.parent() {
            std::fs::create_dir_all(dir).map_err(|e| PanelError::Config(format!("{shown}: {e}")))?;
        }
        let partial = self.path.with_extension("json.partial");
        std::fs::write(&partial, settings.to_json()).map_err(|e| PanelError::Config(format!("{shown}: {e}")))?;
        std::fs::rename(&partial, &self.path).map_err(|e| PanelError::Config(format!("{shown}: {e}")))
    }

    fn defaults(&self) -> PanelConfig {
        self.defaults.clone()
    }

    fn new_bot_defaults(&self) -> NewBotDefaults {
        self.new_bot.clone()
    }

    fn location(&self) -> String {
        self.path.display().to_string()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::application::Theme;

    fn config() -> PanelConfig {
        PanelConfig {
            api_url: "https://127.0.0.1:7879".into(),
            ca_path: "/s/ca.crt".into(),
            token_file: "/s/api.token".into(),
            actor: None,
        }
    }

    fn store(dir: &std::path::Path) -> JsonSettingsStore {
        JsonSettingsStore::new(
            dir.join("panel/settings.json"),
            dir.join("panel/config.toml"),
            config(),
            NewBotDefaults::default(),
        )
    }

    #[test]
    fn saves_and_loads_the_settings() {
        let dir = tempfile::tempdir().unwrap();
        let store = store(dir.path());
        assert_eq!(store.load().unwrap(), None);
        assert_eq!(store.defaults(), config());
        assert_eq!(store.new_bot_defaults(), NewBotDefaults::default());
        assert!(store.location().ends_with("panel/settings.json"));
        let settings = PanelSettings {
            control_plane: Some(config()),
            theme: Theme::Dark,
            ..PanelSettings::default()
        };
        store.save(&settings).unwrap();
        assert_eq!(store.load().unwrap(), Some(settings));
        let text = std::fs::read_to_string(store.path()).unwrap();
        assert!(text.starts_with("{\n  \"controlPlane.apiUrl\": \"https://127.0.0.1:7879\""));
    }

    #[test]
    fn an_older_toml_is_read_once_and_kept() {
        let dir = tempfile::tempdir().unwrap();
        let store = store(dir.path());
        std::fs::create_dir_all(dir.path().join("panel")).unwrap();
        std::fs::write(
            dir.path().join("panel/config.toml"),
            "api_url = \"https://cp:7879\"\nca_path = \"/ca\"\ntoken_file = \"/t\"\n",
        )
        .unwrap();
        let loaded = store.load().unwrap().unwrap();
        assert_eq!(loaded.control_plane.as_ref().unwrap().api_url, "https://cp:7879");
        assert_eq!(loaded.theme, Theme::Light);
        assert!(store.path().is_file() && dir.path().join("panel/config.toml").is_file());
        std::fs::write(dir.path().join("panel/config.toml"), "api_url = ").unwrap();
        assert_eq!(store.load().unwrap().unwrap(), loaded, "the JSON wins once it exists");
        std::fs::remove_file(store.path()).unwrap();
        assert!(matches!(store.load(), Err(PanelError::Config(_))));
    }

    #[test]
    fn a_broken_file_is_a_settings_error() {
        let dir = tempfile::tempdir().unwrap();
        let store = store(dir.path());
        std::fs::create_dir_all(dir.path().join("panel")).unwrap();
        std::fs::write(store.path(), "{\"look.theme\": ").unwrap();
        let error = store.load().unwrap_err();
        assert!(
            matches!(&error, PanelError::Config(m) if m.contains("settings.json")),
            "{error:?}"
        );
        let unreadable = JsonSettingsStore::new(
            dir.path().to_path_buf(),
            dir.path().join("none.toml"),
            config(),
            NewBotDefaults::default(),
        );
        assert!(matches!(unreadable.load(), Err(PanelError::Config(_))));
        let legacy_dir = JsonSettingsStore::new(
            dir.path().join("none.json"),
            dir.path().to_path_buf(),
            config(),
            NewBotDefaults::default(),
        );
        assert!(matches!(legacy_dir.load(), Err(PanelError::Config(_))));
        let blocked = JsonSettingsStore::new(
            store.path().join("inner.json"),
            dir.path().join("none.toml"),
            config(),
            NewBotDefaults::default(),
        );
        assert!(matches!(
            blocked.save(&PanelSettings::default()),
            Err(PanelError::Config(_))
        ));
    }
}
