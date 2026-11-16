//! The README's screenshots, drawn headless from the sample bots with
//! Iced's simulator, the panel's own typefaces and its window size.
//! Not part of the suite: run it to write `docs/screenshots/*.png` again
//! (`ICED_TEST_BACKEND=tiny-skia TZ=UTC cargo test screenshots -- --ignored`).

use std::fs;
use std::path::{Path, PathBuf};
use std::sync::Arc;

use iced_test::Simulator;

use iced::Point;

use super::organic::Signal;
use super::runtime::WINDOW;
use super::test_runtime::{connected, drive, send};
use super::view::tests::app_over;
use super::view_tests::{panel_settings, turn};
use super::*;
use crate::adapters::fake::FakeControlPlane;
use crate::domain::{BotId, TaskStatus, ToolRule};

/// Draws the panel as it stands, in the window's size at start (the
/// simulator draws it at twice the pixels), and writes it as `<name>.png`.
struct Camera {
    out: PathBuf,
}

impl Camera {
    fn new() -> Self {
        Self::into(Path::new(env!("CARGO_MANIFEST_DIR")).join("docs/screenshots"))
    }

    /// A camera that writes into `out`, made if it is not there.
    fn into(out: PathBuf) -> Self {
        fs::create_dir_all(&out).unwrap();
        Self { out }
    }

    /// Draws the panel without the last act's banner ("Connected", "Task
    /// sent"), which would sit on top of every shot.
    fn shoot(&self, app: &mut App, name: &str) {
        self.shoot_at(app, name, WINDOW);
    }

    /// The same, in a window of another size.
    fn shoot_at(&self, app: &mut App, name: &str, window: iced::Size) {
        self.draw(app, name, window, None);
    }

    /// Draws the panel with the pointer at `pointer`, if one is given:
    /// what is under it has the pointer's wash, as in the window.
    fn draw(&self, app: &mut App, name: &str, window: iced::Size, pointer: Option<Point>) {
        let _ = app.apply(Message::DismissBanner);
        let _turn = turn();
        let theme = Tokens::of(app.mode).theme();
        let mut ui = Simulator::with_size(panel_settings(), window, view::view(app));
        if let Some(pointer) = pointer {
            ui.point_at(pointer);
        }
        let snapshot = ui.snapshot(&theme).unwrap();
        // `matches_image` writes a new file named after the renderer, so
        // it writes into an empty directory and the file is moved.
        let scratch = tempfile::tempdir().unwrap();
        assert!(snapshot.matches_image(scratch.path().join(name)).unwrap());
        let drawn = fs::read_dir(scratch.path()).unwrap().next().unwrap().unwrap().path();
        let _ = fs::copy(drawn, self.out.join(format!("{name}.png"))).unwrap();
    }

    /// The same state, light and dark.
    fn both(&self, app: &mut App, name: &str) {
        self.both_at(app, name, None);
    }

    /// The same state, light and dark, with the pointer at `pointer`.
    fn both_at(&self, app: &mut App, name: &str, pointer: Option<Point>) {
        for mode in [ThemeMode::Light, ThemeMode::Dark] {
            app.mode = mode;
            let suffix = if mode == ThemeMode::Dark { "dark" } else { "light" };
            self.draw(app, &format!("{name}-{suffix}"), WINDOW, pointer);
        }
        app.mode = ThemeMode::Light;
    }
}

/// Puts the pointer at `at` as a person would: what the screen tells
/// about it goes to the panel, and the glow fades in whole while its
/// waves move on for `seconds`.
fn hover_at(app: &mut App, at: Point, seconds: f32) {
    let signals: Vec<Signal> = {
        let _turn = turn();
        let mut ui = Simulator::with_size(panel_settings(), WINDOW, view::view(app));
        ui.point_at(at);
        let _ = ui.simulate([iced::Event::Mouse(iced::mouse::Event::CursorMoved { position: at })]);
        ui.into_messages()
            .filter_map(|m| match m {
                Message::Hover(signal) => Some(signal),
                _ => None,
            })
            .collect()
    };
    let when = signals
        .iter()
        .filter_map(|signal| match signal {
            Signal::Over { when, .. } => Some(*when),
            Signal::Left { .. } => None,
        })
        .max()
        .unwrap_or_else(|| panic!("nothing glows at {at:?}"));
    for signal in signals {
        let _ = app.apply(Message::Hover(signal));
    }
    let _ = app.apply(Message::Frame(when + std::time::Duration::from_secs_f32(seconds)));
}

