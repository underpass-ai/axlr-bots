use std::path::PathBuf;

use crate::application::{ConfigStore, PanelConfig, PanelError};

/// The settings as a small TOML file, `~/.config/axlr-bots-panel/config.toml`.
pub struct TomlConfigStore {
    path: PathBuf,
    defaults: PanelConfig,
}

impl TomlConfigStore {
    pub fn new(path: PathBuf, defaults: PanelConfig) -> Self {
        Self { path, defaults }
    }

    pub fn path(&self) -> &PathBuf {
        &self.path
    }
}

impl ConfigStore for TomlConfigStore {
    fn load(&self) -> Result<Option<PanelConfig>, PanelError> {
        let text = match std::fs::read_to_string(&self.path) {
            Ok(text) => text,
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(None),
            Err(e) => return Err(PanelError::Config(format!("{}: {e}", self.path.display()))),
        };
        toml::from_str(&text)
            .map(Some)
            .map_err(|e| PanelError::Config(format!("{} is not valid: {e}", self.path.display())))
    }

    fn save(&self, config: &PanelConfig) -> Result<(), PanelError> {
        let text = toml::to_string_pretty(config).map_err(|e| PanelError::Config(e.to_string()))?;
        let shown = self.path.display().to_string();
        if let Some(dir) = self.path.parent() {
            std::fs::create_dir_all(dir).map_err(|e| PanelError::Config(format!("{shown}: {e}")))?;
        }
        let partial = self.path.with_extension("toml.partial");
        let header = "# AXLR Bots panel: where the operator API is and how to trust it.\n";
        std::fs::write(&partial, format!("{header}{text}")).map_err(|e| PanelError::Config(format!("{shown}: {e}")))?;
        std::fs::rename(&partial, &self.path).map_err(|e| PanelError::Config(format!("{shown}: {e}")))
    }

    fn defaults(&self) -> PanelConfig {
        self.defaults.clone()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn config() -> PanelConfig {
        PanelConfig {
            api_url: "https://127.0.0.1:7879".into(),
            ca_path: "/s/ca.crt".into(),
            token_file: "/s/api.token".into(),
        }
    }

    #[test]
    fn saves_and_loads_the_settings() {
        let dir = tempfile::tempdir().unwrap();
        let store = TomlConfigStore::new(dir.path().join("panel/config.toml"), config());
        assert_eq!(store.load().unwrap(), None);
        assert_eq!(store.defaults(), config());
        store.save(&config()).unwrap();
        assert_eq!(store.load().unwrap(), Some(config()));
        let text = std::fs::read_to_string(store.path()).unwrap();
        assert!(text.starts_with("# AXLR Bots panel") && text.contains("api_url = \"https://127.0.0.1:7879\""));
    }

    #[test]
    fn a_broken_file_is_a_settings_error() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("config.toml");
        std::fs::write(&path, "api_url = ").unwrap();
        let store = TomlConfigStore::new(path, config());
        assert!(matches!(store.load(), Err(PanelError::Config(_))));
        let unreadable = TomlConfigStore::new(dir.path().to_path_buf(), config());
        assert!(matches!(unreadable.load(), Err(PanelError::Config(_))));
        let blocked = TomlConfigStore::new(dir.path().join("config.toml/inner.toml"), config());
        assert!(matches!(blocked.save(&config()), Err(PanelError::Config(_))));
    }
}
