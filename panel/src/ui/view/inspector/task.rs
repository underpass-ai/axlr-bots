//! A task in the inspector: its status, bot and trigger with its acts
//! (Approve, Deny, Stop, Run again), what it is about, its facts, what it
//! waits for or why it ended, the call that waits for a person, and its
//! tabs: Activity, Answer, Files, Runs, Timeline and Raw.

use iced::widget::{Space, column, container, markdown, row, scrollable, text, text_editor};
use iced::{Alignment, Font, Length, Padding};

use super::super::widgets::{
    BODY, El, REGULAR, SMALL, action, bold, chip, copy_button, empty, icon, icon_button, muted, pill, section_title,
};
use super::{Fact, part};
use crate::application::TaskDetail;
use crate::domain::{Event, Execution, ExecutionKind, NodeWait, PostDraft, Task, TaskId, TaskStatus};
use crate::ui::find::TaskWhat;
use crate::ui::inspector::{Tab, TaskTab};
use crate::ui::styles::{self, Kind};
use crate::ui::{App, Icon, Message, Screen, Tokens, Tone, format};

/// How tall the call that waits for a person stands before it scrolls,
/// so the tabs stay in sight under it.
const WAITING_HEIGHT: f32 = 300.0;

/// The task inspected, `width` wide: its header and facts as the fleet
/// last told them until its detail is read, its tabs once it is.
pub fn view<'a>(app: &'a App, id: &'a TaskId, width: f32, tokens: Tokens) -> El<'a> {
    let detail = app.task.as_ref().filter(|d| &d.task.id == id);
    let task = detail
        .map(|d| &d.task)
        .or_else(|| app.fleet.as_ref().and_then(|f| f.tasks.iter().find(|t| &t.id == id)));
    let Some(task) = task else {
        return column![
            super::head(app, Vec::new(), Vec::new(), id.as_str(), None, width, tokens),
            empty(Icon::Hourglass, "Loading the task…", tokens)
        ]
        .into();
    };
    let mut pane = column![
        header(app, task, width, tokens),
        facts(app, task, detail, width, tokens)
    ];
    if let Some(strip) = ended(task, tokens).or_else(|| detail.and_then(|d| waits(app, d, tokens))) {
        pane = pane.push(strip);
    }
    if let Some(waiting) = detail.and_then(|d| approval(app, d, tokens)) {
        pane = pane.push(waiting);
    }
    // The tabs and the Activity, which follows the task by itself, stand
    // while its detail is read: moving from row to row never flickers.
    let tab = app.tabs.task;
    pane = pane.push(super::tabs(Tab::Task(tab), |t| count(app, task, detail, t), tokens));
    let loading = || empty(Icon::Hourglass, "Loading the task…", tokens);
    pane.push(match (tab, detail) {
        (TaskTab::Activity, _) => super::steps::view(app, task.status, width, tokens),
        (_, None) => loading(),
        (TaskTab::Answer, Some(detail)) => super::body(answer(app, detail, tokens)),
        (TaskTab::Files, Some(detail)) => super::body(files(app, detail, tokens)),
        (TaskTab::Runs, Some(detail)) => super::body(runs(app, detail, tokens)),
        (TaskTab::Timeline, Some(detail)) => super::body(timeline(app, detail, tokens)),
        (TaskTab::Raw, Some(_)) => super::body(raw(task, tokens)),
    })
    .into()
}

/// The number beside a tab: the calls of the Activity, the files, the
/// runs.
fn count(app: &App, task: &Task, detail: Option<&TaskDetail>, tab: Tab) -> Option<usize> {
    match tab {
        Tab::Task(TaskTab::Activity) => app.live.as_ref().filter(|l| l.id == task.id && l.read).map(|l| {
            l.activity
                .entries
                .iter()
                .filter(|e| matches!(e, crate::application::ActivityEntry::Tool(_)))
                .count()
        }),
        Tab::Task(TaskTab::Files) => detail.map(|d| d.executions.iter().map(|e| e.result.artifacts.len()).sum()),
        Tab::Task(TaskTab::Runs) => detail.map(|d| d.executions.len()),
        _ => None,
    }
}

