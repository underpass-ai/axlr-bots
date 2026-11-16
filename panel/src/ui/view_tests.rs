//! The screens drawn headless with Iced's simulator: what each shows, and
//! what a click on it sends.

use std::borrow::Cow;
use std::sync::{Mutex, MutexGuard, Once};

use iced::widget::text::Wrapping;
use iced::widget::{Text, container, text};
use iced::{Rectangle, Settings, Size};
use iced_test::{Simulator, simulator};

use super::runtime::{FONTS, MIN_WINDOW, WINDOW};
use super::test_runtime::{connected, send};
use super::view::tests::app;
use super::view::widgets::{self, El};
use super::*;
use crate::application::Density;
use crate::domain::{BotId, NodeId, TaskStatus};

/// The headless renderer is not safe to run on two threads at once: the
/// simulators take turns.
static SIMULATOR: Mutex<()> = Mutex::new(());

pub(super) fn turn() -> MutexGuard<'static, ()> {
    SIMULATOR.lock().unwrap_or_else(|poisoned| poisoned.into_inner())
}

/// A simulator's settings with the panel's typefaces, so its texts are
/// as wide as in the window. The typefaces go into the renderer's shared
/// font system with the first simulator built on these; build it on your
/// turn.
pub(super) fn panel_settings() -> Settings {
    static LOADED: Once = Once::new();
    let mut fonts = Vec::new();
    LOADED.call_once(|| fonts = FONTS.iter().map(|font| Cow::Borrowed(*font)).collect());
    Settings {
        fonts,
        default_font: widgets::REGULAR,
        default_text_size: widgets::BODY.into(),
        ..Settings::default()
    }
}

/// Fails unless `words` show whole on `app` in a window `width` wide
/// (and tall enough that nothing scrolls away): the layout gives their
/// text at least the width `alone` takes with room or, once it wraps,
/// the height its lines take in its width, and no edge cuts it.
/// The simulator finds boxes, not pixels: a text given less than it
/// needs keeps the smaller box and is drawn past it, over a neighbour or
/// cut at the card. `alone` gets the box the text has on the screen.
fn shows_whole(app: &App, width: f32, words: &str, alone: impl Fn(Rectangle) -> Text<'static>) {
    let _turn = turn();
    let window = Size::new(width, 2000.0);
    let mut ui = Simulator::with_size(panel_settings(), window, view::view(app));
    let placed = ui
        .find(words)
        .unwrap_or_else(|e| panic!("{words} is not on {:?}: {e:?}", app.screen));
    let room = placed.bounds();
    let line = box_alone(words, alone(room));
    if room.height < line.height * 1.5 {
        assert!(
            room.width >= line.width - 0.5,
            "{words} has {} of the {} px it takes, {width} px wide",
            room.width,
            line.width
        );
    } else {
        // Broken anywhere in its box's width: a word longer than the box
        // takes more lines than the box holds.
        let lines = box_alone(
            words,
            container(alone(room).wrapping(Wrapping::WordOrGlyph)).width(room.width + 0.5),
        );
        assert!(
            room.height >= lines.height - 0.5,
            "{words} has {} of the {} px high its lines take, {width} px wide",
            room.height,
            lines.height
        );
    }
    let seen = placed.visible_bounds().map_or(0.0, |b| b.width);
    assert!(seen >= room.width - 0.5, "{words} is cut, {width} px wide");
}

/// The box `words` take drawn alone in a wide window.
fn box_alone<'a>(words: &str, alone: impl Into<El<'a>>) -> Rectangle {
    let mut ui = Simulator::with_size(panel_settings(), Size::new(4000.0, 2000.0), alone.into());
    ui.find(words).unwrap().bounds()
}

fn messages_of_click(app: &App, label: &str) -> Vec<Message> {
    let _turn = turn();
    // As wide as the panel opens and tall enough for the whole page: the
    // page scrolls, and a click is about the control, not the fold.
    let mut ui = Simulator::with_size(panel_settings(), Size::new(WINDOW.width, 2000.0), view::view(app));
    ui.click(label)
        .unwrap_or_else(|e| panic!("cannot click {label}: {e:?}"));
    said(ui)
}

/// What the screen sent, without the news of the pointer the glowing
/// rows, cards and tiles under it give with every move.
fn said<'a>(ui: Simulator<'a, Message>) -> Vec<Message> {
    ui.into_messages().filter(|m| !matches!(m, Message::Hover(_))).collect()
}

fn simulator_misses(app: &App, word: &str) -> bool {
    let _turn = turn();
    let mut ui = simulator(view::view(app));
    ui.find(word).is_err()
}

/// Fails unless the screen holds a reader of `words` under `key`.
fn reads(app: &App, key: ReaderKey, words: &str) {
    let reader = app
        .readers
        .get(&key)
        .unwrap_or_else(|| panic!("no reader {key:?} on {:?}", app.screen));
    assert!(reader.source().contains(words), "{key:?} reads {:?}", reader.source());
}

/// Fails unless every word shows in the window's size at start: a
/// list's cells cut their words to their column, so what a narrow
/// window cuts is looked for in this one.
fn shows_wide(app: &App, words: &[&str]) {
    let _turn = turn();
    let mut ui = Simulator::with_size(panel_settings(), WINDOW, view::view(app));
    for word in words {
        assert!(ui.find(*word).is_ok(), "{word} is not on {:?}", app.screen);
    }
}

fn shows(app: &App, words: &[&str]) {
    let _turn = turn();
    let mut ui = simulator(view::view(app));
    for word in words {
        assert!(ui.find(*word).is_ok(), "{word} is not on {:?}", app.screen);
    }
}

#[test]
fn the_first_run_shows_the_settings_and_connects_on_a_click() {
    let app = app();
    shows(
        &app,
        &[
            "Settings",
            "Welcome. Check these three values.",
            "Connect",
            "Not connected",
        ],
    );
    let sent = messages_of_click(&app, "Connect");
    assert!(matches!(sent.as_slice(), [Message::SettingsSave]));
}

/// Connected, the settings screen says so and Connect has nothing to
/// press; the other sections, the JSON editor and the file acts are
/// there, and a bot's configuration is not.
#[test]
fn the_settings_screen_shows_its_sections_and_rests_connect_once_connected() {
    let mut app = connected();
    send(&mut app, Message::Go(Screen::Settings));
    shows(
        &app,
        &[
            "Control plane",
            "Connected",
            "Look",
            "New bot",
            "Model proposed",
            "Settings file",
            "Edit as JSON",
            "Export…",
            "Import…",
            "Reset to defaults",
            "~/.config/axlr-bots-panel/settings.json",
        ],
    );
    assert!(simulator_misses(&app, "Welcome. Check these three values."));
    assert!(simulator_misses(&app, "Mission"));
    assert!(messages_of_click(&app, "Connected").is_empty(), "nothing to connect");
    assert!(matches!(
        messages_of_click(&app, "Export…").as_slice(),
        [Message::SettingsExport]
    ));
    assert!(matches!(
        messages_of_click(&app, "Edit as JSON").as_slice(),
        [Message::SettingsJsonOpen]
    ));
    // The rows' height: the one in use rests, the other is one click.
    shows(&app, &["Rows", "Comfortable", "Compact"]);
    assert!(messages_of_click(&app, "Comfortable").is_empty());
    assert!(matches!(
        messages_of_click(&app, "Compact").as_slice(),
        [Message::SetDensity(crate::application::Density::Compact)]
    ));
    // The hover glow, on by default: Off is one click, and On once off.
    shows(&app, &["Hover glow", "On", "Off"]);
    assert!(messages_of_click(&app, "On").is_empty());
    assert!(matches!(
        messages_of_click(&app, "Off").as_slice(),
        [Message::SetMotion(crate::application::Motion::Off)]
    ));
    app.hover.set_motion(crate::application::Motion::Off);
    assert!(matches!(
        messages_of_click(&app, "On").as_slice(),
        [Message::SetMotion(crate::application::Motion::On)]
    ));
    app.hover.set_motion(crate::application::Motion::On);
    app.apply(Message::SettingsCa("/other.crt".into()));
    shows(&app, &["Changed", "Connect"]);
    assert!(matches!(
        messages_of_click(&app, "Connect").as_slice(),
        [Message::SettingsSave]
    ));
    app.apply(Message::SettingsJsonOpen);
    shows(&app, &["Apply", "Close"]);
    assert!(
        simulator_misses(&app, "Model proposed"),
        "the file takes the form's place"
    );
    app.settings_json.content = iced::widget::text_editor::Content::with_text("{\"look.theme\": 1}");
    app.apply(Message::SettingsJsonApply);
    assert!(
        app.settings_json
            .error
            .as_deref()
            .unwrap()
            .starts_with("settings line 1, column 16")
    );
    shows(&app, &["Apply"]);
}

/// The New bot form offers the settings' models as one-click chips.
#[test]
fn the_new_bot_form_offers_the_models_the_settings_name() {
    let mut app = connected();
    app.apply(Message::SettingsModels("codex/gpt-6-luna".into()));
    send(&mut app, Message::SettingsSavePreferences);
    send(&mut app, Message::Go(Screen::Bots));
    send(&mut app, Message::RegisterOpen);
    app.apply(Message::RegisterAdvanced(true));
    shows(&app, &["anthropic/claude-haiku-5.5", "codex/gpt-6-luna"]);
    assert!(matches!(
        messages_of_click(&app, "codex/gpt-6-luna").as_slice(),
        [Message::RegisterModel(m)] if m == "codex/gpt-6-luna"
    ));
    assert!(
        messages_of_click(&app, "anthropic/claude-haiku-5.5").is_empty(),
        "the one in the field is lit"
    );
}

/// Home lists the five newest calls and the way to all of them, and
/// the nodes that are away with the others.
#[test]
fn home_caps_what_needs_you_and_lists_the_nodes_that_are_away() {
    let mut app = connected();
    let fleet = app.fleet.as_mut().unwrap();
    let waiting = fleet
        .tasks
        .iter()
        .find(|t| t.status == TaskStatus::WaitingApproval)
        .unwrap()
        .clone();
    let shown = fleet.waiting().len();
    for n in shown..7 {
        let mut copy = waiting.clone();
        copy.id = crate::domain::TaskId::new(&format!("task-wait-{n}")).unwrap();
        fleet.tasks.push(copy);
    }
    fleet.nodes[1].status = crate::domain::NodeStatus::Offline;
    shows(&app, &["2 more wait for you", "See all 7", "Offline"]);
    assert!(matches!(
        messages_of_click(&app, "See all 7").as_slice(),
        [Message::FilterStatus(Some(TaskStatus::WaitingApproval))]
    ));
}

#[test]
fn home_shows_the_counts_and_what_needs_a_person() {
    let mut app = connected();
    shows(
        &app,
        &[
            "Home",
            "2/2",
            "nodes online",
            "need you",
            "failed today",
            "stopped today",
            "Needs OK",
            "Review",
            "Fleet",
            "Latest",
            "All tasks",
            "2/2 nodes online",
            "127.0.0.1:7879",
        ],
    );
    shows_wide(&app, &["wants local_write · hello", "Approve", "Deny"]);
    // Each node says how long ago it was last seen.
    let node = &app.fleet.as_ref().unwrap().nodes[0];
    let seen = crate::ui::format::age(node.last_seen_at, app.now);
    shows(&app, &[seen.as_str()]);
    assert!(matches!(
        messages_of_click(&app, "Approve").as_slice(),
        [Message::Approve(_)]
    ));
    assert!(matches!(
        messages_of_click(&app, "Deny").as_slice(),
        [Message::AskDeny(_)]
    ));
    let sent = messages_of_click(&app, "Review");
    assert!(matches!(sent.as_slice(), [Message::Go(Screen::Task(_))]));
    // Stopped and expired share a number, which goes to the expired.
    let sent = messages_of_click(&app, "stopped today");
    assert!(matches!(
        sent.as_slice(),
        [Message::FilterStatus(Some(TaskStatus::Expired))]
    ));
    // Every number goes to its list, the cost to the tasks that made it.
    for (words, goes) in [
        ("nodes online", Message::Go(Screen::Nodes)),
        ("bots on", Message::Go(Screen::Bots)),
        ("running", Message::FilterStatus(Some(TaskStatus::Running))),
        ("need you", Message::FilterStatus(Some(TaskStatus::WaitingApproval))),
        ("failed today", Message::FilterStatus(Some(TaskStatus::Failed))),
        ("cost today", Message::FilterStatus(None)),
    ] {
        let sent = messages_of_click(&app, words);
        assert_eq!(format!("{sent:?}"), format!("{:?}", [goes]), "{words}");
    }
    // A bot of the fleet is a tile that opens it, and a latest task a
    // line that opens it.
    let sent = messages_of_click(&app, "bot-wrx80");
    assert!(
        matches!(sent.as_slice(), [Message::Go(Screen::Bot(id))] if id.as_str() == "bot-wrx80"),
        "{sent:?}"
    );
    let sent = messages_of_click(&app, "Say the time");
    assert!(matches!(sent.as_slice(), [Message::Go(Screen::Task(_))]), "{sent:?}");
    let sent = messages_of_click(&app, "Nodes");
    assert!(matches!(sent.as_slice(), [Message::Go(Screen::Nodes)]));
    app.apply(Message::ToggleTheme);
    shows(&app, &["Home", "cost today"]);
}

#[test]
fn the_cost_of_the_day_shows_whole_and_the_short_counts_stay_big() {
    let mut app = connected();
    for usd in [0.0036, 12.40, 1234.56] {
        app.fleet.as_mut().unwrap().overview.cost_usd_since = usd;
        let cost = format::cost(usd);
        for width in [MIN_WINDOW.width, WINDOW.width] {
            // In the pixel face at the size its cell gives it, which its
            // line's height tells; the point in a cell of its own.
            for part in cost.split('.') {
                shows_whole(&app, width, part, |room| {
                    widgets::number(part.to_string()).size(room.height)
                });
            }
        }
    }
    // Runs that did not tell their cost: the number says at least.
    app.fleet.as_mut().unwrap().overview.cost_unknown_since = 2;
    shows(&app, &["≥"]);
    let _turn = turn();
    for width in [MIN_WINDOW.width, WINDOW.width] {
        let mut ui = Simulator::with_size(panel_settings(), Size::new(width, 2000.0), view::view(&app));
        let count = ui.find("2/2").unwrap().bounds();
        assert!(
            (count.height - widgets::NUMBER).abs() < 0.01,
            "2/2 is smaller, {width} px wide"
        );
    }
}

