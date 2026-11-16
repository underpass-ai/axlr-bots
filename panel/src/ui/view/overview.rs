//! Home: what needs a person, the fleet's numbers, and the fleet, the
//! latest tasks and the nodes side by side. The page never scrolls: at
//! most a column scrolls its own rows.

use iced::widget::scrollable::{Direction, Scrollbar};
use iced::widget::{Space, button, column, container, responsive, row, scrollable, text, tooltip};
use iced::{Alignment, Color, Length, Padding};

use super::sheet::{self, Ink};
use super::widgets::{El, LABEL, SMALL, action, bold, card, empty, icon, muted, pixel_number, semibold};
use super::{grid, status};
use crate::application::Fleet;
use crate::domain::{Bot, BotStatus, NodeStatus, TaskStatus};
use crate::ui::find::{Collapsed, FoldRepeats, NodeRows, Query, TaskLine, TaskWhat, TeamGroup};
use crate::ui::format;
use crate::ui::styles::{self, Kind};
use crate::ui::view::widgets::{NUMBER, number_width};
use crate::ui::{App, Icon, Message, Screen, Tokens, Tone};

/// The room between Home's parts and around them.
const GAP: f32 = 12.0;
/// The height of a line of Home's lists.
const LINE: f32 = 30.0;
/// The width of a status in Home's lists, so the words after it start
/// at one edge.
const STATUS: f32 = 86.0;
/// The width of a bot's name in Home's lists.
const BOT: f32 = 124.0;
/// How many waiting tasks Home lists before «See all».
const NEEDS_YOU_SHOWN: usize = 5;
/// How many of the newest tasks Home lists.
const LATEST_SHOWN: usize = 20;

pub fn view<'a>(app: &'a App, tokens: Tokens) -> El<'a> {
    let Some(fleet) = &app.fleet else {
        return loading(app, tokens);
    };
    column![
        needs_you(app, fleet, tokens),
        numbers(app, fleet, tokens),
        row![
            container(fleet_card(fleet, tokens)).width(Length::FillPortion(21)),
            container(latest(app, fleet, tokens)).width(Length::FillPortion(27)),
            container(nodes(app, fleet, tokens)).width(Length::FillPortion(16)),
        ]
        .spacing(GAP)
        .height(Length::Fill),
    ]
    .spacing(GAP)
    .padding(Padding::from([GAP, sheet::SIDE]))
    .into()
}

/// The first line of Home: the tasks that wait for a person, each with
/// its answers, in an amber block; a bot's ask to listen when only that
/// waits; or one quiet line when nothing does.
fn needs_you<'a>(app: &'a App, fleet: &'a Fleet, tokens: Tokens) -> El<'a> {
    let waiting = fleet.waiting();
    if waiting.is_empty() {
        return match fleet.bots.iter().find(|b| b.pending_asks().next().is_some()) {
            // Asks to listen and no waiting task: the line opens the bot
            // that asks.
            Some(bot) => {
                let asks = app.pending_asks();
                let words = if asks == 1 {
                    format!("{} asks to listen to a topic.", bot.id)
                } else {
                    format!("{asks} asks to listen to a topic wait for your answer.")
                };
                container(
                    row![
                        status::task(TaskStatus::WaitingApproval, tokens),
                        text(words).width(Length::Fill),
                        action(
                            Kind::Secondary,
                            Icon::Next,
                            "Review",
                            Some(Message::Go(Screen::Bot(bot.id.clone()))),
                            tokens
                        ),
                    ]
                    .spacing(10)
                    .align_y(Alignment::Center),
                )
                .padding(Padding::from([6, 12]))
                .width(Length::Fill)
                .style(styles::tone_card(tokens, Tone::Wait))
                .into()
            }
            None => {
                let failed = fleet.overview.tasks_since_in(TaskStatus::Failed);
                let rest = if failed == 0 {
                    "No task waits for your OK. Nothing failed today.".to_string()
                } else {
                    format!(
                        "No task waits for your OK. {} failed today.",
                        format::count(u64::from(failed), "task", "tasks")
                    )
                };
                container(
                    row![
                        super::widgets::status(Icon::Check, Tone::Good, "Nothing needs you", false, tokens),
                        text(rest).color(tokens.muted)
                    ]
                    .spacing(10)
                    .align_y(Alignment::Center),
                )
                .padding(Padding::from([9, 12]))
                .width(Length::Fill)
                .style(styles::card(tokens))
                .into()
            }
        };
    }
    let total = waiting.len();
    let block = responsive(move |space| {
        let mut list = column![].spacing(4);
        // The age and the buttons keep their room; what the task asks
        // has the rest, on one line.
        let what = (space.width - 24.0 - STATUS - ASKER - 44.0 - ANSWERS - 4.0 * 10.0).max(80.0);
        for task in waiting.iter().take(NEEDS_YOU_SHOWN) {
            // What the bot wants to do, so an OK here is not blind.
            let asked = TaskWhat::of(task).title();
            let wants = fleet
                .call_of(&task.id)
                .map(|call| format!("wants {}", call.summary(40)))
                .unwrap_or_default();
            list = list.push(
                row![
                    container(status::task(task.status, tokens)).width(STATUS),
                    container(sheet::words(task.bot_id.to_string(), ASKER, Ink::Strong, tokens)).width(ASKER),
                    container(sheet::pair(asked, &wants, what, &Query::default(), tokens)).width(Length::Fill),
                    container(Ink::Quiet.write(format::age(task.created_at, app.now), tokens))
                        .width(44)
                        .align_x(Alignment::End),
                    super::decide::buttons(app, &task.id, tokens),
                    action(
                        Kind::Ghost,
                        Icon::Next,
                        "Review",
                        Some(Message::Go(Screen::Task(task.id.clone()))),
                        tokens
                    ),
                ]
                .spacing(10)
                .align_y(Alignment::Center),
            );
        }
        // The newest few, then the way to all of them: what came in last
        // stays in sight.
        if total > NEEDS_YOU_SHOWN {
            list = list.push(
                row![
                    muted(format!("{} more wait for you", total - NEEDS_YOU_SHOWN), tokens),
                    Space::new().width(Length::Fill),
                    link(
                        format!("See all {total}"),
                        Message::FilterStatus(Some(TaskStatus::WaitingApproval)),
                        tokens
                    ),
                ]
                .align_y(Alignment::Center),
            );
        }
        container(list)
            .padding(Padding::from([8, 12]))
            .width(Length::Fill)
            .style(styles::tone_card(tokens, Tone::Wait))
            .into()
    });
    block.height(Length::Shrink).into()
}

