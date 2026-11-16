//! A task's Activity tab: one line per step in the order it happened
//! (a tool call with why it was made and how long it took, an event it
//! emitted, what it said), the find over it with its matches, the chips
//! that narrow it, a step opened in place, and a running task followed
//! at its newest step until the person scrolls up.

use iced::widget::text::{Span, Wrapping};
use iced::widget::{Space, button, column, container, rich_text, row, scrollable, span, stack, text, text_input};
use iced::{Alignment, Color, Font, Length, Padding};

use super::super::widgets::{El, REGULAR, SMALL, empty, icon, key_cap, label, step_id};
use super::super::{grid, listbar};
use crate::application::{ActivityEntry, DelegationActivity, ToolActivity, ToolState};
use crate::domain::TaskStatus;
use crate::ui::find::{Query, StepFilter, StepText};
use crate::ui::format;
use crate::ui::styles::{self, Kind};
use crate::ui::{App, Field, Icon, LiveStream, LiveTask, Message, Screen, Tokens, Tone};

/// The height of a step's line.
const LINE: f32 = 30.0;
/// The columns of a step's line: its time, its glyph, its tool, and how
/// long it took with its bar.
const TIME: f32 = 58.0;
const GLYPH: f32 = 16.0;
const TOOL: f32 = 150.0;
const TOOK: f32 = 92.0;
/// The longest bar of a duration, for the longest call.
const BAR: f32 = 40.0;
/// How close to its end the steps count as showing it.
const AT_END: f32 = 24.0;
/// How much of the model's words a line shows; its details show them all.
const WORDS_SHOWN: usize = 600;
/// The width of the find's field.
const FIND: f32 = 200.0;
/// The narrowest inspector whose find and chips share one line.
const ONE_LINE: f32 = 620.0;
/// The room the scroll bar takes at the right of the steps.
const SCROLL_BAR: f32 = 10.0;
/// How wide an average character of a step's words is, in its size.
const EM: f32 = 0.53;

/// The Activity tab of the task inspected: the find and the chips over
/// the steps, the steps alone scrolling under them.
pub fn view<'a>(app: &'a App, status: TaskStatus, width: f32, tokens: Tokens) -> El<'a> {
    let live = match &app.screen {
        Screen::Task(id) => app.live.as_ref().filter(|l| &l.id == id),
        _ => None,
    };
    let steps = app.steps_text();
    let column = column![bar(app, &steps, width, tokens)];
    let Some(live) = live.filter(|l| l.read) else {
        return column
            .push(empty(Icon::Hourglass, "Reading what it does…", tokens))
            .into();
    };
    if live.activity.entries.is_empty() {
        let words = if status.is_terminal() {
            "It told nothing of what it did"
        } else {
            "Nothing yet: no tool call and no words"
        };
        return column.push(empty(Icon::Hourglass, words, tokens)).into();
    }
    column.push(stream(app, live, &steps, status, width, tokens)).into()
}

/// The bar over the steps: the find with where it is among its matches
/// and its arrows, then the chips with their counts.
fn bar<'a>(app: &'a App, steps: &[StepText], width: f32, tokens: Tokens) -> El<'a> {
    let find = &app.find;
    let field = text_input("Find in activity", find.typed())
        .id(Field::ActivityFind.id())
        .on_input(Message::FindTyped)
        .on_submit(Message::FindSubmit)
        .size(SMALL)
        .padding(Padding {
            top: 5.0,
            bottom: 5.0,
            left: 28.0,
            right: 52.0,
        })
        .style(styles::field(tokens));
    let ends = row![
        icon(Icon::Lens, 14.0, tokens.muted),
        Space::new().width(Length::Fill),
        key_cap("Ctrl F", tokens)
    ]
    .align_y(Alignment::Center)
    .height(28)
    .padding(Padding::from([0, 8]));
    let find_part = row![
        container(stack![field, ends]).width(FIND).height(28),
        container(text(find.label(steps)).size(12.0).color(tokens.muted)).center_x(36),
        small_button(Icon::Up, "Previous match (Shift+Enter)", Message::FindPrevious, tokens),
        small_button(Icon::Down, "Next match (Enter)", Message::FindNext, tokens),
    ]
    .spacing(4)
    .align_y(Alignment::Center);
    let mut chips = row![].spacing(6).align_y(Alignment::Center);
    for (kind, count) in StepFilter::chips(steps) {
        chips = chips.push(listbar::chip(
            kind.label(),
            count,
            find.kind() == kind,
            Message::FindShow(kind),
            tokens,
        ));
    }
    // On one line in the window at start, the chips at its right; in a
    // narrower one they go on to a line of their own.
    let line: El<'a> = if width >= ONE_LINE {
        row![find_part, Space::new().width(Length::Fill), chips]
            .align_y(Alignment::Center)
            .into()
    } else {
        column![find_part, chips].spacing(6).into()
    };
    super::ruled(
        container(line)
            .padding(Padding::from([7, 10]))
            .width(Length::Fill)
            .into(),
        tokens,
    )
}

