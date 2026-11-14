//! A task with everything under it: the task and the tasks it delegated,
//! each with its executions (how long, what it cost), the tools they
//! called with their state and duration, and a mark where the steps were
//! completed from the transcript.

use iced::widget::{Space, column, container, row, scrollable, text};
use iced::{Alignment, Length, Padding};

use super::widgets::{El, SMALL, action, bold, card, chip, empty, icon, link, muted, pill, section_title};
use crate::application::{ActivityBranch, ActivityRun, ToolActivity};
use crate::domain::{ExecutionKind, TaskId};
use crate::ui::format;
use crate::ui::styles::Kind;
use crate::ui::{ActivityScreen, App, Icon, Message, Screen, Tokens, Tone};

/// How far a delegated task is set in, per level.
const INDENT: f32 = 26.0;

pub fn view<'a>(app: &'a App, id: &'a TaskId, tokens: Tokens) -> El<'a> {
    let back = action(
        Kind::Ghost,
        Icon::Back,
        "Task",
        Some(Message::Go(Screen::Task(id.clone()))),
        tokens,
    );
    let Some(screen) = app.activity.as_ref().filter(|a| &a.id == id && a.tree.is_some()) else {
        return column![back, empty(Icon::Hourglass, "Loading everything it did…", tokens)]
            .spacing(14)
            .into();
    };
    let mut page = column![header(back, screen, tokens)].spacing(16);
    for branch in &screen.outline {
        page = page.push(branch_card(app, branch, tokens));
    }
    page.into()
}

fn header<'a>(back: El<'a>, screen: &'a ActivityScreen, tokens: Tokens) -> El<'a> {
    let runs: usize = screen.outline.iter().map(|b| b.runs.len()).sum();
    let usage = screen.outline.first().map(|b| b.subtree).unwrap_or_default();
    let mut head = row![back, section_title(Icon::Bolt, "Activity", tokens)]
        .spacing(12)
        .align_y(Alignment::Center);
    if !screen.finished() {
        head = head.push(pill(Tone::Busy, "live", tokens));
    }
    head = head
        .push(Space::new().width(Length::Fill))
        .push(chip(
            format::count(screen.outline.len() as u64, "task", "tasks"),
            tokens,
        ))
        .push(chip(format::count(runs as u64, "run", "runs"), tokens))
        .push(chip(format!("{} tokens", format::tokens(usage.total_tokens())), tokens))
        .push(chip(format!("cost {}", format::spend(usage.cost())), tokens));
    head.spacing(8).into()
}

fn branch_card<'a>(app: &'a App, branch: &'a ActivityBranch, tokens: Tokens) -> El<'a> {
    let task = &branch.task;
    let mut head = row![
        pill(Tone::of_task(task.status), task.status.label(), tokens),
        link(
            task.bot_id.to_string(),
            Message::Go(Screen::Bot(task.bot_id.clone())),
            tokens
        ),
        text(task.headline(70)).size(SMALL).width(Length::Fill),
    ]
    .spacing(10)
    .align_y(Alignment::Center);
    if branch.subtree != branch.own {
        head = head.push(muted(
            format!("with what it asked: {}", format::spend(branch.subtree.cost())),
            tokens,
        ));
    }
    head = head.push(action(
        Kind::Ghost,
        Icon::Tasks,
        "Open",
        Some(Message::Go(Screen::Task(task.id.clone()))),
        tokens,
    ));
    let mut body = column![head].spacing(12);
    if branch.runs.is_empty() {
        body = body.push(muted("No run yet", tokens));
    }
    for run in &branch.runs {
        body = body.push(run_block(app, run, tokens));
    }
    container(card(body, tokens))
        .padding(Padding {
            left: INDENT * branch.depth as f32,
            ..Default::default()
        })
        .into()
}

fn run_block<'a>(app: &'a App, run: &'a ActivityRun, tokens: Tokens) -> El<'a> {
    let execution = &run.execution;
    let kind = match execution.kind {
        ExecutionKind::Run => "Run".to_string(),
        ExecutionKind::Decide => format!("After {}", if execution.decision == "deny" { "deny" } else { "OK" }),
        ExecutionKind::Unknown => "Unknown".to_string(),
    };
    let mut head = row![
        pill(Tone::of_outcome(execution.outcome), execution.outcome.label(), tokens),
        chip(kind, tokens),
        icon(Icon::Nodes, 16.0, tokens.muted),
        bold(execution.place().to_string()),
        muted(format::run_length(execution.duration(app.now)), tokens),
    ]
    .spacing(10)
    .align_y(Alignment::Center);
    if run.from_transcript {
        head = head.push(pill_from_transcript(tokens));
    }
    head = head.push(Space::new().width(Length::Fill)).push(muted(
        format!(
            "{} calls · {} tokens · {}",
            execution.usage.requests,
            format::tokens(execution.usage.total_tokens()),
            format::run_cost(&execution.usage)
        ),
        tokens,
    ));
    let mut block = column![head].spacing(6);
    if let Some(cut) = &run.cut {
        block = block.push(muted(cut.summary(), tokens));
    }
    for tool in &run.tools {
        block = block.push(tool_line(tool, tokens));
    }
    if !execution.failure_reason.is_empty() {
        block = block.push(muted(format!("Failed: {}", execution.failure_reason), tokens));
    }
    container(block)
        .padding(Padding {
            left: 8.0,
            ..Default::default()
        })
        .into()
}

fn pill_from_transcript<'a>(tokens: Tokens) -> El<'a> {
    super::widgets::pill_with(Icon::File, Tone::Wait, "completed from transcript", tokens)
}

fn tool_line<'a>(tool: &'a ToolActivity, tokens: Tokens) -> El<'a> {
    let mut line = row![
        icon(Icon::Wrench, 14.0, tokens.muted),
        container(bold(tool.name().to_string()).size(SMALL)).width(190),
        pill(Tone::of_tool(tool.state), tool.state.label(), tokens),
    ]
    .spacing(10)
    .align_y(Alignment::Center);
    if !tool.error.is_empty() {
        line = line.push(muted(tool.error.clone(), tokens));
    }
    line = line.push(Space::new().width(Length::Fill));
    line = line.push(muted(tool.took(), tokens).wrapping(iced::widget::text::Wrapping::None));
    container(line)
        .padding(Padding {
            left: 12.0,
            ..Default::default()
        })
        .into()
}

/// The screen, scrolling as one page.
pub fn page<'a>(app: &'a App, id: &'a TaskId, tokens: Tokens) -> El<'a> {
    scrollable(container(view(app, id, tokens)).padding(Padding {
        right: 12.0,
        ..Default::default()
    }))
    .id(super::widgets::PAGE)
    .height(Length::Fill)
    .into()
}
