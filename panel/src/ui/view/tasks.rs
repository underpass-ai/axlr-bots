//! Tasks: the list across bots, filters, sending a task and stopping one.

use std::rc::Rc;

use iced::widget::{Space, column, container, pick_list, row, text, text_input};
use iced::{Alignment, Length, Padding};

use super::sheet::{self, Column, Head, Ink, Line};
use super::widgets::{
    BODY, El, SMALL, Voice, action, bold, card, empty, icon, icon_button, muted, reader, section_title,
};
use super::{grid, listbar, status};
use crate::domain::TaskStatus;
use crate::ui::find::{FoldRepeats, Query, TaskGroup, TaskLine, TaskWhat};
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
    let query = app.list_query();
    // Beside the inspector the list keeps its filter, its note and its
    // act; its chips and options wait for it to close.
    let narrow = app.screen.inspected().is_some();
    let through = app.tasks_through().len();
    let groups = app.task_groups();
    let lines: Vec<TaskLine<'a>> = groups.iter().flat_map(|g| g.lines.iter().cloned()).collect();
    let total = format::count(through as u64, "task", "tasks");
    let count = if lines.len() < through {
        format!("{} · {total}", format::count(lines.len() as u64, "line", "lines"))
    } else if through == fleet.tasks.len() {
        total
    } else {
        format!("{through} of {}", fleet.tasks.len())
    };
    // The filter, then a chip per status that has tasks, and the one
    // that is on even when its last task left it.
    let mut chips = vec![listbar::filter(&app.tables.query, tokens)];
    if !narrow {
        chips.push(listbar::chip(
            "All",
            fleet.tasks.len(),
            app.filter.status.is_none(),
            Message::FilterStatus(None),
            tokens,
        ));
    }
    for status in FILTERS.into_iter().filter(|_| !narrow) {
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
    let grouping = app.tables.grouping;
    let options = vec![
        listbar::flag(
            "Fold repeats",
            None,
            app.tables.fold.is_on(),
            Message::FoldToggled,
            tokens,
        ),
        listbar::flag(
            "Group:",
            Some(grouping.label()),
            grouping.is_grouped(),
            Message::GroupingCycled,
            tokens,
        ),
        pick_list(bots, selected, Message::FilterBot)
            .padding([3, 8])
            .text_size(12.5)
            .width(150)
            .style(styles::picker(tokens))
            .menu_style(styles::menu(tokens))
            .into(),
    ];
    let acts = vec![action(
        Kind::Primary,
        Icon::Plus,
        "New task",
        (!app.submit.open).then_some(Message::SubmitOpen(None)),
        tokens,
    )];
    let mut page = column![listbar::bar("Tasks", count, chips, acts, tokens)];
    if app.submit.open {
        page = page.push(listbar::panel(submit_form(app, tokens), tokens));
    }
    let (options, keys) = if narrow {
        (Vec::new(), None)
    } else {
        (options, Some(listbar::keys(tokens)))
    };
    page = page.push(listbar::note(
        FoldRepeats::note(&lines, fleet.tasks.len()),
        None,
        options,
        keys,
        tokens,
    ));
    page = page.push(if through == 0 && !query.is_empty() {
        listbar::nothing(empty(
            Icon::Lens,
            format!(
                "Nothing matches {} in the newest {} tasks.",
                query.text(),
                fleet.tasks.len()
            ),
            tokens,
        ))
    } else if through == 0 {
        listbar::nothing(empty(Icon::Tasks, "No task here", tokens))
    } else {
        list(app, groups, query, narrow, tokens)
    });
    grid::with_bar(app, List::Tasks, page.into(), tokens)
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

/// The `×12` of a line that folds repeats, and the room it takes.
fn badge<'a>(line: &TaskLine<'_>, tokens: Tokens) -> Option<(El<'a>, f32)> {
    let words = line.badge()?;
    let width = 12.0 + 7.0 * words.chars().count() as f32;
    let tag = container(text(words).size(11.5).color(tokens.muted))
        .padding(Padding::from([0, 5]))
        .style(styles::count_tag(tokens));
    Some((tag.into(), width))
}