/// Home never scrolls as a page: in the window's size at start its
/// three columns end inside the window, with the fleet of a real size.
#[test]
fn home_fits_the_window_with_a_large_fleet() {
    let fake = std::sync::Arc::new(crate::adapters::fake::FakeControlPlane::large(
        "2026-10-25T10:00:00Z".parse().unwrap(),
    ));
    let mut app = super::view::tests::app_over(fake);
    send(&mut app, Message::SettingsSave);
    let _turn = turn();
    let mut ui = Simulator::with_size(panel_settings(), WINDOW, view::view(&app));
    for words in [
        "Fleet",
        "Latest",
        "Nodes",
        "40 revoked hidden",
        "All bots",
        "stopped today",
    ] {
        let placed = ui.find(words).unwrap_or_else(|e| panic!("{words}: {e:?}")).bounds();
        assert!(
            placed.y + placed.height <= WINDOW.height && placed.x + placed.width <= WINDOW.width,
            "{words} is out of the window at {placed:?}"
        );
    }
    // Twenty of the newest tasks, one line each, the last one in sight.
    let last = &app.fleet.as_ref().unwrap().tasks[19];
    let age = crate::ui::format::age(last.created_at, app.now);
    let placed = ui.find(age.as_str()).unwrap().visible_bounds();
    assert!(placed.is_some(), "the twentieth task is cut out of Latest");
    drop(ui);
    // In the smallest window each card keeps its link whole and cuts a
    // node's name with an ellipsis, not at its edge.
    let mut ui = Simulator::with_size(panel_settings(), MIN_WINDOW, view::view(&app));
    for words in ["All bots", "All tasks", "All", "Nodes", "stopped today"] {
        let placed = ui.find(words).unwrap_or_else(|e| panic!("{words}: {e:?}"));
        let seen = placed
            .visible_bounds()
            .unwrap_or_else(|| panic!("{words} is out of sight"));
        assert!(seen.width >= placed.bounds().width - 0.5, "{words} is cut at {seen:?}");
    }
    assert!(ui.find("gx10-ff95").is_err(), "a long name is cut to its column");
    let online = ui.find("wrx80").unwrap();
    let seen = online.visible_bounds().unwrap();
    assert!(seen.width >= online.bounds().width - 0.5, "wrx80 is cut at {seen:?}");
}

/// Objectives longer than Home shows: one with a path, one with a name
/// no line can break between its words.
const LONG_OBJECTIVES: [&str; 2] = [
    "Write the weekly report of every node into workspace/reports/weekly-summary.txt and tell the team",
    "Read weekly_summary_of_every_node_and_every_bot_in_the_fleet.txt and say what changed",
];

/// Gives the first task in `status` the objective `words`.
fn set_objective(app: &mut App, status: TaskStatus, words: &str) {
    let tasks = &mut app.fleet.as_mut().unwrap().tasks;
    tasks.iter_mut().find(|t| t.status == status).unwrap().objective = words.to_string();
}

/// A waiting task is one line on Home: what it asks whole when it is
/// short, cut when it is long, and its age and its answers whole
/// beside it either way.
#[test]
fn home_shows_what_a_waiting_task_asks_on_one_line_beside_its_age_and_buttons() {
    let mut app = connected();
    let tokens = Tokens::of(app.mode);
    shows_whole(&app, WINDOW.width, "Write hello into hello.txt", |_| {
        text("Write hello into hello.txt")
    });
    shows_whole(&app, WINDOW.width, "wants local_write · hello", |_| {
        text("wants local_write · hello").color(tokens.muted)
    });
    for objective in ["Write hello into hello.txt"].iter().chain(&LONG_OBJECTIVES) {
        set_objective(&mut app, TaskStatus::WaitingApproval, objective);
        for width in [MIN_WINDOW.width, WINDOW.width] {
            for button in ["Approve", "Deny", "Review"] {
                shows_whole(&app, width, button, |_| widgets::semibold(button).size(widgets::BODY));
            }
            shows_whole(&app, width, "5m", |_| text("5m").size(widgets::SMALL));
        }
    }
    // A long one is cut to its line, with an ellipsis.
    assert!(simulator_misses(&app, LONG_OBJECTIVES[1]));
}

/// A latest task is one line too: whole when short in the window's
/// size at start, cut when long, its age whole beside it.
#[test]
fn home_shows_the_latest_tasks_on_one_line_beside_their_ages() {
    let mut app = connected();
    shows_whole(&app, WINDOW.width, "List the files in your workspace", |_| {
        text("List the files in your workspace").size(widgets::SMALL)
    });
    for objective in LONG_OBJECTIVES {
        set_objective(&mut app, TaskStatus::Completed, objective);
        assert!(simulator_misses(&app, objective));
        for width in [MIN_WINDOW.width, WINDOW.width] {
            shows_whole(&app, width, "30m", |_| text("30m").size(widgets::SMALL));
        }
    }
}

/// The panel over the fleet of a real size, connected, on `screen`.
fn large(screen: Screen) -> App {
    let fake = std::sync::Arc::new(crate::adapters::fake::FakeControlPlane::large(
        "2026-10-25T10:00:00Z".parse().unwrap(),
    ));
    let mut app = super::view::tests::app_over(fake);
    send(&mut app, Message::SettingsSave);
    send(&mut app, Message::Go(screen));
    app
}

/// How many rows of the list on `app` show whole in the window's size
/// at start, from where its first two rows are laid: `first` and
/// `second` are words of those rows.
fn rows_in_sight(app: &App, first: &str, second: &str) -> (usize, f32) {
    let _turn = turn();
    let mut ui = Simulator::with_size(panel_settings(), WINDOW, view::view(app));
    let top = ui.find(first).unwrap().bounds();
    let next = ui.find(second).unwrap().bounds();
    let pitch = next.y - top.y;
    let centre = top.y + top.height / 2.0;
    let mut rows = 0;
    while centre + pitch * rows as f32 + pitch / 2.0 <= WINDOW.height {
        rows += 1;
    }
    (rows, pitch)
}

/// The Tasks list of a real fleet in the window's size at start: one
/// line a task at the density's height, nineteen of them in sight
/// under a header that stays, twenty-three when compact.
#[test]
fn the_tasks_list_shows_nineteen_rows_and_more_when_compact() {
    let mut app = large(Screen::Tasks);
    let fleet = app.fleet.as_ref().unwrap();
    let lines = find::FoldRepeats::on().lines(&fleet.tasks);
    let folded = find::FoldRepeats::folded(&lines);
    let count = format!("{} lines · 200 tasks", lines.len());
    shows(&app, &[count.as_str(), "All", "Done", "Stopped", "Failed"]);
    shows(&app, &[format!("Newest 200 tasks · {folded} repeats folded").as_str()]);
    let (rows, pitch) = rows_in_sight(&app, "Say the time", "Write the weekly report");
    assert_eq!(pitch, Density::Comfortable.row_height());
    assert!(rows >= 19, "{rows} rows in sight");
    send(&mut app, Message::SetDensity(Density::Compact));
    let (compact, pitch) = rows_in_sight(&app, "Say the time", "Write the weekly report");
    assert_eq!(pitch, Density::Compact.row_height());
    assert!(compact >= 23, "{compact} rows in sight when compact");
    // The header is over the rows, not among them: it is where it was
    // whatever the rows do, right under the bar and its note.
    let _turn = turn();
    let mut ui = Simulator::with_size(panel_settings(), WINDOW, view::view(&app));
    let head = ui.find("STATUS").unwrap().bounds();
    let row = ui.find("Say the time").unwrap().bounds();
    assert!(head.y < row.y && row.y - head.y < Density::Compact.row_height());
    // A task an event made says the event's words first.
    assert!(
        ui.find("item-4198 · service-3 · gate failed: the unit tests of the client")
            .is_ok()
    );
    assert!(ui.find("ops.item.assigned").is_ok() && ui.find("round").is_ok());
}

/// Bots are listed by team under headers that fold; a team with no
/// bot on is met closed; the chips narrow them.
#[test]
fn the_bots_list_groups_by_team_and_folds_and_narrows() {
    let mut app = large(Screen::Bots);
    shows(
        &app,
        &[
            "40",
            "ops",
            "4 on · 1 paused",
            "ops-lead",
            "audit",
            "4 paused",
            "no team",
            "3 on",
        ],
    );
    assert!(
        simulator_misses(&app, "audit-lead"),
        "a team with no bot on starts closed"
    );
    let sent = messages_of_click(&app, "audit");
    assert!(
        matches!(sent.as_slice(), [Message::GroupToggled(find::GroupKey::Team(team))] if team == "audit"),
        "{sent:?}"
    );
    send(&mut app, Message::GroupToggled(find::GroupKey::Team("audit".into())));
    shows(&app, &["audit-lead", "audit-spend"]);
    send(&mut app, Message::GroupToggled(find::GroupKey::Team("ops".into())));
    assert!(simulator_misses(&app, "ops-lead"));
    // A bot's name opens its page.
    let sent = messages_of_click(&app, "audit-lead");
    assert!(
        matches!(sent.as_slice(), [Message::Go(Screen::Bot(id))] if id.as_str() == "audit-lead"),
        "{sent:?}"
    );
    // The chips come before the rows, whose status says the same word.
    let sent = messages_of_click(&app, "Paused");
    assert!(
        matches!(sent.as_slice(), [Message::ShowBots(crate::ui::table::BotShow::Paused)]),
        "{sent:?}"
    );
    send(&mut app, Message::ShowBots(crate::ui::table::BotShow::On));
    shows(&app, &["support-lead", "bot-wrx80"]);
    assert!(simulator_misses(&app, "audit"), "a team with no bot on is left out");
    assert!(simulator_misses(&app, "support-survey"));
    assert!(messages_of_click(&app, "On").is_empty(), "the chip that is on rests");
}

/// Revoked nodes are put away under a note that brings them back.
#[test]
fn the_nodes_list_puts_revoked_nodes_away_until_asked_for() {
    let mut app = large(Screen::Nodes);
    shows(
        &app,
        &["8", "Revoked", "40 revoked nodes are hidden.", "Show them", "node-03"],
    );
    assert!(simulator_misses(&app, "old-node-01"));
    // The nodes online come first.
    let _turn = turn();
    let mut ui = Simulator::with_size(panel_settings(), WINDOW, view::view(&app));
    let online = ui.find("wrx80").unwrap().bounds();
    let away = ui.find("node-05").unwrap().bounds();
    assert!(online.y < away.y);
    drop(ui);
    drop(_turn);
    let sent = messages_of_click(&app, "Show them");
    assert!(matches!(sent.as_slice(), [Message::ShowRevoked(true)]), "{sent:?}");
    send(&mut app, Message::ShowRevoked(true));
    shows(&app, &["48", "40 revoked nodes are shown.", "Hide them", "old-node-01"]);
    let sent = messages_of_click(&app, "Revoked");
    assert!(matches!(sent.as_slice(), [Message::ShowRevoked(false)]), "{sent:?}");
}

/// In the smallest window a list's chips go on to a second line and
/// its acts stay in the window; the top bar keeps its right end with a
/// long address and a version.
#[test]
fn the_bars_keep_their_acts_in_the_smallest_window() {
    let mut app = large(Screen::Tasks);
    app.endpoint = Some("control-plane.fleet.example.internal:7879".into());
    app.fleet.as_mut().unwrap().build = Some(crate::domain::BuildStatus {
        running: crate::domain::BuildIdentity {
            version: "0.4.7".into(),
            ..Default::default()
        },
        ..Default::default()
    });
    let _turn = turn();
    let mut ui = Simulator::with_size(panel_settings(), MIN_WINDOW, view::view(&app));
    for words in [
        // The bot picker stands before New task, the last in the bar.
        "New task",
        "All",
        "Stopped",
        "· v0.4.7",
        "updated 10:00:00",
        "Search bots, tasks, nodes, actions…",
    ] {
        let placed = ui.find(words).unwrap_or_else(|e| panic!("{words}: {e:?}"));
        let seen = placed
            .visible_bounds()
            .unwrap_or_else(|| panic!("{words} is out of sight"));
        assert!(
            seen.width >= placed.bounds().width - 0.5 && seen.x + seen.width <= MIN_WINDOW.width,
            "{words} is cut at {seen:?}"
        );
    }
    drop(ui);
    // In the window's size at start the chips are one line and the bar
    // is as high as the design's.
    let mut ui = Simulator::with_size(panel_settings(), WINDOW, view::view(&app));
    let (all, stopped) = (ui.find("All").unwrap().bounds(), ui.find("Stopped").unwrap().bounds());
    assert_eq!(all.y, stopped.y);
    let head = ui.find("STATUS").unwrap().bounds();
    assert!(head.y < 44.0 + 1.0 + 46.0 + 1.0 + 30.0 + 30.0, "{head:?}");
}

/// A form open over a list takes the room it needs and scrolls by
/// itself: in the smallest window the list's bar stays where it was
/// and the form's foot is cut by the form's own scroll, not by a page
/// that grew.
#[test]
fn a_form_open_over_a_list_scrolls_by_itself_in_the_smallest_window() {
    let mut app = large(Screen::Bots);
    app.apply(Message::RegisterOpen);
    app.apply(Message::RegisterAdvanced(true));
    let _turn = turn();
    let mut ui = Simulator::with_size(panel_settings(), MIN_WINDOW, view::view(&app));
    let bar = ui.find("From file").unwrap();
    assert!(bar.visible_bounds().is_some() && bar.bounds().y < 44.0 + 46.0);
    let name = ui.find("What should it do?").unwrap();
    assert!(name.visible_bounds().is_some());
    let foot = ui.find("It starts when you give it a task.").unwrap();
    assert!(
        foot.bounds().y > MIN_WINDOW.height || foot.visible_bounds().is_none(),
        "the form's foot is reached by scrolling the form"
    );
    drop(ui);
    // In the window's size at start the whole form and the first rows show.
    let mut ui = Simulator::with_size(panel_settings(), WINDOW, view::view(&app));
    for words in ["It starts when you give it a task.", "STATUS", "ops"] {
        let placed = ui.find(words).unwrap();
        assert!(placed.visible_bounds().is_some(), "{words} is out of sight");
    }
}

