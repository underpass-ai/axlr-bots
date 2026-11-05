//! One task: the call waiting for a person, the answer, the executions
//! and the timeline.

use iced::widget::{Space, column, container, markdown, responsive, row, scrollable, text, text_editor, tooltip};
use iced::{Alignment, Font, Length};

use super::widgets::{
    BODY, El, REGULAR, SMALL, action, bold, card, chip, copy_button, empty, fact, icon, icon_button, muted, pill,
    section, section_title, section_with,
};
use crate::application::TaskDetail;
use crate::domain::{Event, Execution, ExecutionKind, NodeWait, PostDraft, TaskId, TaskStatus};
use crate::ui::format;
use crate::ui::styles::{self, Kind};
use crate::ui::{App, Icon, Message, Screen, Tokens, Tone};

pub fn view<'a>(app: &'a App, id: &'a TaskId, tokens: Tokens) -> El<'a> {
    let back = action(
        Kind::Ghost,
        Icon::Back,
        "Tasks",
        Some(Message::Go(Screen::Tasks)),
        tokens,
    );
    let Some(detail) = app.task.as_ref().filter(|d| &d.task.id == id) else {
        return column![back, empty(Icon::Hourglass, "Loading the task…", tokens)]
            .spacing(14)
            .into();
    };
    let task = &detail.task;
    let stop = task.status.can_cancel().then(|| {
        action(
            Kind::Secondary,
            Icon::Stop,
            "Stop task",
            Some(Message::AskCancel(task.id.clone())),
            tokens,
        )
    });
    let mut header = row![back, pill(Tone::of_task(task.status), task.status.label(), tokens)]
        .spacing(12)
        .align_y(Alignment::Center);
    if task.is_urgent() {
        header = header.push(pill(Tone::Wait, "urgent", tokens));
    }
    header = header.push(headline(&task.objective, tokens));
    header = header.push(copy_button("Copy task", task.objective.clone(), tokens));
    if let Some(stop) = stop {
        header = header.push(stop);
    }

    // Who told it: a teammate's event, said once; nothing when no
    // teammate did, as Started by already says where it came from.
    let mut origin = column![
        fact(
            "Bot",
            super::widgets::link(
                task.bot_id.to_string(),
                Message::Go(Screen::Bot(task.bot_id.clone())),
                tokens
            ),
            tokens
        ),
        fact("Started by", text(task.trigger.describe()), tokens),
    ]
    .spacing(8)
    .width(Length::FillPortion(1));
    if let Some(told) = task.told_by() {
        origin = origin.push(fact("Told by", text(told), tokens));
    }
    origin = origin.push(fact(
        "Created",
        text(format!(
            "{} · {}",
            format::when(task.created_at),
            format::ago(task.created_at, app.now)
        )),
        tokens,
    ));
    let facts = card(
        row![
            origin,
            column![
                fact("Task id", copyable(task.id.to_string(), tokens), tokens),
                fact("Trace id", copyable(task.correlation_id.clone(), tokens), tokens),
                fact("Key", muted(task.idempotency_key.clone(), tokens), tokens),
            ]
            .spacing(8)
            .width(Length::FillPortion(1)),
        ]
        .spacing(20),
        tokens,
    );

    let mut page = column![header].spacing(16);
    // A call that waits for the person comes first: Approve and Deny in
    // the window's first view.
    if let Some(waiting) = approval(app, detail, tokens) {
        page = page.push(waiting);
    }
    // A decision queued for a node that is away or busy: what it waits
    // for, in a sentence, while it waits.
    if let Some(waiting) = node_wait(app, detail, tokens) {
        page = page.push(waiting);
    }
    // What it does comes next, live and once it ended alike: the cards
    // keep their places when the task ends, so the eye keeps its own.
    if let Some(pane) = super::activity::pane(app, detail, tokens) {
        page = page.push(pane);
    }
    // The header shows the objective's first line, cut to its room; an
    // objective the narrowest window may cut is here whole.
    if shows_whole_below(&task.objective) {
        page = page.push(section(Icon::Tasks, "Task", text(task.objective.clone()), tokens));
    }
    page = page.push(facts);
    let knew = task.knew();
    if !knew.is_empty() {
        let mut notes = column![muted("What its bot had heard, given to it when it started", tokens)].spacing(6);
        for note in knew {
            notes = notes.push(text(note).size(SMALL));
        }
        page = page.push(section(Icon::Clock, "Knew", notes, tokens));
    }
    if !task.failure_reason.is_empty() {
        // An expired task is nobody's decision in time, not a crash.
        let (glyph, why, tone) = match task.status {
            TaskStatus::Expired => (Icon::Hourglass, "Why it expired", Tone::Late),
            TaskStatus::Cancelled => (Icon::Stop, "Why it stopped", Tone::Idle),
            _ => (Icon::Cross, "Why it failed", Tone::Bad),
        };
        page = page.push(
            container(
                row![
                    icon(glyph, 20.0, tone.color(&tokens)),
                    bold(why),
                    text(task.failure_reason.clone()).width(Length::Fill),
                    copy_button("Copy reason", task.failure_reason.clone(), tokens),
                ]
                .spacing(12)
                .align_y(Alignment::Center),
            )
            .padding(16)
            .width(Length::Fill)
            .style(styles::tone_card(tokens, tone)),
        );
    }
    if let Some(answer) = &app.answer {
        // Markdown to read; plain text to select a part of and copy.
        let (body, toggle): (El<'a>, El<'a>) = if answer.selecting {
            (
                text_editor(&answer.plain)
                    .on_action(Message::AnswerSelect)
                    .font(REGULAR)
                    .size(BODY)
                    .padding(12)
                    .style(styles::reader(tokens))
                    .into(),
                action(
                    Kind::Ghost,
                    Icon::File,
                    "Formatted",
                    Some(Message::AnswerSelecting(false)),
                    tokens,
                ),
            )
        } else {
            let style = super::widgets::markdown_style(tokens);
            (
                markdown::view(answer.markdown.items(), markdown::Settings::with_text_size(BODY, style))
                    .map(Message::Copy),
                action(
                    Kind::Ghost,
                    Icon::File,
                    "Select text",
                    Some(Message::AnswerSelecting(true)),
                    tokens,
                ),
            )
        };
        // Only a task that ended has an answer; before, its last run's
        // words are what it said before it stopped to ask.
        let (glyph, title) = match task.status {
            TaskStatus::WaitingApproval => (Icon::Hourglass, "What it said before asking you"),
            status if !status.is_terminal() => (Icon::Hourglass, "What it said before it stopped"),
            _ => (Icon::Check, "Answer"),
        };
        page = page.push(section_with(
            glyph,
            title,
            vec![toggle, copy_button("Copy answer", answer.source.clone(), tokens)],
            body,
            tokens,
        ));
    }
    if let Some(files) = artifacts(app, detail, tokens) {
        page = page.push(files);
    }
    let usage = detail.total_usage();
    let mut runs = column![
        row![
            chip(format::count(detail.executions.len() as u64, "run", "runs"), tokens),
            chip(
                format::count(u64::from(usage.requests), "model call", "model calls"),
                tokens
            ),
            chip(format!("{} tokens", format::tokens(usage.total_tokens())), tokens),
            chip(
                format::count(u64::from(usage.tool_calls), "tool call", "tool calls"),
                tokens
            ),
            chip(format!("cost {}", format::spend(detail.cost())), tokens),
        ]
        .spacing(8)
    ]
    .spacing(14);
    for execution in &detail.executions {
        runs = runs.push(execution_card(app, execution, tokens));
    }
    if detail.executions.is_empty() {
        runs = runs.push(empty(Icon::Hourglass, "Not run yet: waiting for a node", tokens));
    }
    page = page.push(
        row![
            container(section(Icon::Play, "Runs", runs, tokens)).width(Length::FillPortion(3)),
            container(section_with(
                Icon::Clock,
                "Timeline",
                timeline_tools(app, detail, tokens),
                timeline(app, detail, tokens),
                tokens
            ))
            .width(Length::FillPortion(2)),
        ]
        .spacing(14),
    );
    scrollable(page.padding(iced::Padding {
        right: 12.0,
        ..Default::default()
    }))
    .id(super::widgets::PAGE)
    .height(Length::Fill)
    .into()
}