/// The status, its bot (which opens it), what started it, the acts and
/// what it is about: the event's message with the objective under it,
/// or the objective.
fn header<'a>(app: &'a App, task: &'a Task, width: f32, tokens: Tokens) -> El<'a> {
    let mut first = vec![pill(Tone::of_task(task.status), task.status.label(), tokens)];
    if task.is_urgent() {
        first.push(pill(Tone::Wait, "urgent", tokens));
    }
    first.push(super::super::widgets::link(
        task.bot_id.to_string(),
        Message::Go(Screen::Bot(task.bot_id.clone())),
        tokens,
    ));
    first.push(
        row![
            icon(
                super::super::tasks::trigger_icon(task.trigger.kind()),
                13.0,
                tokens.muted
            ),
            muted(task.trigger.label().to_string(), tokens).wrapping(text::Wrapping::None)
        ]
        .spacing(4)
        .align_y(Alignment::Center)
        .into(),
    );
    let mut acts = Vec::new();
    // The bot's chat writes to its running task.
    if task.status == TaskStatus::Running {
        acts.push(action(
            Kind::Ghost,
            Icon::Bots,
            "Talk to it",
            Some(Message::Go(Screen::Bot(task.bot_id.clone()))),
            tokens,
        ));
    }
    if task.status == TaskStatus::WaitingApproval {
        acts.push(if app.deciding.is_some() {
            row![icon(Icon::Hourglass, 16.0, tokens.wait), bold("Sending…").size(SMALL)]
                .spacing(6)
                .align_y(Alignment::Center)
                .into()
        } else {
            row![
                action(
                    Kind::Good,
                    Icon::Check,
                    "Approve",
                    Some(Message::Approve(task.id.clone())),
                    tokens
                ),
                action(
                    Kind::Danger,
                    Icon::Cross,
                    "Deny",
                    Some(Message::AskDeny(task.id.clone())),
                    tokens
                ),
            ]
            .spacing(6)
            .into()
        });
    }
    if task.status.can_cancel() {
        acts.push(action(
            Kind::Secondary,
            Icon::Stop,
            "Stop",
            Some(Message::AskCancel(task.id.clone())),
            tokens,
        ));
    } else {
        acts.push(action(
            Kind::Secondary,
            Icon::Update,
            "Run again",
            (!app.submit.open).then(|| Message::RunAgain(task.id.clone())),
            tokens,
        ));
    }
    let what = TaskWhat::of(task);
    let under = what.message.map(|_| what.objective);
    super::head(app, first, acts, what.title(), under, width, tokens)
}

/// Node, started, took, cost, from, team, task id and trace.
fn facts<'a>(app: &'a App, task: &'a Task, detail: Option<&'a TaskDetail>, width: f32, tokens: Tokens) -> El<'a> {
    let node = detail
        .and_then(|d| d.executions.last())
        .map(|e| e.place().to_string())
        .unwrap_or_else(|| "—".into());
    let took = match detail {
        _ if !task.status.is_terminal() => format!("{} so far", format::span(app.now - task.created_at)),
        Some(d) => format::span(d.last_change() - task.created_at),
        None => format::span(task.updated_at - task.created_at),
    };
    let cost = detail.map_or_else(
        || "—".to_string(),
        |d| {
            format!(
                "{} · {}",
                format::count(u64::from(d.total_usage().tool_calls), "call", "calls"),
                format::spend(d.cost())
            )
        },
    );
    let team = app
        .fleet
        .as_ref()
        .and_then(|f| f.bots.iter().find(|b| b.id == task.bot_id))
        .map(|b| b.groups.join(", "))
        .filter(|t| !t.is_empty())
        .unwrap_or_else(|| "—".into());
    let from = task.told_from().map_or_else(|| task.trigger.describe(), str::to_string);
    super::facts(
        vec![
            ("Node", Fact::Words(node)),
            (
                "Started",
                Fact::Words(format!(
                    "{} · {}",
                    format::clock(task.created_at),
                    format::ago(task.created_at, app.now)
                )),
            ),
            ("Took", Fact::Words(took)),
            ("Cost", Fact::Words(cost)),
            ("From", Fact::Words(from)),
            ("Team", Fact::Words(team)),
            ("Task id", Fact::Copy(task.id.to_string())),
            ("Trace", Fact::Copy(task.correlation_id.clone())),
        ],
        width,
        tokens,
    )
}

