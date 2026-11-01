//! One bot, in plain words: Pause and Stop, what waits for a person,
//! what it does now and an urgent order, its events and recent work; its
//! definition under Advanced, closed by default.

use iced::widget::{Space, column, container, mouse_area, pick_list, row, scrollable, text, text_input};
use iced::{Alignment, Length, mouse};

use super::levels;
use super::widgets::{
    El, SMALL, action, bold, chip, copy_button, empty, fact, icon, muted, pill, section, section_title, section_with,
    title,
};
use crate::application::{BotNow, ChatLine};
use crate::domain::{Bot, BotId, BotStatus, TaskId};
use crate::ui::format;
use crate::ui::styles::{self, Kind};
use crate::ui::{App, BotView, Icon, Message, ModeChoice, Screen, Tokens, Tone};

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
    for card in asks(app, bot, tokens) {
        left = left.push(card);
    }
    left = left
        .push(section_with(
            Icon::Play,
            "Now",
            vec![super::order::button(app, bot, tokens)],
            now_and_order(app, bot, tokens),
            tokens,
        ))
        .push(section(Icon::Bolt, "Talk to it", chat(app, view, tokens), tokens))
        .push(listens(app, bot, tokens));
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
        section_with(
            Icon::Bolt,
            "What it does",
            vec![copy_button("Copy mission", bot.mission.clone(), tokens)],
            text(bot.mission.clone()),
            tokens,
        ),
        team(app, bot, tokens),
        advanced_toggle,
        section_with(
            Icon::Clock,
            "Events",
            event_tools(app, tokens),
            events(app, tokens),
            tokens,
        ),
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

/// The bot's chat in time order: messages that ran as tasks with their
/// answers, and messages to a running task with what its run said back;
/// a box to write the next one.
fn chat<'a>(app: &'a App, view: &'a BotView, tokens: Tokens) -> El<'a> {
    let mut list = column![].spacing(8);
    let running = app.running_task(&view.bot);
    let hint = match running.as_ref() {
        Some(_) => "It is working now: your message goes to its run, and it answers between its steps.",
        None => "Ask it something or give it an order. Each message runs as a task.",
    };
    let lines = ChatLine::log(&view.chat, &view.feed.events);
    if lines.is_empty() || running.is_some() {
        list = list.push(muted(hint, tokens));
    }
    for line in lines {
        let bubble = mouse_area(
            container(text(cut(&line.text, 400)).size(15.0))
                .padding(10)
                .max_width(560)
                .style(styles::bubble(tokens, line.mine)),
        )
        .on_press(Message::Go(Screen::Task(line.task)))
        .interaction(mouse::Interaction::Pointer);
        list = list.push(if line.mine {
            row![Space::new().width(Length::Fill), bubble]
        } else {
            row![bubble, Space::new().width(Length::Fill)]
        });
    }
    let ready = !app.chat_busy && !app.chat_draft.trim().is_empty() && view.bot.status.accepts_tasks();
    let input = text_input(&format!("Message {}…", view.bot.id), &app.chat_draft)
        .on_input(Message::ChatInput)
        .on_submit_maybe(ready.then_some(Message::ChatSend))
        .padding(10)
        .style(styles::field(tokens));
    let mut body = column![
        list,
        row![
            input,
            action(
                Kind::Primary,
                Icon::Play,
                if app.chat_busy { "Sending…" } else { "Send" },
                ready.then_some(Message::ChatSend),
                tokens
            )
        ]
        .spacing(10)
        .align_y(Alignment::Center),
    ]
    .spacing(12);
    if !view.bot.status.accepts_tasks() {
        body = body.push(muted("Turn it on to talk to it.", tokens));
    }
    body.into()
}

/// A text cut to `max` characters with an ellipsis.
fn cut(words: &str, max: usize) -> String {
    if words.chars().count() <= max {
        return words.to_string();
    }
    format!("{}…", words.chars().take(max.saturating_sub(1)).collect::<String>())
}

