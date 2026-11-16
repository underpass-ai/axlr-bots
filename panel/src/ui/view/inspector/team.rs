//! A team in the inspector: how many of its bots are on, Publish an
//! event, and its tabs. What happens: the operator's shortcuts, the form
//! that publishes and the team's timeline as it happens. Who is in it:
//! each member with what it is for and an urgent order, and the topics.

use iced::widget::{Space, column, container, mouse_area, row, text};
use iced::{Alignment, Length, mouse};

use super::super::widgets::{El, SMALL, Voice, action, bold, empty, field, icon, muted, pill, reader};
use super::part;
use crate::domain::{BotStatus, Event, TaskId, Team};
use crate::ui::format;
use crate::ui::inspector::{Tab, TeamTab};
use crate::ui::styles::Kind;
use crate::ui::{App, Icon, Message, ReaderKey, Screen, Tokens, Tone};

/// How many events of a team the timeline shows, newest first.
const SHOWN: usize = 40;

/// The team inspected, `width` wide. The Teams list always inspects a
/// team, so it has no close button.
pub fn view<'a>(app: &'a App, name: &'a str, width: f32, tokens: Tokens) -> El<'a> {
    let title = format!("Team {name}");
    let Some(team) = app.teams().into_iter().find(|t| t.name == name) else {
        return column![
            super::head(app, Vec::new(), Vec::new(), &title, None, width, tokens),
            empty(Icon::Team, "No bot is in this team now", tokens)
        ]
        .into();
    };
    let on = team.members.iter().filter(|b| b.status == BotStatus::Enabled).count();
    let first = vec![pill(
        if on > 0 { Tone::Busy } else { Tone::Idle },
        format!("{on} on · {}", format::count(team.members.len() as u64, "bot", "bots")),
        tokens,
    )];
    let acts = vec![action(
        Kind::Secondary,
        Icon::Bolt,
        "Publish an event",
        (!app.publish.is_open_for(name)).then(|| Message::PublishOpen(name.to_string())),
        tokens,
    )];
    let tab = app.tabs.team;
    let shown = app.watch.feed(name).map_or(0, |f| f.events.len().min(SHOWN));
    let members = team.members.len();
    let counts = move |t: Tab| match t {
        Tab::Team(TeamTab::Happens) => Some(shown),
        Tab::Team(TeamTab::Members) => Some(members),
        _ => None,
    };
    let content: El<'a> = match tab {
        TeamTab::Happens => happens(app, name, tokens),
        TeamTab::Members => members_of(app, team, name, tokens),
    };
    column![
        super::head(app, first, acts, &title, None, width, tokens),
        super::tabs(Tab::Team(tab), counts, tokens),
        super::body(content),
    ]
    .into()
}

/// What happens: the shortcuts, the form that publishes once asked for,
/// and the team's timeline, newest first.
fn happens<'a>(app: &'a App, name: &'a str, tokens: Tokens) -> El<'a> {
    let mut page = column![].spacing(16);
    if let Some(buttons) = shortcuts(app, name, tokens) {
        page = page.push(buttons);
    }
    if app.publish.is_open_for(name) {
        page = page.push(publish(app, tokens));
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
    page.push(timeline).into()
}

/// Who is in it: each member, what it is for and its urgent order; then
/// the team's topics.
fn members_of<'a>(app: &'a App, team: Team, name: &'a str, tokens: Tokens) -> El<'a> {
    let mut members = column![].spacing(12);
    for bot in team.members {
        let first = row![
            pill(Tone::of_bot(bot.status), bot.status.name(), tokens),
            super::super::widgets::link(bot.id.to_string(), Message::Go(Screen::Bot(bot.id.clone())), tokens),
            muted(bot.placement.describe(), tokens),
        ]
        .spacing(10)
        .align_y(Alignment::Center);
        let purpose = Team::purpose(&bot);
        let tools = format!("tools: {}", bot.allowed_tools.join(", "));
        members = members.push(
            column![
                first,
                row![
                    reader(
                        &app.readers,
                        ReaderKey::MemberPurpose(bot.id.clone()),
                        purpose,
                        Voice::Small,
                        tokens
                    ),
                    order_button(app, &bot, tokens),
                ]
                .spacing(10)
                .align_y(Alignment::Center),
                muted(tools, tokens),
            ]
            .spacing(4),
        );
    }
    column![members, part("Topics", topics(app, name, tokens), tokens)]
        .spacing(18)
        .into()
}

/// The urgent order's button for a member, as the fleet last read it.
fn order_button<'a>(app: &'a App, bot: &crate::domain::Bot, tokens: Tokens) -> El<'a> {
    super::super::order::button(app, bot, tokens)
}