/// The files the runs delivered: name, media type and size, and Save for
/// those the control plane keeps.
fn artifacts<'a>(app: &'a App, detail: &'a TaskDetail, tokens: Tokens) -> Option<El<'a>> {
    let mut list = column![].spacing(10);
    let mut any = false;
    for execution in &detail.executions {
        for artifact in &execution.result.artifacts {
            any = true;
            let mut about = vec![];
            if !artifact.media_type.is_empty() {
                about.push(artifact.media_type.clone());
            }
            if artifact.size > 0 {
                about.push(format::bytes(artifact.size));
            }
            let save: El<'a> = if artifact.stored {
                action(
                    Kind::Secondary,
                    Icon::File,
                    "Save",
                    (!app.downloading).then(|| Message::Download(execution.id.clone(), artifact.name.clone())),
                    tokens,
                )
            } else {
                muted("not kept", tokens).wrapping(text::Wrapping::None).into()
            };
            list = list.push(
                row![
                    icon(Icon::File, 18.0, tokens.muted),
                    column![
                        bold(artifact.name.clone()).wrapping(text::Wrapping::WordOrGlyph),
                        muted(about.join(" · "), tokens),
                    ]
                    .spacing(2)
                    .width(Length::Fill),
                    save,
                ]
                .spacing(10)
                .align_y(Alignment::Center),
            );
        }
    }
    any.then(|| section(Icon::File, "Files", list, tokens))
}