/// The frame: the search button asks for the palette, the rail goes to
/// each place and counts what needs a person on Tasks.
#[test]
fn the_top_bar_searches_and_the_rail_goes_places() {
    let mut app = connected();
    let sent = messages_of_click(&app, "Search bots, tasks, nodes, actions…");
    assert!(matches!(sent.as_slice(), [Message::PaletteOpen]), "{sent:?}");
    let before = app.screen.clone();
    assert_eq!(
        app.apply(Message::PaletteOpen),
        vec![Effect::Focus(Field::Palette)],
        "the palette opens with the keyboard in its field"
    );
    assert!(app.palette.is_open());
    assert_eq!(app.screen, before);
    app.apply(Message::PaletteClose);
    shows(&app, &["Ctrl K", "Home", "Tasks", "Bots", "Teams", "Nodes", "Settings"]);
    for (place, screen) in [
        ("Tasks", Screen::Tasks),
        ("Bots", Screen::Bots),
        ("Teams", Screen::Teams),
        ("Settings", Screen::Settings),
    ] {
        let sent = messages_of_click(&app, place);
        assert!(
            matches!(sent.as_slice(), [Message::Go(s)] if *s == screen),
            "{place}: {sent:?}"
        );
    }
    // The rail is 68 px wide under a bar 44 px high, and the count of
    // what needs a person sits on Tasks.
    let _turn = turn();
    let mut ui = Simulator::with_size(panel_settings(), WINDOW, view::view(&app));
    let tasks = ui.find("Tasks").unwrap().bounds();
    assert!(
        tasks.x >= 0.0 && tasks.x + tasks.width <= 68.0 && tasks.y > 44.0,
        "{tasks:?}"
    );
    let badge = ui.find("1").unwrap().bounds();
    assert!(badge.x < 68.0 && badge.y < tasks.y && badge.y > 44.0, "{badge:?}");
    let search = ui.find("Search bots, tasks, nodes, actions…").unwrap().bounds();
    assert!(search.y + search.height < 44.0 && search.width < 520.0, "{search:?}");
}

/// With nothing waiting, Home says so in one quiet line; a bot's ask
/// to listen, which is no task, still calls for a person.
#[test]
fn home_says_when_nothing_needs_a_person() {
    let fake = std::sync::Arc::new(crate::adapters::fake::FakeControlPlane::sample(
        "2026-10-25T10:00:00Z".parse().unwrap(),
    ));
    let mut app = super::view::tests::app_over(fake.clone());
    send(&mut app, Message::SettingsSave);
    let waiting = app.fleet.as_ref().unwrap().waiting()[0].id.clone();
    fake.finish_task(&waiting, TaskStatus::Completed);
    let tick = app.now + chrono::Duration::seconds(3);
    send(&mut app, Message::Tick(tick));
    assert_eq!(app.waiting_count(), 0);
    shows(
        &app,
        &["Nothing needs you", "No task waits for your OK. 1 task failed today."],
    );
    assert!(simulator_misses(&app, "Approve"));

    let (_, asked) = subscribed_fleet();
    let mut asked = asked;
    let waiting = asked.fleet.as_ref().unwrap().waiting()[0].id.clone();
    asked.fleet.as_mut().unwrap().tasks.retain(|t| t.id != waiting);
    assert!(asked.waiting_count() > 0);
    assert!(simulator_misses(&asked, "Nothing needs you"));
    shows(&asked, &["bot asks to listen to a topic.", "Review"]);
    let sent = messages_of_click(&asked, "Review");
    assert!(
        matches!(sent.as_slice(), [Message::Go(Screen::Bot(id))] if id.as_str() == "bot"),
        "{sent:?}"
    );
}

#[test]
fn the_nodes_screen_lists_the_nodes_and_mints_a_token() {
    let mut app = connected();
    send(&mut app, Message::Go(Screen::Nodes));
    shows(
        &app,
        &[
            "Nodes",
            "wrx80",
            "wrx80-node1",
            "0.14.0",
            "HOST",
            "SEEN",
            "AXLR",
            "AGENT",
        ],
    );
    // Revoke is an icon on each row; picked rows get it in words on the
    // bar.
    assert!(simulator_misses(&app, "Revoke"));
    send(&mut app, Message::PickShown(crate::ui::table::List::Nodes, true));
    shows(&app, &["2 nodes selected", "Revoke"]);
    send(&mut app, Message::ClearPicks(crate::ui::table::List::Nodes));
    // Each node's providers are on its line, a local model by its id.
    shows_wide(&app, &["PROVIDERS", "openrouter"]);
    if let Some(fleet) = app.fleet.as_mut() {
        for node in &mut fleet.nodes {
            node.capabilities.providers.clear();
        }
    }
    assert!(
        simulator_misses(&app, "openrouter"),
        "a node that named no provider shows none"
    );
    assert!(matches!(
        messages_of_click(&app, "New host token").as_slice(),
        [Message::MintOpen]
    ));
    app.apply(Message::MintOpen);
    app.apply(Message::MintNodeId("web-2".into()));
    shows(&app, &["Token for a new host", "Make token"]);
    send(&mut app, Message::MintSubmit);
    shows(
        &app,
        &["Copy it now. It is shown once.", "Copy token", "Copy command", "web-2"],
    );
    let sent = messages_of_click(&app, "Copy token");
    assert!(matches!(sent.as_slice(), [Message::Copy(t)] if t.starts_with("axlr1.")));
    app.apply(Message::AskRevoke(NodeId::new("wrx80").unwrap()));
    shows(&app, &["Revoke wrx80?", "Keep it"]);
    assert!(matches!(
        messages_of_click(&app, "Keep it").as_slice(),
        [Message::ConfirmNo]
    ));
}

#[test]
fn the_bots_screen_lists_switches_and_adds() {
    let mut app = connected();
    send(&mut app, Message::Go(Screen::Bots));
    shows(
        &app,
        &[
            "Bots",
            "bot-wrx80",
            "Ask first",
            "wrx80",
            "From file",
            "RUNS ON",
            "no team",
            "3 on",
        ],
    );
    // A cell cuts its words to its column in the smallest window; in
    // the window's size at start they are whole.
    shows_wide(
        &app,
        &["claude-haiku-5.5", "bot-act-with-approval", "MODEL", "STARTS BY"],
    );
    app.apply(Message::RegisterOpen);
    shows(
        &app,
        &[
            "New bot",
            "Name",
            "What should it do?",
            "Where does it run?",
            "Ask me before acting: it works on its own and asks you first before anything that changes the world.",
            "Add bot",
            "Advanced",
        ],
    );
    assert!(simulator_misses(&app, "Tools"), "Advanced is closed by default");
    assert!(matches!(
        messages_of_click(&app, "Advanced").as_slice(),
        [Message::RegisterAdvanced(true)]
    ));
    app.apply(Message::RegisterAdvanced(true));
    shows(
        &app,
        &[
            "How much may it do alone?",
            "Tools",
            "Model",
            "Hide advanced",
            "It starts when you give it a task.",
        ],
    );
    assert!(matches!(
        messages_of_click(&app, "Act on its own within its rules").as_slice(),
        [Message::RegisterLevel(crate::domain::AutonomyLevel::ActWithinPolicy)]
    ));
    assert!(
        messages_of_click(&app, "Ask me before acting").is_empty(),
        "the picked level"
    );
    app.apply(Message::RegisterLevel(crate::domain::AutonomyLevel::Observe));
    shows(
        &app,
        &["Only watch and report: it works on its own and only watches and reports."],
    );
    app.apply(Message::RegisterLevel(crate::domain::AutonomyLevel::ActWithinPolicy));
    shows(
        &app,
        &["Act on its own within its rules: it works on its own and acts within its rules without asking."],
    );
    app.apply(Message::RegisterAdvanced(false));
    assert!(simulator_misses(&app, "Tools"));
    // Where does it run? warns when the node named its providers and none
    // serves the model typed; Add bot stays.
    let wrx80 = crate::ui::NodeChoice::One(NodeId::new("wrx80").unwrap());
    app.apply(Message::RegisterNode(wrx80.clone()));
    assert!(
        simulator_misses(&app, "has no"),
        "wrx80 has OpenRouter for the default model"
    );
    app.apply(Message::RegisterModel("codex/gpt-6.1-sol".into()));
    shows(
        &app,
        &[
            "wrx80 has no codex for codex/gpt-6.1-sol: it has openrouter.",
            "Add bot",
        ],
    );
    app.apply(Message::RegisterNode(crate::ui::NodeChoice::One(
        NodeId::new("gx10-ff95").unwrap(),
    )));
    assert!(simulator_misses(&app, "has no"), "gx10-ff95 has the Codex proxy");
    app.apply(Message::RegisterNode(crate::ui::NodeChoice::Any));
    assert!(simulator_misses(&app, "has no"), "any node: nothing to compare with");
    app.apply(Message::RegisterNode(wrx80));
    app.apply(Message::RegisterModel(String::new()));
    assert!(
        simulator_misses(&app, "has no"),
        "no model typed: nothing to compare with"
    );
    send(
        &mut app,
        Message::Go(Screen::Bot(BotId::new("bot-act-with-approval").unwrap())),
    );
    shows(
        &app,
        &[
            "Pause",
            "Stop",
            "Edit",
            "Delete",
            "It asks before acting",
            "wants local_write · hello",
            "Approve",
            "RIGHT NOW",
            "Needs OK",
            "It waits for your OK before it goes on.",
            "LATEST WORK",
            "STARTS BY",
            "Urgent order",
        ],
    );
    assert!(matches!(
        messages_of_click(&app, "Open task").as_slice(),
        [Message::Go(Screen::Task(_))]
    ));
    assert!(
        simulator_misses(&app, "Only watch and report"),
        "its rules have their own tab"
    );
    assert!(matches!(
        messages_of_click(&app, "Approve").as_slice(),
        [Message::Approve(_)]
    ));
    assert!(matches!(
        messages_of_click(&app, "Stop").as_slice(),
        [Message::AskStopBot(_)]
    ));
    send(&mut app, Message::Tab(inspector::Tab::Bot(inspector::BotTab::Mission)));
    shows(&app, &["WHAT IT DOES", "Copy mission"]);
    let sent = messages_of_click(&app, "Copy mission");
    assert!(matches!(
        sent.as_slice(),
        [Message::Copy(mission)] if mission == "Do what each task asks, within the policy."
    ));
    send(&mut app, Message::Tab(inspector::Tab::Bot(inspector::BotTab::Listens)));
    shows(&app, &["EVENTS", "EMITS"]);
    assert!(!app.feed().unwrap().events.is_empty(), "the sample bot has events");
    assert!(matches!(
        messages_of_click(&app, "Copy events").as_slice(),
        [Message::Copy(events)] if !events.is_empty()
    ));
    send(&mut app, Message::Tab(inspector::Tab::Bot(inspector::BotTab::Work)));
    let sent = messages_of_click(&app, "New task");
    assert!(matches!(sent.as_slice(), [Message::SubmitOpen(Some(_))]));

    send(&mut app, Message::Tab(inspector::Tab::Bot(inspector::BotTab::Rules)));
    shows(
        &app,
        &[
            "HOW MUCH MAY IT DO ALONE",
            "Ask me before acting",
            "Act on its own within its rules",
            "Only watch and report",
            "Starts by",
            "Budgets",
        ],
    );
    assert!(
        simulator_misses(&app, "Propose only"),
        "Propose shows only on a bot that has it"
    );
    assert!(matches!(
        messages_of_click(&app, "Only watch and report").as_slice(),
        [Message::SetLevel(_, crate::domain::AutonomyLevel::Observe)]
    ));
    assert!(
        messages_of_click(&app, "Ask me before acting").is_empty(),
        "the bot's own level is not a button"
    );
    app.switching = true;
    assert!(
        messages_of_click(&app, "Only watch and report").is_empty(),
        "no change while one is on its way"
    );
    app.switching = false;
    send(
        &mut app,
        Message::SetLevel(
            BotId::new("bot-act-with-approval").unwrap(),
            crate::domain::AutonomyLevel::Propose,
        ),
    );
    shows(&app, &["Propose only"]);
    send(&mut app, Message::Go(Screen::Bots));
    assert_eq!(
        app.tabs.bot,
        inspector::BotTab::Rules,
        "the next bot opens on the same tab"
    );
}

#[test]
fn the_tasks_screen_filters_and_sends() {
    let mut app = connected();
    send(&mut app, Message::Go(Screen::Tasks));
    shows(
        &app,
        &[
            "Tasks",
            "5 tasks",
            "All",
            "Needs OK",
            "Failed",
            "Expired",
            "Write hello into hello.txt",
            "STARTED BY",
            "manual",
            "AT",
            "AGE",
        ],
    );
    // A chip only for a status that has tasks.
    assert!(simulator_misses(&app, "Stopped"));
    assert!(simulator_misses(&app, "Queued"));
    // A row's acts are icons with tips; the words are on the bar once
    // rows are picked.
    send(&mut app, Message::PickShown(crate::ui::table::List::Tasks, true));
    shows(
        &app,
        &["5 tasks selected", "Approve 1 of 5", "Deny 1 of 5", "Stop 2 of 5"],
    );
    assert!(matches!(
        messages_of_click(&app, "Approve 1 of 5").as_slice(),
        [Message::AskBulk(_, crate::application::BulkAction::Approve)]
    ));
    send(&mut app, Message::ClearPicks(crate::ui::table::List::Tasks));
    // The chip comes before the rows, whose status says the same word.
    let sent = messages_of_click(&app, "Failed");
    assert!(matches!(
        sent.as_slice(),
        [Message::FilterStatus(Some(TaskStatus::Failed))]
    ));
    app.apply(Message::FilterStatus(Some(TaskStatus::Completed)));
    shows(&app, &["List the files in your workspace", "1 of 5"]);
    // The chip that is on stays when its last task leaves it.
    app.apply(Message::FilterStatus(Some(TaskStatus::Cancelled)));
    shows(&app, &["Stopped", "No task here", "0 of 5"]);
    app.apply(Message::FilterStatus(Some(TaskStatus::Completed)));
    app.apply(Message::SubmitOpen(Some(BotId::new("bot-wrx80").unwrap())));
    app.apply(Message::SubmitObjective("Count the files".into()));
    shows(&app, &["New task", "Send"]);
    reads(&app, ReaderKey::SubmitKey, "key test-1: sending twice makes one task");
    assert!(matches!(
        messages_of_click(&app, "Send").as_slice(),
        [Message::SubmitSend]
    ));
}

