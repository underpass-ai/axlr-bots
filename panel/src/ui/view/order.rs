//! An urgent order to a bot: the button that asks for it, wherever the
//! bot is listed, and the form on the bot's page that sends it.

use iced::widget::{column, row};

use super::widgets::{El, action, field, muted};
use crate::domain::Bot;
use crate::ui::styles::Kind;
use crate::ui::{App, Icon, Message, Tokens};

/// Urgent order, for any bot that takes tasks; it waits while another
/// act on a bot is on its way, and while its form is open.
pub fn button<'a>(app: &'a App, bot: &Bot, tokens: Tokens) -> El<'a> {
    let free = !app.switching && bot.status.accepts_tasks() && !app.order.is_open_for(&bot.id);
    action(
        Kind::Secondary,
        Icon::Alert,
        "Urgent order",
        free.then(|| Message::OrderOpen(bot.id.clone())),
        tokens,
    )
}

/// The order in the operator's words; it goes only with words in it.
pub fn form<'a>(app: &'a App, bot: &Bot, tokens: Tokens) -> El<'a> {
    let form = &app.order;
    let ready = form.ready() && !app.switching && bot.status.accepts_tasks();
    column![
        field(
            "Order",
            "what it must do now, in one line",
            &form.text,
            Message::OrderText,
            ready.then_some(Message::OrderSend),
            tokens
        ),
        muted("It goes first: the bot stops what it is doing and does this.", tokens),
        row![
            action(
                Kind::Primary,
                Icon::Bolt,
                if app.switching { "Sending…" } else { "Send the order" },
                ready.then_some(Message::OrderSend),
                tokens
            ),
            action(Kind::Ghost, Icon::Cross, "Cancel", Some(Message::OrderClose), tokens),
        ]
        .spacing(10),
    ]
    .spacing(10)
    .into()
}