/// A small icon button with a tip, for the bar over the steps.
fn small_button<'a>(glyph: Icon, tip: &'a str, press: Message, tokens: Tokens) -> El<'a> {
    iced::widget::tooltip(
        button(icon(glyph, 14.0, tokens.text))
            .padding(6)
            .style(styles::button_style(tokens, Kind::Ghost))
            .on_press(press),
        container(text(tip).size(SMALL)).padding(6).style(styles::chip(tokens)),
        iced::widget::tooltip::Position::Bottom,
    )
    .into()
}

/// The steps the chip shows, the find's matches washed and the current
/// one marked, the open one with its details under it; on a running
/// task that the person scrolled up in, Jump to latest over its foot.
fn stream<'a>(
    app: &'a App,
    live: &'a LiveTask,
    steps: &[StepText],
    status: TaskStatus,
    width: f32,
    tokens: Tokens,
) -> El<'a> {
    let find = &app.find;
    let query = find.query();
    let hits = find.hits(steps);
    let current = find.current(steps);
    let longest = live
        .activity
        .entries
        .iter()
        .filter_map(|entry| match entry {
            ActivityEntry::Tool(call) => call.duration_ms,
            _ => None,
        })
        .max()
        .unwrap_or(1);
    // The room of a step's words: what the line leaves after its time,
    // glyph, tool and duration.
    let room = (width - 20.0 - SCROLL_BAR - TIME - GLYPH - TOOL - TOOK - 4.0 * 8.0).max(60.0);
    let mut list = column![];
    for at in find.shown(steps) {
        let entry = &live.activity.entries[at];
        let mark = Mark {
            hit: hits.contains(&at),
            current: current == Some(at),
            open: live.selected == Some(at),
        };
        let mut lines = column![step(at, entry, mark, &query, longest, room, tokens)];
        if mark.open {
            lines = lines.push(details(entry, tokens));
        }
        list = list.push(container(lines).id(step_id(at)));
    }
    if live.activity.dropped > 0 {
        list = list.push(note(
            format!("{} steps were lost on the way from its node", live.activity.dropped),
            tokens,
        ));
    }
    if live.activity.truncated {
        list = list.push(note(
            "Its later steps were not kept: the run told too many".to_string(),
            tokens,
        ));
    }
    // Every step, its words and its thinking, to copy at once.
    list = list.push(
        container(super::super::widgets::copy_button(
            "Copy activity",
            format::activity(&live.activity),
            tokens,
        ))
        .padding(Padding::from([6, 6])),
    );
    let steps_box = scrollable(list.width(Length::Fill))
        .direction(iced::widget::scrollable::Direction::Vertical(
            iced::widget::scrollable::Scrollbar::new()
                .width(6)
                .scroller_width(6)
                .margin(2),
        ))
        .id(LiveStream::Steps.id())
        .width(Length::Fill)
        .height(Length::Fill)
        .on_scroll(|view| {
            let left = view.content_bounds().height - view.bounds().height - view.absolute_offset().y;
            Message::ActivityScrolled(LiveStream::Steps, left <= AT_END)
        });
    let jump: El<'a> = if !status.is_terminal() && !live.follows(LiveStream::Steps) {
        container(
            button(
                text("↓ Jump to latest")
                    .size(12.5)
                    .font(super::super::widgets::SEMIBOLD),
            )
            .padding(Padding::from([5, 12]))
            .style(styles::button_style(tokens, Kind::Primary))
            .on_press(Message::JumpToLatest),
        )
        .width(Length::Fill)
        .height(Length::Fill)
        .align_x(Alignment::Center)
        .align_y(Alignment::End)
        .padding(Padding::ZERO.bottom(10))
        .into()
    } else {
        Space::new().into()
    };
    stack![steps_box, jump].width(Length::Fill).height(Length::Fill).into()
}