/// Why a task that ended badly ended, on a line in its tone: an expired
/// task is nobody's decision in time, a stopped one a person's.
fn ended<'a>(task: &'a Task, tokens: Tokens) -> Option<El<'a>> {
    if task.failure_reason.is_empty() {
        return None;
    }
    let (glyph, why, tone) = match task.status {
        TaskStatus::Expired => (Icon::Hourglass, "Why it expired", Tone::Late),
        TaskStatus::Cancelled => (Icon::Stop, "Why it stopped", Tone::Idle),
        _ => (Icon::Cross, "Why it failed", Tone::Bad),
    };
    Some(strip(
        row![
            icon(glyph, 16.0, tone.color(&tokens)),
            bold(why).size(SMALL),
            text(task.failure_reason.clone()).size(SMALL).width(Length::Fill),
            copy_button("Copy reason", task.failure_reason.clone(), tokens),
        ]
        .spacing(10)
        .align_y(Alignment::Center)
        .into(),
        tone,
        tokens,
    ))
}

/// What the task waits for: its decision while its node is away or
/// busy; a placed bot's Pending task while no node its placement admits
/// may take it.
fn waits<'a>(app: &'a App, detail: &'a TaskDetail, tokens: Tokens) -> Option<El<'a>> {
    let said = match detail.task.status {
        TaskStatus::Pending => detail.task.waits_for()?,
        TaskStatus::Running => format::wait(&NodeWait::current(timeline_source(app, detail))?),
        _ => return None,
    };
    Some(strip(
        row![
            icon(Icon::Hourglass, 16.0, Tone::Late.color(&tokens)),
            bold(said).size(SMALL).width(Length::Fill),
        ]
        .spacing(10)
        .align_y(Alignment::Center)
        .into(),
        Tone::Late,
        tokens,
    ))
}

/// A line of the inspector in a tone, with the rule under it.
fn strip<'a>(content: El<'a>, tone: Tone, tokens: Tokens) -> El<'a> {
    super::ruled(
        container(content)
            .padding(Padding::from([7, 14]))
            .width(Length::Fill)
            .style(styles::tone_card(tokens, tone))
            .into(),
        tokens,
    )
}

/// The call the task stopped on: the tool, the post it would publish as
/// it would read, and its arguments, each with a copy. Approve and Deny
/// are in the header.
fn approval<'a>(app: &'a App, detail: &'a TaskDetail, tokens: Tokens) -> Option<El<'a>> {
    let pending = detail.pending_call()?;
    let mut arguments = column![].spacing(6);
    for (name, value) in pending.argument_lines() {
        arguments = arguments.push(
            row![
                container(bold(name).size(SMALL)).width(96),
                container(text(value.clone()).font(Font::MONOSPACE).size(12.5))
                    .padding(6)
                    .width(Length::Fill)
                    .style(styles::chip(tokens)),
                icon_button(Icon::Copy, "Copy", Some(Message::Copy(value)), tokens),
            ]
            .spacing(8)
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
        super::super::post_draft::view(draft, author, tokens)
    });
    let mut body = column![
        row![
            section_title(Icon::Alert, "Needs OK", tokens),
            muted("it wants to call", tokens),
            bold(pending.tool.clone()).size(BODY),
            Space::new().width(Length::Fill),
            copy_button("Copy call", pending.call_text(), tokens),
        ]
        .spacing(8)
        .align_y(Alignment::Center)
    ]
    .spacing(10);
    if let Some(draft) = draft {
        body = body.push(draft);
    }
    body = body.push(arguments);
    Some(super::ruled(
        container(scrollable(container(body).padding(Padding::from([10, 14]))).width(Length::Fill))
            .max_height(WAITING_HEIGHT)
            .width(Length::Fill)
            .style(styles::tone_card(tokens, Tone::Wait))
            .into(),
        tokens,
    ))
}

