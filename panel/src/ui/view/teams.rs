//! Teams: who is in each, what each member is for, and the team's
//! timeline as it happens.

use iced::widget::{Space, column, mouse_area, row, scrollable, text};
use iced::{Alignment, Length, mouse};

use super::widgets::{El, SMALL, action, bold, card, chip, empty, icon, muted, pill, section, title};
use crate::domain::{Event, TaskId, Team};
use crate::ui::format;
use crate::ui::styles::Kind;
use crate::ui::{App, Icon, Message, Screen, Tokens, Tone};

/// How many events of a team the timeline shows, newest first.
const SHOWN: usize = 40;

/// Every team with its members and its last word.
pub fn list<'a>(app: &'a App, tokens: Tokens) -> El<'a> {
    if app.fleet.is_none() {
        return super::overview::loading(app, tokens);
    }
    let teams = app.teams();
    let mut page = column![row![title(Icon::Team, "Teams", tokens)].align_y(Alignment::Center)].spacing(18);
    if teams.is_empty() {
        page = page.push(card(
            column![
                empty(Icon::Team, "No team yet", tokens),
                muted(
                    "Give two bots the same Team, in New bot or on a bot's page: they know each other, ask each other and warn each other.",
                    tokens
                ),
            ]
            .spacing(10),
            tokens,
        ));
        return page.into();
    }
    for team in teams {
        let last = app
            .watch
            .feed(&team.name)
            .and_then(|f| f.events.iter().rev().find(|e| e.event_type == "bot.event.emitted"));
        let mut head = row![
            icon(Icon::Team, 18.0, tokens.text),
            bold(team.name.clone()).size(18.0),
            chip(format!("{} bots", team.members.len()), tokens),
        ]
        .spacing(10)
        .align_y(Alignment::Center);
        if team.lonely() {
            head = head.push(pill(Tone::Wait, "only one bot: a typo?", tokens));
        }
        head = head.push(Space::new().width(Length::Fill)).push(action(
            Kind::Ghost,
            Icon::Play,
            "Open",
            Some(Message::Go(Screen::Team(team.name.clone()))),
            tokens,
        ));
        let names: Vec<String> = team.members.iter().map(|b| b.id.to_string()).collect();
        let mut body = column![head, muted(names.join(" · "), tokens)].spacing(8);
        if let Some(event) = last {
            body = body.push(line(event, tokens));
        }
        page = page.push(card(body, tokens));
    }
    scrollable(page.padding(iced::Padding {
        right: 12.0,
        ..Default::default()
    }))
    .height(Length::Fill)
    .into()
}

/// One team: its members, what each is for, and its timeline.
pub fn detail<'a>(app: &'a App, name: &'a str, tokens: Tokens) -> El<'a> {
    let back = action(
        Kind::Ghost,
        Icon::Back,
        "Teams",
        Some(Message::Go(Screen::Teams)),
        tokens,
    );
    let Some(team) = app.teams().into_iter().find(|t| t.name == name) else {
        return column![back, empty(Icon::Team, "No bot is in this team now", tokens)]
            .spacing(14)
            .into();
    };
    let header = row![
        back,
        title(Icon::Team, format!("Team {name}"), tokens),
        chip(format!("{} bots", team.members.len()), tokens),
    ]
    .spacing(12)
    .align_y(Alignment::Center);
    let mut members = column![].spacing(12);
    for bot in &team.members {
        let mut first = row![
            pill(Tone::of_bot(bot.status), bot.status.name(), tokens),
            mouse_area(bold(bot.id.to_string()).color(tokens.accent))
                .on_press(Message::Go(Screen::Bot(bot.id.clone())))
                .interaction(mouse::Interaction::Pointer),
            muted(bot.placement.describe(), tokens),
            Space::new().width(Length::Fill),
        ]
        .spacing(10)
        .align_y(Alignment::Center);
        if bot.can_return_home() {
            first = first.push(action(
                Kind::Secondary,
                Icon::Back,
                "Recall",
                (!app.switching && bot.status.accepts_tasks()).then(|| Message::Recall(bot.id.clone())),
                tokens,
            ));
        }
        members = members.push(
            column![
                first,
                text(Team::purpose(bot)).size(SMALL),
                muted(format!("tools: {}", bot.allowed_tools.join(", ")), tokens),
            ]
            .spacing(4),
        );
    }
    let timeline = match app.watch.feed(name) {
        Some(feed) if !feed.events.is_empty() => {
            let mut list = column![].spacing(8);
            for event in feed.events.iter().rev().take(SHOWN) {
                list = list.push(line(event, tokens));
            }
            El::from(list)
        }
        _ => empty(
            Icon::Clock,
            "Nothing yet: what the bots tell each other shows here as it happens",
            tokens,
        ),
    };
    let page = column![
        header,
        row![
            column![section(Icon::Bots, "Who is in it", members, tokens)].width(Length::FillPortion(2)),
            column![section(Icon::Clock, "What happens", timeline, tokens)].width(Length::FillPortion(3)),
        ]
        .spacing(14),
    ]
    .spacing(18);
    scrollable(page.padding(iced::Padding {
        right: 12.0,
        ..Default::default()
    }))
    .height(Length::Fill)
    .into()
}

/// One event of a team: when, who, what, why; a click opens its task.
fn line<'a>(event: &'a Event, tokens: Tokens) -> El<'a> {
    let tone = Tone::of_event(event.tone());
    let mut words = row![
        muted(format::clock(event.created_at), tokens),
        icon(tone.event_icon(), 14.0, tone.color(&tokens)),
        bold(event.label()).size(SMALL),
    ]
    .spacing(10)
    .align_y(Alignment::Center);
    if event.event_type != "bot.event.emitted" {
        words = words.push(muted(event.bot_id.clone(), tokens));
    }
    words = words.push(text(event.detail()).size(SMALL).width(Length::Fill));
    match TaskId::new(&event.task_id) {
        Ok(task) => mouse_area(words)
            .on_press(Message::Go(Screen::Task(task)))
            .interaction(mouse::Interaction::Pointer)
            .into(),
        Err(_) => words.into(),
    }
}
