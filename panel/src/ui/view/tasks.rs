//! Tasks: the list across bots, filters, sending a task and stopping one.

use iced::widget::{Space, column, container, pick_list, row, text_input};
use iced::{Alignment, Length};

use super::sheet::{self, Column, Head, Ink, Line};
use super::widgets::{
    BODY, El, SMALL, Voice, action, bold, card, empty, icon, icon_button, muted, reader, section_title,
};
use super::{grid, listbar, status};
use crate::application::RECENT_TASKS;
use crate::domain::{Task, TaskStatus};
use crate::ui::format;
use crate::ui::styles::{self, Kind};
use crate::ui::table::{ColumnRef, List, RowRef, SortColumn, TaskColumn};
use crate::ui::{App, BotChoice, Icon, Message, ReaderKey, Screen, Tokens, passages};

/// The status chips, in the order an operator looks for them, each
/// named as its tasks' statuses are.
const FILTERS: [TaskStatus; 7] = [
    TaskStatus::WaitingApproval,
    TaskStatus::Running,
    TaskStatus::Pending,
    TaskStatus::Failed,
    TaskStatus::Expired,
    TaskStatus::Completed,
    TaskStatus::Cancelled,
];

pub fn view<'a>(app: &'a App, tokens: Tokens) -> El<'a> {
    let Some(fleet) = &app.fleet else {
        return super::overview::loading(app, tokens);
    };
    let shown = app.filter.apply(&fleet.tasks);
    let count = if shown.len() == fleet.tasks.len() {
        format::count(shown.len() as u64, "task", "tasks")
    } else {
        format!("{} of {}", shown.len(), fleet.tasks.len())
    };
    // A chip per status that has tasks, and the one that is on even
    // when its last task left it.
    let mut chips = vec![listbar::chip(
        "All",
        fleet.tasks.len(),
        app.filter.status.is_none(),
        Message::FilterStatus(None),
        tokens,
    )];
    for status in FILTERS {
        let on = app.filter.status == Some(status);
        let count = fleet.tasks.iter().filter(|t| t.status == status).count();
        if count > 0 || on {
            chips.push(listbar::chip(
                status.label(),
                count,
                on,
                Message::FilterStatus(Some(status)),
                tokens,
            ));
        }
    }
    let bots: Vec<BotChoice> = std::iter::once(BotChoice::All)
        .chain(fleet.bots.iter().map(|b| BotChoice::One(b.id.clone())))
        .collect();
    let selected = Some(app.filter.bot.clone().map(BotChoice::One).unwrap_or(BotChoice::All));
    let acts = vec![
        pick_list(bots, selected, Message::FilterBot)
            .padding([5, 8])
            .text_size(SMALL)
            .width(190)
            .style(styles::picker(tokens))
            .menu_style(styles::menu(tokens))
            .into(),
        action(
            Kind::Primary,
            Icon::Plus,
            "New task",
            (!app.submit.open).then_some(Message::SubmitOpen(None)),
            tokens,
        ),
    ];
    let mut page = column![listbar::bar("Tasks", count, chips, acts, tokens)];
    if app.submit.open {
        page = page.push(listbar::panel(submit_form(app, tokens), tokens));
    }
    if fleet.tasks.len() >= RECENT_TASKS as usize {
        page = page.push(listbar::note(
            format!("Newest {RECENT_TASKS} tasks. The counts, the order and Select all cover only those."),
            None,
            tokens,
        ));
    }
    page = page.push(if shown.is_empty() {
        listbar::nothing(empty(Icon::Tasks, "No task here", tokens))
    } else {
        list(app, shown, tokens)
    });
    grid::with_bar(app, List::Tasks, page.into(), tokens)
}

/// What a task is about, for its one line in a list: the words of the
/// event that made it when there is one, else what it was asked; and
/// what follows them, muted: what a queued task waits for, or what the
/// event's task was asked.
pub fn what(task: &Task) -> (String, String) {
    let asked = task.ask().to_string();
    let (first, rest) = match task.event_message() {
        Some(message) => (message.lines().next().unwrap_or_default().to_string(), asked),
        None => (asked, String::new()),
    };
    match task.waits_for() {
        Some(why) => (first, why),
        None => (first, rest),
    }
}

/// The glyph of what started a task, by the kind of its trigger.
pub fn trigger_icon(kind: &str) -> Icon {
    match kind {
        "timer" => Icon::Clock,
        "event" => Icon::Bolt,
        "webhook" => Icon::Hook,
        "delegation" => Icon::Bots,
        _ => Icon::Person,
    }
}