/// How a line stands to the find and to the person: a match, the match
/// the find is at, open.
#[derive(Debug, Clone, Copy)]
struct Mark {
    hit: bool,
    current: bool,
    open: bool,
}

/// One step on its line; a click opens its details in place.
fn step<'a>(
    at: usize,
    entry: &'a ActivityEntry,
    mark: Mark,
    query: &Query,
    longest: u64,
    room: f32,
    tokens: Tokens,
) -> El<'a> {
    // Words cut with an ellipsis to the characters that fit a width.
    let fit =
        |words: String, width: f32, size: f32| grid::shorten(&words, ((width / (EM * size)).floor() as usize).max(4));
    let time = text(format::clock(entry.at()))
        .size(12.0)
        .color(tokens.muted)
        .wrapping(Wrapping::None);
    let (glyph, ink) = glyph(entry, tokens);
    let mut cells = row![
        container(time).width(TIME),
        container(icon(glyph, 12.0, ink)).width(GLYPH),
    ]
    .spacing(8)
    .align_y(Alignment::Center);
    let mut tall = false;
    cells = match entry {
        ActivityEntry::Tool(call) => {
            let failed = call.state == ToolState::Failed;
            let name_ink = if failed {
                styles::pill_ink(tokens, Tone::Bad)
            } else {
                tokens.text
            };
            let what = match call.why() {
                Some(why) => cut(marked(
                    fit(why, room, SMALL),
                    query,
                    SMALL,
                    REGULAR,
                    tokens.text,
                    tokens,
                )),
                None => cut(marked(
                    fit(call.summary.clone(), room, 12.5 * 1.15),
                    query,
                    12.5,
                    Font::MONOSPACE,
                    tokens.muted,
                    tokens,
                )),
            };
            cells
                .push(
                    container(cut(marked(
                        fit(call.name().to_string(), TOOL, SMALL * 1.15),
                        query,
                        SMALL,
                        Font::MONOSPACE,
                        name_ink,
                        tokens,
                    )))
                    .width(TOOL),
                )
                .push(container(what).width(Length::Fill))
                .push(took(call, longest, tokens))
        }
        ActivityEntry::Emitted(event) => cells
            .push(
                container(cut(marked(
                    fit(event.name.clone(), TOOL, SMALL * 1.15),
                    query,
                    SMALL,
                    Font::MONOSPACE,
                    tokens.text,
                    tokens,
                )))
                .width(TOOL),
            )
            .push(
                container(cut(marked(
                    fit(event.message.clone(), room, SMALL),
                    query,
                    SMALL,
                    REGULAR,
                    tokens.text,
                    tokens,
                )))
                .width(Length::Fill),
            )
            .push(
                container(cut(marked(
                    fit(event.recipients(), TOOK, 12.0),
                    query,
                    12.0,
                    REGULAR,
                    tokens.muted,
                    tokens,
                )))
                .width(TOOK)
                .align_x(Alignment::End),
            ),
        ActivityEntry::Words { text: words, .. } => {
            tall = true;
            let shown = if words.chars().count() > WORDS_SHOWN {
                format!("{}…", words.chars().take(WORDS_SHOWN - 1).collect::<String>())
            } else {
                words.clone()
            };
            cells.push(
                container(marked_wrapped(shown, query, tokens))
                    .width(Length::Fill)
                    .padding(Padding::from([6, 0])),
            )
        }
        ActivityEntry::Delegation(asked) => {
            let words = match (&asked.task, asked.refused.is_empty()) {
                (_, false) => format!("Asked {}: refused, {}", asked.bot, asked.refused),
                _ => format!("Asked {}", asked.bot),
            };
            let mut cells = cells
                .push(
                    container(cut(marked(
                        "delegate".to_string(),
                        query,
                        SMALL,
                        Font::MONOSPACE,
                        tokens.muted,
                        tokens,
                    )))
                    .width(TOOL),
                )
                .push(container(cut(marked(words, query, SMALL, REGULAR, tokens.text, tokens))).width(Length::Fill));
            if let DelegationActivity { task: Some(child), .. } = asked {
                cells = cells.push(
                    button(
                        row![text("Open task").size(12.0), icon(Icon::Next, 12.0, tokens.muted)]
                            .spacing(4)
                            .align_y(Alignment::Center),
                    )
                    .padding(Padding::from([2, 6]))
                    .style(styles::button_style(tokens, Kind::Ghost))
                    .on_press(Message::Go(Screen::Task(child.clone()))),
                );
            }
            cells
        }
        ActivityEntry::Waiting { tool, .. } => cells
            .push(
                container(cut(marked(
                    tool.clone(),
                    query,
                    SMALL,
                    Font::MONOSPACE,
                    tokens.text,
                    tokens,
                )))
                .width(TOOL),
            )
            .push(
                container(cut(marked(
                    entry.headline(),
                    query,
                    SMALL,
                    REGULAR,
                    tokens.text,
                    tokens,
                )))
                .width(Length::Fill),
            ),
        ActivityEntry::YouWrote { text: words, .. } | ActivityEntry::Said { text: words, .. } => {
            let who = if matches!(entry, ActivityEntry::YouWrote { .. }) {
                "you wrote"
            } else {
                "it answered"
            };
            cells
                .push(container(text(who).size(12.0).color(tokens.muted).wrapping(Wrapping::None)).width(TOOL))
                .push(
                    container(cut(marked(words.clone(), query, SMALL, REGULAR, tokens.text, tokens)))
                        .width(Length::Fill),
                )
        }
    };
    // The match the find is at: the accent's bar at its left.
    let selected = mark.current;
    let accent = container(Space::new().width(3).height(Length::Fill)).style(move |_| container::Style {
        background: selected.then(|| tokens.accent.into()),
        ..container::Style::default()
    });
    let line = button(row![
        accent,
        container(cells)
            .padding(Padding {
                left: 7.0,
                right: 10.0 + SCROLL_BAR,
                ..Padding::ZERO
            })
            .height(if tall { Length::Shrink } else { Length::Fill })
            .align_y(Alignment::Center)
    ])
    .width(Length::Fill)
    .padding(0)
    .style(styles::row(tokens, mark.hit || mark.open))
    .on_press(Message::ActivityPick(at));
    let line: El<'a> = if tall {
        container(line).width(Length::Fill).into()
    } else {
        container(line.height(LINE - 1.0)).width(Length::Fill).into()
    };
    column![
        line,
        container(Space::new().width(Length::Fill).height(1)).style(styles::hairline(tokens))
    ]
    .into()
}

