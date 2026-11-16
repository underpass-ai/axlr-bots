//! The parts every list shares beside its sheet: a box per row and one
//! for all, words cut to a length, a row's acts as icons, and the bar
//! of acts on the picked rows.

use iced::alignment::Vertical;
use iced::widget::{Space, button, container, row, stack, text, tooltip};
use iced::{Alignment, Length, Padding};

use super::widgets::{El, SMALL, action, icon, muted, semibold};
use crate::ui::styles::{self, Kind};
use crate::ui::table::List;
use crate::ui::{App, Icon, Message, Tokens};

/// The width of a row's icon button, and of the room kept for one that
/// the row does not have, so the acts after it line up row under row.
pub const ACT: f32 = 26.0;

/// An act in its fixed slot of a row, or the empty slot.
pub fn act_slot<'a>(act: Option<El<'a>>) -> El<'a> {
    match act {
        Some(act) => container(act).width(ACT).center_x(ACT).into(),
        None => Space::new().width(ACT).into(),
    }
}

/// A row's box, or the header's box for every row shown.
pub fn check<'a>(ticked: bool, on_toggle: impl Fn(bool) -> Message + 'a, tokens: Tokens) -> El<'a> {
    let mark: El<'a> = if ticked {
        icon(Icon::Check, 10.0, tokens.on_accent)
    } else {
        Space::new().width(10).height(10).into()
    };
    button(container(mark).center_x(16).center_y(16))
        .padding(0)
        .style(styles::check(tokens, ticked))
        .on_press(on_toggle(!ticked))
        .into()
}

/// The words cut to `max` characters, the last an ellipsis.
pub fn shorten(words: &str, max: usize) -> String {
    if words.chars().count() <= max {
        return words.to_string();
    }
    let cut: String = words.chars().take(max.saturating_sub(1)).collect();
    format!("{}…", cut.trim_end())
}

/// An icon alone in a button of a kind, with a tip: a row's act where
/// a word would not fit.
pub fn icon_act<'a>(kind: Kind, glyph: Icon, tip: &'a str, on_press: Option<Message>, tokens: Tokens) -> El<'a> {
    tooltip(
        button(icon(glyph, 14.0, super::widgets::ink(kind, tokens)))
            .padding(6)
            .style(styles::button_style(tokens, kind))
            .on_press_maybe(on_press),
        container(text(tip).size(SMALL)).padding(6).style(styles::chip(tokens)),
        tooltip::Position::Bottom,
    )
    .into()
}

/// The bar over a list with picked rows: how many, Clear, and each act
/// that fits the list, saying when it fits only some. Nothing when no
/// row is picked.
pub fn bar<'a>(app: &'a App, list: List, tokens: Tokens) -> Option<El<'a>> {
    let picked = app.tables.picked(list);
    if picked == 0 {
        return None;
    }
    let busy = app.tables.busy;
    // The count and Clear, then the acts; in a narrow window the acts
    // go on to a second line rather than out of sight.
    let mut line = row![
        semibold(format!("{} selected", list.count(picked))),
        action(
            Kind::Ghost,
            Icon::Cross,
            "Clear",
            Some(Message::ClearPicks(list)),
            tokens
        ),
    ]
    .spacing(10)
    .align_y(Alignment::Center);
    if let Some(running) = busy {
        line = line.push(muted(format!("{}…", running.label()), tokens));
    }
    for &act in list.actions() {
        let plan = app.bulk_plan(list, act);
        let kind = if act.destructive() {
            Kind::Danger
        } else {
            Kind::Secondary
        };
        let glyph = match act {
            crate::application::BulkAction::TurnOn => Icon::Play,
            crate::application::BulkAction::Approve => Icon::Check,
            crate::application::BulkAction::Pause => Icon::Pause,
            crate::application::BulkAction::Stop | crate::application::BulkAction::Cancel => Icon::Stop,
            crate::application::BulkAction::Delete => Icon::Trash,
            crate::application::BulkAction::Deny | crate::application::BulkAction::Revoke => Icon::Ban,
        };
        line = line.push(action(
            kind,
            glyph,
            plan.button(),
            (plan.ready() && busy.is_none()).then_some(Message::AskBulk(list, act)),
            tokens,
        ));
    }
    Some(
        container(line.wrap().vertical_spacing(8))
            .padding(Padding::from([10, 14]))
            .max_width(980)
            .style(styles::floating(tokens))
            .into(),
    )
}

/// The room kept under a list's last row for the bar of acts, which
/// floats there once a row is picked: the rows never move as it comes
/// and goes, so the next click lands where it was aimed.
pub const BAR_ROOM: f32 = 88.0;

/// A list's page with the bar of acts on its picked rows floating at
/// its foot, over the rows, not among them. Always two layers, so the
/// page keeps its widgets (where its rows were scrolled to) when the
/// bar comes and goes.
pub fn with_bar<'a>(app: &'a App, list: List, page: El<'a>, tokens: Tokens) -> El<'a> {
    let over: El<'a> = match bar(app, list, tokens) {
        Some(bar) => container(bar)
            .width(Length::Fill)
            .height(Length::Fill)
            .align_x(Alignment::Center)
            .align_y(Vertical::Bottom)
            .padding(Padding {
                right: 24.0,
                bottom: 18.0,
                left: 12.0,
                top: 0.0,
            })
            .into(),
        None => Space::new().into(),
    };
    stack![page, over].width(Length::Fill).height(Length::Fill).into()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn shortens_long_words_with_an_ellipsis() {
        assert_eq!(shorten("worker-1", 10), "worker-1");
        assert_eq!(shorten("web-reporter-with-a-long-name", 10), "web-repor…");
        assert_eq!(shorten("ab cd", 4), "ab…");
    }
}