/// The tasks the filter shows, in the table's order, one line each: a
/// box, the status, the bot, what it is about, what started it, when,
/// how long ago, and the row's acts as icons.
fn list<'a>(app: &'a App, tasks: Vec<&'a Task>, tokens: Tokens) -> El<'a> {
    let state = &app.tables.tasks;
    let shown = state.sort.order(tasks);
    let now = app.now;
    let head = |c: TaskColumn| Head::Sort {
        label: c.label(),
        badge: state.sort.badge(c),
        column: ColumnRef::Tasks(c),
    };
    let all = state.selection.covers(shown.iter().map(|t| &t.id));
    // Each act keeps its slot, there or not, so Stop and Open stand in
    // one column down the table whatever the row's status.
    let deciding = shown.iter().any(|t| t.status == TaskStatus::WaitingApproval);
    let slots = if deciding { 4.0 } else { 2.0 };
    let columns = vec![
        Column::fixed(Head::Check { all, list: List::Tasks }, 18.0, move |t: &'a Task, _| {
            let id = t.id.clone();
            grid::check(
                state.selection.contains(&t.id),
                move |on| Message::Pick(RowRef::Task(id.clone()), on),
                tokens,
            )
        }),
        Column::fixed(head(TaskColumn::Status), 96.0, move |t: &'a Task, _| {
            status::task(t.status, tokens)
        }),
        Column::fixed(head(TaskColumn::Bot), 150.0, move |t: &'a Task, width| {
            sheet::words(t.bot_id.to_string(), width, Ink::Strong, tokens)
        }),
        Column::share(head(TaskColumn::Task), 1, move |t: &'a Task, width| {
            let (first, rest) = what(t);
            sheet::pair(&first, &rest, width, tokens)
        }),
        Column::fixed(head(TaskColumn::From), 180.0, move |t: &'a Task, width| {
            row![
                icon(trigger_icon(t.trigger.kind()), 12.0, tokens.muted),
                sheet::words(t.trigger.label(), width - 18.0, Ink::Quiet, tokens)
            ]
            .spacing(6)
            .align_y(Alignment::Center)
            .into()
        }),
        Column::fixed(Head::Plain("At"), 40.0, move |t: &'a Task, _| {
            Ink::Quiet.write(format::hour(t.created_at), tokens).into()
        })
        .end(),
        Column::fixed(head(TaskColumn::Age), 40.0, move |t: &'a Task, _| {
            Ink::Quiet.write(format::age(t.created_at, now), tokens).into()
        })
        .end(),
        Column::fixed(
            Head::None,
            slots * grid::ACT + (slots - 1.0) * 4.0,
            move |t: &'a Task, _| {
                let mut buttons = row![].spacing(4).align_y(Alignment::Center);
                if deciding {
                    buttons = buttons.push(if t.status == TaskStatus::WaitingApproval {
                        super::decide::icons(app, &t.id, tokens)
                    } else {
                        Space::new().width(2.0 * grid::ACT + 4.0).into()
                    });
                }
                buttons
                    .push(grid::act_slot(t.status.can_cancel().then(|| {
                        grid::icon_act(
                            Kind::Ghost,
                            Icon::Stop,
                            "Stop",
                            Some(Message::AskCancel(t.id.clone())),
                            tokens,
                        )
                    })))
                    .push(grid::act_slot(Some(grid::icon_act(
                        Kind::Ghost,
                        Icon::Next,
                        "Open",
                        Some(Message::Go(Screen::Task(t.id.clone()))),
                        tokens,
                    ))))
                    .into()
            },
        ),
    ];
    let lines = shown
        .into_iter()
        .map(|t| Line::Row {
            row: t,
            picked: state.selection.contains(&t.id),
        })
        .collect();
    sheet::sheet(columns, lines, app.density, app.tables.picked(List::Tasks) > 0, tokens)
}

fn submit_form<'a>(app: &'a App, tokens: Tokens) -> El<'a> {
    let form = &app.submit;
    let bots: Vec<BotChoice> = app
        .fleet
        .iter()
        .flat_map(|f| {
            f.bots
                .iter()
                .filter(|b| b.status.accepts_tasks())
                .map(|b| BotChoice::One(b.id.clone()))
        })
        .collect();
    let selected = form.bot.clone().map(BotChoice::One);
    let objective = text_input("What should the bot do?", &form.objective)
        .on_input(Message::SubmitObjective)
        .on_submit(Message::SubmitSend)
        .padding(12)
        .size(BODY)
        .style(styles::field(tokens));
    card(
        column![
            row![
                section_title(Icon::Plus, "New task", tokens),
                Space::new().width(Length::Fill),
                icon_button(Icon::Cross, "Close", Some(Message::SubmitClose), tokens)
            ]
            .align_y(Alignment::Center),
            row![
                column![
                    bold("Bot").size(SMALL),
                    pick_list(bots, selected, Message::SubmitBot)
                        .placeholder("Pick a bot")
                        .padding(10)
                        .style(styles::picker(tokens))
                        .menu_style(styles::menu(tokens))
                ]
                .spacing(6),
                container(column![bold("Task").size(SMALL), objective].spacing(6)).width(Length::Fill),
            ]
            .spacing(14)
            .align_y(Alignment::End),
            row![
                action(
                    Kind::Primary,
                    Icon::Play,
                    if form.busy { "Sending…" } else { "Send" },
                    form.ready().then_some(Message::SubmitSend),
                    tokens
                ),
                action(
                    Kind::Chip(form.urgent),
                    Icon::Bolt,
                    "Urgent",
                    Some(Message::SubmitUrgent(!form.urgent)),
                    tokens
                ),
                match passages::submit_key(form) {
                    Some(words) => reader(&app.readers, ReaderKey::SubmitKey, words, Voice::Muted, tokens),
                    None => muted(
                        "urgent: it goes first, stops what the bot is doing and drops its queue",
                        tokens
                    )
                    .into(),
                },
            ]
            .spacing(14)
            .align_y(Alignment::Center),
        ]
        .spacing(14),
        tokens,
    )
    .into()
}
