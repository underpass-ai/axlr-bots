use chrono::Duration;

use super::test_runtime::{connected, drive, send};
use super::view::tests::{app, config};
use super::*;
use crate::application::PanelError;
use crate::domain::{AutonomyLevel, BotId, BotStatus, NodeId, NodeStatus, TaskId, TaskStatus};

fn task_in(app: &App, status: TaskStatus) -> TaskId {
    app.fleet
        .as_ref()
        .unwrap()
        .tasks
        .iter()
        .find(|t| t.status == status)
        .unwrap()
        .id
        .clone()
}

#[test]
fn the_first_run_saves_the_settings_once_the_control_plane_answers() {
    let mut app = app();
    assert_eq!(app.screen, Screen::Settings);
    assert!(!app.settings.saved && !app.connected());
    app.apply(Message::SettingsUrl("https://10.9.9.9:7879".into()));
    send(&mut app, Message::SettingsSave);
    assert!(!app.connected());
    assert_eq!(app.banner.as_ref().unwrap().title, "Cannot connect");
    assert_eq!(app.screen, Screen::Settings);
    app.apply(Message::SettingsUrl("https://127.0.0.1:7879".into()));
    app.apply(Message::SettingsCa("/ca.crt".into()));
    app.apply(Message::SettingsToken("/api.token".into()));
    send(&mut app, Message::SettingsSave);
    assert!(app.connected() && app.settings.saved);
    assert_eq!(app.screen, Screen::Overview);
    let fleet = app.fleet.as_ref().unwrap();
    assert_eq!(fleet.nodes.len(), 2);
    assert_eq!(app.waiting_count(), 1);
    assert!(app.last_refresh.is_some());
    assert_eq!(app.banner.as_ref().unwrap().title, "Connected");
}

#[test]
fn saved_settings_connect_at_start_and_open_the_asked_screen() {
    let base = app();
    let launch = Launch {
        theme: ThemeMode::Dark,
        screen: Screen::Nodes,
    };
    let (mut started, effects) = App::new(base.services.clone(), launch, &config(), true);
    assert_eq!(
        effects,
        vec![Effect::Connect {
            config: config(),
            save: false
        }]
    );
    assert!(started.settings.busy);
    drive(&mut started, effects);
    assert_eq!((started.screen.clone(), started.mode), (Screen::Nodes, ThemeMode::Dark));
    assert!(started.fleet.is_some() && !started.settings.busy);

    let mut bad = config();
    bad.api_url = "https://10.9.9.9:7879".into();
    let (mut failing, effects) = App::new(base.services.clone(), Launch::default(), &bad, true);
    drive(&mut failing, effects);
    assert_eq!(failing.screen, Screen::Settings);
    assert!(matches!(failing.fleet_error, Some(PanelError::Unreachable(_))));
}

#[test]
fn ticks_refresh_once_at_a_time_and_drop_stale_answers() {
    let mut app = connected();
    let now = app.now;
    let first = app.apply(Message::Tick(now + Duration::seconds(3)));
    let Some(Effect::Refresh { generation }) = first.first().cloned() else {
        panic!("{first:?}")
    };
    assert!(
        app.apply(Message::Tick(now + Duration::seconds(6))).is_empty(),
        "one refresh at a time"
    );
    let again = app.apply(Message::Tick(now + Duration::seconds(30)));
    assert!(matches!(again.first(), Some(Effect::Refresh { generation: g }) if *g == generation + 1));
    let stale = Message::FleetLoaded {
        generation,
        result: Err(PanelError::Server("old".into())),
    };
    app.apply(stale);
    assert!(app.fleet_error.is_none(), "an older answer is dropped");
    let current = Message::FleetLoaded {
        generation: generation + 1,
        result: Err(PanelError::Unreachable("down".into())),
    };
    app.apply(current);
    assert!(matches!(app.fleet_error, Some(PanelError::Unreachable(_))));
    assert!(app.fleet.is_some(), "the last fleet stays on screen");
    drive(
        &mut app,
        vec![Effect::Refresh {
            generation: generation + 2,
        }],
    );
    assert!(
        app.fleet_error.is_some(),
        "an answer of a generation not asked for is dropped"
    );
    let effects = app.apply(Message::Tick(now + Duration::seconds(33)));
    drive(&mut app, effects);
    assert!(app.fleet_error.is_none());
}