/// A placed bot's Pending task whose node is away says why, in the
/// Tasks table under its objective and on its page.
#[test]
fn a_pending_task_whose_node_is_away_says_what_it_waits_for() {
    let now = "2026-10-25T10:00:00Z".parse().unwrap();
    let control_plane = std::sync::Arc::new(crate::adapters::fake::FakeControlPlane::sample(now));
    control_plane.set_node_status("wrx80", crate::domain::NodeStatus::Offline);
    let submission = crate::domain::TaskSubmission {
        bot_id: BotId::new("bot-wrx80").unwrap(),
        objective: crate::domain::Objective::new("Count the files").unwrap(),
        key: crate::domain::IdempotencyKey::new("k-away").unwrap(),
        urgent: false,
    };
    let task = futures::executor::block_on(crate::application::ControlPlane::submit_task(
        control_plane.as_ref(),
        submission,
    ))
    .unwrap()
    .task;
    let mut app = super::view::tests::app_over(control_plane);
    send(&mut app, Message::SettingsSave);
    send(&mut app, Message::Go(Screen::Tasks));
    shows_wide(&app, &["Count the files", "Waiting for node wrx80, which is Offline"]);
    send(&mut app, Message::Go(Screen::Task(task.id)));
    shows(&app, &["Waiting for node wrx80, which is Offline"]);
}

#[test]
fn a_waiting_task_shows_the_call_and_its_answers() {
    let mut app = connected();
    let id = app.fleet.as_ref().unwrap().waiting()[0].id.clone();
    send(&mut app, Message::Go(Screen::Task(id)));
    shows(
        &app,
        &[
            "Needs OK",
            "local_write",
            "hello.txt",
            "Approve",
            "Deny",
            "Runs",
            "Timeline",
        ],
    );
    send(&mut app, Message::Tab(inspector::Tab::Task(inspector::TaskTab::Runs)));
    shows(&app, &["It asked you", "gx10-ff95", "trace carried"]);
    assert!(matches!(
        messages_of_click(&app, "Approve").as_slice(),
        [Message::Approve(_)]
    ));
    assert!(matches!(
        messages_of_click(&app, "Deny").as_slice(),
        [Message::AskDeny(_)]
    ));
    assert!(matches!(
        messages_of_click(&app, "Copy call").as_slice(),
        [Message::Copy(call)] if call.starts_with("local_write\n") && call.contains("path: hello.txt")
    ));
    app.deciding = app.task.as_ref().map(|t| t.task.id.clone());
    shows(&app, &["Sending…"]);
}

/// The call a bot stops on before it publishes a post: a program on the
/// node with the words and two pictures as arguments.
pub(crate) fn publishing_call() -> crate::domain::PendingDecision {
    crate::domain::PendingDecision {
        tool: "local_exec".into(),
        call_id: "call-9".into(),
        session_id: "sess-9".into(),
        arguments: Some(serde_json::json!({
            "program": "/home/me/x-ai-news/bin/x-post",
            "args": [
                "--topic", "hardware",
                "--text", "A r/LocalLLaMA user reports DeepSeek-V4.1-Flash on 4x RTX 3090 and 256GB DDR4 at 524k context: prefill 400-600 tok/s, decode 30-40 tok/s.\n\nA home multi-GPU speed reference. No logs or config linked.\nhttps://www.reddit.com/r/LocalLLaMA/comments/1xnkr5q/",
                "--card", "{\"template\":\"headline\",\"headline\":\"DeepSeek V4.1 Flash at 524k context on 4x RTX 3090\",\"kicker\":\"r/LocalLLaMA user report\"}",
                "--card", "{\"template\":\"flow\",\"title\":\"How Maya runs a 748B MoE on a home rig\",\"steps\":[]}"
            ],
            "timeout_ms": 120000
        })),
    }
}

#[test]
fn a_waiting_call_that_publishes_shows_the_post_as_it_would_read() {
    use crate::adapters::fake::FakeControlPlane;
    let fake = std::sync::Arc::new(FakeControlPlane::sample("2026-10-25T10:00:00Z".parse().unwrap()));
    let mut app = super::view::tests::app_over(fake.clone());
    send(&mut app, Message::SettingsSave);
    let id = fake.add_waiting_call(
        "bot-act-with-approval",
        "Propose today's post about AI news",
        publishing_call(),
    );
    let draft = crate::domain::PostDraft::from_call(&publishing_call()).unwrap();
    send(&mut app, Message::Go(Screen::Task(id)));
    shows(
        &app,
        &[
            "Needs OK",
            "local_exec",
            "As it would read",
            "bot-act-with-approval",
            "· x-post · not posted yet",
            "headline · DeepSeek V4.1 Flash at 524k context on 4x RTX 3090",
            "flow · How Maya runs a 748B MoE on a home rig",
            &format!("{} characters · 1 link · 2 attachments", draft.characters()),
            "Copy text",
            "Approve",
            "Deny",
        ],
    );
    // The call that writes a file is not a post.
    let id = app.fleet.as_ref().unwrap().waiting()[0].id.clone();
    send(&mut app, Message::Go(Screen::Task(id)));
    assert!(simulator_misses(&app, "As it would read"));
}

#[test]
fn a_finished_task_shows_its_answer_and_failure() {
    let mut app = connected();
    let done = app
        .fleet
        .as_ref()
        .unwrap()
        .tasks
        .iter()
        .find(|t| t.status == TaskStatus::Completed)
        .unwrap()
        .id
        .clone();
    // The bot's own rules about the tool show under the step.
    let bot = app
        .fleet
        .as_ref()
        .unwrap()
        .tasks
        .iter()
        .find(|t| t.id == done)
        .unwrap()
        .bot_id
        .clone();
    if let Some(b) = app.fleet.as_mut().unwrap().bots.iter_mut().find(|b| b.id == bot) {
        b.tool_rules.allow.push(crate::domain::ToolRule {
            tool: "local_list".into(),
            arguments: [("path".to_string(), "*".to_string())].into(),
        });
    }
    send(&mut app, Message::Go(Screen::Task(done)));
    // Its runs, with each call and why it ran, on Runs.
    send(&mut app, Message::Tab(inspector::Tab::Task(inspector::TaskTab::Runs)));
    shows_wide(
        &app,
        &[
            "Its rules: allows local_list(path=*)",
            "Steps",
            "Allowed by its rules",
            "done",
            "40 ms",
            "wrx80",
        ],
    );
    send(
        &mut app,
        Message::Tab(inspector::Tab::Task(inspector::TaskTab::Timeline)),
    );
    assert!(matches!(
        messages_of_click(&app, "Copy timeline").as_slice(),
        [Message::Copy(timeline)] if timeline.contains("Done")
    ));
    // Its answer, what it was asked, to copy or to select, on Answer.
    send(&mut app, Message::Tab(inspector::Tab::Task(inspector::TaskTab::Answer)));
    shows(&app, &["ANSWER", "Copy task", "Copy answer", "Select text"]);
    assert!(matches!(
        messages_of_click(&app, "Copy answer").as_slice(),
        [Message::Copy(answer)] if answer == "There are 3 files."
    ));
    let objective = app.task.as_ref().unwrap().task.objective.clone();
    assert!(matches!(
        messages_of_click(&app, "Copy task").as_slice(),
        [Message::Copy(copied)] if *copied == objective
    ));
    assert!(matches!(
        messages_of_click(&app, "Select text").as_slice(),
        [Message::AnswerSelecting(true)]
    ));
    app.apply(Message::AnswerSelecting(true));
    shows(&app, &["Formatted", "Copy answer"]);
    assert!(matches!(
        messages_of_click(&app, "Formatted").as_slice(),
        [Message::AnswerSelecting(false)]
    ));
    // A long objective shows whole under what it was asked.
    app.task.as_mut().unwrap().task.objective = "Count the files\nthen list them".into();
    shows(&app, &["Count the files\nthen list them"]);
    assert_eq!(
        app.answer.as_ref().map(|a| a.source.as_str()),
        Some("There are 3 files.")
    );
    let failed = app
        .fleet
        .as_ref()
        .unwrap()
        .tasks
        .iter()
        .find(|t| t.status == TaskStatus::Failed)
        .unwrap()
        .id
        .clone();
    send(&mut app, Message::Go(Screen::Task(failed)));
    shows(&app, &["Why it failed", "the file does not exist"]);
    assert!(matches!(
        messages_of_click(&app, "Copy reason").as_slice(),
        [Message::Copy(reason)] if reason.contains("the file does not exist")
    ));
    let expired = app
        .fleet
        .as_ref()
        .unwrap()
        .tasks
        .iter()
        .find(|t| t.status == TaskStatus::Expired)
        .unwrap()
        .id
        .clone();
    send(&mut app, Message::Go(Screen::Task(expired)));
    shows(&app, &["Expired", "Why it expired"]);
}

#[test]
fn errors_and_empty_places_say_so() {
    let mut app = connected();
    app.fleet_error = Some(crate::application::PanelError::Unauthorized("bad token".into()));
    shows(&app, &["Wrong token"]);
    app.fleet = None;
    shows(&app, &["bad token", "Open settings"]);
    app.fleet_error = None;
    shows(&app, &["Loading the fleet…"]);
    app.apply(Message::Copy("x".into()));
    shows(&app, &["Copied"]);
    send(
        &mut app,
        Message::Go(Screen::Task(crate::domain::TaskId::new("task-none").unwrap())),
    );
    shows(&app, &["Loading the task…", "Not found"]);
    send(&mut app, Message::Go(Screen::Bot(BotId::new("ghost").unwrap())));
    shows(&app, &["Loading the bot…"]);
}

#[test]
fn now_says_what_each_bot_is_doing() {
    let mut app = connected();
    send(&mut app, Message::Go(Screen::Bots));
    // In the list a bot that is doing something says that in the place
    // of its status; one that is not says it is on.
    shows(&app, &["Working", "Needs OK", "On"]);
    assert!(simulator_misses(&app, "Enabled"));
    send(&mut app, Message::Go(Screen::Bot(BotId::new("bot").unwrap())));
    shows(&app, &["Working", "Say the time", "Open task"]);
    send(&mut app, Message::Go(Screen::Bot(BotId::new("bot-wrx80").unwrap())));
    shows(&app, &["Idle", "It waits for a task."]);
    send(&mut app, Message::Pause(BotId::new("bot-wrx80").unwrap()));
    shows(&app, &["Paused", "It takes no new task until you turn it on."]);
}

#[test]
fn the_chat_sends_and_shows_the_turns() {
    let mut app = connected();
    let id = BotId::new("bot-wrx80").unwrap();
    send(&mut app, Message::Go(Screen::Bot(id.clone())));
    shows(
        &app,
        &[
            "TALK TO IT",
            "Ask it something or give it an order. Each message runs as a task.",
            "Send",
        ],
    );
    assert!(messages_of_click(&app, "Send").is_empty(), "nothing to send yet");
    app.apply(Message::ChatInput("How many files are there?".into()));
    assert!(matches!(
        messages_of_click(&app, "Send").as_slice(),
        [Message::ChatSend]
    ));
    send(&mut app, Message::ChatSend);
    shows(
        &app,
        &["How many files are there?", "Queued: it waits for a free node."],
    );
    assert!(matches!(
        messages_of_click(&app, "Got it. It starts as soon as a node is free.").as_slice(),
        [Message::Go(Screen::Task(_))]
    ));
    app.chat_busy = true;
    app.apply(Message::ChatInput("more".into()));
    shows(&app, &["Sending…"]);
    app.chat_busy = false;
    send(&mut app, Message::Pause(id));
    shows(&app, &["Turn it on to talk to it."]);
}

#[test]
fn the_teams_screen_shows_who_is_in_each_and_what_happens() {
    let fake = std::sync::Arc::new(crate::adapters::fake::FakeControlPlane::sample(
        "2026-10-25T10:00:00Z".parse().unwrap(),
    ));
    let mut app = super::view::tests::app_over(fake.clone());
    send(&mut app, Message::SettingsSave);
    send(&mut app, Message::Go(Screen::Teams));
    shows(&app, &["Teams", "No team yet"]);
    fake.set_groups("bot", &["ops"]);
    fake.set_groups("bot-wrx80", &["ops"]);
    fake.set_groups("bot-act-with-approval", &["lab"]);
    let tick = app.now + chrono::Duration::seconds(3);
    send(&mut app, Message::Tick(tick));
    fake.publish(
        "bot.event.emitted",
        "bot",
        serde_json::json!({"name": "stop.halt", "severity": "warning", "message": "a deploy is starting", "to": ["bot-wrx80"]}),
    );
    send(&mut app, Message::Tick(tick + chrono::Duration::seconds(3)));
    // The first team opens in the inspector as the list fills.
    assert_eq!(app.screen, Screen::Team("lab".into()));
    shows(&app, &["ops", "BOTS", "ON", "lab", "Team lab"]);
    // A team of one bot has a mark beside its name whose tip asks
    // whether the name is a typo; the simulator draws no tip, so the
    // words are not looked for here.
    // A team's name opens it in the inspector.
    let sent = messages_of_click(&app, "ops");
    assert!(matches!(sent.as_slice(), [Message::Go(Screen::Team(name))] if name == "ops"));
    send(&mut app, Message::Go(Screen::Team("ops".into())));
    shows(
        &app,
        &[
            "Team ops",
            "Who is in it",
            "What happens",
            "Warning to the team",
            "bot → bot-wrx80: a deploy is starting",
        ],
    );
    send(
        &mut app,
        Message::Tab(inspector::Tab::Team(inspector::TeamTab::Members)),
    );
    shows(&app, &["bot", "bot-wrx80", "Urgent order"]);
    assert!(matches!(
        messages_of_click(&app, "Urgent order").as_slice(),
        [Message::OrderOpen(_)]
    ));
    send(&mut app, Message::Go(Screen::Team("web".into())));
    shows(&app, &["No bot is in this team now"]);
}

#[test]
fn a_new_bot_shows_the_id_and_the_teams_its_words_become() {
    let mut app = connected();
    send(&mut app, Message::Go(Screen::Bots));
    app.apply(Message::RegisterOpen);
    app.apply(Message::RegisterName("NanoBot secured on ESP32".into()));
    app.apply(Message::RegisterTeam("NANOTEAM".into()));
    shows(
        &app,
        &[
            "Its id will be nanobot-secured-on-esp32",
            "It will be saved as nanoteam",
        ],
    );
    app.apply(Message::RegisterMission("Watch.".into()));
    assert!(matches!(
        messages_of_click(&app, "Add bot").as_slice(),
        [Message::RegisterSubmit]
    ));
    app.apply(Message::RegisterName("¡¡!!".into()));
    shows(&app, &["bot name needs a letter or a digit to make the bot's id from"]);
}