/// An id with a copy button.
fn copyable<'a>(value: String, tokens: Tokens) -> El<'a> {
    if value.is_empty() {
        return muted("—", tokens).into();
    }
    row![
        text(value.clone()).size(SMALL).font(Font::MONOSPACE),
        icon_button(Icon::Copy, "Copy", Some(Message::Copy(value)), tokens)
    ]
    .spacing(4)
    .align_y(Alignment::Center)
    .into()
}

/// The call the task stopped on, its arguments, and the two answers.
fn approval<'a>(app: &'a App, detail: &'a TaskDetail, tokens: Tokens) -> Option<El<'a>> {
    let pending = detail.pending_call()?;
    let id = detail.task.id.clone();
    let deciding = app.deciding.is_some();
    let mut arguments = column![].spacing(6);
    for (name, value) in pending.argument_lines() {
        arguments = arguments.push(
            row![
                container(bold(name).size(SMALL)).width(120),
                container(text(value.clone()).font(Font::MONOSPACE).size(SMALL))
                    .padding(6)
                    .width(Length::Fill)
                    .style(styles::chip(tokens)),
                icon_button(Icon::Copy, "Copy", Some(Message::Copy(value)), tokens),
            ]
            .spacing(10)
            .align_y(Alignment::Center),
        );
    }
    let draft = PostDraft::from_call(pending).map(|draft| {
        let author = app
            .fleet
            .as_ref()
            .and_then(|f| f.bots.iter().find(|b| b.id == detail.task.bot_id))
            .map(|b| b.name.clone())
            .unwrap_or_else(|| detail.task.bot_id.to_string());
        (draft, author)
    });
    let buttons = if deciding {
        row![icon(Icon::Hourglass, 18.0, tokens.wait), bold("Sending your answer…")]
            .spacing(10)
            .align_y(Alignment::Center)
    } else {
        row![
            action(
                Kind::Good,
                Icon::Check,
                "Approve",
                Some(Message::Approve(id.clone())),
                tokens
            ),
            action(Kind::Danger, Icon::Cross, "Deny", Some(Message::AskDeny(id)), tokens),
        ]
        .spacing(12)
    };
    Some(
        container(
            column![
                section_title(Icon::Alert, "Needs OK", tokens),
                row![
                    muted("The bot wants to call", tokens),
                    bold(pending.tool.clone()).size(20.0),
                    Space::new().width(Length::Fill),
                    copy_button("Copy call", pending.call_text(), tokens),
                ]
                .spacing(10)
                .align_y(Alignment::Center),
                // The answer before the call's words: a long argument
                // never pushes Approve and Deny out of the first view.
                buttons,
                draft.map_or_else(
                    || Space::new().height(0).into(),
                    |(draft, author)| super::post_draft::view(draft, author, tokens),
                ),
                arguments,
            ]
            .spacing(14),
        )
        .padding(20)
        .width(Length::Fill)
        .style(styles::tone_card(tokens, Tone::Wait))
        .into(),
    )
}