#[test]
fn banners_go_away_and_can_be_closed() {
    let mut app = connected();
    assert!(app.banner.is_some());
    app.apply(Message::Tick(app.now + Duration::seconds(10)));
    assert!(app.banner.is_none());
    app.apply(Message::Copy("x".into()));
    assert!(app.banner.is_some());
    app.apply(Message::DismissBanner);
    assert!(app.banner.is_none());
    app.apply(Message::ToggleTheme);
    assert_eq!(app.mode, ThemeMode::Dark);
    app.apply(Message::SetTheme(ThemeMode::Light));
    assert_eq!(app.mode, ThemeMode::Light);
}

#[test]
fn a_waiting_task_is_approved_from_its_page() {
    let mut app = connected();
    let id = task_in(&app, TaskStatus::WaitingApproval);
    send(&mut app, Message::Go(Screen::Task(id.clone())));
    let detail = app.task.as_ref().unwrap();
    assert_eq!(detail.pending_call().unwrap().tool, "local_write");
    let effects = app.apply(Message::Approve(id.clone()));
    assert_eq!(
        effects,
        vec![Effect::Decide(id.clone(), crate::domain::Decision::Approve)]
    );
    assert!(
        app.apply(Message::Approve(id.clone())).is_empty(),
        "one decision at a time"
    );
    drive(&mut app, effects);
    assert!(app.deciding.is_none());
    assert_eq!(app.banner.as_ref().unwrap().title, "Approved");
    assert_eq!(app.task.as_ref().unwrap().task.status, TaskStatus::Running);
    assert!(app.task.as_ref().unwrap().pending_call().is_none());
    // A second decision is refused by the control plane.
    send(&mut app, Message::Approve(id));
    assert_eq!(app.banner.as_ref().unwrap().title, "Not now");
}

#[test]
fn a_deny_waits_for_the_second_click() {
    let mut app = connected();
    let id = task_in(&app, TaskStatus::WaitingApproval);
    send(&mut app, Message::Go(Screen::Task(id.clone())));
    assert!(app.apply(Message::AskDeny(id.clone())).is_empty());
    assert_eq!(app.confirm, Some(Confirm::Deny(id.clone())));
    app.apply(Message::ConfirmNo);
    assert!(app.confirm.is_none());
    app.apply(Message::AskDeny(id.clone()));
    send(&mut app, Message::ConfirmYes);
    assert_eq!(app.banner.as_ref().unwrap().title, "Denied");
    assert!(app.apply(Message::ConfirmYes).is_empty());
}

#[test]
fn a_task_is_sent_once_with_the_forms_key_and_opened() {
    let mut app = connected();
    assert!(app.apply(Message::SubmitSend).is_empty(), "no form open");
    app.apply(Message::FilterBot(BotChoice::One(BotId::new("bot-wrx80").unwrap())));
    send(&mut app, Message::SubmitOpen(None));
    assert_eq!(app.screen, Screen::Tasks);
    assert_eq!(app.submit.bot.as_ref().unwrap().as_str(), "bot-wrx80");
    let key = app.submit.key.clone().unwrap();
    assert!(key.as_str().starts_with("test-"));
    assert!(app.apply(Message::SubmitSend).is_empty(), "no words yet");
    app.apply(Message::SubmitBot(BotChoice::All));
    app.apply(Message::SubmitObjective("Count the files".into()));
    assert!(app.apply(Message::SubmitSend).is_empty(), "no bot");
    app.apply(Message::SubmitBot(BotChoice::One(BotId::new("bot-wrx80").unwrap())));
    let effects = app.apply(Message::SubmitSend);
    assert!(matches!(&effects[..], [Effect::Submit { key: k, .. }] if *k == key));
    assert!(app.apply(Message::SubmitSend).is_empty(), "already sending");
    drive(&mut app, effects);
    let Screen::Task(id) = app.screen.clone() else {
        panic!("{:?}", app.screen)
    };
    assert_eq!(app.task.as_ref().unwrap().task.id, id);
    assert_eq!(app.banner.as_ref().unwrap().title, "Task sent");
    assert!(!app.submit.open);
    // The same key again names the same task.
    let control_plane = app.services.control_plane.clone().unwrap();
    let again = futures::executor::block_on(control_plane.submit_task(crate::domain::TaskSubmission {
        bot_id: BotId::new("bot-wrx80").unwrap(),
        objective: crate::domain::Objective::new("Count the files").unwrap(),
        key,
    }));
    send(&mut app, Message::Submitted(again));
    assert_eq!(app.banner.as_ref().unwrap().title, "Already sent");
    app.apply(Message::SubmitOpen(Some(BotId::new("bot").unwrap())));
    app.apply(Message::SubmitClose);
    assert!(!app.submit.open);
    send(
        &mut app,
        Message::Submitted(Err(PanelError::State("bot is Paused".into()))),
    );
    assert_eq!(app.banner.as_ref().unwrap().title, "Not now");
}

