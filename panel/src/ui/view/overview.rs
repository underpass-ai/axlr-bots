//! Home: the fleet at a glance and what needs a person.

use iced::widget::text::Wrapping;
use iced::widget::{Space, button, column, container, mouse_area, row, scrollable, text};
use iced::{Alignment, Length, Padding, mouse};

use super::widgets::{
    El, PILL_SLOT, SMALL, action, bold, card, empty, icon, muted, open, pill, section, semibold, slot, title,
};
use crate::domain::{BotStatus, NodeStatus, TaskStatus};
use crate::ui::format;
use crate::ui::styles::{self, Kind};
use crate::ui::{App, Icon, Message, Screen, Tokens, Tone};

pub fn view<'a>(app: &'a App, tokens: Tokens) -> El<'a> {
    let Some(fleet) = &app.fleet else {
        return loading(app, tokens);
    };
    let o = &fleet.overview;
    let online = o.nodes_in(NodeStatus::Online);
    let waiting = app.waiting_count();
    // Asks to listen and no waiting task: the count opens the bot that asks.
    let need_you = match fleet.bots.iter().find(|b| b.pending_asks().next().is_some()) {
        Some(bot) if o.tasks_in(TaskStatus::WaitingApproval) == 0 => Message::Go(Screen::Bot(bot.id.clone())),
        _ => Message::FilterStatus(Some(TaskStatus::WaitingApproval)),
    };
    let failed_today = o.tasks_since_in(TaskStatus::Failed);
    let expired_today = o.tasks_since_in(TaskStatus::Expired);
    let stats = row![
        stat(
            Icon::Nodes,
            format!("{online}/{}", o.total_nodes()),
            "nodes online",
            if o.nodes_down() > 0 { Tone::Bad } else { Tone::Good },
            Some(Message::Go(Screen::Nodes)),
            tokens
        ),
        stat(
            Icon::Bots,
            o.bots_in(BotStatus::Enabled).to_string(),
            "bots on",
            Tone::Good,
            Some(Message::Go(Screen::Bots)),
            tokens
        ),
        stat(
            Icon::Play,
            o.tasks_in(TaskStatus::Running).to_string(),
            "running",
            if o.tasks_in(TaskStatus::Running) > 0 {
                Tone::Busy
            } else {
                Tone::Idle
            },
            Some(Message::FilterStatus(Some(TaskStatus::Running))),
            tokens
        ),
        stat(
            Icon::Alert,
            waiting.to_string(),
            "need you",
            if waiting > 0 { Tone::Wait } else { Tone::Idle },
            Some(need_you),
            tokens
        ),
        stat(
            Icon::Cross,
            failed_today.to_string(),
            "failed today",
            if failed_today > 0 { Tone::Bad } else { Tone::Idle },
            Some(Message::FilterStatus(Some(TaskStatus::Failed))),
            tokens
        ),
        stat(
            Icon::Hourglass,
            expired_today.to_string(),
            "expired today",
            if expired_today > 0 { Tone::Late } else { Tone::Idle },
            Some(Message::FilterStatus(Some(TaskStatus::Expired))),
            tokens
        ),
        stat(
            Icon::Coin,
            format::spend(o.cost_since()),
            "cost today",
            Tone::Idle,
            None,
            tokens
        ),
    ]
    .spacing(14);
    // Runs stopped before they said what they spent: the cost today is a
    // floor, and a note under its card says how many.
    let stats: El<'a> = if o.cost_since().is_floor() {
        let runs = o.cost_unknown_since;
        let note = if runs == 1 {
            "1 run unknown".to_string()
        } else {
            format!("{runs} runs unknown")
        };
        column![
            stats,
            row![Space::new().width(Length::Fill), muted(note, tokens)].padding(Padding {
                right: 12.0,
                ..Padding::ZERO
            })
        ]
        .spacing(4)
        .into()
    } else {
        stats.into()
    };

    let mut page = column![
        row![
            title(Icon::Overview, "Home", tokens),
            Space::new().width(Length::Fill),
            action(
                Kind::Primary,
                Icon::Plus,
                "New task",
                Some(Message::SubmitOpen(None)),
                tokens
            )
        ]
        .align_y(Alignment::Center),
        stats
    ]
    .spacing(20);

    let waiting_tasks = fleet.waiting();
    if !waiting_tasks.is_empty() {
        let mut list = column![].spacing(10);
        for task in waiting_tasks.iter().take(NEEDS_YOU_SHOWN) {
            // What the bot wants to do, so an OK here is not blind.
            let wants: El<'a> = match fleet.call_of(&task.id) {
                Some(call) => row![
                    icon(Icon::Wrench, 14.0, tokens.muted),
                    muted(format!("wants {}", call.summary(40)), tokens).wrapping(Wrapping::WordOrGlyph)
                ]
                .spacing(6)
                .align_y(Alignment::Center)
                .into(),
                None => Space::new().into(),
            };
            // The bot and what it asks share a line while they fit and go
            // one under the other when they do not; the age and the
            // buttons are laid out first and keep their room.
            let words = row![
                bold(task.bot_id.to_string()),
                column![text(task.headline(70)).wrapping(Wrapping::WordOrGlyph), wants].spacing(2),
            ]
            .spacing(14)
            .align_y(Alignment::Center)
            .width(Length::Fill)
            .wrap()
            .vertical_spacing(2);
            list = list.push(
                row![
                    pill(Tone::Wait, TaskStatus::WaitingApproval.label(), tokens),
                    words,
                    muted(format::age(task.created_at, app.now), tokens),
                    super::decide::buttons(app, &task.id, tokens),
                    open("Review", Message::Go(Screen::Task(task.id.clone())), tokens),
                ]
                .spacing(14)
                .align_y(Alignment::Center),
            );
        }
        // The newest few, then the way to all of them: what came in last
        // stays above the fold.
        if waiting_tasks.len() > NEEDS_YOU_SHOWN {
            list = list.push(more(
                format!("{} more wait for you", waiting_tasks.len() - NEEDS_YOU_SHOWN),
                format!("See all {}", waiting_tasks.len()),
                Message::FilterStatus(Some(TaskStatus::WaitingApproval)),
                tokens,
            ));
        }
        page = page.push(
            container(column![super::widgets::section_title(Icon::Alert, "Needs you", tokens), list].spacing(14))
                .padding(18)
                .width(Length::Fill)
                .style(styles::tone_card(tokens, Tone::Wait)),
        );
    }

    // The nodes that are not online first, as they are the news; a
    // few, then the way to the rest.
    let mut ordered: Vec<_> = fleet.nodes.iter().collect();
    ordered.sort_by_key(|n| n.status == NodeStatus::Online);
    let mut nodes = column![].spacing(10);
    for node in ordered.iter().take(NODES_SHOWN) {
        nodes = nodes.push(
            row![
                slot(pill(Tone::of_node(node.status), node.status.name(), tokens), PILL_SLOT),
                container(super::grid::fitted(node.id.to_string(), true, tokens)).width(Length::FillPortion(3)),
                container(super::grid::fitted_muted(node.host_name.clone(), tokens)).width(Length::FillPortion(2)),
                container(muted(format!("seen {}", format::age(node.last_seen_at, app.now)), tokens).wrapping(Wrapping::None))
                    .width(SEEN)
                    .align_right(SEEN),
            ]
            .spacing(12)
            .align_y(Alignment::Center),
        );
    }
    if fleet.nodes.len() > NODES_SHOWN {
        let rest = &ordered[NODES_SHOWN..];
        let words = if rest.iter().all(|n| n.status == NodeStatus::Online) {
            format!("{} more, all online", rest.len())
        } else {
            format!("{} more", rest.len())
        };
        nodes = nodes.push(more(
            words,
            format!("See all {}", fleet.nodes.len()),
            Message::Go(Screen::Nodes),
            tokens,
        ));
    }
    if fleet.nodes.is_empty() {
        nodes = nodes.push(empty(Icon::Nodes, "No node yet", tokens));
    }

    // A headline wraps in what the age leaves it, as on a bot's page.
    let mut latest = column![].spacing(8);
    for task in fleet.tasks.iter().take(8) {
        latest = latest.push(
            mouse_area(
                row![
                    slot(pill(Tone::of_task(task.status), task.status.label(), tokens), PILL_SLOT),
                    container(super::grid::fitted(task.bot_id.to_string(), true, tokens)).width(BOT_SLOT),
                    text(task.headline(48))
                        .size(SMALL)
                        .wrapping(Wrapping::WordOrGlyph)
                        .width(Length::Fill),
                    muted(format::age(task.created_at, app.now), tokens),
                ]
                .spacing(10)
                .align_y(Alignment::Center),
            )
            .on_press(Message::Go(Screen::Task(task.id.clone())))
            .interaction(mouse::Interaction::Pointer),
        );
    }
    if fleet.tasks.is_empty() {
        latest = latest.push(empty(Icon::Tasks, "No task yet", tokens));
    }

    page = page.push(
        row![
            container(section(Icon::Nodes, "Nodes", nodes, tokens)).width(Length::FillPortion(2)),
            container(section(Icon::Tasks, "Latest tasks", latest, tokens)).width(Length::FillPortion(3)),
        ]
        .spacing(16),
    );
    scrollable(page.padding(iced::Padding {
        right: 12.0,
        ..Default::default()
    }))
    .id(super::widgets::PAGE)
    .height(Length::Fill)
    .into()
}

