//! Runs the panel with Iced: the state machine's effects become tasks,
//! the clock ticks every few seconds.

use std::borrow::Cow;
use std::time::Duration;

use chrono::Utc;
use iced::widget::operation::RelativeOffset;
use iced::{Size, Subscription, Task, Theme, window};

use super::view::widgets;
use super::{App, Effect, Keypress, Message, Tokens, brand, view};

/// How often the panel reads the control plane again.
pub const REFRESH_EVERY: Duration = Duration::from_secs(3);

/// The window's size at start: it fits the tables.
pub const WINDOW: Size = Size::new(1360.0, 860.0);

/// The smallest window: every screen still shows its words whole.
pub const MIN_WINDOW: Size = Size::new(1040.0, 640.0);

/// The panel's typefaces, embedded with the binary: Lexend in its three
/// weights for words, DotGothic16 for big numbers.
pub const FONTS: [&[u8]; 4] = [
    include_bytes!("../../assets/fonts/Lexend-Regular.ttf"),
    include_bytes!("../../assets/fonts/Lexend-SemiBold.ttf"),
    include_bytes!("../../assets/fonts/Lexend-Bold.ttf"),
    include_bytes!("../../assets/fonts/DotGothic16-Latin.ttf"),
];

/// The application's settings: its id, the embedded typefaces and Lexend
/// as the default. Iced's `.settings()` replaces every field, so the
/// typefaces go in here and nowhere else, or the window draws with the
/// system's sans.
pub fn settings() -> iced::Settings {
    iced::Settings {
        id: Some("axlr-bots-panel".into()),
        fonts: FONTS.iter().map(|font| Cow::Borrowed(*font)).collect(),
        default_font: widgets::REGULAR,
        ..iced::Settings::default()
    }
}

/// The application's update: apply the message, run its effects.
pub fn update(app: &mut App, message: Message) -> Task<Message> {
    let effects = app.apply(message);
    Task::batch(effects.into_iter().map(|effect| run(app, effect)))
}

/// One effect as an Iced task: the clipboard here, the rest through the
/// services, off the interface's thread.
pub fn run(app: &App, effect: Effect) -> Task<Message> {
    match effect {
        Effect::Copy(text) => iced::clipboard::write(text),
        Effect::FocusWindow => window::latest().and_then(window::gain_focus),
        Effect::ShowEnd(stream) => iced::widget::operation::snap_to_end(stream.id()),
        Effect::ScrollTop => iced::widget::operation::snap_to(widgets::PAGE, RelativeOffset::START),
        other => {
            let services = app.services.clone();
            Task::perform(services.execute(other), |message| message).and_then(Task::done)
        }
    }
}

/// The clock, and Esc and Enter when no widget took them.
pub fn subscription(_: &App) -> Subscription<Message> {
    Subscription::batch([
        iced::time::every(REFRESH_EVERY).map(|_| Message::Tick(Utc::now())),
        iced::keyboard::listen().filter_map(Keypress::of).map(Message::Key),
    ])
}

pub fn theme(app: &App) -> Theme {
    Tokens::of(app.mode).theme()
}

pub fn title(app: &App) -> String {
    app.window_title()
}

/// Starts the window: the brand's icon, the embedded typefaces, a size
/// that fits the tables, the state the boot function builds.
pub fn launch(boot: impl Fn() -> (App, Vec<Effect>) + 'static) -> iced::Result {
    let icon = window::icon::from_file_data(brand::WINDOW_ICON_PNG, None).ok();
    iced::application(
        move || {
            let (app, effects) = boot();
            let tasks: Vec<Task<Message>> = effects.into_iter().map(|effect| run(&app, effect)).collect();
            (app, Task::batch(tasks))
        },
        update,
        view::view,
    )
    .title(title)
    .theme(theme)
    .subscription(subscription)
    .window(window::Settings {
        size: WINDOW,
        min_size: Some(MIN_WINDOW),
        icon,
        platform_specific: window::settings::PlatformSpecific {
            application_id: "axlr-bots-panel".into(),
            ..Default::default()
        },
        ..window::Settings::default()
    })
    .settings(settings())
    .run()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_window_gets_the_typefaces_and_lexend_by_default() {
        let settings = settings();
        assert_eq!(settings.fonts.len(), FONTS.len());
        for (given, embedded) in settings.fonts.iter().zip(FONTS) {
            assert_eq!(given.as_ref(), embedded);
        }
        assert_eq!(settings.default_font, widgets::REGULAR);
        assert_eq!(settings.id.as_deref(), Some("axlr-bots-panel"));
    }
}
