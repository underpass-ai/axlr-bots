//! A task's Activity: what its run does as it does it, step by step, and
//! the model's reasoning and words as they come. While it runs, each
//! stands in a box that keeps its newest lines in view, as a console
//! does, until the person scrolls up to read. A step opens its details,
//! a bot asked opens its own task, and every part copies.

use iced::widget::{Space, button, column, container, row, scrollable, text, text_editor};
use iced::{Alignment, Font, Length, Padding};

use super::widgets::{BODY, El, REGULAR, SMALL, action, bold, copy_button, empty, icon, muted, pill, section_with};
use crate::application::{ActivityEntry, TaskDetail, ToolActivity};
use crate::domain::TaskStatus;
use crate::ui::format;
use crate::ui::styles::{self, Kind};
use crate::ui::{App, Icon, LiveStream, LiveTask, Message, Screen, Tokens, Tone};

/// How tall each box stands at most before it scrolls: its steps while
/// the run works, its reasoning and its words, all in one window. A box
/// with less in it is as tall as what it holds.
const LIVE_STEPS_HEIGHT: f32 = 210.0;
const LIVE_THINKING_HEIGHT: f32 = 110.0;
const LIVE_WORDS_HEIGHT: f32 = 230.0;
/// How close to its end a box counts as showing it.
const AT_END: f32 = 4.0;
/// How much of a call's arguments, and of a failure's error, a step's
/// line shows; its details show them whole.
const LINE_SUMMARY: usize = 60;
const LINE_ERROR: usize = 90;

/// The Activity of the task on screen: while it may run, and after it
/// ended when its run told anything.
pub fn pane<'a>(app: &'a App, detail: &'a TaskDetail, tokens: Tokens) -> Option<El<'a>> {
    let task = &detail.task;
    let running = !task.status.is_terminal();
    let live = app.live.as_ref().filter(|l| l.id == task.id);
    if !running && live.is_none_or(|l| l.activity.is_empty()) {
        return None;
    }
    let mut tools: Vec<El<'a>> = Vec::new();
    if task.status == TaskStatus::Running {
        tools.push(pill(Tone::Busy, "live", tokens));
        // The bot's page has the chat that writes to this run.
        tools.push(action(
            Kind::Ghost,
            Icon::Bots,
            "Talk to it",
            Some(Message::Go(Screen::Bot(task.bot_id.clone()))),
            tokens,
        ));
    }
    if let Some(live) = live.filter(|l| !l.activity.is_empty()) {
        tools.push(copy_button("Copy activity", format::activity(&live.activity), tokens));
    }
    let body = match live {
        Some(live) if live.read => steps(live, running, tokens),
        _ => empty(Icon::Hourglass, "Reading what it does…", tokens),
    };
    Some(section_with(Icon::Bolt, "Activity", tools, body, tokens))
}

