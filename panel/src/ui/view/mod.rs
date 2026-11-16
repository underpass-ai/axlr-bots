//! What the panel draws for each screen.

mod bot_delete;
mod bot_edit;
mod bots;
mod decide;
mod grid;
mod inspector;
mod levels;
mod listbar;
mod nodes;
mod order;
mod overview;
mod palette;
mod post_draft;
mod settings;
mod sheet;
mod shell;
mod status;
mod tasks;
mod teams;
mod voice;
pub mod widgets;

use iced::Element;

use super::find::RowSpot;
use super::{App, Message, Screen, Scroller, Tokens};
use crate::application::Density;

/// The whole window for the panel's state.
pub fn view(app: &App) -> Element<'_, Message> {
    let tokens = Tokens::of(app.mode);
    // Home and the lists reach the window's edges and scroll their own
    // rows, a list with the row it inspects beside it; Settings is a page
    // in a margin.
    let content = match &app.screen {
        Screen::Overview => overview::view(app, tokens),
        Screen::Nodes | Screen::Node(_) => inspector::beside(app, tokens, move || nodes::view(app, tokens)),
        Screen::Bots | Screen::Bot(_) => inspector::beside(app, tokens, move || bots::view(app, tokens)),
        Screen::Tasks | Screen::Task(_) => inspector::beside(app, tokens, move || tasks::view(app, tokens)),
        Screen::Teams | Screen::Team(_) => inspector::beside(app, tokens, move || teams::list(app, tokens)),
        Screen::Settings => page(settings::view(app, tokens)),
    };
    shell::frame(app, content, tokens)
}

/// Where a row is drawn in what scrolls it, in pixels: its top in the
/// content and its height. The lists' rows are as tall as the density
/// says under group headers of their own height; the palette's results
/// sit under their section headings.
pub fn span(scroller: Scroller, spot: RowSpot, density: Density) -> (f32, f32) {
    match scroller {
        Scroller::Rows => {
            let row = density.row_height();
            (spot.top(0.0, row, sheet::GROUP), row)
        }
        Scroller::Palette => (spot.top(palette::PAD, palette::ENTRY, palette::SECTION), palette::ENTRY),
    }
}

/// A screen that is a page to read: in a margin, away from the rail.
fn page<'a>(content: widgets::El<'a>) -> widgets::El<'a> {
    iced::widget::container(content)
        .padding(iced::Padding::from([14, 18]))
        .width(iced::Length::Fill)
        .height(iced::Length::Fill)
        .into()
}

#[cfg(test)]
pub(crate) mod tests {
    use std::sync::Arc;

    use crate::adapters::fake::{
        CountingKeys, FakeConnector, FakeControlPlane, FixedClock, FixedShortcuts, MemoryConfigStore, MemoryFiles,
        RecordingNotifier,
    };
    use crate::application::{Connect, PanelConfig, PanelSettings};
    use crate::domain::ShortcutFile;

    /// An operator's buttons on the team `ops` of the sample bots.
    pub(crate) const SHORTCUTS: &str = r#"
[[shortcut]]
team = "ops"
label = "Notice"
name = "info.notice"
message = "A deploy is starting."
data = { service = "service-1" }

[[shortcut]]
team = "ops"
label = "Stop all"
name = "stop.all"
message = "Stop."
"#;
    use crate::ui::{App, Launch, Services};

    pub(crate) fn config() -> PanelConfig {
        PanelConfig {
            api_url: "https://127.0.0.1:7879".into(),
            ca_path: "/ca.crt".into(),
            token_file: "/api.token".into(),
            actor: None,
            cert_path: None,
            key_path: None,
        }
    }

    /// The sample settings before a first Connect: the control plane
    /// values filled from the install.
    pub(crate) fn settings() -> PanelSettings {
        PanelSettings {
            control_plane: Some(config()),
            ..PanelSettings::default()
        }
    }

    /// A panel over the sample bots, not connected yet.
    pub(crate) fn app() -> App {
        let now = "2026-10-25T10:00:00Z".parse().unwrap();
        app_over(Arc::new(FakeControlPlane::sample(now)))
    }

    /// A panel over the fake given, which a test keeps to change it.
    pub(crate) fn app_over(control_plane: Arc<FakeControlPlane>) -> App {
        app_with(control_plane, Arc::new(MemoryFiles::default()))
    }

    /// A panel over the sample bots and the files given, which a test
    /// keeps to add files to pick and to read what was saved.
    pub(crate) fn app_with_files() -> (App, Arc<MemoryFiles>) {
        let now = "2026-10-25T10:00:00Z".parse().unwrap();
        let files = Arc::new(MemoryFiles::default());
        (app_with(Arc::new(FakeControlPlane::sample(now)), files.clone()), files)
    }

    fn app_with(control_plane: Arc<FakeControlPlane>, files: Arc<MemoryFiles>) -> App {
        let now = "2026-10-25T10:00:00Z".parse().unwrap();
        let clock = Arc::new(FixedClock::new(now));
        let store = Arc::new(MemoryConfigStore::new(None, config()));
        let connector = Arc::new(FakeConnector::new("https://127.0.0.1:7879", control_plane));
        let services = Services {
            control_plane: None,
            clock: clock.clone(),
            keys: Arc::new(CountingKeys::default()),
            picker: files.clone(),
            reader: files,
            connect: Arc::new(Connect::new(connector, store, clock)),
            notifier: Arc::new(RecordingNotifier::default()),
            shortcuts: Arc::new(FixedShortcuts(ShortcutFile::parse(SHORTCUTS).unwrap())),
        };
        App::new(services, Launch::default(), &settings(), false).0
    }
}