/// Where words are drawn on the panel as it stands.
fn place_of(app: &App, words: &str) -> iced::Rectangle {
    let _turn = turn();
    let mut ui = Simulator::with_size(panel_settings(), WINDOW, view::view(app));
    ui.find(words)
        .unwrap_or_else(|e| panic!("{words} is not on {:?}: {e:?}", app.screen))
        .bounds()
}

fn task_with(app: &App, status: TaskStatus) -> crate::domain::TaskId {
    let fleet = app.fleet.as_ref().unwrap();
    fleet.tasks.iter().find(|t| t.status == status).unwrap().id.clone()
}

fn bot(id: &str) -> BotId {
    BotId::new(id).unwrap()
}

#[test]
#[ignore = "writes docs/screenshots; run it with --ignored"]
fn screenshots() {
    let camera = Camera::new();
    let mut app = connected();

    camera.both(&mut app, "overview");
    send(&mut app, Message::Go(Screen::Nodes));
    camera.both(&mut app, "nodes");
    send(&mut app, Message::Go(Screen::Bots));
    camera.both(&mut app, "bots");
    camera.shoot(&mut app, "now-bots-light");
    send(&mut app, Message::Go(Screen::Tasks));
    camera.both(&mut app, "tasks");

    let waiting = task_with(&app, TaskStatus::WaitingApproval);
    send(&mut app, Message::Go(Screen::Task(waiting)));
    camera.both(&mut app, "task-waiting");

    let done = task_with(&app, TaskStatus::Completed);
    send(&mut app, Message::Go(Screen::Task(done.clone())));
    camera.both(&mut app, "task");

    // A task and the task it delegated, with what each ran and cost.
    let root = crate::domain::TaskId::new("task-root").unwrap();
    let delegating = super::view_tests::delegating_activity();
    app.screen = Screen::Task(root.clone());
    app.tabs.task = inspector::TaskTab::Runs;
    app.task = Some(crate::application::TaskDetail {
        task: delegating.task.clone(),
        executions: Vec::new(),
        events: Vec::new(),
    });
    let mut tree = ActivityScreen::new(root);
    tree.loaded(delegating);
    app.activity = Some(tree);
    camera.both(&mut app, "task-activity");
    app.tabs.task = inspector::TaskTab::Activity;
    app.apply(Message::AnswerSelecting(true));
    camera.shoot(&mut app, "task-copy-light");
    app.apply(Message::AnswerSelecting(false));

    // The bot's own rules about the tool show under the step.
    if let Some(b) = app
        .fleet
        .as_mut()
        .unwrap()
        .bots
        .iter_mut()
        .find(|b| b.id == bot("bot-wrx80"))
    {
        b.tool_rules.allow.push(ToolRule {
            tool: "local_list".into(),
            arguments: [("path".to_string(), "workspace/*".to_string())].into(),
        });
    }
    send(&mut app, Message::Go(Screen::Task(done)));
    camera.shoot(&mut app, "steps-light");

    send(&mut app, Message::Go(Screen::Bot(bot("bot-wrx80"))));
    app.apply(Message::Tab(inspector::Tab::Bot(inspector::BotTab::Rules)));
    camera.both(&mut app, "bot");
    app.apply(Message::Tab(inspector::Tab::Bot(inspector::BotTab::Now)));

    send(&mut app, Message::Go(Screen::Bot(bot("bot"))));
    camera.shoot(&mut app, "now-bot-light");

    send(&mut app, Message::Go(Screen::Bot(bot("bot-act-with-approval"))));
    camera.shoot(&mut app, "control-bot-light");
    app.apply(Message::Tab(inspector::Tab::Bot(inspector::BotTab::Rules)));
    camera.shoot(&mut app, "control-advanced-light");
    app.apply(Message::Tab(inspector::Tab::Bot(inspector::BotTab::Now)));

    send(&mut app, Message::Go(Screen::Bot(bot("bot-wrx80"))));
    app.apply(Message::ChatInput("How many files are in the workspace?".into()));
    send(&mut app, Message::ChatSend);
    app.apply(Message::ChatInput("Then list the three biggest".into()));
    camera.shoot(&mut app, "chat-light");
    app.apply(Message::ChatInput(String::new()));

    send(&mut app, Message::Go(Screen::Bots));
    app.apply(Message::RegisterOpen);
    app.apply(Message::RegisterName("disk-watch".into()));
    app.apply(Message::RegisterMission(
        "Check the free disk space on every node each hour and tell me when one falls under 10 %.".into(),
    ));
    camera.shoot(&mut app, "new-bot-light");
    app.apply(Message::RegisterAdvanced(true));
    camera.shoot(&mut app, "new-bot-advanced-light");
    app.apply(Message::RegisterClose);

    send(&mut app, Message::Go(Screen::Settings));
    app.apply(Message::SettingsCa(
        "/home/operator/.local/state/axlr-bots/ca/ca.crt".into(),
    ));
    app.apply(Message::SettingsToken(
        "/home/operator/.local/state/axlr-bots/api.token".into(),
    ));
    camera.both(&mut app, "settings");
    app.apply(Message::SettingsJsonOpen);
    camera.shoot(&mut app, "settings-json-light");
    app.apply(Message::SettingsJsonClose);

    // A message to a running task, and what its run says back.
    let fake = Arc::new(FakeControlPlane::sample("2026-10-25T10:00:00Z".parse().unwrap()));
    let mut app = app_over(fake.clone());
    send(&mut app, Message::SettingsSave);
    send(&mut app, Message::Go(Screen::Bot(bot("bot"))));
    let running = app.running_task(&app.bot.as_ref().unwrap().bot.clone()).unwrap();
    app.apply(Message::ChatInput("Say the date too, please.".into()));
    let effects = app.apply(Message::ChatSend);
    drive(&mut app, effects);
    fake.say(&running, "OK, I will say the date too: 25 October 2026.");
    let tick = app.now + chrono::Duration::seconds(5);
    send(&mut app, Message::Tick(tick));
    camera.shoot(&mut app, "live-chat-light");

    // A call that publishes a post: the post drawn as it would read.
    let publishing = fake.add_waiting_call(
        "bot-act-with-approval",
        "Propose today's post about AI news",
        super::view_tests::publishing_call(),
    );
    send(&mut app, Message::Go(Screen::Task(publishing)));
    camera.both(&mut app, "task-post-draft");
}

