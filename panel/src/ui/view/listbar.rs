//! The parts over a list's sheet: the bar with its title, its chips and
//! its acts, the note line under it, and the place of a form that is
//! open over the rows.

use iced::widget::{Space, button, column, container, row, scrollable, text};
use iced::{Alignment, Length, Padding};

use super::sheet::SIDE;
use super::widgets::{El, HEADING, SMALL, muted, semibold};
use crate::ui::styles::{self, Kind};
use crate::ui::{Message, Tokens};

/// The bar's height, its line apart.
pub const BAR: f32 = 46.0;

/// The bar over a list: its title with how many it holds, what narrows
/// it at the left and its acts at the right, on one line.
pub fn bar<'a>(title: &'a str, count: String, left: Vec<El<'a>>, right: Vec<El<'a>>, tokens: Tokens) -> El<'a> {
    let mut line = row![
        row![semibold(title).size(HEADING), muted(count, tokens)]
            .spacing(8)
            .align_y(Alignment::End)
    ]
    .spacing(8)
    .align_y(Alignment::Center);
    line = line.push(Space::new().width(4));
    for part in left {
        line = line.push(part);
    }
    line = line.push(Space::new().width(Length::Fill));
    for part in right {
        line = line.push(part);
    }
    ruled(
        container(line)
            .padding(Padding::from([0.0, SIDE]))
            .height(BAR)
            .align_y(Alignment::Center)
            .into(),
        tokens,
    )
}

/// A chip that narrows the list: its word and how many rows it takes.
/// One of several rests while it is on; see `toggle` for one alone.
pub fn chip<'a>(label: &'a str, count: usize, on: bool, press: Message, tokens: Tokens) -> El<'a> {
    toggle(label, count, on, (!on).then_some(press), tokens)
}

/// A chip that stands alone: a press turns it on, another turns it off.
pub fn toggle<'a>(label: &'a str, count: usize, on: bool, press: Option<Message>, tokens: Tokens) -> El<'a> {
    let kind = Kind::Filter(on);
    let ink = if on { styles::accent_ink(tokens) } else { tokens.muted };
    button(
        row![
            semibold(label).size(SMALL),
            text(count.to_string()).size(SMALL).color(ink)
        ]
        .spacing(6)
        .align_y(Alignment::Center),
    )
    .height(28)
    .padding(Padding::from([0, 9]))
    .style(styles::button_style(tokens, kind))
    .on_press_maybe(press)
    .into()
}

/// A line of small words under the bar, with a link at its end when
/// there is something to do about what it says.
pub fn note<'a>(words: String, link: Option<(&'a str, Message)>, tokens: Tokens) -> El<'a> {
    let mut line = row![text(words).size(12.5).color(tokens.muted)]
        .spacing(10)
        .align_y(Alignment::Center);
    if let Some((label, press)) = link {
        line = line.push(
            button(semibold(label).size(12.5).color(styles::accent_ink(tokens)))
                .padding(0)
                .style(styles::button_style(tokens, Kind::Link))
                .on_press(press),
        );
    }
    ruled(container(line).padding(Padding::from([5.0, SIDE])).into(), tokens)
}

/// A form open over a list: it takes the room it needs and scrolls by
/// itself when the window is short, so the page never does.
pub fn panel<'a>(form: El<'a>, tokens: Tokens) -> El<'a> {
    ruled(
        scrollable(container(form).padding(Padding::from([12.0, SIDE])))
            .width(Length::Fill)
            .into(),
        tokens,
    )
}

/// What a list says in the place of its rows when it has none.
pub fn nothing<'a>(content: El<'a>) -> El<'a> {
    container(content).width(Length::Fill).height(Length::Fill).into()
}

/// A part of the list's head with the line under it.
fn ruled<'a>(part: El<'a>, tokens: Tokens) -> El<'a> {
    column![
        part,
        container(Space::new().width(Length::Fill).height(1)).style(styles::rule(tokens))
    ]
    .into()
}