/// The answer to read or to select, what it said so far while it has
/// none, what it was asked, what it knew and what it thought.
fn answer<'a>(app: &'a App, detail: &'a TaskDetail, tokens: Tokens) -> El<'a> {
    let task = &detail.task;
    let live = app.live.as_ref().filter(|l| l.id == task.id && l.read);
    let mut page = column![].spacing(16);
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
            let style = super::super::widgets::markdown_style(tokens);
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
        let title = match task.status {
            TaskStatus::WaitingApproval => "What it said before asking you",
            status if !status.is_terminal() => "What it said before it stopped",
            _ => "Answer",
        };
        page = page.push(part(
            title,
            column![
                row![toggle, copy_button("Copy answer", answer.source.clone(), tokens)].spacing(6),
                body
            ]
            .spacing(8),
            tokens,
        ));
    } else if let Some(live) = live.filter(|l| !l.activity.text.is_empty()) {
        // While it runs: its words so far, formatted as they come, or as
        // text to select.
        let (body, toggle): (El<'a>, El<'a>) = if live.selecting {
            (
                text_editor(&live.words)
                    .on_action(Message::WordsSelect)
                    .font(REGULAR)
                    .size(BODY)
                    .padding(12)
                    .style(styles::reader(tokens))
                    .into(),
                action(
                    Kind::Ghost,
                    Icon::File,
                    "Formatted",
                    Some(Message::WordsSelecting(false)),
                    tokens,
                ),
            )
        } else {
            let style = super::super::widgets::markdown_style(tokens);
            (
                markdown::view(live.markdown.items(), markdown::Settings::with_text_size(BODY, style))
                    .map(Message::Copy),
                action(
                    Kind::Ghost,
                    Icon::File,
                    "Select words",
                    Some(Message::WordsSelecting(true)),
                    tokens,
                ),
            )
        };
        let mut tools = row![toggle, copy_button("Copy words", live.activity.text.clone(), tokens)].spacing(6);
        if live.activity.text_cut {
            tools = tools.push(muted("cut at 32 KiB", tokens));
        }
        page = page.push(part("What it says so far", column![tools, body].spacing(8), tokens));
    } else {
        page = page.push(muted("No answer yet.", tokens));
    }
    page = page.push(part(
        "It was asked",
        column![
            text(task.objective.clone()).size(BODY),
            copy_button("Copy task", task.objective.clone(), tokens)
        ]
        .spacing(6),
        tokens,
    ));
    let knew = task.knew();
    if !knew.is_empty() {
        let mut notes = column![muted("What its bot had heard, given to it when it started", tokens)].spacing(4);
        for note in knew {
            notes = notes.push(text(note).size(SMALL));
        }
        page = page.push(part("Knew", notes, tokens));
    }
    let earlier = task.earlier();
    if !earlier.is_empty() {
        let mut notes = column![muted("What its node read of the team's memory before the run", tokens)].spacing(4);
        for read in earlier {
            notes = notes.push(text(read).size(SMALL));
        }
        page = page.push(part("Read before", notes, tokens));
    }
    if let Some(live) = live.filter(|l| !l.activity.thinking.is_empty()) {
        let mut tools = row![copy_button("Copy thinking", live.activity.thinking.clone(), tokens)].spacing(6);
        if live.activity.thinking_cut {
            tools = tools.push(muted("cut at 16 KiB", tokens));
        }
        page = page.push(part(
            "What it thought",
            column![
                tools,
                text(live.activity.thinking.clone())
                    .size(SMALL)
                    .font(Font {
                        style: iced::font::Style::Italic,
                        ..REGULAR
                    })
                    .color(tokens.muted)
            ]
            .spacing(6),
            tokens,
        ));
    }
    page.into()
}

/// The files the runs delivered: name, media type and size, and Save for
/// those the control plane keeps.
fn files<'a>(app: &'a App, detail: &'a TaskDetail, tokens: Tokens) -> El<'a> {
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
    if !any {
        return empty(Icon::File, "It delivered no file", tokens);
    }
    list.into()
}

