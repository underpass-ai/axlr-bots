//! The parts every list's table shares: a header that sorts, a box per
//! row and one for all, a cell that keeps to its width, and the bar of
//! acts on the picked rows.

use iced::alignment::Vertical;
use iced::widget::text::Wrapping;
use iced::widget::{Space, button, container, responsive, row, stack, table, text, tooltip};
use iced::{Alignment, Length, Padding};

use super::widgets::{BODY, El, SMALL, action, bold, icon, muted, semibold};
use crate::ui::styles::{self, Kind};
use crate::ui::table::{List, SortBadge, SortDirection};
use crate::ui::{App, Icon, Message, Tokens};

/// A column's header: its words, and when it sorts the table an arrow
/// with its place among the sorting columns. A click sorts by it alone;
/// Shift+click adds it.
pub fn header<'a>(label: &'a str, badge: Option<SortBadge>, press: Message, tokens: Tokens) -> El<'a> {
    let mut words = row![bold(label)].spacing(4).align_y(Alignment::Center);
    if let Some(badge) = badge {
        let glyph = match badge.direction {
            SortDirection::Ascending => Icon::Up,
            SortDirection::Descending => Icon::Down,
        };
        words = words.push(icon(glyph, 14.0, tokens.accent));
        if badge.several {
            words = words.push(semibold(badge.order.to_string()).size(12.0).color(tokens.accent));
        }
    }
    let tip = match badge {
        Some(b) => format!(
            "Sorted {}. Click to turn it; Shift+click to add a column.",
            b.direction.describe()
        ),
        None => "Click to sort by it; Shift+click to add it to the order.".to_string(),
    };
    tooltip(
        button(words)
            .padding(Padding::from([2, 4]))
            .style(styles::button_style(tokens, Kind::Ghost))
            .on_press(press),
        container(text(tip).size(SMALL)).padding(6).style(styles::chip(tokens)),
        tooltip::Position::Bottom,
    )
    .into()
}

/// A list's table in the house style: rows parted by a hairline only,
/// no rule between columns, every cell on its row's middle line, so a
/// pill, a name and a button sit level.
pub fn sheet<'a, T: Clone>(columns: Vec<table::Column<'a, 'a, T, Message>>, rows: impl IntoIterator<Item = T>) -> El<'a> {
    table::table(columns.into_iter().map(|c| c.align_y(Vertical::Center)), rows)
        .padding_x(8)
        .padding_y(ROW_PAD)
        .separator_x(0)
        .width(Length::Fill)
        .into()
}

/// The room above and under a table row's words.
pub const ROW_PAD: f32 = 10.0;

/// The width of a row's icon button, and of the room kept for one that
/// the row does not have, so the acts after it line up row under row.
pub const ACT: f32 = 32.0;

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
        icon(Icon::Check, 12.0, tokens.on_accent)
    } else {
        Space::new().width(12).height(12).into()
    };
    button(container(mark).center_x(18).center_y(18))
        .padding(0)
        .style(styles::check(tokens, ticked))
        .on_press(on_toggle(!ticked))
        .into()
}

/// Words that keep to one line: cut to `max` characters with an
/// ellipsis, the whole words on hover when cut.
pub fn clipped<'a>(words: String, max: usize, strong: bool, tokens: Tokens) -> El<'a> {
    let short = shorten(&words, max);
    let cut = short != words;
    let line = if strong { bold(short) } else { text(short) };
    let line = container(line.wrapping(Wrapping::None)).clip(true);
    if !cut {
        return line.into();
    }
    tooltip(
        line,
        container(text(words).size(SMALL))
            .padding(6)
            .style(styles::chip(tokens)),
        tooltip::Position::Bottom,
    )
    .into()
}

/// Words on one line as wide as their column lets them be: cut with an
/// ellipsis to the characters that fit its width, whole on hover when
/// cut. For a column that shares the table's width (`FillPortion`).
pub fn fitted<'a>(words: String, strong: bool, tokens: Tokens) -> El<'a> {
    responsive(move |space| clipped(words.clone(), fits(space.width, strong), strong, tokens))
        .height(Length::Shrink)
        .into()
}

/// A secondary line, small and muted, as wide as its column lets it
/// be: cut with an ellipsis, whole on hover when cut.
pub fn fitted_muted<'a>(words: String, tokens: Tokens) -> El<'a> {
    responsive(move |space| {
        let fit = ((space.width / (0.57 * SMALL)).floor() as usize).max(4);
        let short = shorten(&words, fit);
        let line: El<'a> = container(muted(short.clone(), tokens).wrapping(Wrapping::None))
            .clip(true)
            .into();
        if short == words {
            return line;
        }
        tooltip(
            line,
            container(text(words.clone()).size(SMALL))
                .padding(6)
                .style(styles::chip(tokens)),
            tooltip::Position::Bottom,
        )
        .into()
    })
    .height(Length::Shrink)
    .into()
}

/// How many characters of Lexend at the body size fit `width`: a bold
/// one is about 0.62 em wide on average, a regular one 0.57.
pub fn fits(width: f32, strong: bool) -> usize {
    let em = if strong { 0.62 } else { 0.57 } * BODY;
    ((width / em).floor() as usize).max(4)
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
        button(icon(glyph, 16.0, super::widgets::ink(kind, tokens)))
            .padding(7)
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
            crate::application::BulkAction::Resolve => Icon::Check,
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
/// its foot, over the rows, not among them.
pub fn with_bar<'a>(app: &'a App, list: List, page: El<'a>, tokens: Tokens) -> El<'a> {
    match bar(app, list, tokens) {
        Some(bar) => stack![
            page,
            container(bar)
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
        ]
        .width(Length::Fill)
        .height(Length::Fill)
        .into(),
        None => page,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn shortens_long_words_with_an_ellipsis() {
        assert_eq!(shorten("worker-1", 10), "worker-1");
        assert_eq!(shorten("qa-reporter-with-a-long-name", 10), "qa-report…");
        assert_eq!(shorten("ab cd", 4), "ab…");
    }

    #[test]
    fn a_wider_column_fits_more_characters() {
        assert_eq!(fits(100.0, false), 10);
        assert!(fits(300.0, true) < fits(300.0, false));
        assert_eq!(fits(0.0, true), 4);
    }
}
