//! What the panel draws for each screen.

mod bot_detail;
mod bots;
mod decide;
mod nodes;
mod overview;
mod settings;
mod shell;
mod task_detail;
mod tasks;
pub mod widgets;

use iced::Element;

use super::{App, Message, Screen, Tokens};

/// The whole window for the panel's state.
pub fn view(app: &App) -> Element<'_, Message> {
    let tokens = Tokens::of(app.mode);
    let content = match &app.screen {
        Screen::Overview => overview::view(app, tokens),
        Screen::Nodes => nodes::view(app, tokens),
        Screen::Bots => bots::view(app, tokens),
        Screen::Bot(id) => bot_detail::view(app, id, tokens),
        Screen::Tasks => tasks::view(app, tokens),
        Screen::Task(id) => task_detail::view(app, id, tokens),
        Screen::Settings => settings::view(app, tokens),
    };
    shell::frame(app, content, tokens)
}

#[cfg(test)]
pub(crate) mod tests {
    use std::sync::Arc;

    use crate::adapters::fake::{
        CountingKeys, FakeConnector, FakeControlPlane, FixedClock, MemoryConfigStore, MemoryFiles,
    };
    use crate::application::{Connect, PanelConfig};
    use crate::ui::{App, Launch, Services};

    pub(crate) fn config() -> PanelConfig {
        PanelConfig {
            api_url: "https://127.0.0.1:7879".into(),
            ca_path: "/ca.crt".into(),
            token_file: "/api.token".into(),
        }
    }

    /// A panel over the sample fleet, not connected yet.
    pub(crate) fn app() -> App {
        let now = "2026-10-25T10:00:00Z".parse().unwrap();
        let control_plane = Arc::new(FakeControlPlane::sample(now));
        let clock = Arc::new(FixedClock::new(now));
        let store = Arc::new(MemoryConfigStore::new(None, config()));
        let connector = Arc::new(FakeConnector::new("https://127.0.0.1:7879", control_plane));
        let files = Arc::new(MemoryFiles::default());
        let services = Services {
            control_plane: None,
            clock: clock.clone(),
            keys: Arc::new(CountingKeys::default()),
            picker: files.clone(),
            reader: files,
            connect: Arc::new(Connect::new(connector, store, clock)),
        };
        App::new(services, Launch::default(), &config(), false).0
    }
}
