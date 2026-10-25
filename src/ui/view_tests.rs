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
            "Needs you",
            "Review",
            "gx10-ff95",
            "Latest tasks",
            "2/2 nodes online",
        ],
    );
    let sent = messages_of_click(&app, "Review");
    assert!(matches!(sent.as_slice(), [Message::Go(Screen::Task(_))]));
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
    shows(&app, &["New bot", "Mission", "Add bot", "Runs on"]);
    send(
        &mut app,
        Message::Go(Screen::Bot(BotId::new("bot-act-with-approval").unwrap())),
    );
    shows(
        &app,
        &[
            "Mission",
            "Writes only after a person says OK.",
            "Budgets",
            "5m",
            "Events",
            "Latest tasks",
            "New task",
        ],
    );
    let sent = messages_of_click(&app, "New task");
    assert!(matches!(sent.as_slice(), [Message::SubmitOpen(Some(_))]));
}

#[test]
fn the_tasks_screen_filters_and_sends() {
    let mut app = connected();
    send(&mut app, Message::Go(Screen::Tasks));
    shows(
        &app,
        &[
            "Tasks",
            "All 4",
            "Waiting 1",
            "Failed 1",
            "Write hello into hello.txt",
            "Open",
        ],
    );
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
    send(&mut app, Message::Go(Screen::Task(done)));
    shows(&app, &["Answer", "auto OK", "wrx80"]);
    assert_eq!(
        app.answer.as_ref().map(|(source, _)| source.as_str()),
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
