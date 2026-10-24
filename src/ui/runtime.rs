//! Runs the panel with Iced: the state machine's effects become tasks,
//! the clock ticks every few seconds.

use std::time::Duration;

use chrono::Utc;
use iced::{Size, Subscription, Task, Theme, window};

use super::{App, Effect, Message, Tokens, brand, view};

/// How often the panel reads the control plane again.
pub const REFRESH_EVERY: Duration = Duration::from_secs(3);

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
        other => {
            let services = app.services.clone();
            Task::perform(services.execute(other), |message| message).and_then(Task::done)
        }
    }
}

pub fn subscription(_: &App) -> Subscription<Message> {
    iced::time::every(REFRESH_EVERY).map(|_| Message::Tick(Utc::now()))
}

pub fn theme(app: &App) -> Theme {
    Tokens::of(app.mode).theme()
}

pub fn title(app: &App) -> String {
    match app.waiting_count() {
        0 => "AXLR Bots".to_string(),
        n => format!("AXLR Bots · {n} need you"),
    }
}

/// Starts the window: the brand's icon, a size that fits the tables, the
/// state the boot function builds.
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
        size: Size::new(1360.0, 860.0),
        min_size: Some(Size::new(1040.0, 640.0)),
        icon,
        platform_specific: window::settings::PlatformSpecific {
            application_id: "axlr-bots-panel".into(),
            ..Default::default()
        },
        ..window::Settings::default()
    })
    .settings(iced::Settings {
        id: Some("axlr-bots-panel".into()),
        ..iced::Settings::default()
    })
    .run()
}
