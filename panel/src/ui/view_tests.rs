//! The screens drawn headless with Iced's simulator: what each shows, and
//! what a click on it sends.

use std::sync::{Mutex, MutexGuard};

use iced_test::simulator;

use super::test_runtime::{connected, send};
use super::view::tests::app;
use super::*;
use crate::domain::{BotId, NodeId, TaskStatus};

/// The headless renderer is not safe to run on two threads at once: the
/// simulators take turns.
static SIMULATOR: Mutex<()> = Mutex::new(());

fn turn() -> MutexGuard<'static, ()> {
    SIMULATOR.lock().unwrap_or_else(|poisoned| poisoned.into_inner())
}

fn messages_of_click(app: &App, label: &str) -> Vec<Message> {
    let _turn = turn();
    let mut ui = simulator(view::view(app));
    ui.click(label)
        .unwrap_or_else(|e| panic!("cannot click {label}: {e:?}"));
    ui.into_messages().collect()
}

fn simulator_misses(app: &App, word: &str) -> bool {
    let _turn = turn();
    let mut ui = simulator(view::view(app));
    ui.find(word).is_err()
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
            "expired today",
            "Needs you",
            "Review",
            "gx10-ff95",
            "Latest tasks",
            "2/2 nodes online",
            "127.0.0.1:7879",
        ],
    );
    shows(&app, &["wants local_write · hello", "Approve", "Deny"]);
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
    let sent = messages_of_click(&app, "expired today");
    assert!(matches!(
        sent.as_slice(),
        [Message::FilterStatus(Some(TaskStatus::Expired))]
    ));
    let sent = messages_of_click(&app, "Nodes");
    assert!(matches!(sent.as_slice(), [Message::Go(Screen::Nodes)]));
    app.apply(Message::ToggleTheme);
    shows(&app, &["Home", "cost today"]);
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
            "gpu=rtx3090",
            "364 days",
            "0.14.0",
            "Revoke",
        ],
    );
    if let Some(fleet) = app.fleet.as_mut() {
        fleet.nodes[1].capabilities.dedicated = true;
    }
    shows(&app, &["dedicated"]);
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
            "bot-act-with-approval",
            "Ask first",
            "claude-haiku-5.5",
            "wrx80",
            "From file",
            "Pause",
        ],
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
    send(
        &mut app,
        Message::Go(Screen::Bot(BotId::new("bot-act-with-approval").unwrap())),
    );
    shows(
        &app,
        &[
            "Pause",
            "Stop",
            "It asks before acting",
            "wants local_write · hello",
            "Approve",
            "What it does",
            "Events",
            "Recent work",
            "New task",
            "Copy mission",
            "Advanced",
            "Now",
            "Waits for you",
            "It waits for your OK before it goes on.",
        ],
    );
    assert!(matches!(
        messages_of_click(&app, "Open task").as_slice(),
        [Message::Go(Screen::Task(_))]
    ));
    assert!(
        simulator_misses(&app, "How much may it do alone?"),
        "Advanced is closed by default"
    );
    let sent = messages_of_click(&app, "Copy mission");
    assert!(matches!(
        sent.as_slice(),
        [Message::Copy(mission)] if mission == "Do what each task asks, within the policy."
    ));
    assert!(!app.feed().unwrap().events.is_empty(), "the sample bot has events");
    assert!(matches!(
        messages_of_click(&app, "Copy events").as_slice(),
        [Message::Copy(events)] if !events.is_empty()
    ));
    let sent = messages_of_click(&app, "New task");
    assert!(matches!(sent.as_slice(), [Message::SubmitOpen(Some(_))]));
    assert!(matches!(
        messages_of_click(&app, "Approve").as_slice(),
        [Message::Approve(_)]
    ));
    assert!(matches!(
        messages_of_click(&app, "Stop").as_slice(),
        [Message::AskStopBot(_)]
    ));
    assert!(matches!(
        messages_of_click(&app, "Advanced").as_slice(),
        [Message::ShowAdvanced(true)]
    ));

    app.apply(Message::ShowAdvanced(true));
    shows(
        &app,
        &[
            "How much may it do alone?",
            "Ask me before acting",
            "Act on its own within its rules",
            "Only watch and report",
            "Starts by",
            "Budgets",
            "Hide advanced",
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
    app.apply(Message::ShowAdvanced(true));
    shows(&app, &["Propose only"]);
    send(&mut app, Message::Go(Screen::Bots));
    assert!(!app.advanced, "Advanced closes when the page changes");
}

#[test]
fn the_tasks_screen_filters_and_sends() {
    let mut app = connected();
    send(&mut app, Message::Go(Screen::Tasks));
    shows(
        &app,
        &[
            "Tasks",
            "All 5",
            "Waiting 1",
            "Failed 1",
            "Expired 1",
            "Write hello into hello.txt",
            "Open",
        ],
    );
    assert!(matches!(
        messages_of_click(&app, "Approve").as_slice(),
        [Message::Approve(_)]
    ));
    app.deciding = app.fleet.as_ref().unwrap().waiting().first().map(|t| t.id.clone());
    assert!(messages_of_click(&app, "Approve").is_empty(), "one answer at a time");
    app.deciding = None;
    let sent = messages_of_click(&app, "Failed 1");
    assert!(matches!(
        sent.as_slice(),
        [Message::FilterStatus(Some(TaskStatus::Failed))]
    ));
    app.apply(Message::FilterStatus(Some(TaskStatus::Completed)));
    shows(&app, &["List the files in your workspace"]);
    app.apply(Message::SubmitOpen(Some(BotId::new("bot-wrx80").unwrap())));
    app.apply(Message::SubmitObjective("Count the files".into()));
    shows(&app, &["New task", "Send", "key test-1: sending twice makes one task"]);
    assert!(matches!(
        messages_of_click(&app, "Send").as_slice(),
        [Message::SubmitSend]
    ));
}

#[test]
fn a_waiting_task_shows_the_call_and_its_answers() {
    let mut app = connected();
    let id = app.fleet.as_ref().unwrap().waiting()[0].id.clone();
    send(&mut app, Message::Go(Screen::Task(id)));
    shows(
        &app,
        &[
            "Needs your OK",
            "It asked you",
            "local_write",
            "hello.txt",
            "Approve",
            "Deny",
            "Runs",
            "Timeline",
            "Needs OK",
            "gx10-ff95",
            "trace carried",
        ],
    );
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
    shows(&app, &["Sending your answer…"]);
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
    shows(&app, &["Its rules: allows local_list(path=*)"]);
    shows(
        &app,
        &[
            "Answer",
            "Steps",
            "Allowed by its rules",
            "done",
            "40 ms",
            "wrx80",
            "Copy task",
            "Copy answer",
            "Select text",
            "Copy timeline",
        ],
    );
    assert!(matches!(
        messages_of_click(&app, "Copy answer").as_slice(),
        [Message::Copy(answer)] if answer == "There are 3 files."
    ));
    assert!(matches!(
        messages_of_click(&app, "Copy timeline").as_slice(),
        [Message::Copy(timeline)] if timeline.contains("Done")
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
    // A long objective shows whole under the header's first line.
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
    shows(&app, &["Now", "Working", "Say the time", "Waits for you", "Idle"]);
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
            "Talk to it",
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
    fake.set_groups("bot", &["port"]);
    fake.set_groups("bot-wrx80", &["port"]);
    fake.set_groups("bot-act-with-approval", &["lab"]);
    let tick = app.now + chrono::Duration::seconds(3);
    send(&mut app, Message::Tick(tick));
    fake.publish(
        "bot.event.emitted",
        "bot",
        serde_json::json!({"name": "recall", "severity": "warning", "message": "a ship is entering", "to": ["bot-wrx80"]}),
    );
    send(&mut app, Message::Tick(tick + chrono::Duration::seconds(3)));
    shows(
        &app,
        &["port", "2 bots", "lab", "only one bot: a typo?", "bot · bot-wrx80"],
    );
    let sent = messages_of_click(&app, "Open");
    assert!(matches!(sent.as_slice(), [Message::Go(Screen::Team(name))] if name == "lab"));
    send(&mut app, Message::Go(Screen::Team("port".into())));
    shows(
        &app,
        &[
            "Team port",
            "Who is in it",
            "What happens",
            "Warning to the team",
            "bot → bot-wrx80: a ship is entering",
        ],
    );
    send(&mut app, Message::Go(Screen::Team("sea".into())));
    shows(&app, &["No bot is in this team now"]);
}

#[test]
fn a_bot_page_shows_its_team_and_recalls_a_vehicle() {
    let mut app = connected();
    send(&mut app, Message::Go(Screen::Bots));
    app.apply(Message::RegisterOpen);
    shows(
        &app,
        &["Which team is it in?", "a name its teammates share; empty: on its own"],
    );
    app.apply(Message::RegisterName("pilot-quad-1".into()));
    app.apply(Message::RegisterMission("Fly quad-1.".into()));
    app.apply(Message::RegisterTools("local_read, return_home".into()));
    app.apply(Message::RegisterTeam("port".into()));
    send(&mut app, Message::RegisterSubmit);
    send(&mut app, Message::Enable(BotId::new("pilot-quad-1").unwrap()));
    send(&mut app, Message::Go(Screen::Bot(BotId::new("pilot-quad-1").unwrap())));
    shows(&app, &["Team", "port", "Change", "Recall"]);
    let sent = messages_of_click(&app, "Recall");
    assert!(matches!(sent.as_slice(), [Message::Recall(id)] if id.as_str() == "pilot-quad-1"));
    let sent = messages_of_click(&app, "Change");
    assert!(matches!(sent.as_slice(), [Message::TeamEdit(_, words)] if words == "port"));
    app.apply(Message::TeamEdit(BotId::new("pilot-quad-1").unwrap(), "port".into()));
    shows(&app, &["Save", "Cancel"]);
    send(&mut app, Message::Go(Screen::Bot(BotId::new("bot").unwrap())));
    shows(&app, &["In no team: it knows no other bot"]);
    assert!(simulator_misses(&app, "Recall"));
}

#[test]
fn a_task_says_it_was_urgent_and_why_it_stopped() {
    let mut app = connected();
    app.apply(Message::SubmitOpen(Some(BotId::new("bot-wrx80").unwrap())));
    shows(&app, &["Urgent"]);
    app.apply(Message::SubmitUrgent(true));
    shows(&app, &["urgent: it goes first and stops what the bot is doing"]);
    app.apply(Message::SubmitObjective("Come back".into()));
    send(&mut app, Message::SubmitSend);
    shows(&app, &["urgent", "Told by", "nobody: not a teammate's event"]);
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
    shows(&app, &["check row F", "OK, row F next.", "You wrote", "It answered"]);
    assert!(matches!(
        messages_of_click(&app, "OK, row F next.").as_slice(),
        [Message::Go(Screen::Task(id))] if *id == running
    ));
}