#[test]
fn a_name_whose_id_is_taken_cannot_be_added() {
    let mut app = connected();
    send(&mut app, Message::Go(Screen::Bots));
    app.apply(Message::RegisterOpen);
    app.apply(Message::RegisterName("Bot WRX80".into()));
    app.apply(Message::RegisterMission("Watch.".into()));
    shows(&app, &["The id bot-wrx80 is taken: choose another name"]);
    assert!(messages_of_click(&app, "Add bot").is_empty());
}

#[test]
fn a_bot_page_shows_its_team_and_an_urgent_order() {
    let mut app = connected();
    send(&mut app, Message::Go(Screen::Bots));
    app.apply(Message::RegisterOpen);
    shows(
        &app,
        &["Which team is it in?", "a name its teammates share; empty: on its own"],
    );
    app.apply(Message::RegisterName("worker-1".into()));
    app.apply(Message::RegisterMission("Watch service-1.".into()));
    app.apply(Message::RegisterTools("local_read, rollback".into()));
    app.apply(Message::RegisterTeam("ops".into()));
    send(&mut app, Message::RegisterSubmit);
    let id = BotId::new("worker-1").unwrap();
    send(&mut app, Message::Go(Screen::Bot(id.clone())));
    // Its team is on its Mission tab; the urgent order at the foot of Now.
    send(&mut app, Message::Tab(inspector::Tab::Bot(inspector::BotTab::Mission)));
    shows(&app, &["TEAM", "ops", "Change"]);
    let sent = messages_of_click(&app, "Change");
    assert!(matches!(sent.as_slice(), [Message::TeamEdit(_, words)] if words == "ops"));
    app.apply(Message::TeamEdit(id.clone(), "ops".into()));
    shows(&app, &["Save", "Cancel"]);
    app.apply(Message::TeamCancel);
    send(&mut app, Message::Tab(inspector::Tab::Bot(inspector::BotTab::Now)));
    shows(&app, &["Urgent order"]);
    assert!(
        messages_of_click(&app, "Urgent order").is_empty(),
        "a bot not switched on takes no order"
    );
    send(&mut app, Message::Enable(id.clone()));
    let sent = messages_of_click(&app, "Urgent order");
    assert!(matches!(sent.as_slice(), [Message::OrderOpen(to)] if *to == id));

    // The order asks for its words, and goes only with some.
    send(&mut app, Message::OrderOpen(id.clone()));
    shows(
        &app,
        &[
            "Order",
            "It goes first: the bot stops what it is doing, drops its queue and does this.",
            "Send the order",
            "Cancel",
        ],
    );
    assert!(messages_of_click(&app, "Send the order").is_empty());
    assert!(messages_of_click(&app, "Urgent order").is_empty(), "its form is open");
    app.apply(Message::OrderText("Roll back service-1 now".into()));
    assert!(matches!(
        messages_of_click(&app, "Send the order").as_slice(),
        [Message::OrderSend]
    ));
    assert!(matches!(
        messages_of_click(&app, "Cancel").as_slice(),
        [Message::OrderClose]
    ));

    // Any bot takes one, in a team or not.
    send(&mut app, Message::Go(Screen::Bot(BotId::new("bot").unwrap())));
    shows(&app, &["Urgent order"]);
    assert!(simulator_misses(&app, "Send the order"));
    assert!(matches!(
        messages_of_click(&app, "Urgent order").as_slice(),
        [Message::OrderOpen(to)] if to.as_str() == "bot"
    ));
}

/// None of `words` is on the screen.
fn hides(app: &App, words: &[&str]) {
    let _turn = turn();
    let mut ui = simulator(view::view(app));
    for word in words {
        assert!(ui.find(*word).is_err(), "{word} is on {:?}", app.screen);
    }
}

#[test]
fn a_task_says_it_was_urgent_and_why_it_stopped() {
    let mut app = connected();
    app.apply(Message::SubmitOpen(Some(BotId::new("bot-wrx80").unwrap())));
    shows(&app, &["Urgent"]);
    app.apply(Message::SubmitUrgent(true));
    shows(
        &app,
        &["urgent: it goes first, stops what the bot is doing and drops its queue"],
    );
    app.apply(Message::SubmitObjective("Come back".into()));
    send(&mut app, Message::SubmitSend);
    shows(&app, &["urgent", "STARTED"]);
    hides(&app, &["Told by", "nobody"]);
    send(&mut app, Message::Tab(inspector::Tab::Task(inspector::TaskTab::Runs)));
    shows(&app, &["Not run yet: waiting for a node"]);
    // Cancelled before a node took it: it never ran and waits for nothing.
    let Screen::Task(queued) = app.screen.clone() else {
        panic!("not on the task: {:?}", app.screen)
    };
    send(&mut app, Message::AskCancel(queued));
    send(&mut app, Message::ConfirmYes);
    shows(&app, &["Never ran: it ended before a node took it"]);
    hides(&app, &["Not run yet: waiting for a node"]);
}

#[test]
fn the_chat_writes_to_a_running_task_and_shows_its_answer() {
    let fake = std::sync::Arc::new(crate::adapters::fake::FakeControlPlane::sample(
        "2026-10-25T10:00:00Z".parse().unwrap(),
    ));
    let mut app = super::view::tests::app_over(fake.clone());
    send(&mut app, Message::SettingsSave);
    let bot = BotId::new("bot").unwrap();
    send(&mut app, Message::Go(Screen::Bot(bot.clone())));
    shows(
        &app,
        &["It is working now: your message goes to its run, and it answers between its steps."],
    );
    let running = app.running_task(&app.bot.as_ref().unwrap().bot.clone()).unwrap();
    app.apply(Message::ChatInput("check row F".into()));
    let effects = app.apply(Message::ChatSend);
    assert!(matches!(effects.as_slice(), [Effect::SendChat { running: Some(id), .. }] if *id == running));
    super::test_runtime::drive(&mut app, effects);
    assert!(app.chat_draft.is_empty());
    fake.say(&running, "OK, row F next.");
    let tick = app.now + chrono::Duration::seconds(5);
    send(&mut app, Message::Tick(tick));
    shows(&app, &["check row F", "OK, row F next."]);
    assert!(matches!(
        messages_of_click(&app, "OK, row F next.").as_slice(),
        [Message::Go(Screen::Task(id))] if *id == running
    ));
    send(&mut app, Message::Tab(inspector::Tab::Bot(inspector::BotTab::Listens)));
    shows(&app, &["You wrote", "It answered"]);
}

#[test]
fn a_bot_page_shows_the_task_of_each_event_whole() {
    let fake = std::sync::Arc::new(crate::adapters::fake::FakeControlPlane::sample(
        "2026-10-25T10:00:00Z".parse().unwrap(),
    ));
    let mut app = super::view::tests::app_over(fake.clone());
    send(&mut app, Message::SettingsSave);
    send(&mut app, Message::Go(Screen::Bot(BotId::new("bot").unwrap())));
    let running = app.running_task(&app.bot.as_ref().unwrap().bot.clone()).unwrap();
    app.apply(Message::ChatInput("Say the date too, please.".into()));
    let effects = app.apply(Message::ChatSend);
    super::test_runtime::drive(&mut app, effects);
    fake.say(&running, "OK, I will say the date too: 25 October 2026.");
    let tick = app.now + chrono::Duration::seconds(5);
    send(&mut app, Message::Tick(tick));
    // A task id as long as the control plane's, with a long reason.
    let failed: crate::domain::Event = serde_json::from_str(
        r#"{"eventId":"evt-long","type":"bot.task.failed","botId":"bot","taskId":"task-68edb0ffeae524e9",
            "createdAt":"2026-10-25T10:00:06Z","payload":{"reason":"workspace/reports/weekly-summary.txt does not exist on this node"}}"#,
    )
    .unwrap();
    app.bot.as_mut().unwrap().feed.events.push(failed);
    send(&mut app, Message::Tab(inspector::Tab::Bot(inspector::BotTab::Listens)));
    let tokens = Tokens::of(app.mode);
    for width in [MIN_WINDOW.width, WINDOW.width] {
        for id in [running.as_str(), "task-68ed…"] {
            shows_whole(&app, width, id, |_| widgets::muted(id.to_string(), tokens));
        }
    }
}

#[test]
fn a_bot_page_keeps_the_button_that_changes_its_teams_in_sight() {
    let mut app = connected();
    send(&mut app, Message::Go(Screen::Bot(BotId::new("bot").unwrap())));
    send(&mut app, Message::Tab(inspector::Tab::Bot(inspector::BotTab::Mission)));
    let tokens = Tokens::of(app.mode);
    let none = "In no team: it knows no other bot";
    for teams in [&[][..], &["ops", "night-shift", "weekly-reports", "storage"]] {
        app.bot.as_mut().unwrap().bot.groups = teams.iter().map(|t| t.to_string()).collect();
        for width in [MIN_WINDOW.width, WINDOW.width] {
            shows_whole(&app, width, "Change", |_| {
                widgets::semibold("Change").size(widgets::BODY)
            });
            if teams.is_empty() {
                shows_whole(&app, width, none, |_| widgets::muted(none, tokens));
            }
            for team in teams {
                shows_whole(&app, width, team, |_| text(*team).size(widgets::SMALL));
            }
        }
    }
}

fn subscribed_fleet() -> (std::sync::Arc<crate::adapters::fake::FakeControlPlane>, App) {
    let fake = std::sync::Arc::new(crate::adapters::fake::FakeControlPlane::sample(
        "2026-10-25T10:00:00Z".parse().unwrap(),
    ));
    fake.set_groups("bot", &["ops"]);
    fake.set_groups("bot-wrx80", &["ops"]);
    let ask: crate::domain::Subscription = serde_json::from_str(
        r#"{"topic":"error.*","status":"pending","askedBy":"bot","why":"para reintentar si recepcion falla","askedAt":"2026-10-25T09:00:00Z"}"#,
    )
    .unwrap();
    let active =
        crate::domain::Subscription::new("pedido.*", Some(crate::domain::DeliveryMode::Task), "bot-wrx80").unwrap();
    fake.set_subscriptions("bot", vec![active, ask]);
    let mut app = super::view::tests::app_over(fake.clone());
    send(&mut app, Message::SettingsSave);
    (fake, app)
}

#[test]
fn a_bot_page_shows_what_it_listens_to_and_its_asks() {
    let (fake, mut app) = subscribed_fleet();
    let waiting_before = app.waiting_count();
    assert_eq!(app.pending_asks(), 1);
    send(&mut app, Message::Go(Screen::Bot(BotId::new("bot").unwrap())));
    // Its asks wait on Now; what it listens to has its own tab.
    shows(
        &app,
        &[
            "bot asks to listen to error.*",
            "\u{201c}para reintentar si recepcion falla\u{201d}",
            "Approve",
            "Deny",
        ],
    );
    send(&mut app, Message::Tab(inspector::Tab::Bot(inspector::BotTab::Listens)));
    shows(
        &app,
        &[
            "LISTENS TO",
            "pedido.*",
            "from bot-wrx80",
            "Listen to a topic",
            "Remove",
        ],
    );
    let sent = messages_of_click(&app, "Listen to a topic");
    assert!(matches!(sent.as_slice(), [Message::ListenOpen(id)] if id.as_str() == "bot"));
    app.apply(Message::ListenOpen(BotId::new("bot").unwrap()));
    app.apply(Message::ListenTopic("warn.stock".into()));
    shows(&app, &["Topic", "Mode", "by kind: message", "From", "Listen"]);
    // Approve: one PATCH with the ask active; the card leaves; the count drops.
    let patches = |fake: &crate::adapters::fake::FakeControlPlane| {
        fake.calls().iter().filter(|c| c.starts_with("patch_bot")).count()
    };
    let before = patches(&fake);
    send(
        &mut app,
        Message::ApproveAsk(BotId::new("bot").unwrap(), "error.*".into()),
    );
    assert_eq!(patches(&fake), before + 1);
    let bot = &app.bot.as_ref().unwrap().bot;
    assert!(bot.subscriptions.iter().all(|s| !s.is_pending()));
    assert!(simulator_misses(&app, "bot asks to listen to error.*"));
    assert_eq!(app.waiting_count(), waiting_before - 1);
    // Listen to one more, then remove it.
    app.apply(Message::ListenOpen(BotId::new("bot").unwrap()));
    app.apply(Message::ListenTopic("warn.stock".into()));
    app.apply(Message::ListenMode(ModeChoice::Mode(crate::domain::DeliveryMode::Note)));
    send(&mut app, Message::ListenSave);
    shows(&app, &["warn.stock", "note"]);
    send(
        &mut app,
        Message::Unsubscribe(BotId::new("bot").unwrap(), "warn.stock".into()),
    );
    assert!(simulator_misses(&app, "warn.stock"));
    send(&mut app, Message::Go(Screen::Bot(BotId::new("bot-wrx80").unwrap())));
    shows(&app, &["Hears what is addressed to it and the team's stop and danger"]);
}

#[test]
fn a_bad_topic_is_refused_before_the_call() {
    let (fake, mut app) = subscribed_fleet();
    send(&mut app, Message::Go(Screen::Bot(BotId::new("bot").unwrap())));
    app.apply(Message::ListenOpen(BotId::new("bot").unwrap()));
    app.apply(Message::ListenTopic("Bad Topic".into()));
    let before = fake.calls().len();
    assert!(app.apply(Message::ListenSave).is_empty());
    assert_eq!(fake.calls().len(), before);
    assert!(app.banner.is_some());
    app.apply(Message::ListenClose);
    assert!(!app.listen.is_open_for(&BotId::new("bot").unwrap()));
}

#[test]
fn a_team_shows_its_topics_and_the_operator_publishes() {
    let (fake, mut app) = subscribed_fleet();
    fake.publish(
        "bot.event.emitted",
        "bot-wrx80",
        serde_json::json!({"name": "pedido.nuevo", "from": "bot-wrx80", "message": "table 4"}),
    );
    fake.publish(
        "bot.event.emitted",
        "bot-wrx80",
        serde_json::json!({"name": "info.turno", "from": "bot-wrx80", "message": "shift"}),
    );
    send(&mut app, Message::Go(Screen::Team("ops".into())));
    send(
        &mut app,
        Message::Tab(inspector::Tab::Team(inspector::TeamTab::Members)),
    );
    shows(
        &app,
        &[
            "TOPICS",
            "pedido.nuevo",
            "publishes bot-wrx80",
            "listens bot (task)",
            "info.turno",
            "nobody listens",
            "Publish an event",
        ],
    );
    let sent = messages_of_click(&app, "Publish an event");
    assert!(matches!(sent.as_slice(), [Message::PublishOpen(team)] if team == "ops"));
    send(
        &mut app,
        Message::Tab(inspector::Tab::Team(inspector::TeamTab::Happens)),
    );
    send(&mut app, Message::PublishOpen("ops".into()));
    app.apply(Message::PublishName("stop.all".into()));
    app.apply(Message::PublishMessage("Everyone stop".into()));
    shows(&app, &["Name", "To", "Message", "Publish"]);
    send(&mut app, Message::PublishSend);
    assert!(fake.calls().iter().any(|c| c == "publish_event ops stop.all"));
    assert!(app.publish.team.is_none());
    let tick = app.now + chrono::Duration::seconds(3);
    send(&mut app, Message::Tick(tick));
    shows(&app, &["You published", "you → the team: stop.all · Everyone stop"]);
}

