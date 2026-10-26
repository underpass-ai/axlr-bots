use std::sync::Mutex;

use crate::application::{ConfigStore, PanelConfig, PanelError};

/// Settings in memory, with the defaults a test gives.
pub struct MemoryConfigStore {
    saved: Mutex<Option<PanelConfig>>,
    defaults: PanelConfig,
}

impl MemoryConfigStore {
    pub fn new(saved: Option<PanelConfig>, defaults: PanelConfig) -> Self {
        Self {
            saved: Mutex::new(saved),
            defaults,
        }
    }

    pub fn saved(&self) -> Option<PanelConfig> {
        self.saved.lock().unwrap().clone()
    }
}

impl ConfigStore for MemoryConfigStore {
    fn load(&self) -> Result<Option<PanelConfig>, PanelError> {
        Ok(self.saved())
    }

    fn save(&self, config: &PanelConfig) -> Result<(), PanelError> {
        *self.saved.lock().unwrap() = Some(config.clone());
        Ok(())
    }

    fn defaults(&self) -> PanelConfig {
        self.defaults.clone()
    }
}