/// What the bot is doing now and, once asked for, the urgent order that
/// stops it.
fn now_and_order<'a>(app: &'a App, bot: &'a Bot, tokens: Tokens) -> El<'a> {
    if !app.order.is_open_for(&bot.id) {
        return now(app, bot, tokens);
    }
    column![now(app, bot, tokens), super::order::form(app, bot, tokens)]
        .spacing(14)
        .into()
}

/// What the bot is doing right now, in a sentence, and its newest step.
fn now<'a>(app: &'a App, bot: &'a Bot, tokens: Tokens) -> El<'a> {
    let tasks = app.fleet.as_ref().map(|f| f.tasks.as_slice()).unwrap_or_default();
    let events = app.feed().map(|f| f.events.as_slice()).unwrap_or_default();
    let state = BotNow::of(bot, tasks, events);
    let (headline, detail) = match &state {
        BotNow::Waiting { task } => (task.headline(70), "It waits for your OK before it goes on.".to_string()),
        BotNow::Working { task, since, last } => {
            let mut detail = format!("for {}", format::span(app.now - *since));
            if let Some(step) = last {
                let what = match step.detail() {
                    words if words.is_empty() => step.label(),
                    words => format!("{} · {words}", step.label()),
                };
                detail = format!(
                    "{detail} · last step: {}, {}",
                    cut(&what, 60),
                    format::ago(step.created_at, app.now)
                );
            }
            (task.headline(70), detail)
        }
        BotNow::Queued { task } => (task.headline(70), "Queued: it waits for a free node.".to_string()),
        BotNow::Off(BotStatus::Paused) => (
            "Paused".to_string(),
            "It takes no new task until you turn it on.".to_string(),
        ),
        BotNow::Off(_) => (
            "Not switched on".to_string(),
            "Turn it on to give it tasks.".to_string(),
        ),
        BotNow::Idle => ("Idle".to_string(), "It waits for a task.".to_string()),
    };
    let mut body = row![
        pill(Tone::of_now(&state), state.word(), tokens),
        column![bold(headline).size(18.0), muted(detail, tokens)]
            .spacing(4)
            .width(Length::Fill),
    ]
    .spacing(12)
    .align_y(Alignment::Center);
    if let Some(task) = state.task() {
        body = body.push(action(
            Kind::Ghost,
            Icon::Play,
            "Open task",
            Some(Message::Go(Screen::Task(task.id.clone()))),
            tokens,
        ));
    }
    body.into()
}

/// Its teams, and the field that changes them.
fn team<'a>(app: &'a App, bot: &'a Bot, tokens: Tokens) -> El<'a> {
    let editing = app.watch.editing.as_ref().filter(|(id, _)| id == &bot.id);
    let body: El<'a> = match editing {
        Some((_, words)) => row![
            container(super::widgets::field(
                "Team",
                "ops",
                words,
                {
                    let id = bot.id.clone();
                    move |w| Message::TeamEdit(id.clone(), w)
                },
                Some(Message::TeamSave),
                tokens
            ))
            .width(Length::Fill),
            action(
                Kind::Primary,
                Icon::Check,
                "Save",
                (!app.switching).then_some(Message::TeamSave),
                tokens
            ),
            action(Kind::Ghost, Icon::Cross, "Cancel", Some(Message::TeamCancel), tokens),
        ]
        .spacing(10)
        .align_y(Alignment::End)
        .into(),
        None => {
            let mut line = row![].spacing(8).align_y(Alignment::Center);
            if bot.groups.is_empty() {
                line = line.push(muted("In no team: it knows no other bot", tokens));
            }
            for name in &bot.groups {
                line = line.push(
                    mouse_area(chip(name.clone(), tokens))
                        .on_press(Message::Go(Screen::Team(name.clone())))
                        .interaction(mouse::Interaction::Pointer),
                );
            }
            line = line.push(Space::new().width(Length::Fill)).push(action(
                Kind::Ghost,
                Icon::Team,
                "Change",
                Some(Message::TeamEdit(bot.id.clone(), bot.groups.join(", "))),
                tokens,
            ));
            line.into()
        }
    };
    section(Icon::Team, "Team", body, tokens)
}

