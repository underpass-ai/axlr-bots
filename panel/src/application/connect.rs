use std::sync::Arc;

use super::{Clock, ConfigStore, Connector, ControlPlane, NewBotDefaults, PanelConfig, PanelError, PanelSettings};

/// Checks settings against the control plane before keeping them: builds
/// a client, asks for the overview, and saves the control plane section
/// once it answers. The other sections are saved as they change.
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
        control_plane
            .overview(self.clock.start_of_today())
            .await
            .map_err(|error| match error {
                PanelError::NotFound(_) => PanelError::Config(
                    "this control plane has no /v1/overview: the panel needs axlr-bots with its routes \
                 (underpass-ai/axlr-bots#3)"
                        .into(),
                ),
                other => other,
            })?;
        Ok(control_plane)
    }

    /// Tries the control plane settings and keeps them when they work,
    /// leaving the other sections as they are.
    pub async fn save(&self, config: PanelConfig) -> Result<Arc<dyn ControlPlane>, PanelError> {
        let control_plane = self.try_config(&config).await?;
        let mut settings = self.store.load()?.unwrap_or_default();
        settings.control_plane = Some(config);
        self.store.save(&settings)?;
        Ok(control_plane)
    }

    /// Keeps the sections that need no control plane: the look and the
    /// New bot defaults. The control plane section stays as saved.
    pub fn save_preferences(&self, settings: &PanelSettings) -> Result<PanelSettings, PanelError> {
        let mut kept = self.store.load()?.unwrap_or_default();
        kept.theme = settings.theme;
        kept.new_bot = settings.new_bot.tidy();
        self.store.save(&kept)?;
        Ok(kept)
    }

    /// The saved settings, with the control plane section filled from
    /// the local install on a first run, and whether that section was
    /// saved before.
    pub fn initial(&self) -> (PanelSettings, bool) {
        let mut settings = match self.store.load() {
            Ok(Some(settings)) => settings,
            _ => PanelSettings {
                new_bot: self.store.new_bot_defaults(),
                ..PanelSettings::default()
            },
        };
        let saved = settings.control_plane.is_some();
        if !saved {
            settings.control_plane = Some(self.store.defaults());
        }
        (settings, saved)
    }

    /// The install's control plane values and the New bot defaults of
    /// this host: what Reset goes back to.
    pub fn defaults(&self) -> (PanelConfig, NewBotDefaults) {
        (self.store.defaults(), self.store.new_bot_defaults())
    }

    /// Where the settings live, in words.
    pub fn location(&self) -> String {
        self.store.location()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::adapters::fake::{FakeConnector, MemoryConfigStore};
    use crate::application::test_support::{fleet, run};
    use crate::application::{NewBotDefaults, Theme};

    fn config(url: &str) -> PanelConfig {
        PanelConfig {
            api_url: url.into(),
            ca_path: "/ca.crt".into(),
            token_file: "/api.token".into(),
            actor: None,
        }
    }

    #[test]
    fn saves_settings_only_once_the_control_plane_answers() {
        let (control_plane, clock) = fleet();
        let store = Arc::new(MemoryConfigStore::new(None, config("https://127.0.0.1:7879")));
        let connector = Arc::new(FakeConnector::new("https://127.0.0.1:7879", control_plane.clone()));
        let connect = Connect::new(connector, store.clone(), clock);
        let (initial, saved) = connect.initial();
        assert_eq!(
            (initial.control_plane.unwrap().api_url.as_str(), saved),
            ("https://127.0.0.1:7879", false)
        );
        assert!(connect.location().ends_with("settings.json"));
        assert_eq!(connect.defaults().0, config("https://127.0.0.1:7879"));
        assert!(matches!(
            run(connect.save(config("https://10.0.0.9:7879"))),
            Err(PanelError::Unreachable(_))
        ));
        assert!(matches!(
            run(connect.save(config("http://x"))),
            Err(PanelError::Config(_))
        ));
        assert!(store.saved().is_none());
        control_plane.fail_with(Some(PanelError::NotFound("404 page not found".into())));
        let older = run(connect.save(config("https://127.0.0.1:7879/"))).err();
        assert!(
            matches!(older, Some(PanelError::Config(ref m)) if m.contains("/v1/overview")),
            "{older:?}"
        );
        control_plane.fail_with(Some(PanelError::Unauthorized("bad token".into())));
        assert!(matches!(
            run(connect.save(config("https://127.0.0.1:7879/"))),
            Err(PanelError::Unauthorized(_))
        ));
        control_plane.fail_with(None);
        run(connect.save(config("https://127.0.0.1:7879/"))).unwrap();
        assert_eq!(
            store.saved().unwrap().control_plane.unwrap().api_url,
            "https://127.0.0.1:7879/"
        );
        assert!(connect.initial().1);
    }

    #[test]
    fn preferences_are_kept_without_a_control_plane_and_never_touch_its_section() {
        let (control_plane, clock) = fleet();
        let store = Arc::new(MemoryConfigStore::new(None, config("https://127.0.0.1:7879")));
        let connector = Arc::new(FakeConnector::new("https://127.0.0.1:7879", control_plane));
        let connect = Connect::new(connector, store.clone(), clock);
        let kept = connect
            .save_preferences(&PanelSettings {
                control_plane: Some(config("https://evil:1")),
                theme: Theme::Dark,
                new_bot: NewBotDefaults {
                    model: " codex/gpt-6-luna ".into(),
                    models: vec![],
                },
            })
            .unwrap();
        assert_eq!(kept.control_plane, None);
        assert_eq!(kept.theme, Theme::Dark);
        assert_eq!(kept.new_bot.model, "codex/gpt-6-luna");
        let (initial, saved) = connect.initial();
        assert!(!saved && initial.theme == Theme::Dark);
        assert_eq!(initial.control_plane, Some(config("https://127.0.0.1:7879")));
        run(connect.save(config("https://127.0.0.1:7879"))).unwrap();
        let saved = store.saved().unwrap();
        assert!(saved.control_plane.is_some() && saved.theme == Theme::Dark);
    }
}