#[test]
fn a_task_is_stopped_after_the_second_click() {
    let mut app = connected();
    let id = task_in(&app, TaskStatus::Running);
    app.apply(Message::AskCancel(id.clone()));
    let effects = app.apply(Message::ConfirmYes);
    assert_eq!(effects, vec![Effect::Cancel(id.clone())]);
    drive(&mut app, effects);
    assert_eq!(app.banner.as_ref().unwrap().title, "Task stopped");
    send(&mut app, Message::Cancelled(Err(PanelError::State("done".into()))));
    assert_eq!(app.banner.as_ref().unwrap().title, "Not now");
    send(&mut app, Message::Go(Screen::Task(id.clone())));
    app.apply(Message::AskCancel(id.clone()));
    send(&mut app, Message::ConfirmYes);
    assert_eq!(app.banner.as_ref().unwrap().title, "Already stopped");
}

#[test]
fn a_host_token_is_minted_shown_once_and_copied() {
    let mut app = connected();
    send(&mut app, Message::Go(Screen::Nodes));
    app.apply(Message::MintOpen);
    assert_eq!(app.mint.ttl, "1h");
    app.apply(Message::MintNodeId("web-2".into()));
    app.apply(Message::MintLabels("zone=eu".into()));
    app.apply(Message::MintTtl("2h".into()));
    let effects = app.apply(Message::MintSubmit);
    assert!(app.apply(Message::MintSubmit).is_empty(), "already minting");
    drive(&mut app, effects);
    let token = app.mint.token.clone().unwrap();
    assert_eq!(token.node_id.as_str(), "web-2");
    let copied = send(&mut app, Message::Copy(token.token.expose().to_string()));
    assert_eq!(copied, vec![token.token.expose().to_string()]);
    app.apply(Message::MintClose);
    assert!(app.mint.token.is_none() && !app.mint.open);
    app.apply(Message::MintOpen);
    app.apply(Message::MintLabels("zone".into()));
    send(&mut app, Message::MintSubmit);
    assert_eq!(app.banner.as_ref().unwrap().title, "Check the input");
}

#[test]
fn a_node_is_revoked_after_the_second_click() {
    let mut app = connected();
    let wrx80 = NodeId::new("wrx80").unwrap();
    app.apply(Message::AskRevoke(wrx80.clone()));
    assert_eq!(app.confirm, Some(Confirm::Revoke(wrx80.clone())));
    let effects = app.apply(Message::ConfirmYes);
    assert_eq!(effects, vec![Effect::Revoke(wrx80.clone())]);
    drive(&mut app, effects);
    assert_eq!(app.banner.as_ref().unwrap().title, "wrx80 revoked");
    let node = app
        .fleet
        .as_ref()
        .unwrap()
        .nodes
        .iter()
        .find(|n| n.id == wrx80)
        .unwrap();
    assert_eq!(node.status, NodeStatus::Revoked);
    send(&mut app, Message::Revoked(Err(PanelError::State("revoked".into()))));
    assert_eq!(app.banner.as_ref().unwrap().title, "Not now");
}

