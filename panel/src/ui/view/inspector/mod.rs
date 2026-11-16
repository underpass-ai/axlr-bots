//! The inspector beside a list: the row's header with its acts, its
//! facts, its tabs and the tab open, of which only the body scrolls. A
//! task, a bot, a node or a team; the list stays beside it, narrower.

mod bot;
mod node;
mod steps;
mod task;
mod team;
mod tree;

use iced::widget::text::Wrapping;
use iced::widget::{Space, button, column, container, responsive, row, scrollable, text};
use iced::{Alignment, Font, Length, Padding};

use super::grid;
use super::widgets::{El, INSPECTOR, SMALL, icon, icon_button, label, semibold};
use crate::ui::inspector::Tab;
use crate::ui::styles::{self, Kind};
use crate::ui::{App, Icon, Message, Screen, Tokens};

/// The inspector's width in the window at start; a narrower window
/// gives it half the room beside the rail.
pub const WIDE: f32 = 640.0;

/// The room at each side of the inspector's parts.
const SIDE: f32 = 14.0;
/// The room between two columns of facts.
const FACT_GAP: f32 = 14.0;
/// How wide an average character of a fact is, in its size.
const EM: f32 = 0.54;
/// The size of the title.
const TITLE: f32 = 16.0;
/// How wide an average character of a tab's semibold word is, in its
/// size.
const TAB_EM: f32 = 0.6;

/// How wide the inspector is when the page has `room`.
pub fn width(room: f32) -> f32 {
    WIDE.min((room / 2.0).floor())
}

/// The list on show and, when a row of it is inspected, the inspector
/// beside it. The same widgets either way, so the list keeps where its
/// rows were scrolled to as the inspector opens and closes.
pub fn beside<'a>(app: &'a App, tokens: Tokens, list: impl Fn() -> El<'a> + 'a) -> El<'a> {
    responsive(move |size| {
        let open = app.screen.inspected().is_some();
        let wide = if open { width(size.width) } else { 0.0 };
        let pane: El<'a> = if open {
            pane(app, wide, tokens)
        } else {
            Space::new().into()
        };
        row![
            container(list()).width(Length::Fill).height(Length::Fill),
            container(Space::new().width(if open { 1 } else { 0 }).height(Length::Fill)).style(styles::rule(tokens)),
            container(pane)
                .width(wide)
                .height(Length::Fill)
                .style(styles::raised(tokens)),
        ]
        .into()
    })
    .into()
}

/// The inspector of the row the screen names, `width` wide.
fn pane<'a>(app: &'a App, width: f32, tokens: Tokens) -> El<'a> {
    match &app.screen {
        Screen::Task(id) => task::view(app, id, width, tokens),
        Screen::Bot(id) => bot::view(app, id, width, tokens),
        Screen::Node(id) => node::view(app, id, width, tokens),
        Screen::Team(name) => team::view(app, name, width, tokens),
        _ => Space::new().into(),
    }
}

/// The header of the inspector: its first line (the status, who, what
/// started it) with the acts at its right and the close button, then
/// the title on at most two lines and, under it, a line of what the
/// title does not say.
fn head<'a>(
    first: Vec<El<'a>>,
    acts: Vec<El<'a>>,
    close: Option<Message>,
    title: &str,
    under: Option<&str>,
    width: f32,
    tokens: Tokens,
) -> El<'a> {
    // The acts keep their room; what is before them takes the rest and
    // is cut at its edge in a narrow window.
    let mut line = row![
        container(row(first).spacing(8).align_y(Alignment::Center))
            .width(Length::Fill)
            .clip(true)
    ]
    .spacing(6)
    .align_y(Alignment::Center);
    for act in acts {
        line = line.push(act);
    }
    if let Some(close) = close {
        line = line.push(icon_button(Icon::Cross, "Close (Esc)", Some(close), tokens));
    }
    let room = width - 2.0 * SIDE;
    let fits = ((room / (EM * TITLE)).floor() as usize).max(8);
    let mut body = column![
        line,
        semibold(grid::shorten(title.trim(), 2 * fits))
            .size(TITLE)
            .wrapping(Wrapping::WordOrGlyph)
            .width(Length::Fill),
    ]
    .spacing(6);
    if let Some(under) = under.map(str::trim).filter(|u| !u.is_empty()) {
        let first_line = under.lines().next().unwrap_or(under);
        body = body.push(
            container(
                text(grid::shorten(
                    first_line,
                    ((room / (EM * SMALL)).floor() as usize).max(8),
                ))
                .size(SMALL)
                .color(tokens.muted)
                .wrapping(Wrapping::None),
            )
            .clip(true),
        );
    }
    ruled(
        container(body)
            .padding(Padding {
                top: 10.0,
                right: SIDE,
                bottom: 8.0,
                left: SIDE,
            })
            .width(Length::Fill)
            .into(),
        tokens,
    )
}

/// The close button's message: back to the list alone.
fn close_of(app: &App) -> Option<Message> {
    app.screen.list().map(|list| Message::Go(Screen::of_list(list)))
}

