//! Teams: who is in each, what each member is for, and the team's
//! timeline as it happens.

use std::rc::Rc;

use iced::widget::{Space, column, container, mouse_area, row, scrollable, text};
use iced::{Alignment, Length, mouse};

use super::sheet::{self, Column, Head, Ink, Line};
use super::widgets::{El, SMALL, Voice, action, bold, chip, empty, field, icon, muted, pill, reader, section, title};
use super::{grid, listbar};
use crate::domain::{Event, TaskId, Team};
use crate::ui::format;
use crate::ui::styles::Kind;
use crate::ui::table::{ColumnRef, List, RowRef, SortColumn, TeamColumn};
use crate::ui::{App, Icon, Message, ReaderKey, Screen, Tokens, Tone};

/// How many events of a team the timeline shows, newest first.
const SHOWN: usize = 40;

/// Every team with its members and its last word, one line each.
pub fn list<'a>(app: &'a App, tokens: Tokens) -> El<'a> {
    if app.fleet.is_none() {
        return super::overview::loading(app, tokens);
    }
    let teams = app.teams();
    let bar = listbar::bar("Teams", teams.len().to_string(), Vec::new(), Vec::new(), tokens);
    if teams.is_empty() {
        return column![
            bar,
            listbar::nothing(
                column![
                    empty(Icon::Team, "No team yet", tokens),
                    container(muted(
                        "Give two bots the same Team, in New bot or on a bot's page: they know each other, ask each other and warn each other.",
                        tokens
                    ))
                    .center_x(Length::Fill),
                ]
                .spacing(10)
                .into()
            )
        ]
        .into();
    }
    let state = &app.tables.teams;
    let shown: Rc<Vec<Team>> = Rc::new(state.sort.order(&teams).into_iter().cloned().collect());
    let head = |c: TeamColumn| Head::Sort {
        label: c.label(),
        badge: state.sort.badge(c),
        column: ColumnRef::Teams(c),
    };
    let all = state.selection.covers(shown.iter().map(|t| &t.name));
    // A row is its team's place in `shown`, which every cell keeps.
    let of = |cell: fn(&'a App, &Team, f32, Tokens) -> El<'a>| {
        let shown = shown.clone();
        move |at: usize, width: f32| cell(app, &shown[at], width, tokens)
    };
    let columns = vec![
        Column::fixed(
            Head::Check { all, list: List::Teams },
            18.0,
            of(|app, team, _, tokens| {
                let name = team.name.clone();
                grid::check(
                    app.tables.teams.selection.contains(&team.name),
                    move |on| Message::Pick(RowRef::Team(name.clone()), on),
                    tokens,
                )
            }),
        ),
        Column::share(
            head(TeamColumn::Team),
            3,
            of(|_, team, width, tokens| {
                let lonely = team.lonely();
                let room = if lonely { width - 24.0 } else { width };
                let open = Message::Go(Screen::Team(team.name.clone()));
                let mut cell = row![sheet::link(team.name.clone(), room, open, tokens)]
                    .spacing(4)
                    .align_y(Alignment::Center);
                if lonely {
                    cell = cell.push(super::widgets::mark(
                        Icon::Alert,
                        tokens.wait,
                        "Only one bot: a typo?".to_string(),
                        tokens,
                    ));
                }
                cell.into()
            }),
        ),
        Column::fixed(
            head(TeamColumn::Bots),
            44.0,
            of(|_, team, _, tokens| Ink::Plain.write(team.members.len().to_string(), tokens).into()),
        ),
        Column::share(
            Head::Plain("Members"),
            8,
            of(|_, team, width, tokens| {
                let names: Vec<String> = team.members.iter().map(|b| b.id.to_string()).collect();
                sheet::words(names.join(" · "), width, Ink::Quiet, tokens)
            }),
        ),
        Column::share(
            Head::Plain("Last word"),
            8,
            of(|app, team, width, tokens| {
                let last = app
                    .watch
                    .feed(&team.name)
                    .and_then(|f| f.events.iter().rev().find(|e| e.event_type == "bot.event.emitted"));
                match last {
                    Some(event) => sheet::pair(
                        &format!("{} {}", format::clock(event.created_at), event.label()),
                        &format::event_detail(event),
                        width,
                        tokens,
                    ),
                    None => muted("—", tokens).into(),
                }
            }),
        ),
        Column::fixed(
            Head::None,
            grid::ACT,
            of(|_, team, _, tokens| {
                grid::act_slot(Some(grid::icon_act(
                    Kind::Ghost,
                    Icon::Next,
                    "Open",
                    Some(Message::Go(Screen::Team(team.name.clone()))),
                    tokens,
                )))
            }),
        ),
    ];
    let lines = shown
        .iter()
        .enumerate()
        .map(|(at, team)| Line::Row {
            row: at,
            picked: state.selection.contains(&team.name),
        })
        .collect();
    let page = column![
        bar,
        sheet::sheet(columns, lines, app.density, app.tables.picked(List::Teams) > 0, tokens)
    ];
    grid::with_bar(app, List::Teams, page.into(), tokens)
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
        Space::new().width(Length::Fill),
        action(
            Kind::Primary,
            Icon::Bolt,
            "Publish an event",
            (!app.publish.is_open_for(name)).then(|| Message::PublishOpen(name.to_string())),
            tokens,
        ),
    ]
    .spacing(12)
    .align_y(Alignment::Center);
    let mut members = column![].spacing(12);
    for bot in &team.members {
        let first = row![
            pill(Tone::of_bot(bot.status), bot.status.name(), tokens),
            super::widgets::link(bot.id.to_string(), Message::Go(Screen::Bot(bot.id.clone())), tokens),
            muted(bot.placement.describe(), tokens),
        ]
        .spacing(10)
        .align_y(Alignment::Center);
        members = members.push(
            column![
                first,
                row![
                    reader(
                        &app.readers,
                        ReaderKey::MemberPurpose(bot.id.clone()),
                        Team::purpose(bot),
                        Voice::Small,
                        tokens
                    ),
                    super::order::button(app, bot, tokens),
                ]
                .spacing(10)
                .align_y(Alignment::Center),
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
    let mut page = column![header].spacing(18);
    if let Some(buttons) = shortcuts(app, name, tokens) {
        page = page.push(buttons);
    }
    if app.publish.is_open_for(name) {
        page = page.push(publish(app, tokens));
    }
    let page = page.push(
        row![
            column![
                section(Icon::Bots, "Who is in it", members, tokens),
                section(Icon::Bolt, "Topics", topics(app, name, tokens), tokens),
            ]
            .spacing(14)
            .width(Length::FillPortion(2)),
            column![section(Icon::Clock, "What happens", timeline, tokens)].width(Length::FillPortion(3)),
        ]
        .spacing(14),
    );
    scrollable(page.padding(iced::Padding {
        right: 12.0,
        ..Default::default()
    }))
    .id(super::widgets::PAGE)
    .height(Length::Fill)
    .into()
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
        section(
            Icon::Bolt,
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
    section(Icon::Bolt, "Publish an event", body, tokens)
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
