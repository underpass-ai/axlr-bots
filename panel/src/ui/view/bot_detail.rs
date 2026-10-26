//! One bot, in plain words: Pause and Stop, what waits for a person,
//! what it does, its events and recent work; its definition under
//! Advanced, closed by default.

use iced::widget::{Space, column, container, mouse_area, row, scrollable, text};
use iced::{Alignment, Length, mouse};

use super::levels;
use super::widgets::{
    El, SMALL, action, bold, chip, copy_button, empty, fact, icon, muted, pill, section, section_title, section_with,
    title,
};
use crate::domain::{Bot, BotId, TaskId};
use crate::ui::format;
use crate::ui::styles::{self, Kind};
use crate::ui::{App, Icon, Message, Screen, Tokens, Tone};

pub fn view<'a>(app: &'a App, id: &'a BotId, tokens: Tokens) -> El<'a> {
    let back = action(Kind::Ghost, Icon::Back, "Bots", Some(Message::Go(Screen::Bots)), tokens);
    let Some(view) = app.bot.as_ref().filter(|v| &v.bot.id == id) else {
        return column![back, empty(Icon::Hourglass, "Loading the bot…", tokens)]
            .spacing(14)
            .into();
    };
    let bot = &view.bot;
    // Pause and Stop, always in sight.
    let switch = if bot.status.can_pause() {
        action(
            Kind::Secondary,
            Icon::Pause,
            "Pause",
            (!app.switching).then(|| Message::Pause(bot.id.clone())),
            tokens,
        )
    } else if bot.status.can_enable() {
        action(
            Kind::Good,
            Icon::Play,
            "Turn on",
            (!app.switching).then(|| Message::Enable(bot.id.clone())),
            tokens,
        )
    } else {
        Space::new().into()
    };
    let header = row![
        back,
        title(Icon::Bots, bot.id.to_string(), tokens),
        pill(Tone::of_bot(bot.status), bot.status.name(), tokens),
        Space::new().width(Length::Fill),
        switch,
        action(
            Kind::Danger,
            Icon::Stop,
            "Stop",
            (!app.switching).then(|| Message::AskStopBot(bot.id.clone())),
            tokens,
        ),
    ]
    .spacing(12)
    .align_y(Alignment::Center);

    let mut left = column![].spacing(14).width(Length::FillPortion(3));
    if let Some(asking) = asking(app, &bot.id, tokens) {
        left = left.push(asking);
    }
    left = left
        .push(section_with(
            Icon::Bolt,
            "What it does",
            vec![copy_button("Copy mission", bot.mission.clone(), tokens)],
            text(bot.mission.clone()),
            tokens,
        ))
        .push(section_with(
            Icon::Clock,
            "Events",
            event_tools(app, tokens),
            events(app, tokens),
            tokens,
        ));
    let new_task = action(
        Kind::Ghost,
        Icon::Plus,
        "New task",
        bot.status
            .accepts_tasks()
            .then(|| Message::SubmitOpen(Some(bot.id.clone()))),
        tokens,
    );
    let advanced_toggle = action(
        Kind::Ghost,
        if app.advanced { Icon::Cross } else { Icon::Settings },
        if app.advanced { "Hide advanced" } else { "Advanced" },
        Some(Message::ShowAdvanced(!app.advanced)),
        tokens,
    );
    let right = column![
        section_with(
            Icon::Tasks,
            "Recent work",
            vec![new_task],
            tasks(app, &bot.id, tokens),
            tokens
        ),
        advanced_toggle,
    ]
    .spacing(14)
    .width(Length::FillPortion(2));

    // Open, Advanced comes first, under the name, as the operator asked
    // for it.
    let mut page = column![header].spacing(18);
    if app.advanced {
        page = page.push(advanced(app, bot, tokens));
    }
    page = page.push(row![left, right].spacing(14));
    scrollable(page.padding(iced::Padding {
        right: 12.0,
        ..Default::default()
    }))
    .height(Length::Fill)
    .into()
}

/// The calls of this bot waiting for a person, each answered in one
/// click.
fn asking<'a>(app: &'a App, bot: &BotId, tokens: Tokens) -> Option<El<'a>> {
    let fleet = app.fleet.as_ref()?;
    let waiting: Vec<_> = fleet.waiting().into_iter().filter(|t| &t.bot_id == bot).collect();
    if waiting.is_empty() {
        return None;
    }
    let mut list = column![section_title(Icon::Alert, "It asks before acting", tokens)].spacing(12);
    for task in waiting {
        let wants = fleet
            .call_of(&task.id)
            .map(|call| format!("wants {}", call.summary(48)))
            .unwrap_or_default();
        list = list.push(
            row![
                column![bold(task.headline(60)), muted(wants, tokens)]
                    .spacing(2)
                    .width(Length::Fill),
                super::decide::buttons(app, &task.id, tokens),
            ]
            .spacing(12)
            .align_y(Alignment::Center),
        );
    }
    Some(
        container(list)
            .padding(18)
            .width(Length::Fill)
            .style(styles::tone_card(tokens, Tone::Wait))
            .into(),
    )
}

