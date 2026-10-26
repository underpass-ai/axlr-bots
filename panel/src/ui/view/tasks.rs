//! Tasks: the list across bots, filters, sending a task and stopping one.

use iced::widget::{Space, button, column, container, pick_list, row, scrollable, table, text, text_input};
use iced::{Alignment, Length, Padding};

use super::widgets::{BODY, El, SMALL, action, bold, card, empty, icon_button, muted, pill, section_title, title};
use crate::domain::{Task, TaskStatus};
use crate::ui::format;
use crate::ui::styles::{self, Kind};
use crate::ui::{App, BotChoice, Icon, Message, Screen, Tokens, Tone};

/// The status chips, in the order an operator looks for them.
const FILTERS: [(Option<TaskStatus>, &str); 8] = [
    (None, "All"),
    (Some(TaskStatus::WaitingApproval), "Waiting"),
    (Some(TaskStatus::Running), "Running"),
    (Some(TaskStatus::Pending), "Pending"),
    (Some(TaskStatus::Failed), "Failed"),
    (Some(TaskStatus::Expired), "Expired"),
    (Some(TaskStatus::Completed), "Done"),
    (Some(TaskStatus::Cancelled), "Stopped"),
];

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
    page = page.push(
        row![
            chips,
            Space::new().width(Length::Fill),
            pick_list(bots, selected, Message::FilterBot).padding(8)
        ]
        .align_y(Alignment::Center),
    );

    let shown = app.filter.apply(&fleet.tasks);
    let now = app.now;
    let columns = vec![
        table::column(bold("Status"), move |t: &'a Task| {
            pill(Tone::of_task(t.status), t.status.label(), tokens)
        }),
        table::column(bold("Bot"), move |t: &'a Task| bold(t.bot_id.to_string())),
        table::column(bold("Task"), move |t: &'a Task| text(t.headline(64))).width(Length::Fill),
        table::column(bold("From"), move |t: &'a Task| muted(t.trigger.describe(), tokens)),
        table::column(bold("Age"), move |t: &'a Task| text(format::age(t.created_at, now))),
        table::column(text(""), move |t: &'a Task| -> El<'a> {
            let mut buttons = row![].spacing(6).align_y(Alignment::Center);
            if t.status == TaskStatus::WaitingApproval {
                buttons = buttons.push(super::decide::buttons(app, &t.id, tokens));
            }
            if t.status.can_cancel() {
                buttons = buttons.push(icon_button(
                    Icon::Stop,
                    "Stop",
                    Some(Message::AskCancel(t.id.clone())),
                    tokens,
                ));
            }
            buttons
                .push(action(
                    Kind::Ghost,
                    Icon::Play,
                    "Open",
                    Some(Message::Go(Screen::Task(t.id.clone()))),
                    tokens,
                ))
                .into()
        }),
    ];
    let list: El<'a> = if shown.is_empty() {
        empty(Icon::Tasks, "No task here", tokens)
    } else {
        table::table(columns, shown)
            .padding_x(12)
            .padding_y(8)
            .width(Length::Fill)
            .into()
    };
    page = page.push(card(list, tokens));
    scrollable(page.padding(iced::Padding {
        right: 12.0,
        ..Default::default()
    }))
    .height(Length::Fill)
    .into()
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
    let key = form.key.as_ref().map(|k| k.as_str().to_string()).unwrap_or_default();
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
                muted(format!("key {key}: sending twice makes one task"), tokens),
            ]
            .spacing(14)
            .align_y(Alignment::Center),
        ]
        .spacing(14),
        tokens,
    )
    .into()
}