/// The screens of the redesign at the size of a real fleet, light and
/// dark, into the directory `PANEL_SHOTS_DIR` names: what a slice of
/// the redesign is looked at before it is called done. It never writes
/// into `docs/screenshots`.
#[test]
#[ignore = "writes into PANEL_SHOTS_DIR; run it with --ignored"]
fn redesign_shots() {
    let out = std::env::var("PANEL_SHOTS_DIR").expect("PANEL_SHOTS_DIR names where the shots go");
    let camera = Camera::into(PathBuf::from(out));
    let fake = Arc::new(FakeControlPlane::large("2026-10-25T10:00:00Z".parse().unwrap()));
    let mut app = app_over(fake);
    send(&mut app, Message::SettingsSave);

    camera.both(&mut app, "s1-home");
    send(&mut app, Message::Go(Screen::Tasks));
    camera.both(&mut app, "s1-tasks");
    send(&mut app, Message::Go(Screen::Bots));
    camera.both(&mut app, "s1-bots");
    send(&mut app, Message::Go(Screen::Nodes));
    camera.both(&mut app, "s1-nodes");
    send(&mut app, Message::Go(Screen::Settings));
    camera.both(&mut app, "s1-settings");

    // The same lists as a person changes them: compact rows, the
    // revoked nodes shown, a closed team opened, rows picked, a form
    // open over the rows.
    send(&mut app, Message::SetDensity(crate::application::Density::Compact));
    send(&mut app, Message::Go(Screen::Tasks));
    camera.shoot(&mut app, "s1-tasks-compact-light");
    send(&mut app, Message::SetDensity(crate::application::Density::Comfortable));
    camera.shoot_at(&mut app, "s1-tasks-small-light", super::runtime::MIN_WINDOW);
    send(&mut app, Message::Go(Screen::Overview));
    camera.shoot_at(&mut app, "s1-home-small-light", super::runtime::MIN_WINDOW);
    send(&mut app, Message::Go(Screen::Tasks));
    send(&mut app, Message::FilterStatus(Some(TaskStatus::Failed)));
    send(&mut app, Message::PickShown(table::List::Tasks, true));
    camera.shoot(&mut app, "s1-tasks-picked-light");
    send(&mut app, Message::ClearPicks(table::List::Tasks));
    send(&mut app, Message::FilterStatus(None));
    app.apply(Message::SubmitOpen(None));
    camera.shoot(&mut app, "s1-tasks-new-light");
    app.apply(Message::SubmitClose);
    send(&mut app, Message::Go(Screen::Nodes));
    send(&mut app, Message::ShowRevoked(true));
    camera.shoot(&mut app, "s1-nodes-revoked-light");
    send(&mut app, Message::ShowRevoked(false));
    app.apply(Message::MintOpen);
    send(&mut app, Message::MintSubmit);
    camera.shoot(&mut app, "s1-nodes-token-light");
    app.apply(Message::MintClose);
    send(&mut app, Message::Go(Screen::Bots));
    send(&mut app, Message::GroupToggled(find::GroupKey::Team("docs".into())));
    send(&mut app, Message::ShowBots(table::BotShow::Paused));
    camera.shoot(&mut app, "s1-bots-paused-light");
    send(&mut app, Message::ShowBots(table::BotShow::All));
    app.apply(Message::RegisterOpen);
    app.apply(Message::RegisterAdvanced(true));
    camera.shoot(&mut app, "s1-bots-new-light");
    app.apply(Message::RegisterClose);
    send(&mut app, Message::Go(Screen::Teams));
    camera.shoot(&mut app, "s1-teams-light");

    // The pages the frame and the sizes changed without rebuilding them.
    let waiting = task_with(&app, TaskStatus::WaitingApproval);
    send(&mut app, Message::Go(Screen::Task(waiting)));
    camera.shoot(&mut app, "s1-task-light");
    send(&mut app, Message::Go(Screen::Bot(bot("bot"))));
    camera.shoot(&mut app, "s1-bot-light");
    send(&mut app, Message::Go(Screen::Team("ops".into())));
    camera.shoot(&mut app, "s1-team-light");
}

