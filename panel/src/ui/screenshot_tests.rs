//! The README's screenshots, drawn headless from the sample bots with
//! Iced's simulator, the panel's own typefaces and its window size.
//! Not part of the suite: run it to write `docs/screenshots/*.png` again
//! (`ICED_TEST_BACKEND=tiny-skia TZ=UTC cargo test screenshots -- --ignored`).

use std::borrow::Cow;
use std::fs;
use std::path::{Path, PathBuf};
use std::sync::Arc;

use iced::{Settings, Size};
use iced_test::Simulator;

use super::test_runtime::{connected, drive, send};
use super::view::tests::app_over;
use super::view::widgets;
use super::view_tests::turn;
use super::*;
use crate::adapters::fake::FakeControlPlane;
use crate::domain::{BotId, TaskStatus, ToolRule};

/// The window's size at start (`runtime::launch`); the simulator draws
/// it at twice the pixels.
const WINDOW: Size = Size::new(1360.0, 860.0);

const FONTS: [&[u8]; 4] = [
    include_bytes!("../../assets/fonts/Lexend-Regular.ttf"),
    include_bytes!("../../assets/fonts/Lexend-SemiBold.ttf"),
    include_bytes!("../../assets/fonts/Lexend-Bold.ttf"),
    include_bytes!("../../assets/fonts/DotGothic16-Latin.ttf"),
];

/// Draws the panel as it stands and writes it as `<name>.png`.
struct Camera {
    out: PathBuf,
    fonts_loaded: bool,
}

impl Camera {
    fn new() -> Self {
        let out = Path::new(env!("CARGO_MANIFEST_DIR")).join("docs/screenshots");
        fs::create_dir_all(&out).unwrap();
        Self {
            out,
            fonts_loaded: false,
        }
    }

    /// Draws the panel without the last act's banner ("Connected", "Task
    /// sent"), which would sit on top of every shot.
    fn shoot(&mut self, app: &mut App, name: &str) {
        let _ = app.apply(Message::DismissBanner);
        let _turn = turn();
        // The typefaces go into the renderer's shared font system once.
        let fonts = if self.fonts_loaded {
            Vec::new()
        } else {
            FONTS.iter().map(|font| Cow::Borrowed(*font)).collect()
        };
        self.fonts_loaded = true;
        let settings = Settings {
            fonts,
            default_font: widgets::REGULAR,
            ..Settings::default()
        };
        let theme = Tokens::of(app.mode).theme();
        let mut ui = Simulator::with_size(settings, WINDOW, view::view(app));
        let snapshot = ui.snapshot(&theme).unwrap();
        // `matches_image` writes a new file named after the renderer, so
        // it writes into an empty directory and the file is moved.
        let scratch = tempfile::tempdir().unwrap();
        assert!(snapshot.matches_image(scratch.path().join(name)).unwrap());
        let drawn = fs::read_dir(scratch.path()).unwrap().next().unwrap().unwrap().path();
        let _ = fs::copy(drawn, self.out.join(format!("{name}.png"))).unwrap();
    }

    /// The same state, light and dark.
    fn both(&mut self, app: &mut App, name: &str) {
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
    let mut camera = Camera::new();
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
}