/// Its runs with what they cost and the calls each made, then the tree
/// of everything it did with the tasks it asked.
fn runs<'a>(app: &'a App, detail: &'a TaskDetail, tokens: Tokens) -> El<'a> {
    let usage = detail.total_usage();
    let mut page = column![
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
        .spacing(6)
        .wrap()
        .vertical_spacing(6)
    ]
    .spacing(12);
    for execution in &detail.executions {
        page = page.push(execution_card(app, execution, tokens));
    }
    if detail.executions.is_empty() {
        // A task that ended before it started never waits for a node.
        let never = if detail.task.status.is_terminal() {
            "Never ran: it ended before a node took it"
        } else {
            "Not run yet: waiting for a node"
        };
        page = page.push(empty(Icon::Hourglass, never, tokens));
    }
    page.push(super::tree::view(app, &detail.task.id, tokens)).into()
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
    .spacing(8)
    .align_y(Alignment::Center)
    .wrap()
    .vertical_spacing(6);
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
                    container(muted(when, tokens)).width(62),
                    container(bold(step.tool.clone()).size(SMALL)).width(120),
                    pill(tone, step.why(), tokens),
                    muted(step.ended().to_string(), tokens),
                    Space::new().width(Length::Fill),
                    muted(took, tokens).wrapping(text::Wrapping::None),
                ]
                .spacing(8)
                .align_y(Alignment::Center)
            ]
            .spacing(2);
            let about = rules.map(|r| r.about(&step.tool)).unwrap_or_default();
            if !about.is_empty() {
                line = line.push(
                    container(muted(format!("Its rules: {}", about.join(" · ")), tokens))
                        .padding(Padding::ZERO.left(70.0)),
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
        .spacing(10)
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
        // The level and tools it ran with.
        if !policy.level.is_empty() {
            let tools = match policy.allowed_tools.len() {
                0 => String::new(),
                1 => format!(" · {}", policy.allowed_tools[0]),
                n => format!(" · {n} tools"),
            };
            ids = ids.push(muted(format!("{}{tools}", policy.level), tokens));
        }
    }
    body = body.push(ids.wrap().vertical_spacing(4));
    container(body)
        .padding(12)
        .width(Length::Fill)
        .style(styles::chip(tokens))
        .into()
}

/// The task's timeline, from the live read when it came, which holds the
/// whole of it.
fn timeline_source<'a>(app: &'a App, detail: &'a TaskDetail) -> &'a [Event] {
    match app.live.as_ref().filter(|l| l.id == detail.task.id && l.read) {
        Some(live) => &live.feed.events,
        None => &detail.events,
    }
}

/// What happened to the task, oldest first, without its run's progress,
/// which the Activity shows; a copy of it all.
fn timeline<'a>(app: &'a App, detail: &'a TaskDetail, tokens: Tokens) -> El<'a> {
    let events: Vec<&Event> = timeline_source(app, detail)
        .iter()
        .filter(|e| !e.is_progress())
        .collect();
    if events.is_empty() {
        return empty(Icon::Clock, "No event yet", tokens);
    }
    let mut list = column![copy_button(
        "Copy timeline",
        format::events(events.iter().copied()),
        tokens
    )]
    .spacing(8);
    for event in events {
        let tone = Tone::of_event(event.tone());
        list = list.push(
            row![
                container(muted(format::clock(event.created_at), tokens)).width(62),
                icon(tone.event_icon(), 14.0, tone.color(&tokens)),
                bold(event.label()).size(SMALL),
                muted(format::event_detail(event), tokens).width(Length::Fill),
            ]
            .spacing(8)
            .align_y(Alignment::Center),
        );
    }
    list.into()
}

/// The task as the control plane keeps it, as JSON, and a copy of it.
fn raw<'a>(task: &'a Task, tokens: Tokens) -> El<'a> {
    let json = serde_json::to_string_pretty(task).unwrap_or_default();
    column![
        copy_button("Copy JSON", json.clone(), tokens),
        text(json)
            .size(12.0)
            .font(Font::MONOSPACE)
            .wrapping(text::Wrapping::WordOrGlyph)
    ]
    .spacing(8)
    .into()
}