/// The facts of a row in four columns, label over value, each value on
/// one line cut to its column.
fn facts<'a>(cells: Vec<(&'static str, Fact)>, width: f32, tokens: Tokens) -> El<'a> {
    let column_width = ((width - 2.0 * SIDE - 3.0 * FACT_GAP) / 4.0).floor().max(40.0);
    let mut grid = column![].spacing(4);
    for line in cells.chunks(4) {
        let mut cells_row = row![].spacing(FACT_GAP);
        for (name, value) in line {
            cells_row = cells_row.push(
                column![
                    label(name, tokens).size(11.0).wrapping(Wrapping::None),
                    value.draw(column_width, tokens)
                ]
                .width(column_width),
            );
        }
        grid = grid.push(cells_row);
    }
    ruled(
        container(grid)
            .padding(Padding::from([8.0, SIDE]))
            .width(Length::Fill)
            .into(),
        tokens,
    )
}

/// A value among the facts: words, or an id in the fixed face with a
/// button that copies it whole.
enum Fact {
    Words(String),
    Copy(String),
}

impl Fact {
    fn draw<'a>(&self, width: f32, tokens: Tokens) -> El<'a> {
        match self {
            Self::Words(words) => {
                let fits = ((width / (EM * SMALL)).floor() as usize).max(4);
                text(grid::shorten(words, fits))
                    .size(SMALL)
                    .wrapping(Wrapping::None)
                    .into()
            }
            Self::Copy(value) if value.is_empty() => text("—").size(SMALL).into(),
            Self::Copy(value) => {
                let fits = (((width - 20.0) / (0.61 * 12.5)).floor() as usize).max(4);
                row![
                    text(grid::shorten(value, fits))
                        .size(12.5)
                        .font(Font::MONOSPACE)
                        .wrapping(Wrapping::None),
                    button(icon(Icon::Copy, 12.0, tokens.muted))
                        .padding(2)
                        .style(styles::button_style(tokens, Kind::Ghost))
                        .on_press(Message::Copy(value.clone())),
                ]
                .spacing(4)
                .align_y(Alignment::Center)
                .into()
            }
        }
    }
}

/// The tabs of the row's kind, the open one in ink over the accent's
/// line, each with its count when it has one.
fn tabs<'a>(open: Tab, count: impl Fn(Tab) -> Option<usize>, tokens: Tokens) -> El<'a> {
    let mut line = row![].spacing(2);
    for tab in open.all() {
        let on = tab == open;
        let ink = if on { tokens.text } else { tokens.muted };
        let mut words = row![semibold(tab.label()).size(13.5).color(ink)]
            .spacing(5)
            .align_y(Alignment::Center);
        // The accent's line under the open tab is as wide as its words.
        let mut wide = tab.label().chars().count() as f32 * 13.5 * TAB_EM + 18.0;
        if let Some(n) = count(tab) {
            let n = n.to_string();
            wide += n.chars().count() as f32 * 12.0 * 0.6 + 5.0;
            words = words.push(text(n).size(12.0).color(tokens.muted));
        }
        let mark = container(Space::new().width(wide).height(2)).style(move |_| container::Style {
            background: on.then(|| tokens.accent.into()),
            ..container::Style::default()
        });
        line = line.push(
            button(column![
                container(words.wrap()).padding(Padding::from([9, 9]).bottom(7)),
                mark
            ])
            .padding(0)
            .style(styles::button_style(tokens, Kind::Link))
            .on_press(Message::Tab(tab)),
        );
    }
    ruled(container(line).padding(Padding::from([0, 10])).into(), tokens)
}

/// A tab's body: padded, and the only part that scrolls.
fn body<'a>(content: impl Into<El<'a>>) -> El<'a> {
    scrollable(
        container(content)
            .padding(Padding::from([12.0, SIDE]))
            .width(Length::Fill),
    )
    .id(INSPECTOR)
    .width(Length::Fill)
    .height(Length::Fill)
    .into()
}

/// A part of a tab: its small heading over what it holds.
fn part<'a>(heading: &str, content: impl Into<El<'a>>, tokens: Tokens) -> El<'a> {
    column![label(heading, tokens).size(11.0), content.into()]
        .spacing(6)
        .width(Length::Fill)
        .into()
}

/// Words as small tags in the fixed face, on as many lines as they take.
fn tags<'a>(words: impl IntoIterator<Item = String>, tokens: Tokens) -> El<'a> {
    let mut line = row![].spacing(5);
    for word in words {
        line = line.push(
            container(text(word).size(12.5).font(Font::MONOSPACE))
                .padding(Padding::from([1, 7]))
                .style(styles::chip(tokens)),
        );
    }
    line.wrap().vertical_spacing(5).into()
}

/// A line of a list inside a tab, 30 px, with its hairline: a press
/// opens what it names.
fn mini<'a>(cells: El<'a>, press: Option<Message>, tokens: Tokens) -> El<'a> {
    let line = button(container(cells).height(Length::Fill).align_y(Alignment::Center))
        .width(Length::Fill)
        .height(29)
        .padding(Padding::from([0, 2]))
        .style(styles::row(tokens, false))
        .on_press_maybe(press);
    column![
        line,
        container(Space::new().width(Length::Fill).height(1)).style(styles::hairline(tokens))
    ]
    .into()
}

/// A part of the inspector with the line under it.
fn ruled<'a>(part: El<'a>, tokens: Tokens) -> El<'a> {
    column![
        part,
        container(Space::new().width(Length::Fill).height(1)).style(styles::rule(tokens))
    ]
    .into()
}
