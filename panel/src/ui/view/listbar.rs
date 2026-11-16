//! The parts over a list's sheet: the bar with its title, its chips and
//! its acts, the note line under it, and the place of a form that is
//! open over the rows.

use iced::widget::{Space, button, column, container, row, scrollable, stack, text, text_input};
use iced::{Alignment, Length, Padding};

use super::sheet::SIDE;
use super::widgets::{BODY, El, HEADING, SMALL, icon, key_cap, muted, semibold};
use crate::ui::styles::{self, Kind};
use crate::ui::{Field, Icon, Message, Tokens};

/// The bar's height while its chips fit one line, its line apart.
pub const BAR: f32 = 46.0;

/// The height of a chip.
const CHIP: f32 = 28.0;

/// The height of an act beside the chips: a button, a picker.
const ACT: f32 = 30.0;

/// The width of the filter field.
const FILTER: f32 = 210.0;

/// The bar over a list: its title with how many it holds, what narrows
/// it at the left and its acts at the right, on one line. The chips
/// have the room the title and the acts leave and go on to a second
/// line in a narrow window, so the acts never leave it.
pub fn bar<'a>(title: &'a str, count: String, left: Vec<El<'a>>, right: Vec<El<'a>>, tokens: Tokens) -> El<'a> {
    let mut line = row![
        row![semibold(title).size(HEADING), muted(count, tokens)]
            .spacing(8)
            .align_y(Alignment::End),
        Space::new().width(4),
        container(row(left).spacing(8).wrap().vertical_spacing(6)).width(Length::Fill),
    ]
    .spacing(8)
    .align_y(Alignment::Center);
    for part in right {
        line = line.push(part);
    }
    ruled(
        container(line)
            .padding(Padding::from([(BAR - ACT) / 2.0, SIDE]))
            .align_y(Alignment::Center)
            .into(),
        tokens,
    )
}

/// The filter field of a list: a lens, what is typed (or `Filter…`),
/// and the `/` key that puts the keyboard in it. It filters as it is
/// typed into; Esc empties it as it leaves it. It sends nothing on
/// Enter, so Enter and the arrows reach the list's cursor.
pub fn filter<'a>(query: &str, tokens: Tokens) -> El<'a> {
    let field = text_input("Filter…", query)
        .id(Field::ListFilter.id())
        .on_input(Message::ListFilterTyped)
        .size(BODY)
        .padding(Padding {
            top: 5.0,
            bottom: 5.0,
            left: 28.0,
            right: 30.0,
        })
        .style(styles::field(tokens));
    // The lens and the key over the field's ends: neither takes a click.
    let ends = row![
        icon(Icon::Lens, 14.0, tokens.muted),
        Space::new().width(Length::Fill),
        key_cap("/", tokens)
    ]
    .align_y(Alignment::Center)
    .height(ACT)
    .padding(Padding::from([0, 8]));
    container(stack![field, ends]).width(FILTER).height(ACT).into()
}

/// The keys a list answers to, for the right of its note line:
/// `↑ ↓ move · Enter open · Esc close`.
pub fn keys<'a>(tokens: Tokens) -> El<'a> {
    let words = |w: &'a str| text(w).size(12.5).color(tokens.muted);
    row![
        key_cap("↑", tokens),
        key_cap("↓", tokens),
        words("move ·"),
        key_cap("Enter", tokens),
        words("open ·"),
        key_cap("Esc", tokens),
        words("close"),
    ]
    .spacing(4)
    .align_y(Alignment::Center)
    .into()
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
        .height(Length::Fill)
        .align_y(Alignment::Center),
    )
    .height(CHIP)
    .padding(Padding::from([0, 9]))
    .style(styles::button_style(tokens, kind))
    .on_press_maybe(press)
    .into()
}

/// The height of a list option on the note line.
pub const OPTION: f32 = 24.0;

/// A list option as a small chip for the note line: its words, and its
/// value in semibold after them when it has one (`Group: trigger`); in
/// the accent while it is on. A press changes it.
pub fn flag<'a>(label: &'a str, value: Option<&'a str>, on: bool, press: Message, tokens: Tokens) -> El<'a> {
    let mut words = row![text(label).size(12.5)]
        .spacing(4)
        .height(Length::Fill)
        .align_y(Alignment::Center);
    if let Some(value) = value {
        words = words.push(semibold(value).size(12.5));
    }
    button(words)
        .height(OPTION)
        .padding(Padding::from([0, 8]))
        .style(styles::button_style(tokens, Kind::Filter(on)))
        .on_press(press)
        .into()
}

/// A line of small words under the bar, with a link after them when
/// there is something to do about what they say, the list's options
/// after that (small chips, so the bar keeps its chips on one line),
/// and at its right end what goes there (the keys the list answers to).
pub fn note<'a>(
    words: String,
    link: Option<(&'a str, Message)>,
    options: Vec<El<'a>>,
    right: Option<El<'a>>,
    tokens: Tokens,
) -> El<'a> {
    let mut line = row![text(words).size(12.5).color(tokens.muted)]
        .spacing(10)
        .align_y(Alignment::Center);
    if !options.is_empty() {
        line = line.push(row(options).spacing(6).align_y(Alignment::Center));
    }
    if let Some((label, press)) = link {
        line = line.push(
            button(semibold(label).size(12.5).color(styles::accent_ink(tokens)))
                .padding(0)
                .style(styles::button_style(tokens, Kind::Link))
                .on_press(press),
        );
    }
    if let Some(right) = right {
        line = line.push(Space::new().width(Length::Fill)).push(right);
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
