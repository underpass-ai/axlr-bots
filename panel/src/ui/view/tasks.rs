//! Tasks: the list across bots, filters, sending a task and stopping one.

use iced::widget::text::Wrapping;
use iced::widget::{Space, button, column, container, pick_list, responsive, row, scrollable, table, text, text_input};
use iced::{Alignment, Length, Padding};

use super::grid;
use super::widgets::{
    BODY, El, SMALL, Voice, action, bold, card, empty, icon_button, muted, pill, reader, section_title, title,
};
use crate::application::RECENT_TASKS;
use crate::domain::{Task, TaskStatus};
use crate::ui::format;
use crate::ui::styles::{self, Kind};
use crate::ui::table::{ColumnRef, List, RowRef, SortColumn, TaskColumn};
use crate::ui::{App, BotChoice, Icon, Message, ReaderKey, Screen, Tokens, Tone, passages};

/// The status chips, in the order an operator looks for them, each
/// named as its tasks' pills are.
const FILTERS: [(Option<TaskStatus>, &str); 8] = [
    (None, "All"),
    status(TaskStatus::WaitingApproval),
    status(TaskStatus::Running),
    status(TaskStatus::Pending),
    status(TaskStatus::Failed),
    status(TaskStatus::Expired),
    status(TaskStatus::Completed),
    status(TaskStatus::Cancelled),
];

const fn status(status: TaskStatus) -> (Option<TaskStatus>, &'static str) {
    (Some(status), status.label())
}

pub fn view<'a>(app: &'a App, tokens: Tokens) -> El<'a> {
    let Some(fleet) = &app.fleet else {
        return super::overview::loading(app, tokens);
    };
    let mut page = column![
        row![
            title(Icon::Tasks, "Tasks", tokens),
            Space::new().width(Length::Fill),
            action(
                Kind::Primary,
                Icon::Plus,
                "New task",
                (!app.submit.open).then_some(Message::SubmitOpen(None)),
                tokens
            ),
        ]
        .align_y(Alignment::Center)
    ]
    .spacing(18);
    if app.submit.open {
        page = page.push(submit_form(app, tokens));
    }

    let mut chips = row![].spacing(8).align_y(Alignment::Center);
    for (status, label) in FILTERS {
        let on = app.filter.status == status;
        let count = match status {
            Some(s) => fleet.tasks.iter().filter(|t| t.status == s).count(),
            None => fleet.tasks.len(),
        };
        chips = chips.push(
            button(text(format!("{label} {count}")).size(SMALL).font(super::widgets::BOLD))
                .padding(Padding::from([6, 12]))
                .style(styles::button_style(tokens, Kind::Chip(on)))
                .on_press(Message::FilterStatus(status)),
        );
    }
    let bots: Vec<BotChoice> = std::iter::once(BotChoice::All)
        .chain(fleet.bots.iter().map(|b| BotChoice::One(b.id.clone())))
        .collect();
    let selected = Some(app.filter.bot.clone().map(BotChoice::One).unwrap_or(BotChoice::All));
    // The chips go on to a second line in a narrow window; the bot
    // picker keeps its width beside them.
    page = page.push(
        row![
            container(chips.wrap().vertical_spacing(8)).width(Length::Fill),
            pick_list(bots, selected, Message::FilterBot)
                .padding(8)
                .width(220)
                .style(styles::picker(tokens))
                .menu_style(styles::menu(tokens))
        ]
        .spacing(12)
        .align_y(Alignment::Center),
    );

    if fleet.tasks.len() >= RECENT_TASKS as usize {
        page = page.push(muted(
            format!(
                "The list holds the newest {RECENT_TASKS} tasks. The counts, the order and Select all cover only those."
            ),
            tokens,
        ));
    }

    let list: El<'a> = if app.filter.apply(&fleet.tasks).is_empty() {
        empty(Icon::Tasks, "No task here", tokens)
    } else {
        responsive(move |space| list(app, &fleet.tasks, space.width < NARROW, tokens))
            .height(Length::Shrink)
            .into()
    };
    page = page.push(card(list, tokens));
    let page = scrollable(page.padding(iced::Padding {
        right: 12.0,
        bottom: grid::BAR_ROOM,
        ..Default::default()
    }))
    .id(super::widgets::PAGE)
    .height(Length::Fill);
    grid::with_bar(app, List::Tasks, page.into(), tokens)
}

/// Under this width the table leaves out From, so the bot and the task
/// keep room enough to read: the smallest window.
const NARROW: f32 = 820.0;

/// The tasks the filter shows, in the table's order: a box, the status,
/// the bot and the task sharing what the narrow columns leave, the
/// origin unless `narrow`, the age and the row's acts as icons.
fn list<'a>(app: &'a App, tasks: &'a [Task], narrow: bool, tokens: Tokens) -> El<'a> {
    let state = &app.tables.tasks;
    let shown = state.sort.order(app.filter.apply(tasks));
    let now = app.now;
    let head = |c: TaskColumn| {
        grid::header(
            c.label(),
            state.sort.badge(c),
            Message::SortBy(ColumnRef::Tasks(c)),
            tokens,
        )
    };
    let all = state.selection.covers(shown.iter().map(|t| &t.id));
    let mut columns = vec![
        table::column(
            grid::check(all, |on| Message::PickShown(List::Tasks, on), tokens),
            move |t: &'a Task| {
                let id = t.id.clone();
                grid::check(
                    state.selection.contains(&t.id),
                    move |on| Message::Pick(RowRef::Task(id.clone()), on),
                    tokens,
                )
            },
        ),
        table::column(head(TaskColumn::Status), move |t: &'a Task| {
            pill(Tone::of_task(t.status), t.status.label(), tokens)
        }),
        // The bot and the task share what the narrow columns leave: a
        // long bot name is cut, never the task squeezed.
        table::column(head(TaskColumn::Bot), move |t: &'a Task| {
            grid::fitted(t.bot_id.to_string(), true, tokens)
        })
        .width(Length::FillPortion(if narrow { 3 } else { 2 })),
        table::column(head(TaskColumn::Task), move |t: &'a Task| {
            text(t.headline(64)).wrapping(Wrapping::WordOrGlyph)
        })
        .width(Length::FillPortion(5)),
    ];
    if !narrow {
        columns.push(table::column(head(TaskColumn::From), move |t: &'a Task| -> El<'a> {
            muted(grid::shorten(&t.trigger.describe(), 14), tokens)
                .wrapping(Wrapping::None)
                .into()
        }));
    }
    columns.push(table::column(head(TaskColumn::Age), move |t: &'a Task| {
        text(format::age(t.created_at, now)).wrapping(Wrapping::None)
    }));
    // Each act keeps its slot, there or not, so Stop and Open stand in
    // one column down the table whatever the row's status.
    let deciding = shown.iter().any(|t| t.status == TaskStatus::WaitingApproval);
    columns.push(table::column(text(""), move |t: &'a Task| -> El<'a> {
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
    }));
    grid::sheet(columns, shown)
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
                    None => muted("urgent: it goes first and stops what the bot is doing", tokens).into(),
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