/// The steps, the details of the one open, the model's reasoning and its
/// words. While the task runs, each grows in its own box that follows
/// its end; once it ended, the words are text to select.
fn steps<'a>(live: &'a LiveTask, running: bool, tokens: Tokens) -> El<'a> {
    let activity = &live.activity;
    if activity.is_empty() {
        return empty(Icon::Hourglass, "Nothing yet: no tool call and no words", tokens);
    }
    let mut list = column![].spacing(4);
    for (index, entry) in activity.entries.iter().enumerate() {
        let open = live.selected == Some(index);
        list = list.push(line(index, entry, open, tokens));
        if open {
            list = list.push(details(entry, tokens));
        }
    }
    let mut body = column![].spacing(14);
    if !activity.entries.is_empty() {
        body = body.push(if running {
            follow(LiveStream::Steps, list, LIVE_STEPS_HEIGHT)
        } else {
            list.into()
        });
    }
    if !activity.thinking.is_empty() {
        let mut head = row![
            muted("What it thinks", tokens),
            Space::new().width(Length::Fill),
            copy_button("Copy thinking", activity.thinking.clone(), tokens)
        ]
        .spacing(8)
        .align_y(Alignment::Center);
        if activity.thinking_cut {
            head = head.push(muted("cut at 16 KiB", tokens));
        }
        let thoughts = text(activity.thinking.clone())
            .size(SMALL)
            .font(Font {
                style: iced::font::Style::Italic,
                ..REGULAR
            })
            .color(tokens.muted);
        body = body.push(
            column![
                head,
                framed(thoughts, LIVE_THINKING_HEIGHT, LiveStream::Thinking, tokens)
            ]
            .spacing(6),
        );
    }
    if !activity.text.is_empty() {
        let toggle = if live.selecting {
            action(
                Kind::Ghost,
                Icon::File,
                "Read as it comes",
                Some(Message::WordsSelecting(false)),
                tokens,
            )
        } else {
            action(
                Kind::Ghost,
                Icon::File,
                "Select words",
                Some(Message::WordsSelecting(true)),
                tokens,
            )
        };
        let mut head = row![
            muted("What it says", tokens),
            Space::new().width(Length::Fill),
            toggle,
            copy_button("Copy words", activity.text.clone(), tokens)
        ]
        .spacing(8)
        .align_y(Alignment::Center);
        if activity.text_cut {
            head = head.push(muted("cut at 32 KiB", tokens));
        }
        let words: El<'a> = if live.selecting {
            // The words as they were when the person started selecting:
            // a new word never takes their selection away.
            text_editor(&live.words)
                .on_action(Message::WordsSelect)
                .font(REGULAR)
                .size(BODY)
                .padding(12)
                .max_height(LIVE_WORDS_HEIGHT)
                .style(styles::reader(tokens))
                .into()
        } else {
            // Text to read as it grows, in a box as tall as it is: it
            // scrolls only past its height, and the wheel goes on to the
            // page once it reaches an end.
            framed(
                text(activity.text.clone()).size(BODY),
                LIVE_WORDS_HEIGHT,
                LiveStream::Words,
                tokens,
            )
        };
        body = body.push(column![head, words].spacing(6));
    }
    if activity.dropped > 0 {
        body = body.push(muted(
            format!("{} steps were lost on the way from its node", activity.dropped),
            tokens,
        ));
    }
    if activity.truncated {
        body = body.push(muted("Its later steps were not kept: the run told too many", tokens));
    }
    body.into()
}

/// A box that scrolls past `height` and keeps its end in view while the
/// person has not scrolled up in it.
fn follow<'a>(stream: LiveStream, content: impl Into<El<'a>>, height: f32) -> El<'a> {
    container(
        scrollable(content)
            .id(stream.id())
            .width(Length::Fill)
            .on_scroll(move |view| {
                let left = view.content_bounds().height - view.bounds().height - view.absolute_offset().y;
                Message::ActivityScrolled(stream, left <= AT_END)
            }),
    )
    .max_height(height)
    .width(Length::Fill)
    .into()
}

/// Words that grow, in a framed box that follows its end.
fn framed<'a>(words: impl Into<El<'a>>, height: f32, stream: LiveStream, tokens: Tokens) -> El<'a> {
    container(follow(stream, container(words).padding(12).width(Length::Fill), height))
        .width(Length::Fill)
        .style(styles::well(tokens))
        .into()
}

