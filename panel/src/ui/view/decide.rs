//! The two answers to a call waiting for a person, wherever the task is
//! listed.

use iced::widget::row;

use super::widgets::{El, action};
use crate::domain::TaskId;
use crate::ui::styles::Kind;
use crate::ui::{App, Icon, Message, Tokens};

/// Approve at once, or Deny after a second click; both wait while an
/// answer is on its way.
pub fn buttons<'a>(app: &'a App, id: &TaskId, tokens: Tokens) -> El<'a> {
    let free = app.deciding.is_none();
    row![
        action(
            Kind::Good,
            Icon::Check,
            "Approve",
            free.then(|| Message::Approve(id.clone())),
            tokens
        ),
        action(
            Kind::Danger,
            Icon::Cross,
            "Deny",
            free.then(|| Message::AskDeny(id.clone())),
            tokens
        ),
    ]
    .spacing(8)
    .into()
}
