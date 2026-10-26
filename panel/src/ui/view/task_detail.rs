//! One task: the call waiting for a person, the answer, the executions
//! and the timeline.

use iced::widget::{Space, column, container, markdown, mouse_area, row, scrollable, text, text_editor};
use iced::{Alignment, Font, Length, mouse};

use super::widgets::{
    BODY, El, REGULAR, SMALL, action, bold, card, chip, copy_button, empty, fact, icon, icon_button, muted, pill,
    section, section_title, section_with,
};
use crate::application::TaskDetail;
use crate::domain::{Execution, ExecutionKind, TaskId, TaskStatus};
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
    header = header.push(bold(task.headline(80)).size(22.0).width(Length::Fill));
    header = header.push(copy_button("Copy task", task.objective.clone(), tokens));
    if let Some(stop) = stop {
        header = header.push(stop);
    }

    let facts = card(
        row![
            column![
                fact(
                    "Bot",
                    mouse_area(bold(task.bot_id.to_string()).color(tokens.accent))
                        .on_press(Message::Go(Screen::Bot(task.bot_id.clone())))
                        .interaction(mouse::Interaction::Pointer),
                    tokens
                ),
                fact("Started by", text(task.trigger.describe()), tokens),
                fact(
                    "Told by",
                    text(
                        task.told_by()
                            .unwrap_or_else(|| "nobody: not a teammate's event".into())
                    ),
                    tokens
                ),
                fact(
                    "Created",
                    text(format!(
                        "{} · {}",
                        format::when(task.created_at),
                        format::ago(task.created_at, app.now)
                    )),
                    tokens
                ),
            ]
            .spacing(8)
            .width(Length::FillPortion(1)),
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
    // The header shows the objective's first line, cut; a longer one is
    // here whole.
    if task.headline(80) != task.objective.trim() {
        page = page.push(section(Icon::Tasks, "Task", text(task.objective.clone()), tokens));
    }
    page = page.push(facts);
    if let Some(waiting) = approval(app, detail, tokens) {
        page = page.push(waiting);
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
            let style = markdown::Style::from_palette(tokens.theme().palette());
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
        page = page.push(section_with(
            Icon::Check,
            "Answer",
            vec![toggle, copy_button("Copy answer", answer.source.clone(), tokens)],
            body,
            tokens,
        ));
    }
    let usage = detail.total_usage();
    let mut runs = column![
        row![
            chip(format!("{} runs", detail.executions.len()), tokens),
            chip(format!("{} model calls", usage.requests), tokens),
            chip(format!("{} tokens", format::tokens(usage.total_tokens())), tokens),
            chip(format!("{} tool calls", usage.tool_calls), tokens),
            chip(format!("cost {}", format::cost(usage.cost_usd)), tokens),
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
                timeline_tools(detail, tokens),
                timeline(detail, tokens),
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
    .height(Length::Fill)
    .into()
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
                section_title(Icon::Alert, "Needs your OK", tokens),
                row![
                    muted("The bot wants to call", tokens),
                    bold(pending.tool.clone()).size(20.0),
                    Space::new().width(Length::Fill),
                    copy_button("Copy call", pending.call_text(), tokens),
                ]
                .spacing(10)
                .align_y(Alignment::Center),
                arguments,
                buttons,
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
                format::cost(execution.usage.cost_usd)
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

/// A copy of the whole timeline, once it has an event.
fn timeline_tools<'a>(detail: &'a TaskDetail, tokens: Tokens) -> Vec<El<'a>> {
    if detail.events.is_empty() {
        return Vec::new();
    }
    vec![copy_button("Copy timeline", format::events(&detail.events), tokens)]
}

fn timeline<'a>(detail: &'a TaskDetail, tokens: Tokens) -> El<'a> {
    if detail.events.is_empty() {
        return empty(Icon::Clock, "No event yet", tokens);
    }
    let mut list = column![].spacing(10);
    for event in &detail.events {
        let tone = Tone::of_event(event.tone());
        list = list.push(
            row![
                muted(format::clock(event.created_at), tokens),
                icon(tone.event_icon(), 16.0, tone.color(&tokens)),
                column![bold(event.label()).size(BODY), muted(event.detail(), tokens)].spacing(2),
            ]
            .spacing(10)
            .align_y(Alignment::Center),
        );
    }
    list.into()
}