/// The second slice of the redesign at the size of a real fleet, light
/// and dark, into `PANEL_SHOTS_DIR`: the Tasks list folded, grouped,
/// filtered and with the keyboard on a row; the Bots list filtered; the
/// palette empty and searching.
#[test]
#[ignore = "writes into PANEL_SHOTS_DIR; run it with --ignored"]
fn redesign_shots_s2() {
    let out = std::env::var("PANEL_SHOTS_DIR").expect("PANEL_SHOTS_DIR names where the shots go");
    let camera = Camera::into(PathBuf::from(out));
    let fake = Arc::new(FakeControlPlane::large("2026-10-25T10:00:00Z".parse().unwrap()));
    let mut app = app_over(fake);
    send(&mut app, Message::SettingsSave);

    send(&mut app, Message::Go(Screen::Tasks));
    camera.both(&mut app, "s2-tasks-folded");

    send(&mut app, Message::GroupingCycled);
    let first = app.task_groups()[0].key.clone();
    send(&mut app, Message::GroupToggled(first));
    camera.both(&mut app, "s2-tasks-grouped");
    send(&mut app, Message::GroupingCycled);
    send(&mut app, Message::GroupingCycled);
    send(&mut app, Message::GroupingCycled);

    send(&mut app, Message::ListFilterTyped("gate".into()));
    camera.both(&mut app, "s2-tasks-filtered");
    send(&mut app, Message::ListFilterTyped(String::new()));

    // The keyboard on a line that folds the repeats of a timer.
    for _ in 0..6 {
        send(&mut app, Message::Key(Keypress::Down));
    }
    camera.both(&mut app, "s2-tasks-cursor");
    send(&mut app, Message::Key(Keypress::Escape));

    send(&mut app, Message::Go(Screen::Bots));
    send(&mut app, Message::ListFilterTyped("lead".into()));
    camera.both(&mut app, "s2-bots-filtered");
    send(&mut app, Message::ListFilterTyped(String::new()));

    send(&mut app, Message::Go(Screen::Tasks));
    send(&mut app, Message::PaletteOpen);
    camera.both(&mut app, "s2-palette-empty");
    send(&mut app, Message::PaletteTyped("ops".into()));
    send(&mut app, Message::Key(Keypress::Down));
    camera.both(&mut app, "s2-palette-query");
    send(&mut app, Message::PaletteClose);

    // The smallest window.
    send(&mut app, Message::PaletteOpen);
    camera.shoot_at(&mut app, "s2-palette-small-light", super::runtime::MIN_WINDOW);
    send(&mut app, Message::PaletteClose);
    camera.shoot_at(&mut app, "s2-tasks-small-light", super::runtime::MIN_WINDOW);
}