#[test]
fn a_shortcut_publishes_its_event_in_one_press() {
    let (fake, mut app) = subscribed_fleet();
    send(&mut app, Message::Go(Screen::Team("ops".into())));
    shows(&app, &["SHORTCUTS", "Notice", "Stop all"]);
    let sent = messages_of_click(&app, "Stop all");
    assert!(matches!(sent.as_slice(), [Message::Shortcut(1)]));
    send(&mut app, Message::Shortcut(0));
    send(&mut app, Message::Shortcut(1));
    let calls = fake.calls();
    assert!(calls.iter().any(|c| c == "publish_event ops info.notice"));
    assert!(calls.iter().any(|c| c == "publish_event ops stop.all"));
    assert!(app.publish.team.is_none());
    send(&mut app, Message::Go(Screen::Team("lab".into())));
    assert!(simulator_misses(&app, "Stop all"));
}

#[test]
fn an_older_control_plane_has_no_topics_and_nothing_else_breaks() {
    let (fake, mut app) = subscribed_fleet();
    fake.without_newer_routes();
    send(&mut app, Message::Go(Screen::Team("ops".into())));
    send(
        &mut app,
        Message::Tab(inspector::Tab::Team(inspector::TeamTab::Members)),
    );
    shows(
        &app,
        &[
            "This control plane is older: it has no topics yet. Update it to see them.",
            "Who is in it",
        ],
    );
}

#[test]
fn a_task_an_event_made_says_who_told_it_and_what_it_knew() {
    let (fake, mut app) = subscribed_fleet();
    let tasks = futures::executor::block_on(crate::application::ControlPlane::list_tasks(
        fake.as_ref(),
        crate::domain::TaskFilter::default(),
    ))
    .unwrap();
    let running = tasks
        .iter()
        .find(|t| t.status == TaskStatus::Running)
        .unwrap()
        .id
        .clone();
    fake.set_input(
        &running,
        serde_json::json!({"from": "bot-wrx80", "name": "pedido.nuevo", "message": "table 4",
            "notes": [{"from": "bot-wrx80", "name": "info.turno", "message": "shift at six", "at": "2026-10-25T09:00:00Z"}]}),
    );
    fake.tell(
        &running,
        "Your teammate bot-wrx80 says (warn.stock): low on boxes",
        "evt-9",
    );
    send(&mut app, Message::Go(Screen::Task(running)));
    // The event's message is its title; who told it is its From.
    shows(&app, &["table 4", "FROM", "bot-wrx80"]);
    send(&mut app, Message::Tab(inspector::Tab::Task(inspector::TaskTab::Answer)));
    shows(&app, &["KNEW", "bot-wrx80 · info.turno: shift at six"]);
    send(&mut app, Message::Go(Screen::Bot(BotId::new("bot").unwrap())));
    send(&mut app, Message::Tab(inspector::Tab::Bot(inspector::BotTab::Listens)));
    shows(&app, &["Your teammate bot-wrx80 says (warn.stock): low on boxes"]);
}

#[test]
fn a_finished_task_lists_its_files_and_saves_a_kept_one() {
    let mut app = connected();
    let done = app
        .fleet
        .as_ref()
        .unwrap()
        .tasks
        .iter()
        .find(|t| t.status == TaskStatus::Completed)
        .unwrap()
        .id
        .clone();
    send(&mut app, Message::Go(Screen::Task(done)));
    send(&mut app, Message::Tab(inspector::Tab::Task(inspector::TaskTab::Files)));
    shows(
        &app,
        &[
            "Files",
            "files.md",
            "text/markdown · 21 B",
            "workspace.tar",
            "application/x-tar · 10.0 MB",
            "not kept",
        ],
    );
    assert!(matches!(
        messages_of_click(&app, "Save").as_slice(),
        [Message::Download(_, name)] if name == "files.md"
    ));
    for width in [MIN_WINDOW.width, WINDOW.width] {
        shows_whole(&app, width, "Save", |_| widgets::semibold("Save").size(widgets::BODY));
        shows_whole(&app, width, "not kept", |_| text("not kept").size(widgets::SMALL));
    }
    app.downloading = true;
    assert!(messages_of_click(&app, "Save").is_empty(), "one save at a time");
}

#[test]
fn a_running_task_shows_its_activity_live_and_every_part_copies() {
    let fake = std::sync::Arc::new(crate::adapters::fake::FakeControlPlane::sample(
        "2026-10-25T10:00:00Z".parse().unwrap(),
    ));
    let mut app = super::view::tests::app_over(fake.clone());
    send(&mut app, Message::SettingsSave);
    let tasks = &app.fleet.as_ref().unwrap().tasks;
    let running = tasks
        .iter()
        .find(|t| t.status == TaskStatus::Running)
        .unwrap()
        .id
        .clone();
    let done = tasks
        .iter()
        .find(|t| t.status == TaskStatus::Completed)
        .unwrap()
        .id
        .clone();
    fake.progress(
        &running,
        serde_json::json!([
            {"kind": "delegation", "bot": "bot-wrx80", "taskId": done.to_string()},
            {"kind": "thinking", "text": "The clock may drift."}
        ]),
    );
    send(&mut app, Message::Go(Screen::Task(running.clone())));
    shows(
        &app,
        &[
            "Activity",
            "Talk to it",
            "Copy activity",
            "local_exec",
            "40 ms",
            "get_time",
            "running",
            "Asked bot-wrx80",
            "Open task",
            "I will read the host's clock.",
            "Checking it against the time server.",
        ],
    );
    // The run's progress is in the Activity, not in the timeline.
    assert!(simulator_misses(&app, "Working"));
    assert!(matches!(
        messages_of_click(&app, "Talk to it").as_slice(),
        [Message::Go(Screen::Bot(bot))] if bot.as_str() == "bot"
    ));
    assert!(matches!(
        messages_of_click(&app, "Open task").as_slice(),
        [Message::Go(Screen::Task(child))] if *child == done
    ));
    let copied = messages_of_click(&app, "Copy activity");
    assert!(matches!(
        copied.as_slice(),
        [Message::Copy(all)] if all.contains("local_exec · ok · 40 ms") && all.contains("Checking it against the time server.") && all.contains("Thinking: The clock may drift.")
    ));
    // Its words so far and its thinking, on Answer.
    send(&mut app, Message::Tab(inspector::Tab::Task(inspector::TaskTab::Answer)));
    shows(
        &app,
        &[
            "WHAT IT THOUGHT",
            "The clock may drift.",
            "Copy thinking",
            "WHAT IT SAYS SO FAR",
            "Copy words",
        ],
    );
    assert!(matches!(
        messages_of_click(&app, "Copy thinking").as_slice(),
        [Message::Copy(thoughts)] if thoughts == "The clock may drift."
    ));
    assert!(matches!(
        messages_of_click(&app, "Copy words").as_slice(),
        [Message::Copy(words)] if words.starts_with("I will read the host's clock.")
    ));
    send(
        &mut app,
        Message::Tab(inspector::Tab::Task(inspector::TaskTab::Activity)),
    );

    // A click on a step opens its details, which copy too.
    assert!(matches!(
        messages_of_click(&app, "local_exec").as_slice(),
        [Message::ActivityPick(1)]
    ));
    send(&mut app, Message::ActivityPick(1));
    shows(
        &app,
        &["ARGUMENTS", "{\"program\":\"date\"}", "CALL ID", "c1", "Copy step"],
    );
    assert!(matches!(
        messages_of_click(&app, "Copy step").as_slice(),
        [Message::Copy(step)] if step.starts_with("local_exec · ok · 40 ms\nTool: local_exec")
    ));
    let copied = send(&mut app, Message::Copy("x".into()));
    assert_eq!(copied, ["x"]);
}

#[test]
fn a_finished_task_keeps_its_activity_and_a_task_without_one_shows_none() {
    let fake = std::sync::Arc::new(crate::adapters::fake::FakeControlPlane::sample(
        "2026-10-25T10:00:00Z".parse().unwrap(),
    ));
    let mut app = super::view::tests::app_over(fake.clone());
    send(&mut app, Message::SettingsSave);
    let tasks = &app.fleet.as_ref().unwrap().tasks;
    let running = tasks
        .iter()
        .find(|t| t.status == TaskStatus::Running)
        .unwrap()
        .id
        .clone();
    let done = tasks
        .iter()
        .find(|t| t.status == TaskStatus::Completed)
        .unwrap()
        .id
        .clone();
    send(&mut app, Message::Go(Screen::Task(done)));
    shows(&app, &["It told nothing of what it did"]);
    send(&mut app, Message::Go(Screen::Task(running.clone())));
    send(&mut app, Message::AskCancel(running));
    send(&mut app, Message::ConfirmYes);
    let tick = app.now + chrono::Duration::seconds(3);
    send(&mut app, Message::Tick(tick));
    shows(&app, &["Activity", "local_exec", "no end seen"]);
    assert!(simulator_misses(&app, "Jump to latest"), "it no longer runs");
}

#[test]
fn a_bridged_call_shows_its_mcp_tool_its_arguments_and_why_it_failed() {
    let fake = std::sync::Arc::new(crate::adapters::fake::FakeControlPlane::sample(
        "2026-10-25T10:00:00Z".parse().unwrap(),
    ));
    let mut app = super::view::tests::app_over(fake.clone());
    send(&mut app, Message::SettingsSave);
    let running = app
        .fleet
        .as_ref()
        .unwrap()
        .tasks
        .iter()
        .find(|t| t.status == TaskStatus::Running)
        .unwrap()
        .id
        .clone();
    fake.progress(
        &running,
        serde_json::json!([
            {"kind": "tool.started", "tool": "axlr_call_tool", "mcpTool": "get_status", "callId": "c7",
             "summary": "{\"env\":\"pre\",\"only_failed\":true}"},
            {"kind": "tool.ended", "tool": "axlr_call_tool", "callId": "c7", "status": "failed",
             "error": "the session expired: log in again", "durationMs": 300}
        ]),
    );
    send(&mut app, Message::Go(Screen::Task(running)));
    // A call that says no why shows its arguments; its error waits in
    // its details.
    shows_wide(
        &app,
        &["get_status", "{\"env\":\"pre\",\"only_failed\":true}", "300 ms"],
    );
    let index = app
        .live
        .as_ref()
        .unwrap()
        .activity
        .entries
        .iter()
        .position(|e| e.headline().starts_with("get_status"))
        .unwrap();
    send(&mut app, Message::ActivityPick(index));
    shows(
        &app,
        &[
            "MCP TOOL",
            "axlr_call_tool",
            "WHY IT FAILED",
            "the session expired: log in again",
        ],
    );
}

#[test]
fn a_waiting_task_shows_approve_first_and_its_words_to_read_or_select() {
    use crate::adapters::fake::FakeControlPlane;
    let fake = std::sync::Arc::new(FakeControlPlane::sample("2026-10-25T10:00:00Z".parse().unwrap()));
    let mut app = super::view::tests::app_over(fake.clone());
    send(&mut app, Message::SettingsSave);
    let id = app.fleet.as_ref().unwrap().waiting()[0].id.clone();
    fake.progress(
        &id,
        serde_json::json!([
            {"kind": "text", "text": "The file is **missing**; I want to write it."},
            {"kind": "tool.started", "tool": "local_write", "callId": "c7"},
            {"kind": "decision.required", "tool": "local_write", "callId": "c7"}
        ]),
    );
    send(&mut app, Message::Go(Screen::Task(id)));
    let top = |word: &str| {
        let _turn = turn();
        let mut ui = simulator(view::view(&app));
        ui.find(word).unwrap_or_else(|e| panic!("{word}: {e:?}")).bounds().y
    };
    // Approve and Deny are in the inspector's header, at its top.
    assert!(top("Approve") < 120.0);
    shows(&app, &["Needs OK to call local_write"]);
    // What it said before it asked, on Answer.
    send(&mut app, Message::Tab(inspector::Tab::Task(inspector::TaskTab::Answer)));
    shows(&app, &["Select words"]);
    // The words read formatted: no Markdown marks in sight.
    assert!(simulator_misses(&app, "The file is **missing**; I want to write it."));
    assert!(!app.live.as_ref().unwrap().markdown.items().is_empty());
    assert!(matches!(
        messages_of_click(&app, "Select words").as_slice(),
        [Message::WordsSelecting(true)]
    ));
    send(&mut app, Message::WordsSelecting(true));
    assert!(matches!(
        messages_of_click(&app, "Formatted").as_slice(),
        [Message::WordsSelecting(false)]
    ));
    // What its run said before it asked is not an answer: the task has
    // not ended.
    app.answer = Some(crate::ui::Answer::new("I want to write it.".into()));
    shows(&app, &["WHAT IT SAID BEFORE ASKING YOU"]);
    assert!(simulator_misses(&app, "ANSWER"));
}

#[test]
fn a_bots_edit_and_delete_say_what_they_do_in_words() {
    let mut app = connected();
    let id = BotId::new("bot-wrx80").unwrap();
    send(&mut app, Message::Go(Screen::Bot(id.clone())));
    shows(&app, &["Edit", "Delete"]);
    send(&mut app, Message::EditOpen(id.clone()));
    shows(
        &app,
        &[
            "Edit bot-wrx80",
            "Its tools",
            "local_read",
            "Runs on its own",
            "Asks you first",
            "Never runs",
            "Add a tool",
            "local_search",
            "Save",
        ],
    );
    send(&mut app, Message::DeleteOpen(id));
    shows(
        &app,
        &[
            "Delete bot-wrx80?",
            "Type bot-wrx80 to delete it",
            "Delete bot-wrx80",
            "Keep it",
        ],
    );
}