fn execution_card<'a>(app: &'a App, execution: &'a Execution, tokens: Tokens) -> El<'a> {
    let kind = match execution.kind {
        ExecutionKind::Run => "Run".to_string(),
        ExecutionKind::Decide => format!("After {}", if execution.decision == "deny" { "deny" } else { "OK" }),
        ExecutionKind::Unknown => "Unknown".to_string(),
    };
    let head = row![
        pill(Tone::of_outcome(execution.outcome), execution.outcome.label(), tokens),
        chip(kind, tokens),
        icon(Icon::Nodes, 16.0, tokens.muted),
        bold(execution.place().to_string()),
        muted(format::run_length(execution.duration(app.now)), tokens),
        Space::new().width(Length::Fill),
        muted(
            format!(
                "{} calls · {} tokens · {}",
                execution.usage.requests,
                format::tokens(execution.usage.total_tokens()),
                format::run_cost(&execution.usage)
            ),
            tokens
        ),
    ]
    .spacing(10)
    .align_y(Alignment::Center);
    let mut body = column![head].spacing(10);
    let steps = execution.steps();
    if !steps.is_empty() {
        // Why each call ran or not: its decision in words, and the bot's
        // own rules about the tool.
        let rules = app
            .fleet
            .as_ref()
            .and_then(|f| f.bots.iter().find(|b| b.id == execution.bot_id))
            .map(|b| &b.tool_rules);
        let mut list = column![muted("Steps", tokens)].spacing(6);
        for step in steps {
            let tone = Tone::of_verdict(step.verdict());
            let when = step.started_at.map(format::clock).unwrap_or_default();
            let took = step.duration_ms.map(|ms| format!("{ms} ms")).unwrap_or_default();
            let mut line = column![
                row![
                    container(muted(when, tokens)).width(70),
                    icon(Icon::Wrench, 14.0, tokens.muted),
                    container(bold(step.tool.clone()).size(SMALL)).width(150),
                    pill(tone, step.why(), tokens),
                    muted(step.ended().to_string(), tokens),
                    Space::new().width(Length::Fill),
                    muted(took, tokens).wrapping(iced::widget::text::Wrapping::None),
                ]
                .spacing(10)
                .align_y(Alignment::Center)
            ]
            .spacing(2);
            let about = rules.map(|r| r.about(&step.tool)).unwrap_or_default();
            if !about.is_empty() {
                line = line.push(
                    container(muted(format!("Its rules: {}", about.join(" · ")), tokens)).padding(iced::Padding {
                        left: 80.0,
                        ..Default::default()
                    }),
                );
            }
            list = list.push(line);
        }
        body = body.push(list);
    }
    if !execution.failure_reason.is_empty() {
        body = body.push(
            row![
                icon(Icon::Cross, 14.0, tokens.bad),
                text(execution.failure_reason.clone()).size(SMALL).width(Length::Fill),
                icon_button(
                    Icon::Copy,
                    "Copy error",
                    Some(Message::Copy(execution.failure_reason.clone())),
                    tokens
                ),
            ]
            .spacing(8)
            .align_y(Alignment::Center),
        );
    }
    let mut ids = row![muted(format::short_id(execution.id.as_str(), 18), tokens)]
        .spacing(12)
        .align_y(Alignment::Center);
    if let Some(correlation) = &execution.correlation {
        let carried = if correlation.carried {
            "trace carried"
        } else {
            "trace not carried"
        };
        ids = ids
            .push(icon(
                Icon::Trace,
                14.0,
                if correlation.carried { tokens.good } else { tokens.muted },
            ))
            .push(muted(carried, tokens));
    }
    if let Some(policy) = &execution.policy {
        let enforced = if policy.enforced {
            "policy enforced"
        } else {
            "policy not enforced"
        };
        ids = ids
            .push(icon(
                Icon::Check,
                14.0,
                if policy.enforced { tokens.good } else { tokens.wait },
            ))
            .push(muted(enforced, tokens));
    }
    body = body.push(ids);
    container(body)
        .padding(14)
        .width(Length::Fill)
        .style(styles::chip(tokens))
        .into()
}

