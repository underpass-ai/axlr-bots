use std::sync::Mutex;

use crate::application::{ConfigStore, NewBotDefaults, PanelConfig, PanelError, PanelSettings};

/// Settings in memory, with the defaults a test gives.
pub struct MemoryConfigStore {
    saved: Mutex<Option<PanelSettings>>,
    defaults: PanelConfig,
    new_bot: NewBotDefaults,
}

impl MemoryConfigStore {
    pub fn new(saved: Option<PanelSettings>, defaults: PanelConfig) -> Self {
        Self {
            saved: Mutex::new(saved),
            defaults,
            new_bot: NewBotDefaults::default(),
        }
    }

    pub fn saved(&self) -> Option<PanelSettings> {
        self.saved.lock().unwrap().clone()
    }
}

impl ConfigStore for MemoryConfigStore {
    fn load(&self) -> Result<Option<PanelSettings>, PanelError> {
        Ok(self.saved())
    }

    fn save(&self, settings: &PanelSettings) -> Result<(), PanelError> {
        *self.saved.lock().unwrap() = Some(settings.clone());
        Ok(())
    }

    fn defaults(&self) -> PanelConfig {
        self.defaults.clone()
    }

    fn new_bot_defaults(&self) -> NewBotDefaults {
        self.new_bot.clone()
    }

    fn location(&self) -> String {
        "~/.config/axlr-bots-panel/settings.json".into()
    }
}