/// The team's topics: who publishes, who listens and how, the last
/// event; nobody listens in red.
fn topics<'a>(app: &'a App, name: &str, tokens: Tokens) -> El<'a> {
    match app.watch.topics.get(name) {
        None => muted("Reading the topics…", tokens).into(),
        Some(None) => muted(
            "This control plane is older: it has no topics yet. Update it to see them.",
            tokens,
        )
        .into(),
        Some(Some(list)) if list.is_empty() => {
            muted("No topic yet: what the bots publish and listen to shows here", tokens).into()
        }
        Some(Some(list)) => {
            let mut rows = column![].spacing(10);
            for topic in list {
                let mut head = row![bold(topic.name.clone())].spacing(10).align_y(Alignment::Center);
                if topic.nobody_listens {
                    head = head.push(pill(Tone::Bad, "nobody listens", tokens));
                }
                if let Some(at) = topic.last_at {
                    head = head
                        .push(Space::new().width(Length::Fill))
                        .push(muted(format::clock(at), tokens));
                }
                rows = rows.push(
                    column![
                        head,
                        muted(format!("publishes {}", topic.publishers_text()), tokens),
                        muted(format!("listens {}", topic.listeners_text()), tokens),
                    ]
                    .spacing(2),
                );
            }
            rows.into()
        }
    }
}

/// The operator's one-press buttons for this team (`shortcuts.toml`):
/// each press publishes its event at once, a stop in red.
fn shortcuts<'a>(app: &'a App, name: &str, tokens: Tokens) -> Option<El<'a>> {
    let mut buttons = row![].spacing(10).align_y(Alignment::Center);
    let mut any = false;
    for (index, shortcut) in app.shortcuts.iter().enumerate().filter(|(_, s)| s.team == name) {
        let (kind, glyph) = if shortcut.stops() {
            (Kind::Danger, Icon::Stop)
        } else {
            (Kind::Secondary, Icon::Bolt)
        };
        buttons = buttons.push(action(
            kind,
            glyph,
            shortcut.label.as_str(),
            Some(Message::Shortcut(index)),
            tokens,
        ));
        any = true;
    }
    any.then(|| {
        part(
            "Shortcuts",
            column![
                buttons,
                muted("each press publishes its event on the team at once", tokens)
            ]
            .spacing(8),
            tokens,
        )
    })
}

/// The form that publishes the operator's event on the team.
fn publish<'a>(app: &'a App, tokens: Tokens) -> El<'a> {
    let form = &app.publish;
    let body = column![
        row![
            container(field(
                "Name",
                "stop.all, warn.storm, info.shift",
                &form.name,
                Message::PublishName,
                Some(Message::PublishSend),
                tokens
            ))
            .width(Length::FillPortion(2)),
            container(field(
                "To",
                "the whole team",
                &form.to,
                Message::PublishTo,
                Some(Message::PublishSend),
                tokens
            ))
            .width(Length::FillPortion(2)),
        ]
        .spacing(10),
        field(
            "Message",
            "one line: what happened and why",
            &form.message,
            Message::PublishMessage,
            Some(Message::PublishSend),
            tokens
        ),
        muted(
            "stop. and danger. stop the bots that hear them; warn. reaches them while they work; info. is a note for their next task.",
            tokens
        ),
        row![
            action(
                Kind::Primary,
                Icon::Bolt,
                "Publish",
                form.ready().then_some(Message::PublishSend),
                tokens
            ),
            action(Kind::Ghost, Icon::Cross, "Cancel", Some(Message::PublishClose), tokens),
        ]
        .spacing(10),
    ]
    .spacing(10);
    part("Publish an event", body, tokens)
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
    if !matches!(
        event.event_type.as_str(),
        "bot.event.emitted" | "bot.event.heard" | "group.event.published" | "bot.subscription.asked"
    ) {
        words = words.push(muted(event.bot_id.clone(), tokens));
    }
    words = words.push(text(format::event_detail(event)).size(SMALL).width(Length::Fill));
    match TaskId::new(&event.task_id) {
        Ok(task) => mouse_area(words)
            .on_press(Message::Go(Screen::Task(task)))
            .interaction(mouse::Interaction::Pointer)
            .into(),
        Err(_) => words.into(),
    }
}
