//! One bot: its definition in cards and its events as they happen.

use iced::widget::{Space, column, mouse_area, row, scrollable, text};
use iced::{Alignment, Length, mouse};

use super::widgets::{
    El, SMALL, action, bold, chip, copy_button, empty, fact, icon, muted, pill, section, section_with, title,
};
use crate::domain::{AutonomyLevel, Bot, BotId, TaskId};
use crate::ui::format;
use crate::ui::styles::Kind;
use crate::ui::{App, Icon, Message, Screen, Tokens, Tone};

pub fn view<'a>(app: &'a App, id: &'a BotId, tokens: Tokens) -> El<'a> {
    let back = action(Kind::Ghost, Icon::Back, "Bots", Some(Message::Go(Screen::Bots)), tokens);
    let Some(view) = app.bot.as_ref().filter(|v| &v.bot.id == id) else {
        return column![back, empty(Icon::Hourglass, "Loading the bot…", tokens)]
            .spacing(14)
            .into();
    };
    let bot = &view.bot;
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
    let new_task = action(
        Kind::Primary,
        Icon::Plus,
        "New task",
        bot.status
            .accepts_tasks()
            .then(|| Message::SubmitOpen(Some(bot.id.clone()))),
        tokens,
    );
    let header = row![
        back,
        title(Icon::Bots, bot.id.to_string(), tokens),
        pill(Tone::of_bot(bot.status), bot.status.name(), tokens),
        Space::new().width(Length::Fill),
        switch,
        new_task,
    ]
    .spacing(12)
    .align_y(Alignment::Center);

    let left = column![
        section_with(
            Icon::Bolt,
            "Mission",
            vec![copy_button("Copy mission", bot.mission.clone(), tokens)],
            text(bot.mission.clone()),
            tokens
        ),
        section(
            Icon::Chip,
            "Model",
            column![
                fact("Model", bold(bot.model.model.clone()), tokens),
                fact("Routing", text(bot.model.routing_policy.clone()), tokens),
                fact(
                    "Remote",
                    text(if bot.model.allow_remote_providers {
                        "allowed"
                    } else {
                        "local only"
                    }),
                    tokens
                ),
                fact(
                    "Context",
                    text(format::tokens(bot.model.max_context_tokens.into())),
                    tokens
                ),
            ]
            .spacing(8),
            tokens
        ),
        section(Icon::Wrench, "Tools", tools(bot, tokens), tokens),
        section(
            Icon::Coin,
            "Budgets",
            column![
                fact("Model calls", text(limit(bot.budgets.max_model_calls_per_task)), tokens),
                fact("Tool calls", text(limit(bot.budgets.max_tool_calls_per_task)), tokens),
                fact("Delegation", text(bot.budgets.max_delegation_depth.to_string()), tokens),
                fact("Time limit", text(bot.budgets.timeout_label()), tokens),
            ]
            .spacing(8),
            tokens
        ),
    ]
    .spacing(14)
    .width(Length::FillPortion(1));

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
    let right = column![
        section(
            Icon::Alert,
            "Level",
            column![
                row![chip(bot.level.label(), tokens), muted(bot.level.name(), tokens)]
                    .spacing(10)
                    .align_y(Alignment::Center),
                text(level_words(bot.level)),
            ]
            .spacing(8),
            tokens
        ),
        section(Icon::Pin, "Runs on", text(bot.placement.describe()), tokens),
        section(Icon::Play, "Starts by", triggers, tokens),
        section_with(
            Icon::Clock,
            "Events",
            event_tools(app, tokens),
            events(app, tokens),
            tokens
        ),
        section(Icon::Tasks, "Latest tasks", tasks(app, &bot.id, tokens), tokens),
    ]
    .spacing(14)
    .width(Length::FillPortion(1));

    scrollable(
        column![header, row![left, right].spacing(14)]
            .spacing(18)
            .padding(iced::Padding {
                right: 12.0,
                ..Default::default()
            }),
    )
    .height(Length::Fill)
    .into()
}

fn limit(value: u32) -> String {
    if value == 0 {
        "default".to_string()
    } else {
        value.to_string()
    }
}

/// What a level lets the bot do, in one sentence.
fn level_words(level: AutonomyLevel) -> &'static str {
    match level {
        AutonomyLevel::Observe => "Reads only. It never writes.",
        AutonomyLevel::Propose => "Reads and proposes. A person acts.",
        AutonomyLevel::ActWithApproval => "Writes only after a person says OK.",
        AutonomyLevel::ActWithinPolicy => "Acts alone inside its tool rules.",
        AutonomyLevel::Unknown => "A level this panel does not know yet.",
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