/// The third slice of the redesign at the size of a real fleet, light
/// and dark, into `PANEL_SHOTS_DIR`: a task inspected beside its list on
/// its Activity, found in, with a step open and on its Answer; a task
/// that waits for a person; a bot on Now and on its rules; a node; a
/// team; a running task scrolled up in, with Jump to latest.
#[test]
#[ignore = "writes into PANEL_SHOTS_DIR; run it with --ignored"]
fn redesign_shots_s3() {
    let out = std::env::var("PANEL_SHOTS_DIR").expect("PANEL_SHOTS_DIR names where the shots go");
    let camera = Camera::into(PathBuf::from(out));
    let fake = Arc::new(FakeControlPlane::large("2026-10-25T10:00:00Z".parse().unwrap()));
    let mut app = app_over(fake.clone());
    send(&mut app, Message::SettingsSave);

    // The newest task of the bot that deploys has the long run.
    let long = {
        let fleet = app.fleet.as_ref().unwrap();
        fleet
            .tasks
            .iter()
            .find(|t| t.bot_id.as_str() == "ops-deploy")
            .unwrap()
            .id
            .clone()
    };
    send(&mut app, Message::Go(Screen::Tasks));
    send(&mut app, Message::RowPicked(table::RowRef::Task(long.clone())));
    camera.both(&mut app, "s3-task-activity");
    send(&mut app, Message::FindTyped("tests".into()));
    send(&mut app, Message::FindNext);
    camera.both(&mut app, "s3-task-find");
    send(&mut app, Message::FindTyped(String::new()));
    send(&mut app, Message::ActivityPick(0));
    camera.both(&mut app, "s3-task-step");
    send(&mut app, Message::ActivityPick(0));
    send(&mut app, Message::Tab(inspector::Tab::Task(inspector::TaskTab::Answer)));
    camera.both(&mut app, "s3-task-answer");
    send(
        &mut app,
        Message::Tab(inspector::Tab::Task(inspector::TaskTab::Activity)),
    );

    // A call that publishes a post, waiting for a person.
    let publishing = fake.add_waiting_call(
        "ops-report",
        "Propose today's post about the release",
        super::view_tests::publishing_call(),
    );
    let refresh = app.refresh_now();
    drive(&mut app, refresh);
    send(&mut app, Message::Go(Screen::Task(publishing)));
    camera.both(&mut app, "s3-task-waiting");

    send(&mut app, Message::Go(Screen::Bot(bot("ops-lead"))));
    camera.both(&mut app, "s3-bot-now");
    send(&mut app, Message::Tab(inspector::Tab::Bot(inspector::BotTab::Rules)));
    camera.both(&mut app, "s3-bot-rules");
    send(&mut app, Message::Tab(inspector::Tab::Bot(inspector::BotTab::Now)));

    send(
        &mut app,
        Message::Go(Screen::Node(crate::domain::NodeId::new("gx10-ff95").unwrap())),
    );
    camera.both(&mut app, "s3-node");

    // The Teams list opens on its first team; a team at work shows what
    // happens in it.
    send(&mut app, Message::Go(Screen::Teams));
    send(&mut app, Message::Go(Screen::Team("ops".into())));
    camera.both(&mut app, "s3-team");
    send(
        &mut app,
        Message::Tab(inspector::Tab::Team(inspector::TeamTab::Members)),
    );
    camera.shoot(&mut app, "s3-team-members-light");
    send(
        &mut app,
        Message::Tab(inspector::Tab::Team(inspector::TeamTab::Happens)),
    );

    // The running task, scrolled up in: it stops following its newest
    // step and offers to jump back to it.
    let running = task_with(&app, TaskStatus::Running);
    send(&mut app, Message::Go(Screen::Task(running)));
    send(&mut app, Message::ActivityScrolled(LiveStream::Steps, false));
    camera.both(&mut app, "s3-task-live");

    // The smallest window: the list narrower, the find's chips on a
    // line of their own.
    send(&mut app, Message::Go(Screen::Task(long)));
    camera.shoot_at(&mut app, "s3-task-small-light", super::runtime::MIN_WINDOW);
}