/// The glyph of a step and its colour: ✓ done, ✕ failed, ▶ running,
/// a bolt for an event, a dot for words.
fn glyph(entry: &ActivityEntry, tokens: Tokens) -> (Icon, Color) {
    match entry {
        ActivityEntry::Tool(call) => {
            let tone = Tone::of_tool(call.state);
            let glyph = match call.state {
                ToolState::Ok => Icon::Check,
                ToolState::Failed => Icon::Cross,
                ToolState::Running => Icon::Play,
                ToolState::Waiting => Icon::Alert,
                ToolState::Denied => Icon::Ban,
                ToolState::Unfinished => Icon::Dot,
            };
            (glyph, tone.color(&tokens))
        }
        ActivityEntry::Emitted(_) => (Icon::Bolt, tokens.wait),
        ActivityEntry::Delegation(asked) if !asked.refused.is_empty() => (Icon::Team, tokens.bad),
        ActivityEntry::Delegation(_) => (Icon::Team, tokens.text),
        ActivityEntry::Waiting { decision, .. } if decision.is_empty() => (Icon::Alert, tokens.wait),
        ActivityEntry::Waiting { .. } => (Icon::Check, tokens.muted),
        ActivityEntry::Words { .. } => (Icon::Bots, tokens.busy),
        ActivityEntry::YouWrote { .. } => (Icon::Person, tokens.muted),
        ActivityEntry::Said { .. } => (Icon::Bots, tokens.muted),
    }
}