/// The task lines on show, in their groups, one line each: a box (for
/// every task the line holds), the status, the bot, what it is about
/// with the count of the repeats it folds, what started it, when, how
/// long ago, and the row's acts as icons. A click on a line opens its
/// task in the inspector; beside it, `narrow`, a line keeps the
/// status's glyph, the bot, what it is about and its age.
fn list<'a>(app: &'a App, groups: Vec<TaskGroup<'a>>, query: Query, narrow: bool, tokens: Tokens) -> El<'a> {
    let state = &app.tables.tasks;
    let now = app.now;
    let query = Rc::new(query);
    let head = |c: TaskColumn| Head::Sort {
        label: c.label(),
        badge: state.sort.badge(c),
        column: ColumnRef::Tasks(c),
    };
    let on_show: Vec<&TaskLine<'a>> = TaskGroup::shown(&groups);
    let all = state
        .selection
        .covers(on_show.iter().flat_map(|l| l.alike.iter().map(|t| &t.id)));
    // Each act keeps its slot, there or not, so Stop and Open stand in
    // one column down the table whatever the row's status.
    let deciding = on_show.iter().any(|l| l.task.status == TaskStatus::WaitingApproval);
    let slots = if deciding { 4.0 } else { 2.0 };
    // The lines drawn, each row its line's place among them.
    let mut shown: Vec<TaskLine<'a>> = Vec::new();
    let mut lines = Vec::new();
    let open = state.cursor.selected();
    for group in groups {
        if app.tables.grouping.is_grouped() {
            lines.push(Line::Group {
                name: group.label.clone(),
                note: group.count().to_string(),
                open: group.open,
                press: Box::new(Message::GroupToggled(group.key.clone())),
            });
        }
        if !group.open {
            continue;
        }
        for line in group.lines {
            lines.push(Line::Row {
                row: shown.len(),
                picked: line.alike.iter().all(|t| state.selection.contains(&t.id)),
                selected: open.is_some_and(|id| line.holds(id)),
                press: Box::new(Message::RowPicked(RowRef::Task(line.key(open)))),
            });
            shown.push(line);
        }
    }
    let shown = Rc::new(shown);
    let line = move |at: usize| -> TaskLine<'a> { shown[at].clone() };
    let of = |cell: fn(&'a App, &TaskLine<'a>, f32, &Query, Tokens) -> El<'a>| {
        let line = line.clone();
        let query = query.clone();
        move |at: usize, width: f32| cell(app, &line(at), width, &query, tokens)
    };
    let what = Column::share(
        head(TaskColumn::Task),
        1,
        of(|_, line, width, query, tokens| {
            let what = TaskWhat::of(line.task);
            let rest = what.rest().unwrap_or_default().to_string();
            match badge(line, tokens) {
                Some((tag, room)) => row![sheet::pair(what.title(), &rest, width - room - 6.0, query, tokens), tag]
                    .spacing(6)
                    .align_y(Alignment::Center)
                    .into(),
                None => sheet::pair(what.title(), &rest, width, query, tokens),
            }
        }),
    );
    let age = Column::fixed(head(TaskColumn::Age), 40.0, {
        let line = line.clone();
        move |at: usize, _| {
            Ink::Quiet
                .write(format::age(line(at).task.created_at, now), tokens)
                .into()
        }
    })
    .end();
    if narrow {
        let columns = vec![
            Column::fixed(
                Head::None,
                12.0,
                of(|_, line, _, _, tokens| status::task_glyph_only(line.task.status, tokens)),
            ),
            Column::fixed(
                head(TaskColumn::Bot),
                108.0,
                of(|_, line, width, query, tokens| {
                    sheet::found(line.task.bot_id.to_string(), width, Ink::Strong, query, tokens)
                }),
            ),
            what,
            age,
        ];
        return sheet::sheet(columns, lines, app.density, app.tables.picked(List::Tasks) > 0, tokens);
    }
    let columns = vec![
        Column::fixed(
            Head::Check { all, list: List::Tasks },
            18.0,
            of(|app, line, _, _, tokens| {
                let id = line.task.id.clone();
                let ticked = line.alike.iter().all(|t| app.tables.tasks.selection.contains(&t.id));
                let check = grid::check(ticked, move |on| Message::Pick(RowRef::Task(id.clone()), on), tokens);
                match line.count() {
                    1 => check,
                    n => sheet::tip(check, format!("Picks the {n} tasks this line holds"), tokens),
                }
            }),
        ),
        Column::fixed(
            head(TaskColumn::Status),
            96.0,
            of(|_, line, _, _, tokens| status::task(line.task.status, tokens)),
        ),
        Column::fixed(
            head(TaskColumn::Bot),
            150.0,
            of(|_, line, width, query, tokens| {
                sheet::found(line.task.bot_id.to_string(), width, Ink::Strong, query, tokens)
            }),
        ),
        what,
        Column::fixed(
            head(TaskColumn::From),
            180.0,
            of(|_, line, width, query, tokens| {
                let trigger = &line.task.trigger;
                row![
                    icon(trigger_icon(trigger.kind()), 12.0, tokens.muted),
                    sheet::found(trigger.label(), width - 18.0, Ink::Quiet, query, tokens)
                ]
                .spacing(6)
                .align_y(Alignment::Center)
                .into()
            }),
        ),
        Column::fixed(
            Head::Plain("At"),
            40.0,
            of(|_, line, _, _, tokens| Ink::Quiet.write(format::hour(line.task.created_at), tokens).into()),
        )
        .end(),
        age,
        Column::fixed(Head::None, slots * grid::ACT + (slots - 1.0) * 4.0, {
            let line = line.clone();
            move |at: usize, _| {
                let t = line(at).task;
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
            }
        }),
    ];
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