/// The bot's asks to hear a topic, each a card a person answers in one
/// click: «almacen asks to listen to error.*: "para reintentar"».
fn asks<'a>(app: &'a App, bot: &'a Bot, tokens: Tokens) -> Vec<El<'a>> {
    bot.pending_asks()
        .map(|ask| {
            let words = format!("{} asks to listen to {}", bot.id, ask.topic);
            let why = if ask.why.is_empty() {
                String::new()
            } else {
                format!("\u{201c}{}\u{201d}", ask.why)
            };
            let line = row![
                column![bold(words), muted(why, tokens)].spacing(2).width(Length::Fill),
                action(
                    Kind::Good,
                    Icon::Check,
                    "Approve",
                    (!app.switching).then(|| Message::ApproveAsk(bot.id.clone(), ask.topic.clone())),
                    tokens,
                ),
                action(
                    Kind::Secondary,
                    Icon::Cross,
                    "Deny",
                    (!app.switching).then(|| Message::DenyAsk(bot.id.clone(), ask.topic.clone())),
                    tokens,
                ),
            ]
            .spacing(12)
            .align_y(Alignment::Center);
            container(line)
                .padding(18)
                .width(Length::Fill)
                .style(styles::tone_card(tokens, Tone::Wait))
                .into()
        })
        .collect()
}

/// What the bot listens to: one line per topic with its mode and whom
/// from, Remove on each, and the form to listen to one more.
fn listens<'a>(app: &'a App, bot: &'a Bot, tokens: Tokens) -> El<'a> {
    let mut body = column![].spacing(8);
    let active: Vec<_> = bot.subscriptions.iter().filter(|s| !s.is_pending()).collect();
    if active.is_empty() {
        body = body.push(muted(
            "Hears what is addressed to it and the team's stop and danger",
            tokens,
        ));
    }
    for sub in active {
        let mut line = row![
            bold(sub.topic.clone()),
            chip(sub.mode().name(), tokens),
            muted(format!("from {}", sub.senders()), tokens),
        ]
        .spacing(10)
        .align_y(Alignment::Center);
        if sub.urgent {
            line = line.push(pill(Tone::Wait, "urgent", tokens));
        }
        line = line.push(Space::new().width(Length::Fill)).push(action(
            Kind::Ghost,
            Icon::Cross,
            "Remove",
            (!app.switching).then(|| Message::Unsubscribe(bot.id.clone(), sub.topic.clone())),
            tokens,
        ));
        body = body.push(line);
    }
    if app.listen.is_open_for(&bot.id) {
        let form = &app.listen;
        body = body.push(
            column![
                super::widgets::field(
                    "Topic",
                    "pedido.* or warn.stock",
                    &form.topic,
                    Message::ListenTopic,
                    Some(Message::ListenSave),
                    tokens
                ),
                row![
                    column![
                        bold("Mode").size(SMALL),
                        pick_list(ModeChoice::ALL, Some(form.mode), Message::ListenMode).padding(8),
                        muted(form.default_label(), tokens),
                    ]
                    .spacing(6),
                    container(super::widgets::field(
                        "From",
                        "anyone",
                        &form.from,
                        Message::ListenFrom,
                        Some(Message::ListenSave),
                        tokens
                    ))
                    .width(Length::Fill),
                ]
                .spacing(10),
                row![
                    action(
                        Kind::Primary,
                        Icon::Check,
                        "Listen",
                        (!form.busy && !form.topic.trim().is_empty()).then_some(Message::ListenSave),
                        tokens
                    ),
                    action(Kind::Ghost, Icon::Cross, "Cancel", Some(Message::ListenClose), tokens),
                ]
                .spacing(10),
            ]
            .spacing(10),
        );
    }
    let open = action(
        Kind::Ghost,
        Icon::Plus,
        "Listen to a topic",
        (!app.listen.is_open_for(&bot.id)).then(|| Message::ListenOpen(bot.id.clone())),
        tokens,
    );
    section_with(Icon::Bolt, "Listens to", vec![open], body, tokens)
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