/// The task's timeline without its run's progress, which the Activity
/// shows: from the live read when it came, which holds the whole of it.
fn timeline_events<'a>(app: &'a App, detail: &'a TaskDetail) -> Vec<&'a Event> {
    let events = match app.live.as_ref().filter(|l| l.id == detail.task.id && l.read) {
        Some(live) => &live.feed.events,
        None => &detail.events,
    };
    events.iter().filter(|e| !e.is_progress()).collect()
}

/// How wide a character of the title is on average, in its size: Lexend
/// Bold.
const TITLE_EM: f32 = 0.62;
const TITLE_SIZE: f32 = 22.0;
/// The longest first line the title shows whole in a 1360 px window; a
/// longer objective is shown whole in its own card too. A title cut in a
/// narrower window shows it whole on hover.
const TITLE_WHOLE: usize = 40;

/// Whether the objective needs its own card: more than one line, or a
/// first line the title may cut.
fn shows_whole_below(objective: &str) -> bool {
    let objective = objective.trim();
    objective.lines().count() > 1 || objective.chars().count() > TITLE_WHOLE
}

/// The objective's first line as the page's title: on one line, cut with
/// an ellipsis to what the header's room holds.
fn headline<'a>(objective: &'a str, tokens: Tokens) -> El<'a> {
    responsive(move |space| {
        let fits = ((space.width / (TITLE_EM * TITLE_SIZE)).floor() as usize).clamp(8, 80);
        let first = objective.lines().next().unwrap_or_default().trim();
        let short = super::grid::shorten(first, fits);
        let cut = short != first;
        let line = container(bold(short).size(TITLE_SIZE).wrapping(text::Wrapping::None)).clip(true);
        if !cut {
            return line.into();
        }
        tooltip(
            line,
            container(text(first).size(SMALL))
                .padding(6)
                .style(styles::chip(tokens)),
            tooltip::Position::Bottom,
        )
        .into()
    })
    .height(Length::Shrink)
    .into()
}

/// What the task's decision waits for while its node is away or busy.
fn node_wait<'a>(app: &'a App, detail: &'a TaskDetail, tokens: Tokens) -> Option<El<'a>> {
    if detail.task.status != TaskStatus::Running {
        return None;
    }
    let events = match app.live.as_ref().filter(|l| l.id == detail.task.id && l.read) {
        Some(live) => &live.feed.events,
        None => &detail.events,
    };
    let wait = NodeWait::current(events)?;
    Some(
        container(
            row![
                icon(Icon::Hourglass, 20.0, Tone::Late.color(&tokens)),
                bold(format::wait(&wait)).width(Length::Fill),
            ]
            .spacing(12)
            .align_y(Alignment::Center),
        )
        .padding(16)
        .width(Length::Fill)
        .style(styles::tone_card(tokens, Tone::Late))
        .into(),
    )
}

/// A copy of the whole timeline, once it has an event.
fn timeline_tools<'a>(app: &'a App, detail: &'a TaskDetail, tokens: Tokens) -> Vec<El<'a>> {
    let events = timeline_events(app, detail);
    if events.is_empty() {
        return Vec::new();
    }
    vec![copy_button("Copy timeline", format::events(events), tokens)]
}

fn timeline<'a>(app: &'a App, detail: &'a TaskDetail, tokens: Tokens) -> El<'a> {
    let events = timeline_events(app, detail);
    if events.is_empty() {
        return empty(Icon::Clock, "No event yet", tokens);
    }
    let mut list = column![].spacing(10);
    for event in events {
        let tone = Tone::of_event(event.tone());
        list = list.push(
            row![
                muted(format::clock(event.created_at), tokens),
                icon(tone.event_icon(), 16.0, tone.color(&tokens)),
                column![
                    bold(event.label()).size(BODY),
                    muted(format::event_detail(event), tokens)
                ]
                .spacing(2),
            ]
            .spacing(10)
            .align_y(Alignment::Center),
        );
    }
    list.into()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn an_objective_the_title_may_cut_is_shown_whole_below() {
        assert!(!shows_whole_below("Check the disk"));
        assert!(!shows_whole_below(&"x".repeat(TITLE_WHOLE)));
        assert!(shows_whole_below(&"x".repeat(60)));
        assert!(shows_whole_below("Check the disk\nthen the logs"));
    }
}