#[test]
fn a_bot_is_added_from_the_form_or_a_file_and_switched() {
    let mut app = connected();
    send(&mut app, Message::Go(Screen::Bots));
    app.apply(Message::RegisterOpen);
    assert!(app.register.form.tools.contains("local_read"));
    app.apply(Message::RegisterName("scribe".into()));
    app.apply(Message::RegisterMission("Write notes".into()));
    app.apply(Message::RegisterLevel(AutonomyLevel::Propose));
    app.apply(Message::RegisterTools("local_read".into()));
    app.apply(Message::RegisterModel("".into()));
    app.apply(Message::RegisterNode(NodeChoice::One(NodeId::new("wrx80").unwrap())));
    assert_eq!(app.register.form.node, "wrx80");
    app.apply(Message::RegisterNode(NodeChoice::Any));
    assert_eq!(app.register.form.node, "");
    let effects = app.apply(Message::RegisterSubmit);
    assert!(app.apply(Message::RegisterSubmit).is_empty());
    drive(&mut app, effects);
    let scribe = BotId::new("scribe").unwrap();
    assert_eq!(app.screen, Screen::Bot(scribe.clone()));
    let view = app.bot.as_ref().unwrap();
    assert_eq!((view.bot.status, view.feed.events.len()), (BotStatus::Registered, 1));
    send(&mut app, Message::Enable(scribe.clone()));
    assert_eq!(app.bot.as_ref().unwrap().bot.status, BotStatus::Enabled);
    assert_eq!(app.banner.as_ref().unwrap().title, "scribe is on");
    let effects = app.apply(Message::Pause(scribe.clone()));
    assert!(
        app.apply(Message::Enable(scribe.clone())).is_empty(),
        "one switch at a time"
    );
    drive(&mut app, effects);
    assert_eq!(app.bot.as_ref().unwrap().bot.status, BotStatus::Paused);
    send(&mut app, Message::Pause(scribe));
    assert_eq!(app.banner.as_ref().unwrap().title, "Not now");
    // A second register with the same name is a conflict.
    app.apply(Message::RegisterOpen);
    app.apply(Message::RegisterName("scribe".into()));
    app.apply(Message::RegisterMission("Again".into()));
    send(&mut app, Message::RegisterSubmit);
    assert_eq!(app.banner.as_ref().unwrap().title, "Already there");
    app.apply(Message::RegisterClose);
    assert!(!app.register.open);
}

#[test]
fn a_definition_file_is_picked_and_registered() {
    let mut app = connected();
    let effects = app.apply(Message::PickFile);
    assert_eq!(effects, vec![Effect::PickFile]);
    drive(&mut app, effects);
    assert!(!app.register.busy, "a cancelled dialog does nothing");
    let effects = app.apply(Message::FilePicked(Ok(Some("/none.json".into()))));
    assert_eq!(effects, vec![Effect::RegisterFile("/none.json".into())]);
    drive(&mut app, effects);
    assert_eq!(app.banner.as_ref().unwrap().title, "Cannot read file");
    app.apply(Message::FilePicked(Err(PanelError::File("portal".into()))));
    assert_eq!(app.banner.as_ref().unwrap().detail, "portal");
}

#[test]
fn a_bot_page_follows_its_events() {
    let mut app = connected();
    let bot = BotId::new("bot").unwrap();
    send(&mut app, Message::Go(Screen::Bot(bot.clone())));
    let before = app.feed().unwrap().events.len();
    assert!(before > 0);
    futures::executor::block_on(app.services.control_plane.clone().unwrap().pause_bot(bot.clone())).unwrap();
    let effects = app.apply(Message::Tick(app.now + Duration::seconds(3)));
    assert!(
        effects
            .iter()
            .any(|e| matches!(e, Effect::FollowBot(id, feed) if *id == bot && feed.events.len() == before))
    );
    drive(&mut app, effects);
    assert_eq!(app.feed().unwrap().new, 1);
    // An answer for a bot no longer on screen is dropped.
    send(&mut app, Message::Go(Screen::Bots));
    app.apply(Message::BotLoaded(bot.clone(), Err(PanelError::NotFound("x".into()))));
    assert!(app.banner.as_ref().is_none_or(|b| b.title != "Not found"));
    send(&mut app, Message::Go(Screen::Bot(BotId::new("ghost").unwrap())));
    assert_eq!(app.banner.as_ref().unwrap().title, "Not found");
}

#[test]
fn a_task_page_drops_answers_for_other_tasks() {
    let mut app = connected();
    let done = task_in(&app, TaskStatus::Completed);
    let failed = task_in(&app, TaskStatus::Failed);
    send(&mut app, Message::Go(Screen::Task(done.clone())));
    assert_eq!(app.task.as_ref().unwrap().task.id, done);
    let effects = app.apply(Message::Go(Screen::Task(failed.clone())));
    assert!(app.task.is_none(), "the other task's page is cleared");
    app.apply(Message::TaskLoaded(done, Err(PanelError::NotFound("x".into()))));
    drive(&mut app, effects);
    assert_eq!(app.task.as_ref().unwrap().task.id, failed);
    send(&mut app, Message::Go(Screen::Task(TaskId::new("task-none").unwrap())));
    assert_eq!(app.banner.as_ref().unwrap().title, "Not found");
}

