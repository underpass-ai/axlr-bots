//! The Iced interface: the state machine (`App`), the effects it asks
//! for and the runtime that carries them out, and the views.

mod answer;
mod app;
#[cfg(test)]
mod app_tests;
mod banner;
mod bot_choice;
mod bot_view;
mod confirm;
mod connection;
mod effect;
pub mod format;
mod icon;
mod launch;
mod message;
mod mint_form;
mod register_form;
mod runtime;
mod screen;
mod services;
mod settings_form;
pub mod styles;
mod submit_form;
mod task_view_filter;
#[cfg(test)]
mod test_runtime;
mod theme_mode;
mod tokens;
mod tone;
pub mod view;
#[cfg(test)]
mod view_tests;

pub use answer::Answer;
pub use app::App;
pub use banner::Banner;
pub use bot_choice::{BotChoice, NodeChoice};
pub use bot_view::BotView;
pub use confirm::Confirm;
pub use connection::Connection;
pub use effect::Effect;
pub use icon::{Icon, brand};
pub use launch::{Command, Launch, USAGE};
pub use message::Message;
pub use mint_form::MintForm;
pub use register_form::RegisterForm;
pub use runtime::{REFRESH_EVERY, launch};
pub use screen::{Screen, Section};
pub use services::Services;
pub use settings_form::SettingsForm;
pub use submit_form::SubmitForm;
pub use task_view_filter::TaskViewFilter;
pub use theme_mode::ThemeMode;
pub use tokens::Tokens;
pub use tone::Tone;
