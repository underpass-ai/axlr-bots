//! The README's screenshots, drawn headless from the sample bots with
//! Iced's simulator, the panel's own typefaces and its window size.
//! Not part of the suite: run it to write `docs/screenshots/*.png` again
//! (`ICED_TEST_BACKEND=tiny-skia TZ=UTC cargo test screenshots -- --ignored`).

use std::fs;
use std::path::{Path, PathBuf};
use std::sync::Arc;

use iced_test::Simulator;

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
        let _ = app.apply(Message::DismissBanner);
        let _turn = turn();
        let theme = Tokens::of(app.mode).theme();
        let mut ui = Simulator::with_size(panel_settings(), WINDOW, view::view(app));
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
        for mode in [ThemeMode::Light, ThemeMode::Dark] {
            app.mode = mode;
            let suffix = if mode == ThemeMode::Dark { "dark" } else { "light" };
            self.shoot(app, &format!("{name}-{suffix}"));
        }
        app.mode = ThemeMode::Light;
    }
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
    app.screen = Screen::Activity(root.clone());
    let mut tree = ActivityScreen::new(root);
    tree.loaded(super::view_tests::delegating_activity());
    app.activity = Some(tree);
    camera.both(&mut app, "task-activity");
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
    app.apply(Message::ShowAdvanced(true));
    camera.both(&mut app, "bot");
    app.apply(Message::ShowAdvanced(false));

    send(&mut app, Message::Go(Screen::Bot(bot("bot"))));
    camera.shoot(&mut app, "now-bot-light");

    send(&mut app, Message::Go(Screen::Bot(bot("bot-act-with-approval"))));
    camera.shoot(&mut app, "control-bot-light");
    app.apply(Message::ShowAdvanced(true));
    camera.shoot(&mut app, "control-advanced-light");
    app.apply(Message::ShowAdvanced(false));

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
    send(&mut app, Message::FilterStatus(Some(TaskStatus::Failed)));
    send(&mut app, Message::PickShown(table::List::Tasks, true));
    camera.shoot(&mut app, "s1-tasks-picked-light");
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
    send(&mut app, Message::FoldTeam("docs".into()));
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