#[test]
fn filters_open_the_task_list() {
    let mut app = connected();
    app.apply(Message::FilterStatus(Some(TaskStatus::Failed)));
    assert_eq!(app.screen, Screen::Tasks);
    let shown = app.filter.apply(&app.fleet.as_ref().unwrap().tasks);
    assert_eq!(shown.len(), 1);
    app.apply(Message::FilterStatus(Some(TaskStatus::Expired)));
    let shown = app.filter.apply(&app.fleet.as_ref().unwrap().tasks);
    assert_eq!(shown.len(), 1);
    assert_eq!(shown[0].status, TaskStatus::Expired);
    app.apply(Message::FilterBot(BotChoice::One(BotId::new("bot-wrx80").unwrap())));
    assert!(app.filter.apply(&app.fleet.as_ref().unwrap().tasks).is_empty());
    app.apply(Message::FilterBot(BotChoice::All));
    app.apply(Message::FilterStatus(None));
    assert_eq!(app.filter.apply(&app.fleet.as_ref().unwrap().tasks).len(), 5);
}

#[test]
fn without_a_connection_nothing_is_asked_of_the_control_plane() {
    let mut app = app();
    assert!(app.apply(Message::Tick(app.now)).is_empty());
    assert!(
        app.apply(Message::Go(Screen::Task(TaskId::new("task-1").unwrap())))
            .is_empty()
    );
    let message = futures::executor::block_on(app.services.clone().execute(Effect::Refresh { generation: 1 }));
    assert!(matches!(
        message,
        Some(Message::FleetLoaded {
            result: Err(PanelError::Config(_)),
            ..
        })
    ));
    for effect in [
        Effect::LoadTask(TaskId::new("t").unwrap()),
        Effect::FollowBot(BotId::new("b").unwrap(), Default::default()),
        Effect::Mint {
            node_id: String::new(),
            labels: String::new(),
            ttl: String::new(),
        },
        Effect::Revoke(NodeId::new("n").unwrap()),
        Effect::RegisterForm(Default::default()),
        Effect::RegisterFile("/x".into()),
        Effect::Enable(BotId::new("b").unwrap()),
        Effect::Pause(BotId::new("b").unwrap()),
        Effect::Submit {
            bot: BotId::new("b").unwrap(),
            objective: "x".into(),
            key: crate::domain::IdempotencyKey::new("k").unwrap(),
        },
        Effect::Cancel(TaskId::new("t").unwrap()),
        Effect::Decide(TaskId::new("t").unwrap(), crate::domain::Decision::Deny),
    ] {
        let message = futures::executor::block_on(app.services.clone().execute(effect.clone())).unwrap();
        let text = format!("{message:?}");
        assert!(text.contains("not connected"), "{effect:?} gave {text}");
    }
    assert!(futures::executor::block_on(app.services.clone().execute(Effect::Copy("x".into()))).is_none());
}

#[test]
fn the_answer_turns_into_text_to_select_and_keeps_the_selection_on_reload() {
    use iced::widget::text_editor::{Action, Edit};
    let mut app = connected();
    // Without an answer on screen, both messages do nothing.
    app.apply(Message::AnswerSelecting(true));
    app.apply(Message::AnswerSelect(Action::SelectAll));
    assert!(app.answer.is_none());
    let done = task_in(&app, TaskStatus::Completed);
    send(&mut app, Message::Go(Screen::Task(done)));
    app.apply(Message::AnswerSelecting(true));
    assert!(app.answer.as_ref().unwrap().selecting);
    app.apply(Message::AnswerSelect(Action::Edit(Edit::Paste("x".to_string().into()))));
    app.apply(Message::AnswerSelect(Action::SelectAll));
    let effects = app.apply(Message::Tick(app.now));
    drive(&mut app, effects);
    let answer = app.answer.as_ref().unwrap();
    assert!(answer.selecting, "a reload of the same answer keeps the view");
    assert_eq!(answer.plain.selection().as_deref(), Some("There are 3 files."));
    app.apply(Message::AnswerSelecting(false));
    assert!(!app.answer.as_ref().unwrap().selecting);
    let source = app.answer.as_ref().unwrap().source.clone();
    let copied = send(&mut app, Message::Copy(source));
    assert_eq!(copied, vec!["There are 3 files.".to_string()]);
    assert_eq!(app.banner.as_ref().unwrap().title, "Copied");
}