#[test]
fn versions_show_and_a_bot_with_a_newer_one_offers_the_update() {
    let fake = std::sync::Arc::new(crate::adapters::fake::FakeControlPlane::sample(
        "2026-10-25T10:00:00Z".parse().unwrap(),
    ));
    fake.change_bot("bot", |b| b.version = "1.2.0".into());
    fake.stage_bot("bot", "1.3.0");
    fake.change_bot("bot-wrx80", |b| b.version = "2.0.0".into());
    fake.set_build(Some(crate::domain::BuildStatus {
        running: crate::domain::BuildIdentity {
            version: "0.4.0".into(),
            ..Default::default()
        },
        ..Default::default()
    }));
    fake.set_node_build("wrx80", "0.3.0", "axlr-tui 0.14.0");
    let mut app = super::view::tests::app_over(fake);
    send(&mut app, Message::SettingsSave);
    send(&mut app, Message::Go(Screen::Bots));
    shows(&app, &["· v0.4.0"]);
    {
        let _turn = turn();
        let mut wide = Simulator::with_size(panel_settings(), Size::new(1600.0, 1000.0), view::view(&app));
        for words in ["v1.2.0", "v2.0.0", "claude-haiku-5.5"] {
            assert!(wide.find(words).is_ok(), "{words} is not on the wide Bots");
        }
    }

    let id = BotId::new("bot").unwrap();
    send(&mut app, Message::Go(Screen::Bot(id.clone())));
    shows(&app, &["v1.2.0", "Update to v1.3.0"]);
    let sent = messages_of_click(&app, "Update to v1.3.0");
    assert!(
        matches!(sent.as_slice(), [Message::AskUpdateBot(b)] if *b == id),
        "{sent:?}"
    );
    send(&mut app, Message::AskUpdateBot(id.clone()));
    shows(&app, &["Update bot to v1.3.0?", "Update"]);
    send(&mut app, Message::ConfirmYes);
    shows(&app, &["v1.3.0", "bot updated"]);
    assert!(simulator_misses(&app, "Update to v1.3.0"));

    // A bot at its newest version shows it and offers nothing.
    send(&mut app, Message::Go(Screen::Bot(BotId::new("bot-wrx80").unwrap())));
    shows(&app, &["v2.0.0"]);
    assert!(simulator_misses(&app, "Update to"));

    send(&mut app, Message::Go(Screen::Nodes));
    shows(&app, &["v0.3.0"]);
}

/// A task with a task it delegated, a tool with its duration, a cut and
/// the steps completed from the transcript after it.
pub(super) fn delegating_activity() -> crate::domain::TaskActivity {
    let mut tree = crate::domain::sample_task_activity();
    let kept: crate::domain::Event = serde_json::from_value(serde_json::json!({
        "eventId": "e1", "type": "bot.task.progress", "executionId": "exec-r",
        "createdAt": "2026-11-02T10:00:01Z", "payload": {"items": [
            {"kind": "tool.started", "tool": "local_read", "callId": "c1", "at": "2026-11-02T10:00:01Z"},
            {"kind": "tool.ended", "tool": "local_read", "callId": "c1", "status": "ok", "durationMs": 120}
        ]}
    }))
    .unwrap();
    tree.events.push(kept);
    let run = &mut tree.executions[0];
    run.progress_cut = Some(crate::domain::ProgressCut {
        reason: crate::domain::CutReason::EventLimit,
        events_kept: 400,
        bytes_kept: 1,
        cut_at: None,
        dropped: 0,
        from_transcript: true,
        transcript_sessions: vec!["s".into()],
        completion_bounded: false,
    });
    run.completed_items = serde_json::from_value(serde_json::json!([
        {"kind": "tool.started", "tool": "local_write", "callId": "c2", "at": "2026-11-02T10:00:02Z"},
        {"kind": "tool.ended", "tool": "local_write", "callId": "c2", "status": "failed", "error": "denied", "durationMs": 2400}
    ]))
    .unwrap();
    tree
}

#[test]
fn the_runs_tab_shows_the_tree_its_tools_and_the_transcript_mark() {
    let mut app = connected();
    let tree = delegating_activity();
    let id = tree.task.id.clone();
    app.screen = Screen::Task(id.clone());
    app.tabs.task = inspector::TaskTab::Runs;
    app.task = Some(crate::application::TaskDetail {
        task: tree.task.clone(),
        executions: Vec::new(),
        events: Vec::new(),
    });
    let mut screen = ActivityScreen::new(id.clone());
    assert!(
        !view_has(&app, "completed from transcript"),
        "nothing is drawn before the tree is read"
    );
    screen.loaded(tree);
    app.activity = Some(screen);
    shows_wide(
        &app,
        &[
            "Everything it did, with what it asked",
            "2 tasks",
            "local_read",
            "local_write",
            "failed",
            "completed from transcript",
        ],
    );
    // A task of the tree opens in the inspector.
    assert!(
        messages_of_click(&app, "Open")
            .iter()
            .any(|m| matches!(m, Message::Go(Screen::Task(_))))
    );
}

fn view_has(app: &App, word: &str) -> bool {
    !simulator_misses(app, word)
}

#[test]
fn the_runs_tab_reads_the_tree_and_follows_it_while_it_runs() {
    let mut app = connected();
    let running = app
        .fleet
        .as_ref()
        .unwrap()
        .tasks
        .iter()
        .find(|t| t.status == TaskStatus::Running)
        .unwrap()
        .id
        .clone();
    send(&mut app, Message::Go(Screen::Task(running.clone())));
    assert!(app.activity.is_none(), "the tree waits for its tab");
    assert!(
        messages_of_click(&app, "Runs")
            .iter()
            .any(|m| matches!(m, Message::Tab(inspector::Tab::Task(inspector::TaskTab::Runs))))
    );
    send(&mut app, Message::Tab(inspector::Tab::Task(inspector::TaskTab::Runs)));
    let screen = app.activity.as_ref().unwrap();
    assert!(
        screen.tree.is_some() && screen.cursor.is_some(),
        "read once, then followed"
    );
    assert!(!screen.finished());
    shows_wide(&app, &["Everything it did, with what it asked", "live"]);
    // Another task drops the tree: its own starts empty.
    send(
        &mut app,
        Message::Go(Screen::Task(crate::domain::TaskId::new("other").unwrap())),
    );
    let other = app.activity.as_ref().unwrap();
    assert!(
        other.id.as_str() == "other" && other.tree.is_none(),
        "a task that is not there has no tree"
    );
    assert!(app.banner.is_some(), "and the screen says why");
}

/// A drawing of the panel as pixels: its width and its colours, four
/// bytes each, at twice the window's points.
fn pixels(app: &App) -> (usize, Vec<u8>) {
    let _turn = turn();
    let theme = Tokens::of(app.mode).theme();
    let mut ui = Simulator::with_size(panel_settings(), WINDOW, view::view(app));
    let snapshot = ui.snapshot(&theme).unwrap();
    let scratch = tempfile::tempdir().unwrap();
    assert!(snapshot.matches_image(scratch.path().join("shot")).unwrap());
    let drawn = std::fs::read_dir(scratch.path())
        .unwrap()
        .next()
        .unwrap()
        .unwrap()
        .path();
    let picture = iced::advanced::graphics::image::load(&iced::widget::image::Handle::from_path(&drawn)).unwrap();
    (picture.width() as usize, picture.as_raw().to_vec())
}

/// The colour drawn at a place, in points.
fn colour_at(drawing: &(usize, Vec<u8>), x: f32, y: f32) -> [u8; 3] {
    let at = (((y * 2.0) as usize) * drawing.0 + (x * 2.0) as usize) * 4;
    [drawing.1[at], drawing.1[at + 1], drawing.1[at + 2]]
}

fn rgb(colour: iced::Color) -> [u8; 3] {
    let [r, g, b, _] = colour.into_rgba8();
    [r, g, b]
}

/// The filter: a click puts the keyboard in it and what is typed there
/// (`/`, `j` and `k` too) filters the list; Esc is the field's (it
/// leaves it, and the panel hears it as a key a field took); the arrows
/// and Enter go on to the list's cursor.
#[test]
fn the_filter_keeps_what_is_typed_and_lets_the_arrows_and_enter_through() {
    use iced::event::Status;
    use iced::keyboard::{Key, key::Named};
    let app = large(Screen::Tasks);
    let _turn = turn();
    let mut ui = Simulator::with_size(panel_settings(), WINDOW, view::view(&app));
    ui.click(iced::widget::Id::new(Field::ListFilter.id())).unwrap();
    assert!(ui.find(iced_test::selector::is_focused()).is_ok());
    ui.typewrite("gate/jk");
    assert_eq!(ui.tap_key(Key::Named(Named::ArrowDown)), Status::Ignored);
    assert_eq!(ui.tap_key(Key::Named(Named::Enter)), Status::Ignored);
    assert_eq!(ui.tap_key(Key::Named(Named::Escape)), Status::Captured);
    assert!(
        ui.find(iced_test::selector::is_focused()).is_err(),
        "Esc left the field"
    );
    let typed: Vec<String> = ui
        .into_messages()
        .filter_map(|m| match m {
            Message::ListFilterTyped(typed) => Some(typed),
            _ => None,
        })
        .collect();
    assert_eq!(typed.first().map(String::as_str), Some("g"));
    assert_eq!(typed.last().map(String::as_str), Some("gate/jk"));
}

/// What the filter leaves: the rows that answer, counted, with what it
/// found marked (a marked cell is drawn in pieces, so its words are not
/// one text any more); nothing left says so.
#[test]
fn the_filter_leaves_the_rows_that_answer_and_says_when_none_does() {
    let mut app = large(Screen::Tasks);
    send(&mut app, Message::ListFilterTyped("gate".into()));
    shows(
        &app,
        &["19 of 200", "ops-deploy", "support-triage", "gate", "Newest 200 tasks"],
    );
    assert!(simulator_misses(&app, "Say the time"));
    assert!(
        simulator_misses(
            &app,
            "item-4198 · service-3 · gate failed: the unit tests of the client"
        ),
        "the word found is marked in it"
    );
    send(&mut app, Message::ListFilterTyped("zzzz".into()));
    shows(&app, &["Nothing matches zzzz in the newest 200 tasks."]);
    send(&mut app, Message::Go(Screen::Bots));
    send(&mut app, Message::ListFilterTyped("zzzz".into()));
    shows(&app, &["Nothing matches zzzz among the 40 bots."]);
    send(&mut app, Message::Go(Screen::Nodes));
    send(&mut app, Message::ListFilterTyped("zzzz".into()));
    shows(&app, &["Nothing matches zzzz among the 8 nodes."]);
    send(&mut app, Message::Go(Screen::Teams));
    send(&mut app, Message::ListFilterTyped("zzzz".into()));
    shows(&app, &["Nothing matches zzzz among the 8 teams."]);
}

/// Folded, a line counts the repeats it holds; unfolded, every task is
/// a line. The options sit on the note line.
#[test]
fn a_folded_line_counts_its_repeats() {
    let mut app = large(Screen::Tasks);
    shows(&app, &["×39", "Fold repeats", "Group:", "none"]);
    let sent = messages_of_click(&app, "Fold repeats");
    assert!(matches!(sent.as_slice(), [Message::FoldToggled]), "{sent:?}");
    send(&mut app, Message::FoldToggled);
    assert!(simulator_misses(&app, "×39"));
    shows(&app, &["200 tasks", "Newest 200 tasks"]);
    let sent = messages_of_click(&app, "none");
    assert!(matches!(sent.as_slice(), [Message::GroupingCycled]), "{sent:?}");
}

/// Grouped, the lines go under headers that carry their counts; a
/// press on one closes it.
#[test]
fn a_group_header_counts_its_lines_and_closes() {
    let mut app = large(Screen::Tasks);
    send(&mut app, Message::GroupingCycled);
    shows(&app, &["event ops.item.assigned", "39", "trigger"]);
    let sent = messages_of_click(&app, "event ops.item.assigned");
    let key = find::GroupKey::Tasks(find::TaskGrouping::Trigger, "event ops.item.assigned".into());
    assert!(
        matches!(sent.as_slice(), [Message::GroupToggled(k)] if *k == key),
        "{sent:?}"
    );
    send(&mut app, Message::GroupToggled(key));
    shows(&app, &["event ops.item.assigned", "timer round"]);
    assert!(simulator_misses(&app, "ops-deploy"), "its rows are put away");
}

/// A click on a row's words opens the row; its box, its name and its
/// acts keep their own clicks.
#[test]
fn a_click_on_a_row_opens_it_and_its_box_only_picks() {
    let app = large(Screen::Tasks);
    let running = app
        .fleet
        .as_ref()
        .unwrap()
        .tasks
        .iter()
        .find(|t| t.objective == "Say the time")
        .unwrap()
        .id
        .clone();
    let sent = messages_of_click(&app, "Say the time");
    assert!(
        matches!(sent.as_slice(), [Message::RowPicked(table::RowRef::Task(id))] if *id == running),
        "{sent:?}"
    );
    let _turn = turn();
    let mut ui = Simulator::with_size(panel_settings(), WINDOW, view::view(&app));
    let row = ui.find("Say the time").unwrap().bounds();
    // The row's box is the first thing on its line.
    ui.point_at(iced::Point::new(69.0 + 14.0 + 9.0, row.center_y()));
    let _ = ui.simulate(iced_test::simulator::click());
    let sent = said(ui);
    assert!(
        matches!(sent.as_slice(), [Message::Pick(table::RowRef::Task(id), true)] if *id == running),
        "{sent:?}"
    );
}

/// The row the keyboard is on: the accent's bar at its left and the
/// picked wash over it, light and dark; the rows around keep the page.
#[test]
fn the_row_under_the_cursor_has_the_accents_bar_and_the_wash() {
    let mut app = large(Screen::Tasks);
    send(&mut app, Message::Key(Keypress::Down));
    send(&mut app, Message::Key(Keypress::Down));
    let (first, second) = {
        let _turn = turn();
        let mut ui = Simulator::with_size(panel_settings(), WINDOW, view::view(&app));
        (
            ui.find("Say the time").unwrap().bounds().center_y(),
            ui.find("Write the weekly report").unwrap().bounds().center_y(),
        )
    };
    for mode in [ThemeMode::Light, ThemeMode::Dark] {
        app.mode = mode;
        let tokens = Tokens::of(mode);
        let drawing = pixels(&app);
        let edge = 69.0 + 1.0;
        assert_eq!(colour_at(&drawing, edge, second), rgb(tokens.accent), "{mode:?}");
        assert_eq!(
            colour_at(&drawing, edge + 6.0, second),
            rgb(styles::picked_fill(tokens)),
            "{mode:?}"
        );
        assert_eq!(colour_at(&drawing, edge, first), rgb(tokens.page), "{mode:?}");
        assert_eq!(colour_at(&drawing, edge + 6.0, first), rgb(tokens.page), "{mode:?}");
    }
}