/// The width of the bot's name on a waiting task's line, which has
/// the window's whole width: more than in a column's list.
const ASKER: f32 = 180.0;

/// The room Approve, Deny and Review take at the end of a waiting
/// task's line.
const ANSWERS: f32 = 290.0;

/// The fleet's numbers in one strip, in the order an operator reads
/// them; each goes to the list it counts.
fn numbers<'a>(app: &'a App, fleet: &'a Fleet, tokens: Tokens) -> El<'a> {
    let o = &fleet.overview;
    let online = o.nodes_in(NodeStatus::Online);
    let waiting = app.waiting_count();
    // Asks to listen and no waiting task: the count opens the bot that asks.
    let need_you = match fleet.bots.iter().find(|b| b.pending_asks().next().is_some()) {
        Some(bot) if o.tasks_in(TaskStatus::WaitingApproval) == 0 => Message::Go(Screen::Bot(bot.id.clone())),
        _ => Message::FilterStatus(Some(TaskStatus::WaitingApproval)),
    };
    let running = o.tasks_in(TaskStatus::Running);
    let failed = o.tasks_since_in(TaskStatus::Failed);
    let (stopped, expired) = (
        o.tasks_since_in(TaskStatus::Cancelled),
        o.tasks_since_in(TaskStatus::Expired),
    );
    // Runs stopped before they said what they spent: the cost today is a
    // floor, and the number says on hover how many.
    let cost_note = o.cost_since().is_floor().then(|| {
        format!(
            "At least: {} did not say what it cost.",
            format::count(u64::from(o.cost_unknown_since), "run", "runs")
        )
    });
    let cells = [
        number(
            "nodes online",
            format!("{online}/{}", o.total_nodes()),
            if o.nodes_down() > 0 { Tone::Bad } else { Tone::Idle },
            None,
            Message::Go(Screen::Nodes),
            tokens,
        ),
        number(
            "bots on",
            format!("{}/{}", o.bots_in(BotStatus::Enabled), o.total_bots()),
            Tone::Idle,
            None,
            Message::Go(Screen::Bots),
            tokens,
        ),
        number(
            "running",
            running.to_string(),
            if running > 0 { Tone::Busy } else { Tone::Idle },
            None,
            Message::FilterStatus(Some(TaskStatus::Running)),
            tokens,
        ),
        number(
            "need you",
            waiting.to_string(),
            if waiting > 0 { Tone::Wait } else { Tone::Idle },
            None,
            need_you,
            tokens,
        ),
        number(
            "failed today",
            failed.to_string(),
            if failed > 0 { Tone::Bad } else { Tone::Idle },
            None,
            Message::FilterStatus(Some(TaskStatus::Failed)),
            tokens,
        ),
        // Stopped by a person or expired with nobody deciding: neither
        // ran to its end. It goes to the ones that expired when there
        // are any, as those were nobody's choice.
        number(
            "stopped today",
            (stopped + expired).to_string(),
            if expired > 0 { Tone::Late } else { Tone::Idle },
            (stopped + expired > 0).then(|| format!("{stopped} stopped · {expired} expired")),
            Message::FilterStatus(Some(if expired > 0 || stopped == 0 {
                TaskStatus::Expired
            } else {
                TaskStatus::Cancelled
            })),
            tokens,
        ),
        number(
            "cost today",
            format::spend(o.cost_since()),
            Tone::Idle,
            cost_note,
            Message::FilterStatus(None),
            tokens,
        ),
    ];
    let mut strip = row![].height(Length::Shrink);
    for (index, cell) in cells.into_iter().enumerate() {
        if index > 0 {
            strip = strip.push(container(Space::new().width(1).height(Length::Fill)).style(styles::rule(tokens)));
        }
        strip = strip.push(cell);
    }
    container(strip)
        .width(Length::Fill)
        .height(NUMBERS)
        .style(styles::card(tokens))
        .into()
}

