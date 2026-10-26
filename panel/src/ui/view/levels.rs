//! How much a bot may do alone, in a person's words: the choices the bot
//! page and the New bot form offer.

use iced::widget::{button, column, row, text};
use iced::{Alignment, Length};

use super::widgets::{El, SMALL, icon, semibold};
use crate::domain::AutonomyLevel;
use crate::ui::styles;
use crate::ui::{Icon, Message, Tokens};

/// The levels offered; Propose shows only on a bot that has it.
pub const CHOICES: [AutonomyLevel; 3] = [
    AutonomyLevel::ActWithApproval,
    AutonomyLevel::ActWithinPolicy,
    AutonomyLevel::Observe,
];

/// One level as a card to pick: its name in plain words and what it
/// means; the picked one is lit and has no act left.
pub fn choice<'a>(level: AutonomyLevel, picked: bool, on_press: Option<Message>, tokens: Tokens) -> El<'a> {
    let (name, words) = plain(level);
    button(
        row![
            icon(
                if picked { Icon::Check } else { Icon::Dot },
                16.0,
                if picked { tokens.accent } else { tokens.muted }
            ),
            column![semibold(name), text(words).size(SMALL).color(tokens.muted)].spacing(2),
        ]
        .spacing(10)
        .align_y(Alignment::Center),
    )
    .padding(12)
    .width(Length::Fill)
    .style(styles::choice(tokens, picked))
    .on_press_maybe(on_press)
    .into()
}

/// A level as a person says it.
pub fn plain(level: AutonomyLevel) -> (&'static str, &'static str) {
    match level {
        AutonomyLevel::ActWithApproval => (
            "Ask me before acting",
            "It reads and plans alone. Anything that changes the world waits for your OK.",
        ),
        AutonomyLevel::ActWithinPolicy => (
            "Act on its own within its rules",
            "It acts alone. Its tool rules still forbid what you forbade.",
        ),
        AutonomyLevel::Observe => ("Only watch and report", "It never acts. It tells you what it sees."),
        AutonomyLevel::Propose => ("Propose only", "It reads and proposes. A person acts."),
        AutonomyLevel::Unknown => ("A level this panel does not know", "Pick one of the others to set it."),
    }
}