/// One step in a line; a click opens its details.
fn line<'a>(index: usize, entry: &'a ActivityEntry, open: bool, tokens: Tokens) -> El<'a> {
    let when = muted(format::clock(entry.at()), tokens);
    let (glyph, color) = match entry {
        ActivityEntry::Tool(call) => (Icon::Wrench, Tone::of_tool(call.state).color(&tokens)),
        ActivityEntry::Delegation(asked) if !asked.refused.is_empty() => (Icon::Team, tokens.bad),
        ActivityEntry::Delegation(_) => (Icon::Team, tokens.accent),
        ActivityEntry::Waiting { decision, .. } if decision.is_empty() => (Icon::Alert, tokens.wait),
        ActivityEntry::Waiting { .. } => (Icon::Check, tokens.muted),
        ActivityEntry::YouWrote { .. } | ActivityEntry::Said { .. } => (Icon::Dot, tokens.muted),
    };
    let mut content = row![container(when).width(70), icon(glyph, 14.0, color)]
        .spacing(10)
        .align_y(Alignment::Center);
    content = match entry {
        ActivityEntry::Tool(call) => content
            .push(tool_name(call, tokens))
            .push(pill(Tone::of_tool(call.state), call.state.label(), tokens))
            .push(muted(call.took(), tokens).wrapping(text::Wrapping::None)),
        ActivityEntry::Delegation(asked) => {
            let what = if asked.refused.is_empty() {
                format!("Asked {}", asked.bot)
            } else {
                format!("Asked {}: refused, {}", asked.bot, asked.refused)
            };
            content.push(bold(what).size(SMALL).width(Length::Fill))
        }
        ActivityEntry::Waiting { decision, .. } if decision.is_empty() => content
            .push(bold(entry.headline()).size(SMALL).width(Length::Fill))
            .push(pill(Tone::Wait, "needs you", tokens)),
        // Decided: what the person said, no longer a call to act on.
        ActivityEntry::Waiting { .. } => content.push(text(entry.headline()).size(SMALL).width(Length::Fill)),
        ActivityEntry::YouWrote { text: words, .. } => {
            content.push(text(format!("You wrote: {words}")).size(SMALL).width(Length::Fill))
        }
        ActivityEntry::Said { text: words, .. } => {
            content.push(text(format!("It answered: {words}")).size(SMALL).width(Length::Fill))
        }
    };
    let kind = if open { Kind::Secondary } else { Kind::Ghost };
    let pressed = button(content)
        .padding(Padding::from([4, 8]))
        .width(Length::Fill)
        .style(styles::button_style(tokens, kind))
        .on_press(Message::ActivityPick(index));
    match entry {
        ActivityEntry::Delegation(asked) if asked.task.is_some() => row![
            pressed,
            action(
                Kind::Ghost,
                Icon::Play,
                "Open task",
                asked.task.clone().map(|task| Message::Go(Screen::Task(task))),
                tokens
            )
        ]
        .spacing(6)
        .align_y(Alignment::Center)
        .into(),
        _ => pressed.into(),
    }
}

/// A call's name, the MCP tool a bridged call names, with a short summary
/// of its arguments beside it and, when it failed, its error below.
fn tool_name<'a>(call: &'a ToolActivity, tokens: Tokens) -> El<'a> {
    let mut name = row![bold(call.name()).size(SMALL)]
        .spacing(8)
        .align_y(Alignment::Center);
    if !call.summary.is_empty() {
        name = name.push(
            muted(call.short_summary(LINE_SUMMARY), tokens)
                .font(Font::MONOSPACE)
                .wrapping(text::Wrapping::None),
        );
    }
    let mut lines = column![name].spacing(2).width(Length::Fill);
    if !call.error.is_empty() {
        let error: String = if call.error.chars().count() > LINE_ERROR {
            format!("{}…", call.error.chars().take(LINE_ERROR - 1).collect::<String>())
        } else {
            call.error.clone()
        };
        lines = lines.push(text(error).size(SMALL).color(tokens.bad));
    }
    lines.into()
}

/// Everything a step knows, and a copy of it.
fn details<'a>(entry: &'a ActivityEntry, tokens: Tokens) -> El<'a> {
    let mut facts = column![].spacing(6);
    for (label, value) in entry.details() {
        facts = facts.push(
            row![
                container(muted(label, tokens)).width(110),
                text(value).font(Font::MONOSPACE).size(SMALL).width(Length::Fill),
            ]
            .spacing(10)
            .align_y(Alignment::Center),
        );
    }
    container(
        row![
            facts,
            column![copy_button("Copy step", entry.details_text(), tokens)].spacing(6)
        ]
        .spacing(12),
    )
    .padding(Padding {
        top: 8.0,
        right: 12.0,
        bottom: 8.0,
        left: 88.0,
    })
    .width(Length::Fill)
    .style(styles::chip(tokens))
    .into()
}