/// The height of the strip of numbers.
const NUMBERS: f32 = 60.0;

/// One number of the strip: its words over it, small, and the number
/// in the pixel face; in its tone only when it calls for a look. A
/// click goes where the number comes from; `tip` says more on hover.
fn number<'a>(words: &'a str, value: String, tone: Tone, tip: Option<String>, go: Message, tokens: Tokens) -> El<'a> {
    let ink = match tone {
        Tone::Wait | Tone::Late | Tone::Bad | Tone::Busy => styles::pill_ink(tokens, tone),
        Tone::Good | Tone::Idle => tokens.text,
    };
    // A long cost in a narrow window is drawn smaller, never cut.
    let size = if number_width(&value, NUMBER) > 104.0 {
        NUMBER * 0.8
    } else {
        NUMBER
    };
    let cell = button(
        column![
            text(words).size(LABEL).color(tokens.muted),
            container(pixel_number(&value, size, ink))
                .height(NUMBER)
                .align_y(Alignment::End)
                .clip(false)
        ]
        .spacing(3),
    )
    .width(Length::Fill)
    .padding(Padding::from([7, 12]))
    .style(plain(tokens))
    .on_press(go);
    let cell: El<'a> = match tip {
        Some(tip) => sheet::tip(cell.into(), tip, tokens),
        None => cell.into(),
    };
    container(cell).width(Length::FillPortion(1)).into()
}

/// A line or a number that is pressed whole: nothing around it at
/// rest, the raised grey under the pointer.
fn plain(tokens: Tokens) -> impl Fn(&iced::Theme, button::Status) -> button::Style {
    move |theme, status| button::Style {
        border: iced::Border {
            radius: styles::RADIUS_S.into(),
            color: Color::TRANSPARENT,
            width: 0.0,
        },
        ..styles::button_style(tokens, Kind::Ghost)(theme, status)
    }
}

/// A card of Home: its title with a few words beside it and the way to
/// the whole list, over a body that scrolls by itself when it is long.
fn column_card<'a>(title: &'a str, note: String, all: (&'a str, Message), body: El<'a>, tokens: Tokens) -> El<'a> {
    // The note has what the title and the link leave, cut at its edge
    // in a narrow window: the link never leaves the card.
    let head = row![
        semibold(title).size(SMALL).color(tokens.muted),
        container(
            text(note)
                .size(SMALL)
                .color(tokens.muted)
                .wrapping(text::Wrapping::None)
        )
        .width(Length::Fill)
        .clip(true),
        link(all.0.to_string(), all.1, tokens),
    ]
    .spacing(8)
    .align_y(Alignment::Center);
    container(column![
        container(head).padding(Padding::from([8, 12]).bottom(6)),
        scrollable(body)
            .direction(Direction::Vertical(
                Scrollbar::new().width(6).scroller_width(6).margin(2)
            ))
            .width(Length::Fill)
            .height(Length::Fill)
    ])
    .width(Length::Fill)
    .height(Length::Fill)
    .style(styles::card(tokens))
    .into()
}