/// How many waiting tasks Home lists before «See all».
const NEEDS_YOU_SHOWN: usize = 5;

/// How many nodes Home lists before «See all».
const NODES_SHOWN: usize = 5;

/// The width of a bot's name in Latest tasks, so the tasks' words start
/// at one edge.
const BOT_SLOT: f32 = 150.0;

/// The width of «seen 12m», kept to its right edge.
const SEEN: f32 = 84.0;

/// The line under a capped list: how many more, and the way to all.
fn more<'a>(words: String, label: String, go: Message, tokens: Tokens) -> El<'a> {
    row![
        muted(words, tokens),
        Space::new().width(Length::Fill),
        button(
            row![semibold(label).size(SMALL), icon(Icon::Next, 12.0, tokens.muted)]
                .spacing(6)
                .align_y(Alignment::Center),
        )
        .padding(Padding::from([4, 8]))
        .style(styles::button_style(tokens, Kind::Ghost))
        .on_press(go),
    ]
    .align_y(Alignment::Center)
    .into()
}

/// A big number in a card; see `widgets::stat`.
fn stat<'a>(
    glyph: Icon,
    value: String,
    label: &'a str,
    tone: Tone,
    on_press: Option<Message>,
    tokens: Tokens,
) -> El<'a> {
    super::widgets::stat(glyph, value, label, tone, on_press, tokens)
}

/// Before the first answer: connecting, or why not.
pub fn loading<'a>(app: &'a App, tokens: Tokens) -> El<'a> {
    let body: El<'a> = match &app.fleet_error {
        Some(error) => column![
            row![icon(Icon::Alert, 26.0, tokens.bad), bold(error.title()).size(22.0)]
                .spacing(12)
                .align_y(Alignment::Center),
            text(error.detail()).color(tokens.muted),
            action(
                Kind::Primary,
                Icon::Settings,
                "Open settings",
                Some(Message::Go(Screen::Settings)),
                tokens
            ),
        ]
        .spacing(14)
        .into(),
        None if app.connected() => empty(Icon::Hourglass, "Loading the fleet…", tokens),
        None => column![
            empty(Icon::Hourglass, "Connecting to the control plane…", tokens),
            action(
                Kind::Secondary,
                Icon::Settings,
                "Settings",
                Some(Message::Go(Screen::Settings)),
                tokens
            ),
        ]
        .spacing(12)
        .align_x(Alignment::Center)
        .into(),
    };
    card(body, tokens).into()
}