/// The row under the pointer glows: the signal comes from the screen
/// itself, and once the glow has faded in the row is tinted around the
/// pointer, its words still drawn over it, and nothing else changes: the
/// rows around it, the row the keyboard is on with its wash. With
/// motion off the same pointer draws nothing.
#[test]
fn the_row_under_the_pointer_glows_behind_its_words_and_motion_off_draws_none() {
    use iced::{Event, Point, mouse};

    use super::organic::Signal;

    let mut app = large(Screen::Tasks);
    send(&mut app, Message::Key(Keypress::Down));
    send(&mut app, Message::Key(Keypress::Down));
    let (picked, hovered, below) = {
        let _turn = turn();
        let mut ui = Simulator::with_size(panel_settings(), WINDOW, view::view(&app));
        (
            ui.find("Write the weekly report").unwrap().bounds().center_y(),
            ui.find("Read the missing file").unwrap().bounds().center_y(),
            ui.find("Write hello into hello.txt").unwrap().bounds().center_y(),
        )
    };
    let pointer = Point::new(700.0, hovered);
    // What the row tells as the pointer comes onto it.
    let told = |app: &App| {
        let _turn = turn();
        let mut ui = Simulator::with_size(panel_settings(), WINDOW, view::view(app));
        ui.point_at(pointer);
        let _ = ui.simulate([Event::Mouse(mouse::Event::CursorMoved { position: pointer })]);
        let signals: Vec<Signal> = ui
            .into_messages()
            .filter_map(|m| match m {
                Message::Hover(signal) => Some(signal),
                _ => None,
            })
            .collect();
        signals
    };
    let signals = told(&app);
    let [
        Signal::Over {
            surface,
            at,
            size,
            when,
        },
    ] = signals[..]
    else {
        panic!("{signals:?}");
    };
    let id = {
        let tasks = &app.fleet.as_ref().unwrap().tasks;
        let mut same = tasks.iter().filter(|t| t.objective == "Read the missing file");
        let task = same.next().unwrap();
        assert!(same.next().is_none(), "one task says it");
        task.id.clone()
    };
    assert_eq!(surface, HoverSpot::Row(table::RowRef::Task(id)).surface());
    assert_eq!(size.height, Density::Comfortable.row_height() - 1.0);

    let ms = std::time::Duration::from_millis;
    for (round, mode) in [ThemeMode::Light, ThemeMode::Dark].into_iter().enumerate() {
        app.mode = mode;
        let tokens = Tokens::of(mode);
        let plain = pixels(&app);
        // The same news, told later each round.
        let start = when + ms(2_000 * round as u64);
        let news = Message::Hover(Signal::Over {
            surface,
            at,
            size,
            when: start,
        });
        app.apply(news.clone());
        app.apply(Message::Frame(start + ms(400)));
        assert!(app.hover.shine(surface).is_some_and(|shine| shine.fade == 1.0));
        let lit = pixels(&app);
        let change = |x: f32, y: f32| {
            let (a, b) = (colour_at(&plain, x, y), colour_at(&lit, x, y));
            (0..3).map(|c| a[c].abs_diff(b[c])).max().unwrap_or(0)
        };
        // Around the pointer the row is tinted.
        let around = [-60.0, -20.0, 0.0, 20.0, 60.0]
            .iter()
            .flat_map(|dx| [-8.0, 0.0, 8.0].map(|dy| change(pointer.x + dx, hovered + dy)))
            .max()
            .unwrap_or(0);
        assert!(around >= 8, "{mode:?}: the glow hardly shows ({around})");
        // The rows around it are as they were, the picked one with its
        // wash and the accent's bar.
        for y in [picked, below, hovered - 30.0, hovered + 30.0] {
            for x in [80.0, 400.0, pointer.x, 1200.0] {
                assert_eq!(change(x, y), 0, "{mode:?}: the glow spills to {x},{y}");
            }
        }
        assert_eq!(colour_at(&lit, 70.0, picked), rgb(tokens.accent), "{mode:?}");
        assert_eq!(
            colour_at(&lit, 76.0, picked),
            rgb(styles::picked_fill(tokens)),
            "{mode:?}"
        );
        // The words of the row are still drawn over the glow: its
        // inkiest pixel stays as inky.
        let ink = |drawing: &(usize, Vec<u8>)| {
            let mut darkest = 255u32 * 3;
            let mut lightest = 0u32;
            for x in (300..1100).step_by(1) {
                for dy in [-4.0, -2.0, 0.0, 2.0, 4.0] {
                    let [r, g, b] = colour_at(drawing, x as f32, hovered + dy);
                    let sum = u32::from(r) + u32::from(g) + u32::from(b);
                    darkest = darkest.min(sum);
                    lightest = lightest.max(sum);
                }
            }
            if tokens.dark { lightest } else { darkest }
        };
        let (before, after) = (ink(&plain), ink(&lit));
        assert!(
            before.abs_diff(after) <= 24,
            "{mode:?}: the words fade under the glow ({before} → {after})"
        );

        // Motion off: nothing glows, and the same news draw nothing.
        send(&mut app, Message::SetMotion(crate::application::Motion::Off));
        app.apply(news);
        app.apply(Message::Frame(start + ms(800)));
        let off = pixels(&app);
        assert!(off.1 == plain.1, "{mode:?}: motion off draws the page as it was");
        send(&mut app, Message::SetMotion(crate::application::Motion::On));
    }
    // Off, the rows tell nothing about the pointer either.
    send(&mut app, Message::SetMotion(crate::application::Motion::Off));
    assert!(told(&app).is_empty());
}

/// The palette: its field, its results under their headings with the
/// keys at its foot. What is typed goes to it; the arrows and Enter go
/// on to the panel, Esc is its field's (which the panel hears as a key
/// a field took); a click on a result picks it, one on the dim layer
/// closes it.
#[test]
fn the_palette_takes_what_is_typed_and_its_results_are_clicked() {
    use iced::event::Status;
    use iced::keyboard::{Key, key::Named};
    let mut app = large(Screen::Tasks);
    send(&mut app, Message::PaletteOpen);
    shows(
        &app,
        &[
            "ACTIONS",
            "Go to Home",
            "BOTS",
            "TEAMS",
            "NODES",
            "Esc",
            "Paste a task or trace id to jump to it",
            "audit · Paused",
        ],
    );
    {
        let _turn = turn();
        let mut ui = Simulator::with_size(panel_settings(), WINDOW, view::view(&app));
        ui.click(iced::widget::Id::new(Field::Palette.id())).unwrap();
        ui.typewrite("ops");
        assert_eq!(ui.tap_key(Key::Named(Named::ArrowDown)), Status::Ignored);
        assert_eq!(ui.tap_key(Key::Named(Named::Enter)), Status::Ignored);
        assert_eq!(ui.tap_key(Key::Named(Named::Escape)), Status::Captured);
        let typed: Vec<String> = ui
            .into_messages()
            .filter_map(|m| match m {
                Message::PaletteTyped(typed) => Some(typed),
                _ => None,
            })
            .collect();
        assert_eq!(typed, vec!["o", "op", "ops"]);
    }
    let sent = messages_of_click(&app, "Go to Bots");
    assert!(matches!(sent.as_slice(), [Message::PalettePicked(2)]), "{sent:?}");
    {
        let _turn = turn();
        let mut ui = Simulator::with_size(panel_settings(), WINDOW, view::view(&app));
        ui.point_at(iced::Point::new(200.0, WINDOW.height - 40.0));
        let _ = ui.simulate(iced_test::simulator::click());
        let sent = said(ui);
        assert!(matches!(sent.as_slice(), [Message::PaletteClose]), "{sent:?}");
    }
    // Typing, moving and Enter, as the keys reach the panel.
    app.apply(Message::PaletteTyped("ops".into()));
    shows(&app, &["ops · On", "ops · Paused", "5 bots"]);
    app.apply(Message::Key(Keypress::Down));
    assert_eq!(app.palette.selected(app.palette_entries().len()), Some(1));
    send(&mut app, Message::Key(Keypress::Enter));
    assert_eq!(app.screen, Screen::Bot(BotId::new("ops-night").unwrap()));
    assert!(simulator_misses(&app, "Paste a task or trace id to jump to it"));
    send(&mut app, Message::PaletteOpen);
    app.apply(Message::PaletteTyped("zzzz".into()));
    shows(&app, &["Nothing found. Tasks are searched among the newest 200."]);
}

/// Home's latest tasks are folded too.
#[test]
fn home_folds_the_latest_repeats() {
    let app = large(Screen::Overview);
    shows(&app, &["Latest", "×39"]);
}

/// The large fleet on the task with the long run, inspected beside the
/// Tasks list.
fn long_run_inspected() -> App {
    let mut app = large(Screen::Tasks);
    let id = app
        .fleet
        .as_ref()
        .unwrap()
        .tasks
        .iter()
        .find(|t| t.bot_id.as_str() == "ops-deploy")
        .unwrap()
        .id
        .clone();
    send(&mut app, Message::RowPicked(table::RowRef::Task(id)));
    app
}

#[test]
fn a_task_inspected_shows_its_header_facts_tabs_and_steps_beside_a_narrow_list() {
    let app = long_run_inspected();
    shows_wide(
        &app,
        &[
            "Done",
            "ops-deploy",
            "Run again",
            "item-4198 · service-3 · gate failed: the unit tests of the client",
            "NODE",
            "STARTED",
            "TOOK",
            "COST",
            "FROM",
            "TEAM",
            "TASK ID",
            "TRACE",
            "Activity",
            "Answer",
            "Files",
            "Runs",
            "Timeline",
            "Raw",
            "local_read",
            "notes_search",
            "ops.item.progress",
            "→ ops-lead",
            "Find in activity",
            "12.3 s",
        ],
    );
    // The list beside it keeps its filter, its note and its act.
    shows_wide(&app, &["Filter…", "New task", "BOT", "WHAT", "AGE"]);
    assert!(simulator_misses(&app, "Fold repeats"), "its options wait");
    assert!(simulator_misses(&app, "STARTED BY"), "and its wide columns");
    assert!(matches!(
        messages_of_click(&app, "Run again").as_slice(),
        [Message::RunAgain(_)]
    ));
    assert!(matches!(
        messages_of_click(&app, "Answer").as_slice(),
        [Message::Tab(inspector::Tab::Task(inspector::TaskTab::Answer))]
    ));
    assert!(matches!(
        messages_of_click(&app, "notes_search").as_slice(),
        [Message::ActivityPick(_)]
    ));
}

#[test]
fn the_find_counts_its_matches_and_the_chips_their_steps() {
    let mut app = long_run_inspected();
    shows_wide(&app, &["All", "29", "Tools", "25", "Failed", "1", "Events", "3"]);
    send(&mut app, Message::FindTyped("tests".into()));
    let label = app.find.label(&app.steps_text());
    assert!(label.starts_with("1/"), "{label}");
    shows_wide(&app, &[label.as_str(), "Copy activity"]);
    assert!(matches!(
        messages_of_click(&app, "Events").as_slice(),
        [Message::FindShow(crate::ui::find::StepFilter::Events)]
    ));
    send(&mut app, Message::FindShow(crate::ui::find::StepFilter::Failed));
    shows_wide(&app, &["Read the team's playbook before handl…"]);
    assert!(
        simulator_misses(&app, "notes_search"),
        "the chip shows the failed alone"
    );
}

#[test]
fn a_running_task_scrolled_up_in_offers_to_jump_to_its_newest_step() {
    let mut app = large(Screen::Tasks);
    let running = app
        .fleet
        .as_ref()
        .unwrap()
        .tasks
        .iter()
        .find(|t| t.status == TaskStatus::Running)
        .unwrap()
        .id
        .clone();
    send(&mut app, Message::Go(Screen::Task(running)));
    shows_wide(&app, &["Running", "Talk to it", "Stop", "running"]);
    assert!(simulator_misses(&app, "↓ Jump to latest"), "it follows its newest step");
    send(&mut app, Message::ActivityScrolled(LiveStream::Steps, false));
    assert!(matches!(
        messages_of_click(&app, "↓ Jump to latest").as_slice(),
        [Message::JumpToLatest]
    ));
}

#[test]
fn a_node_inspected_shows_its_facts_labels_providers_and_its_bots() {
    let mut app = large(Screen::Nodes);
    send(&mut app, Message::Go(Screen::Node(NodeId::new("gx10-ff95").unwrap())));
    shows_wide(
        &app,
        &[
            "Online",
            "Revoke",
            "SEEN",
            "AXLR",
            "AGENT",
            "CERTIFICATE",
            "LABELS",
            "host=gx10-ff95",
            "MODEL PROVIDERS",
            "local:codex/gpt-6.1-sol",
            "BOTS THAT RUN HERE · 11",
            "ops-lead",
        ],
    );
    assert!(matches!(
        messages_of_click(&app, "Revoke").as_slice(),
        [Message::AskRevoke(id)] if id.as_str() == "gx10-ff95"
    ));
    assert!(matches!(
        messages_of_click(&app, "ops-lead").as_slice(),
        [Message::Go(Screen::Bot(id))] if id.as_str() == "ops-lead"
    ));
}

#[test]
fn a_task_whose_detail_is_still_read_keeps_its_tabs_and_its_activity() {
    let mut app = long_run_inspected();
    // Moving to another row clears the detail until its read comes back:
    // the header, the tabs and the find stand from the fleet meanwhile.
    app.task = None;
    shows_wide(
        &app,
        &["ops-deploy", "Activity", "Answer", "Find in activity", "local_read"],
    );
    app.tabs.task = inspector::TaskTab::Files;
    shows_wide(&app, &["Loading the task…"]);
}

#[test]
fn a_row_named_at_start_is_under_its_lists_cursor() {
    let now = "2026-10-25T10:00:00Z".parse().unwrap();
    let fake = std::sync::Arc::new(crate::adapters::fake::FakeControlPlane::large(now));
    let bot = BotId::new("support-lead").unwrap();
    let mut app = super::view::tests::app_over(fake);
    app.screen = Screen::Bot(bot.clone());
    app.tables.bots.cursor.select(bot.clone());
    let effects = app.apply(Message::SettingsSave);
    super::test_runtime::drive(&mut app, effects);
    assert_eq!(app.tables.bots.cursor.selected(), Some(&bot));
    assert_eq!(app.cursor_row(table::List::Bots), Some(table::RowRef::Bot(bot)));
}