/// A few words in the accent that go somewhere: `All tasks ›`.
fn link<'a>(words: String, go: Message, tokens: Tokens) -> El<'a> {
    let ink = styles::accent_ink(tokens);
    button(
        row![semibold(words).size(12.5).color(ink), icon(Icon::Next, 10.0, ink)]
            .spacing(3)
            .align_y(Alignment::Center),
    )
    .padding(0)
    .style(styles::button_style(tokens, Kind::Link))
    .on_press(go)
    .into()
}

/// The width of a team's name beside its bots.
const TEAM: f32 = 78.0;

/// Every bot as a small tile, by team: a bot that is on has a dot and
/// stands out, one that is not is muted. A click opens the bot.
fn fleet_card<'a>(fleet: &'a Fleet, tokens: Tokens) -> El<'a> {
    let groups = TeamGroup::of(&fleet.bots, |_| true, false, &Collapsed::default());
    let on = fleet.bots.iter().filter(|b| b.status == BotStatus::Enabled).count();
    let off = fleet.bots.len() - on;
    let note = match (on, off) {
        (_, 0) => format!("{on} on"),
        _ => format!("{on} on · {off} not on"),
    };
    let mut body = column![].spacing(7).padding(Padding::from([0, 12]).bottom(10));
    for group in &groups {
        let mut tiles = row![].spacing(4);
        for bot in &group.bots {
            tiles = tiles.push(tile(bot, team_of(&group.name), tokens));
        }
        body = body.push(
            row![
                container(sheet::words(group.name.clone(), TEAM, Ink::Quiet, tokens))
                    .width(TEAM)
                    .padding(Padding::ZERO.top(4)),
                container(tiles.wrap().vertical_spacing(4)).width(Length::Fill)
            ]
            .spacing(8),
        );
    }
    let body: El<'a> = if fleet.bots.is_empty() {
        empty(Icon::Bots, "No bot yet", tokens)
    } else {
        body.into()
    };
    column_card("Fleet", note, ("All bots", Message::Go(Screen::Bots)), body, tokens)
}

/// The team a group of tiles stands for: none for the bots in no team.
fn team_of(name: &str) -> &str {
    if name == crate::ui::find::NO_TEAM { "" } else { name }
}

/// A bot's name on its tile: without its team's name before it, which
/// the line already says.
fn tile_name(bot: &Bot, team: &str) -> String {
    let id = bot.id.as_str();
    let short = id
        .strip_prefix(team)
        .and_then(|rest| rest.strip_prefix('-'))
        .filter(|rest| !team.is_empty() && !rest.is_empty())
        .unwrap_or(id);
    grid::shorten(short, 20)
}

fn tile<'a>(bot: &'a Bot, team: &str, tokens: Tokens) -> El<'a> {
    let on = bot.status == BotStatus::Enabled;
    let (glyph, ink) = if on {
        (Icon::Dot, tokens.good)
    } else {
        (Icon::Pause, tokens.muted)
    };
    let state = if on { "On" } else { bot.status.name() };
    tooltip(
        button(
            row![icon(glyph, 10.0, ink), text(tile_name(bot, team)).size(LABEL)]
                .spacing(4)
                .align_y(Alignment::Center),
        )
        .height(24)
        .padding(Padding::from([0, 7]))
        .style(styles::tile(tokens, on))
        .on_press(Message::Go(Screen::Bot(bot.id.clone()))),
        container(text(format!("{} · {state}", bot.id)).size(SMALL))
            .padding(6)
            .style(styles::chip(tokens)),
        tooltip::Position::Bottom,
    )
    .into()
}

/// The newest tasks with their repeats folded, one line each: status,
/// bot, what it is about with the count of the repeats it folds, how
/// long ago. A click opens the line's newest task.
fn latest<'a>(app: &'a App, fleet: &'a Fleet, tokens: Tokens) -> El<'a> {
    let now = app.now;
    let lines: Vec<TaskLine<'a>> = FoldRepeats::on()
        .lines(&fleet.tasks)
        .into_iter()
        .take(LATEST_SHOWN)
        .collect();
    let note = match FoldRepeats::folded(&lines) {
        0 => String::new(),
        1 => "1 repeat folded".to_string(),
        folded => format!("{folded} repeats folded"),
    };
    let body: El<'a> = if fleet.tasks.is_empty() {
        empty(Icon::Tasks, "No task yet", tokens)
    } else {
        responsive(move |space| {
            let what = (space.width - 8.0 - 16.0 - STATUS - BOT - SEEN - 3.0 * 10.0).max(24.0);
            let mut list = column![].padding(Padding::from([0, 4]).bottom(6));
            for line in &lines {
                list = list.push(task_line(line, what, now, tokens));
            }
            list.into()
        })
        .height(Length::Shrink)
        .into()
    };
    column_card("Latest", note, ("All tasks", Message::FilterStatus(None)), body, tokens)
}