/// What a person rarely changes: how much the bot may do alone, then its
/// triggers, placement, tools, model and budgets.
fn advanced<'a>(app: &'a App, bot: &'a Bot, tokens: Tokens) -> El<'a> {
    let mut choices = column![bold("How much may it do alone?")].spacing(10);
    let mut offered = levels::CHOICES.to_vec();
    if !offered.contains(&bot.level) {
        offered.push(bot.level);
    }
    for level in offered {
        let current = level == bot.level;
        let pick = (!current && !app.switching).then(|| Message::SetLevel(bot.id.clone(), level));
        choices = choices.push(levels::choice(level, current, pick, tokens));
    }
    choices = choices.push(muted("A change counts from its next step.", tokens));

    let mut triggers = column![].spacing(8);
    for trigger in &bot.triggers {
        triggers = triggers.push(
            row![
                chip(trigger.trigger_type.label(), tokens),
                bold(trigger.display_name().to_string()),
                muted(trigger.schedule(), tokens)
            ]
            .spacing(10)
            .align_y(Alignment::Center),
        );
    }
    let facts = column![
        fact("Starts by", triggers, tokens),
        fact("Runs on", text(bot.placement.describe()), tokens),
        fact("Tools", tools(bot, tokens), tokens),
        fact(
            "Model",
            text(format!(
                "{} · {} · {}",
                bot.model.model,
                bot.model.routing_policy,
                if bot.model.allow_remote_providers {
                    "remote allowed"
                } else {
                    "local only"
                }
            )),
            tokens
        ),
        fact(
            "Budgets",
            text(format!(
                "{} model calls · {} tool calls · {} per task",
                limit(bot.budgets.max_model_calls_per_task),
                limit(bot.budgets.max_tool_calls_per_task),
                bot.budgets.timeout_label()
            )),
            tokens
        ),
    ]
    .spacing(12);
    section(
        Icon::Settings,
        "Advanced",
        row![
            container(choices).width(Length::FillPortion(1)),
            container(facts).width(Length::FillPortion(1))
        ]
        .spacing(24),
        tokens,
    )
}

/// A budget, or `default` when the bot leaves it to the control plane.
fn limit(value: u32) -> String {
    if value == 0 {
        "default".to_string()
    } else {
        value.to_string()
    }
}

fn tools<'a>(bot: &'a Bot, tokens: Tokens) -> El<'a> {
    let mut chips = row![].spacing(6);
    for tool in &bot.allowed_tools {
        chips = chips.push(chip(tool.clone(), tokens));
    }
    let mut body = column![chips.wrap()].spacing(10);
    for rule in &bot.tool_rules.allow {
        body = body.push(row![icon(Icon::Check, 14.0, tokens.good), text(rule.describe()).size(SMALL)].spacing(8));
    }
    for rule in &bot.tool_rules.deny {
        body = body.push(row![icon(Icon::Cross, 14.0, tokens.bad), text(rule.describe()).size(SMALL)].spacing(8));
    }
    body.into()
}

/// A copy of the events, newest first as they show, once there is one.
fn event_tools<'a>(app: &'a App, tokens: Tokens) -> Vec<El<'a>> {
    match app.feed() {
        Some(feed) if !feed.events.is_empty() => vec![copy_button(
            "Copy events",
            format::events(feed.events.iter().rev()),
            tokens,
        )],
        _ => Vec::new(),
    }
}

fn events<'a>(app: &'a App, tokens: Tokens) -> El<'a> {
    let Some(feed) = app.feed() else {
        return empty(Icon::Clock, "No event yet", tokens);
    };
    let mut list = column![].spacing(8);
    for event in feed.events.iter().rev().take(15) {
        let tone = Tone::of_event(event.tone());
        let mut line = row![
            muted(format::clock(event.created_at), tokens),
            icon(tone.event_icon(), 14.0, tone.color(&tokens)),
            bold(event.label()).size(SMALL),
            muted(event.detail(), tokens),
        ]
        .spacing(10)
        .align_y(Alignment::Center);
        if let Ok(task) = TaskId::new(&event.task_id) {
            line = line
                .push(Space::new().width(Length::Fill))
                .push(muted(format::short_id(task.as_str(), 14), tokens));
            list = list.push(
                mouse_area(line)
                    .on_press(Message::Go(Screen::Task(task)))
                    .interaction(mouse::Interaction::Pointer),
            );
        } else {
            list = list.push(line);
        }
    }
    if feed.events.is_empty() {
        return empty(Icon::Clock, "No event yet", tokens);
    }
    list.into()
}

fn tasks<'a>(app: &'a App, bot: &'a BotId, tokens: Tokens) -> El<'a> {
    let mut list = column![].spacing(8);
    let mine: Vec<_> = app
        .fleet
        .iter()
        .flat_map(|f| f.tasks.iter())
        .filter(|t| &t.bot_id == bot)
        .take(8)
        .collect();
    if mine.is_empty() {
        return empty(Icon::Tasks, "No task yet", tokens);
    }
    for task in mine {
        list = list.push(
            mouse_area(
                row![
                    pill(Tone::of_task(task.status), task.status.label(), tokens),
                    text(task.headline(44)).size(SMALL).width(Length::Fill),
                    muted(format::age(task.created_at, app.now), tokens),
                ]
                .spacing(10)
                .align_y(Alignment::Center),
            )
            .on_press(Message::Go(Screen::Task(task.id.clone())))
            .interaction(mouse::Interaction::Pointer),
        );
    }
    list.into()
}
