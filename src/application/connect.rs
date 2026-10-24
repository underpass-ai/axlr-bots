use std::sync::Arc;

use super::{Clock, ConfigStore, Connector, ControlPlane, PanelConfig, PanelError};

/// Checks settings against the control plane before keeping them: builds
/// a client, asks for the overview, and saves the settings once it
/// answers.
pub struct Connect {
    connector: Arc<dyn Connector>,
    store: Arc<dyn ConfigStore>,
    clock: Arc<dyn Clock>,
}

impl Connect {
    pub fn new(connector: Arc<dyn Connector>, store: Arc<dyn ConfigStore>, clock: Arc<dyn Clock>) -> Self {
        Self {
            connector,
            store,
            clock,
        }
    }

    /// The client for the settings, once the control plane answered.
    pub async fn try_config(&self, config: &PanelConfig) -> Result<Arc<dyn ControlPlane>, PanelError> {
        config.validate()?;
        let control_plane = self.connector.connect(config)?;
        control_plane.overview(self.clock.start_of_today()).await?;
        Ok(control_plane)
    }

    /// Tries the settings and saves them when they work.
    pub async fn save(&self, config: PanelConfig) -> Result<Arc<dyn ControlPlane>, PanelError> {
        let control_plane = self.try_config(&config).await?;
        self.store.save(&config)?;
        Ok(control_plane)
    }

    /// The saved settings, or the local install's defaults on a first run,
    /// and whether they were saved.
    pub fn initial(&self) -> (PanelConfig, bool) {
        match self.store.load() {
            Ok(Some(config)) => (config, true),
            _ => (self.store.defaults(), false),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::adapters::fake::{FakeConnector, MemoryConfigStore};
    use crate::application::test_support::{fleet, run};

    fn config(url: &str) -> PanelConfig {
        PanelConfig {
            api_url: url.into(),
            ca_path: "/ca.crt".into(),
            token_file: "/api.token".into(),
        }
    }

    #[test]
    fn saves_settings_only_once_the_control_plane_answers() {
        let (control_plane, clock) = fleet();
        let store = Arc::new(MemoryConfigStore::new(None, config("https://127.0.0.1:7879")));
        let connector = Arc::new(FakeConnector::new("https://127.0.0.1:7879", control_plane.clone()));
        let connect = Connect::new(connector, store.clone(), clock);
        let (initial, saved) = connect.initial();
        assert_eq!((initial.api_url.as_str(), saved), ("https://127.0.0.1:7879", false));
        assert!(matches!(
            run(connect.save(config("https://10.0.0.9:7879"))),
            Err(PanelError::Unreachable(_))
        ));
        assert!(matches!(
            run(connect.save(config("http://x"))),
            Err(PanelError::Config(_))
        ));
        assert!(store.saved().is_none());
        control_plane.fail_with(Some(PanelError::Unauthorized("bad token".into())));
        assert!(matches!(
            run(connect.save(config("https://127.0.0.1:7879/"))),
            Err(PanelError::Unauthorized(_))
        ));
        control_plane.fail_with(None);
        run(connect.save(config("https://127.0.0.1:7879/"))).unwrap();
        assert_eq!(store.saved().unwrap().api_url, "https://127.0.0.1:7879/");
        assert!(connect.initial().1);
    }
}