fn task_line<'a>(line: &TaskLine<'a>, what: f32, now: chrono::DateTime<chrono::Utc>, tokens: Tokens) -> El<'a> {
    let task = line.task;
    let first = TaskWhat::of(task).title().to_string();
    let words: El<'a> = match line.badge() {
        Some(count) => {
            let room = 12.0 + 7.0 * count.chars().count() as f32;
            row![
                sheet::words(first, what - room - 6.0, Ink::Quiet, tokens),
                container(text(count).size(11.0).color(tokens.muted))
                    .padding(Padding::from([0, 4]))
                    .style(styles::count_tag(tokens))
            ]
            .spacing(6)
            .align_y(Alignment::Center)
            .into()
        }
        None => sheet::words(first, what, Ink::Quiet, tokens),
    };
    button(
        row![
            container(status::task(task.status, tokens)).width(STATUS),
            container(sheet::words(task.bot_id.to_string(), BOT, Ink::Strong, tokens)).width(BOT),
            container(words).width(Length::Fill),
            container(Ink::Quiet.write(format::age(task.created_at, now), tokens))
                .width(SEEN)
                .align_x(Alignment::End),
        ]
        .spacing(10)
        .height(Length::Fill)
        .align_y(Alignment::Center),
    )
    .width(Length::Fill)
    .height(LINE)
    .padding(Padding::from([0, 8]))
    .style(plain(tokens))
    .on_press(Message::Go(Screen::Task(task.id.clone())))
    .into()
}

/// The width of an age at the end of a line of Home's lists.
const SEEN: f32 = 40.0;

/// The nodes of the fleet, the ones online first, each with how long
/// ago it was last seen; the revoked ones are only counted.
fn nodes<'a>(app: &'a App, fleet: &'a Fleet, tokens: Tokens) -> El<'a> {
    let now = app.now;
    let live = NodeRows::default().rows(&fleet.nodes);
    let revoked = NodeRows::revoked(&fleet.nodes);
    let note = if revoked > 0 {
        format!("{revoked} revoked hidden")
    } else {
        String::new()
    };
    let body: El<'a> = if live.is_empty() {
        empty(Icon::Nodes, "No node yet", tokens)
    } else {
        responsive(move |space| {
            let name = (space.width - 8.0 - 16.0 - STATUS - SEEN - 2.0 * 10.0).max(24.0);
            let mut list = column![].padding(Padding::from([0, 4]).bottom(6));
            for node in &live {
                list = list.push(
                    button(
                        row![
                            container(status::node(node.status, tokens)).width(STATUS),
                            container(sheet::words(node.id.to_string(), name, Ink::Strong, tokens)).width(Length::Fill),
                            container(Ink::Quiet.write(format::age(node.last_seen_at, now), tokens))
                                .width(SEEN)
                                .align_x(Alignment::End),
                        ]
                        .spacing(10)
                        .height(Length::Fill)
                        .align_y(Alignment::Center),
                    )
                    .width(Length::Fill)
                    .height(LINE)
                    .padding(Padding::from([0, 8]))
                    .style(plain(tokens))
                    .on_press(Message::Go(Screen::Nodes)),
                );
            }
            list.into()
        })
        .height(Length::Shrink)
        .into()
    };
    column_card("Nodes", note, ("All", Message::Go(Screen::Nodes)), body, tokens)
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
    container(card(body, tokens)).padding(Padding::from([14, 18])).into()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_tile_leaves_out_the_team_its_line_already_says() {
        let bot = |id: &str| -> Bot {
            serde_json::from_value(serde_json::json!({
                "id": id, "name": id, "mission": "Do it.", "status": "Enabled",
                "createdAt": "2026-10-26T10:00:00Z", "updatedAt": "2026-10-26T10:00:00Z"
            }))
            .unwrap()
        };
        assert_eq!(tile_name(&bot("ops-watch"), "ops"), "watch");
        assert_eq!(tile_name(&bot("opsy"), "ops"), "opsy");
        assert_eq!(tile_name(&bot("ops-"), "ops"), "ops-");
        assert_eq!(tile_name(&bot("alone"), ""), "alone");
        assert_eq!(
            tile_name(&bot("a-bot-with-a-very-long-name-indeed"), "ops")
                .chars()
                .count(),
            20
        );
    }
}