/// The organic hover on the real screens, light and dark, into
/// `PANEL_SHOTS_DIR`: a task's row, a card, a number and a bot's tile
/// of Home, and the inspector's header, each with the pointer on it and
/// its glow whole; and the Settings screen with the hover glow's switch.
#[test]
#[ignore = "writes into PANEL_SHOTS_DIR; run it with --ignored"]
fn redesign_shots_s4() {
    let out = std::env::var("PANEL_SHOTS_DIR").expect("PANEL_SHOTS_DIR names where the shots go");
    let camera = Camera::into(PathBuf::from(out));
    let fake = Arc::new(FakeControlPlane::large("2026-10-25T10:00:00Z".parse().unwrap()));
    let mut app = app_over(fake);
    send(&mut app, Message::SettingsSave);

    // Home: the Fleet card where no tile is, a number, a tile.
    let team = place_of(&app, "billing");
    let pointer = Point::new(team.x + 20.0, team.y + 60.0);
    hover_at(&mut app, pointer, 3.0);
    camera.both_at(&mut app, "s4-home-card", Some(pointer));
    let number = place_of(&app, "running");
    let pointer = Point::new(number.x + 40.0, number.y + 22.0);
    hover_at(&mut app, pointer, 5.0);
    camera.both_at(&mut app, "s4-home-number", Some(pointer));
    let tile = place_of(&app, "deploy");
    let pointer = Point::new(tile.x + tile.width * 0.6, tile.center_y());
    hover_at(&mut app, pointer, 7.0);
    camera.both_at(&mut app, "s4-home-tile", Some(pointer));

    // A row of the Tasks list.
    send(&mut app, Message::Go(Screen::Tasks));
    let row = place_of(&app, "Read the missing file");
    let pointer = Point::new(row.x + 260.0, row.center_y());
    hover_at(&mut app, pointer, 4.0);
    camera.both_at(&mut app, "s4-task-row", Some(pointer));

    // The inspector's header over a task.
    let long = {
        let fleet = app.fleet.as_ref().unwrap();
        fleet
            .tasks
            .iter()
            .find(|t| t.bot_id.as_str() == "ops-deploy")
            .unwrap()
            .id
            .clone()
    };
    send(&mut app, Message::RowPicked(table::RowRef::Task(long)));
    let tab = place_of(&app, "Activity");
    // Under the top bar's 44 points, on the title's line.
    let pointer = Point::new(tab.x + 360.0, 44.0 + 50.0);
    hover_at(&mut app, pointer, 6.0);
    camera.both_at(&mut app, "s4-inspector-head", Some(pointer));

    // The Settings screen with the switch, on and off.
    send(&mut app, Message::Go(Screen::Settings));
    camera.both(&mut app, "s4-settings");
    send(&mut app, Message::SetMotion(crate::application::Motion::Off));
    camera.shoot(&mut app, "s4-settings-off-light");
}