/// How long a call took and a bar as long as the log of it, the longest
/// call's the longest; `running` while it goes.
fn took<'a>(call: &ToolActivity, longest: u64, tokens: Tokens) -> El<'a> {
    let words = match call.duration_ms {
        Some(_) => call.took(),
        None => call.state.label().to_string(),
    };
    let width = call.duration_ms.map_or(0.0, |ms| {
        let share = (1.0 + ms as f64).log10() / (1.0 + longest.max(1) as f64).log10();
        (f64::from(BAR) * share).round().max(2.0) as f32
    });
    let bar = container(Space::new().width(width).height(4)).style(move |_| container::Style {
        background: Some(tokens.busy.into()),
        border: iced::border::rounded(2),
        ..container::Style::default()
    });
    container(
        row![
            text(words).size(12.0).color(tokens.muted).wrapping(Wrapping::None),
            container(bar).width(BAR).align_x(Alignment::Start)
        ]
        .spacing(6)
        .align_y(Alignment::Center),
    )
    .width(TOOK)
    .align_x(Alignment::End)
    .into()
}

/// Everything a step knows, in place under its line: its arguments in
/// the fixed face, why it failed, the rest, and a copy of it.
fn details<'a>(entry: &'a ActivityEntry, tokens: Tokens) -> El<'a> {
    let mut facts = column![].spacing(6);
    for (name, value) in entry.details() {
        let (name, ink) = match name {
            "Error" => ("Why it failed", styles::pill_ink(tokens, Tone::Bad)),
            other => (other, tokens.text),
        };
        let heading = label(name, tokens).size(11.0);
        let heading = if name == "Why it failed" {
            heading.color(ink)
        } else {
            heading
        };
        facts = facts.push(
            column![
                heading,
                text(value)
                    .size(12.0)
                    .font(Font::MONOSPACE)
                    .color(ink)
                    .wrapping(Wrapping::WordOrGlyph)
            ]
            .spacing(2),
        );
    }
    facts = facts.push(super::super::widgets::copy_button(
        "Copy step",
        entry.details_text(),
        tokens,
    ));
    column![
        container(facts)
            .padding(Padding {
                top: 8.0,
                right: 10.0,
                bottom: 10.0,
                left: TIME + GLYPH + 26.0,
            })
            .width(Length::Fill)
            .style(styles::well(tokens)),
        container(Space::new().width(Length::Fill).height(1)).style(styles::rule(tokens))
    ]
    .into()
}

/// A quiet line at the foot of the steps.
fn note<'a>(words: String, tokens: Tokens) -> El<'a> {
    container(text(words).size(12.0).color(tokens.muted))
        .padding(Padding::from([8, 10]))
        .into()
}

/// Words on one line, cut at their cell's edge.
fn cut<'a>(words: El<'a>) -> El<'a> {
    container(words).clip(true).into()
}

/// Words on one line in a size, a face and an ink, with what the find
/// matched on the mark's wash.
fn marked<'a>(words: String, query: &Query, size: f32, font: Font, ink: Color, tokens: Tokens) -> El<'a> {
    let marks = query.marks(&words);
    if marks.is_empty() {
        return text(words)
            .size(size)
            .font(font)
            .color(ink)
            .wrapping(Wrapping::None)
            .into();
    }
    let wash = styles::mark_fill(tokens);
    let spans: Vec<Span<'a, (), Font>> = marks
        .pieces(&words)
        .into_iter()
        .map(|(piece, found)| {
            let piece = span(piece.to_string());
            if found { piece.background(wash) } else { piece }
        })
        .collect();
    rich_text(spans)
        .size(size)
        .font(font)
        .color(ink)
        .wrapping(Wrapping::None)
        .into()
}

/// The model's words on as many lines as they take, what the find
/// matched on the mark's wash.
fn marked_wrapped<'a>(words: String, query: &Query, tokens: Tokens) -> El<'a> {
    let marks = query.marks(&words);
    if marks.is_empty() {
        return text(words)
            .size(SMALL)
            .font(REGULAR)
            .color(tokens.text)
            .line_height(1.45)
            .wrapping(Wrapping::WordOrGlyph)
            .width(Length::Fill)
            .into();
    }
    let wash = styles::mark_fill(tokens);
    let spans: Vec<Span<'a, (), Font>> = marks
        .pieces(&words)
        .into_iter()
        .map(|(piece, found)| {
            let piece = span(piece.to_string());
            if found { piece.background(wash) } else { piece }
        })
        .collect();
    rich_text(spans)
        .size(SMALL)
        .font(REGULAR)
        .color(tokens.text)
        .line_height(1.45)
        .wrapping(Wrapping::WordOrGlyph)
        .width(Length::Fill)
        .into()
}