#[test]
fn the_window_names_its_control_plane() {
    let mut app = app();
    assert_eq!(app.window_title(), "AXLR Bots");
    send(&mut app, Message::SettingsSave);
    assert_eq!(app.endpoint.as_deref(), Some("127.0.0.1:7879"));
    assert_eq!(app.window_title(), "AXLR Bots · 127.0.0.1:7879 · 1 need you");
}

#[test]
fn the_dial_turns_a_bot_at_once_and_its_feed_says_so() {
    let mut app = connected();
    let id = BotId::new("bot-act-with-approval").unwrap();
    send(&mut app, Message::Go(Screen::Bot(id.clone())));
    assert_eq!(app.bot.as_ref().unwrap().bot.level, AutonomyLevel::ActWithApproval);
    let effects = app.apply(Message::SetLevel(id.clone(), AutonomyLevel::Observe));
    assert!(matches!(
        effects.as_slice(),
        [Effect::SetLevel(_, AutonomyLevel::Observe)]
    ));
    assert!(
        app.apply(Message::SetLevel(id.clone(), AutonomyLevel::Propose))
            .is_empty(),
        "one turn at a time"
    );
    drive(&mut app, effects);
    assert!(!app.switching);
    assert_eq!(app.bot.as_ref().unwrap().bot.level, AutonomyLevel::Observe);
    assert_eq!(
        app.banner.as_ref().unwrap().title,
        "bot-act-with-approval is now Observe"
    );
    let feed = app.feed().unwrap();
    let turned = feed.events.iter().rev().find(|e| e.label() == "Level changed").unwrap();
    assert_eq!(turned.detail(), "Ask first → Observe");

    // A turn the control plane refuses says why.
    send(
        &mut app,
        Message::SetLevel(BotId::new("ghost").unwrap(), AutonomyLevel::ActWithinPolicy),
    );
    assert_eq!(app.banner.as_ref().unwrap().title, "Not found");
    assert!(!app.switching);
}

#[test]
fn a_bot_is_stopped_with_its_open_tasks_after_the_second_click() {
    let mut app = connected();
    let waiting = app.fleet.as_ref().unwrap().waiting()[0].clone();
    let id = waiting.bot_id.clone();
    send(&mut app, Message::Go(Screen::Bot(id.clone())));
    assert!(app.apply(Message::AskStopBot(id.clone())).is_empty());
    assert_eq!(app.confirm, Some(Confirm::StopBot(id.clone())));
    let effects = app.apply(Message::ConfirmYes);
    assert!(matches!(effects.as_slice(), [Effect::StopBot(_)]));
    assert!(app.switching);
    drive(&mut app, effects);
    assert!(!app.switching);
    assert_eq!(app.bot.as_ref().unwrap().bot.status, BotStatus::Paused);
    let banner = app.banner.as_ref().unwrap();
    assert_eq!(banner.title, format!("{id} stopped"));
    assert!(banner.detail.contains("cancelled"), "{}", banner.detail);
    let fleet = app.fleet.as_ref().unwrap();
    assert!(
        fleet
            .tasks
            .iter()
            .filter(|t| t.bot_id == id)
            .all(|t| !t.status.can_cancel())
    );

    // Stopped again: nothing open.
    send(&mut app, Message::AskStopBot(id.clone()));
    send(&mut app, Message::ConfirmYes);
    assert_eq!(app.banner.as_ref().unwrap().detail, "no open task");
    send(&mut app, Message::AskStopBot(BotId::new("ghost").unwrap()));
    send(&mut app, Message::ConfirmYes);
    assert_eq!(app.banner.as_ref().unwrap().title, "Not found");
}

#[test]
fn a_stop_that_could_not_cancel_everything_says_how_many() {
    let mut app = connected();
    let bot = app.fleet.as_ref().unwrap().bots[0].clone();
    app.apply(Message::BotStopped(Ok(crate::application::StoppedBot {
        bot,
        cancelled: 2,
        not_cancelled: 1,
    })));
    assert_eq!(app.banner.as_ref().unwrap().detail, "2 tasks cancelled, 1 could not be");
    let bot = app.fleet.as_ref().unwrap().bots[0].clone();
    app.apply(Message::BotStopped(Ok(crate::application::StoppedBot {
        bot,
        cancelled: 1,
        not_cancelled: 0,
    })));
    assert_eq!(app.banner.as_ref().unwrap().detail, "1 task cancelled");
}
