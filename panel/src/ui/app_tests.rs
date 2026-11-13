use chrono::Duration;

use super::test_runtime::{connected, drive, send};
use super::view::tests::{app, config, settings};
use super::*;
use crate::application::{PanelError, PanelSettings};
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
    app.apply(Message::SettingsActor("alice".into()));
    assert_eq!(app.settings.config().actor.as_deref(), Some("alice"));
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
        theme: Some(ThemeMode::Dark),
        screen: Screen::Nodes,
    };
    let (mut started, effects) = App::new(base.services.clone(), launch, &settings(), true);
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

    let mut bad = settings();
    bad.control_plane.as_mut().unwrap().api_url = "https://10.9.9.9:7879".into();
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
        vec![Effect::Decide(
            id.clone(),
            crate::domain::Decision::Approve,
            Some("call-1".into())
        )]
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
        urgent: false,
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
    // Only the page's own scroll: nothing goes to the control plane.
    assert_eq!(
        app.apply(Message::Go(Screen::Task(TaskId::new("task-1").unwrap()))),
        vec![Effect::ScrollTop]
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
            urgent: true,
        },
        Effect::Order {
            bot: BotId::new("b").unwrap(),
            objective: "x".into(),
            key: crate::domain::IdempotencyKey::new("k").unwrap(),
        },
        Effect::FollowTeam {
            name: "ops".into(),
            feed: Default::default(),
            wait: 0,
        },
        Effect::SetTeams(BotId::new("b").unwrap(), vec!["ops".into()]),
        Effect::Cancel(TaskId::new("t").unwrap()),
        Effect::Decide(TaskId::new("t").unwrap(), crate::domain::Decision::Deny, None),
        Effect::Download(crate::domain::ExecutionId::new("e").unwrap(), "a.md".into()),
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

#[test]
fn a_chat_message_runs_as_a_task_and_shows_in_the_chat() {
    let mut app = connected();
    // Without a bot on screen, nothing is sent.
    app.apply(Message::ChatInput("hello".into()));
    assert!(app.apply(Message::ChatSend).is_empty());
    let id = BotId::new("bot-wrx80").unwrap();
    send(&mut app, Message::Go(Screen::Bot(id.clone())));
    assert!(app.chat_draft.is_empty(), "a new page starts with an empty box");
    assert!(app.bot.as_ref().unwrap().chat.is_empty());
    app.apply(Message::ChatInput("   ".into()));
    assert!(app.apply(Message::ChatSend).is_empty(), "an empty message is not sent");
    app.apply(Message::ChatInput("How many files are there?".into()));
    let effects = app.apply(Message::ChatSend);
    assert!(matches!(effects.as_slice(), [Effect::SendChat { message, .. }] if message == "How many files are there?"));
    assert!(app.apply(Message::ChatSend).is_empty(), "one message at a time");
    drive(&mut app, effects);
    assert!(!app.chat_busy);
    assert!(app.chat_draft.is_empty());
    let chat = &app.bot.as_ref().unwrap().chat;
    assert_eq!(chat.len(), 1);
    assert_eq!(chat[0].message(), "How many files are there?");

    // A message the control plane refuses keeps the words and says why.
    send(&mut app, Message::Pause(id.clone()));
    app.apply(Message::ChatInput("Still there?".into()));
    send(&mut app, Message::ChatSend);
    assert_eq!(app.chat_draft, "Still there?");
    assert_eq!(app.banner.as_ref().unwrap().tone, Tone::Bad);
}

/// The sample bots with bot and bot-wrx80 in team ops, refreshed.
fn with_a_team() -> (App, std::sync::Arc<crate::adapters::fake::FakeControlPlane>) {
    let fake = std::sync::Arc::new(crate::adapters::fake::FakeControlPlane::sample(
        "2026-10-25T10:00:00Z".parse().unwrap(),
    ));
    let mut app = super::view::tests::app_over(fake.clone());
    send(&mut app, Message::SettingsSave);
    fake.set_groups("bot", &["ops"]);
    fake.set_groups("bot-wrx80", &["ops"]);
    let tick = app.now + Duration::seconds(3);
    send(&mut app, Message::Tick(tick));
    (app, fake)
}

#[test]
fn the_panel_follows_each_team_and_raises_a_teammates_warning() {
    let (mut app, fake) = with_a_team();
    assert_eq!(app.teams().len(), 1);
    let first = app.watch.feed("ops").expect("the team is followed").events.len();
    assert!(first > 0);
    assert!(
        app.watch.following.is_empty(),
        "a read that brought nothing waits for the next refresh"
    );
    let banner_before = app.banner.clone();
    fake.publish(
        "bot.event.emitted",
        "bot",
        serde_json::json!({"name": "stop.halt", "severity": "warning", "message": "a deploy is starting", "to": ["bot-wrx80"]}),
    );
    let tick = app.now + Duration::seconds(6);
    send(&mut app, Message::Tick(tick));
    let banner = app.banner.clone().unwrap();
    assert_ne!(Some(banner.clone()), banner_before);
    assert_eq!(banner.title, "Warning to the team · team ops");
    assert_eq!(banner.detail, "bot → bot-wrx80: a deploy is starting");
    assert_eq!(app.watch.feed("ops").unwrap().events.len(), first + 1);
    // An info goes to the timeline, not to a banner.
    app.banner = None;
    fake.publish(
        "bot.event.emitted",
        "bot",
        serde_json::json!({"name": "note", "severity": "info", "message": "x"}),
    );
    send(&mut app, Message::Tick(tick + Duration::seconds(3)));
    assert!(app.banner.is_none());
    // A read that fails is started again by the next refresh.
    assert!(
        app.team_loaded("ops".into(), Err(PanelError::Unreachable("down".into())))
            .is_empty()
    );
    assert!(!app.watch.following.contains("ops"));
}

#[test]
fn a_bot_changes_team() {
    let (mut app, _) = with_a_team();
    let bot = BotId::new("bot").unwrap();
    send(&mut app, Message::Go(Screen::Bot(bot.clone())));
    app.apply(Message::TeamEdit(bot.clone(), "ops, lab".into()));
    assert_eq!(app.watch.editing.as_ref().unwrap().1, "ops, lab");
    send(&mut app, Message::TeamSave);
    assert_eq!(app.banner.as_ref().unwrap().title, "bot is in teams ops, lab");
    assert!(app.watch.editing.is_none());
    assert_eq!(app.teams().len(), 2);
    app.apply(Message::TeamEdit(bot.clone(), "Bad Name".into()));
    send(&mut app, Message::TeamSave);
    assert_eq!(app.banner.as_ref().unwrap().tone, Tone::Bad);
    app.apply(Message::TeamCancel);
    assert!(app.watch.editing.is_none());
    assert!(app.team_save().is_empty());
}

#[test]
fn an_urgent_order_stops_the_bot_and_gives_it_the_typed_order() {
    let (mut app, fake) = with_a_team();
    let bot = BotId::new("bot").unwrap();
    send(&mut app, Message::Go(Screen::Team("ops".into())));

    // From the team's page the order opens on the bot's page.
    send(&mut app, Message::OrderOpen(bot.clone()));
    assert_eq!(app.screen, Screen::Bot(bot.clone()));
    assert!(app.order.is_open_for(&bot));

    // No words, nothing sent.
    let calls = fake.calls().len();
    assert!(app.apply(Message::OrderSend).is_empty());
    app.apply(Message::OrderText("   ".into()));
    assert!(app.apply(Message::OrderSend).is_empty());
    assert_eq!(fake.calls().len(), calls);

    // The words become the objective of an urgent task.
    let order = "Roll back service-1 now and say what you did.";
    app.apply(Message::OrderText(order.into()));
    let effects = app.apply(Message::OrderSend);
    assert!(
        matches!(effects.as_slice(), [Effect::Order { bot: to, objective, .. }] if *to == bot && objective == order),
        "{effects:?}"
    );
    assert!(app.apply(Message::OrderSend).is_empty(), "one order at a time");
    drive(&mut app, effects);
    let banner = app.banner.clone().unwrap();
    assert_eq!(banner.title, "bot got an urgent order");
    assert_eq!(banner.detail, "It stops what it was doing, drops its queue and does it");
    let Screen::Task(id) = app.screen.clone() else {
        panic!("{:?}", app.screen)
    };
    let task = app
        .fleet
        .as_ref()
        .unwrap()
        .tasks
        .iter()
        .find(|t| t.id == id)
        .unwrap()
        .clone();
    assert!(task.is_urgent());
    assert_eq!(task.objective, order);
    assert_eq!(app.order, OrderForm::default());

    // A refused order keeps the form and its words, and says why.
    send(&mut app, Message::OrderOpen(bot.clone()));
    app.apply(Message::OrderText("Stop and wait".into()));
    fake.fail_with(Some(PanelError::Unreachable("down".into())));
    send(&mut app, Message::OrderSend);
    assert_eq!(app.banner.as_ref().unwrap().tone, Tone::Bad);
    assert!(!app.switching);
    assert!(app.order.is_open_for(&bot));
    assert_eq!(app.order.text, "Stop and wait");
    app.apply(Message::OrderClose);
    assert_eq!(app.order, OrderForm::default());
    assert!(app.apply(Message::OrderSend).is_empty());
}

#[test]
fn an_urgent_task_is_sent_as_urgent() {
    let mut app = connected();
    app.apply(Message::SubmitOpen(Some(BotId::new("bot-wrx80").unwrap())));
    app.apply(Message::SubmitObjective("Come back".into()));
    app.apply(Message::SubmitUrgent(true));
    let effects = app.apply(Message::SubmitSend);
    assert!(matches!(effects.as_slice(), [Effect::Submit { urgent: true, .. }]));
    drive(&mut app, effects);
    let Screen::Task(id) = app.screen.clone() else {
        panic!("{:?}", app.screen)
    };
    assert!(
        app.fleet
            .as_ref()
            .unwrap()
            .tasks
            .iter()
            .any(|t| t.id == id && t.is_urgent())
    );
}

#[test]
fn a_new_bot_joins_a_team_from_the_form() {
    let mut app = connected();
    app.apply(Message::RegisterOpen);
    app.apply(Message::RegisterName("worker-1".into()));
    app.apply(Message::RegisterMission("Watch service-1.".into()));
    app.apply(Message::RegisterTools("local_read, rollback".into()));
    app.apply(Message::RegisterTeam("ops".into()));
    send(&mut app, Message::RegisterSubmit);
    let bot = app
        .fleet
        .as_ref()
        .unwrap()
        .bots
        .iter()
        .find(|b| b.id.as_str() == "worker-1")
        .unwrap()
        .clone();
    assert_eq!(bot.groups, vec!["ops"]);
    assert_eq!(bot.allowed_tools, vec!["local_read", "rollback"]);
}

#[test]
fn a_task_that_starts_waiting_raises_a_notice_that_opens_it() {
    use crate::adapters::fake::{FakeControlPlane, RecordingNotifier};
    let fake = std::sync::Arc::new(FakeControlPlane::sample("2026-10-25T10:00:00Z".parse().unwrap()));
    let notices = std::sync::Arc::new(RecordingNotifier::default());
    let mut app = super::view::tests::app_over(fake.clone());
    app.services.notifier = notices.clone();
    send(&mut app, Message::SettingsSave);
    assert!(
        notices.shown().is_empty(),
        "the first read only learns what already waits"
    );
    assert_eq!(app.noticed.len(), 1);

    // A running task starts waiting: one notice, with the control plane.
    let running = task_in(&app, TaskStatus::Running);
    fake.finish_task(&running, TaskStatus::WaitingApproval);
    let tick = app.now + Duration::seconds(30);
    send(&mut app, Message::Tick(tick));
    let shown = notices.shown();
    assert_eq!(shown.len(), 1);
    assert_eq!(shown[0].title, "bot asks for your OK");
    assert!(shown[0].body.ends_with("127.0.0.1:7879"), "{}", shown[0].body);
    // The next read tells nothing new.
    send(&mut app, Message::Tick(tick + Duration::seconds(30)));
    assert_eq!(notices.shown().len(), 1);

    // A click opens the task and brings the window forward.
    let effects = app.apply(Message::NoticeClicked(Screen::Task(running.clone())));
    assert_eq!(app.screen, Screen::Task(running.clone()));
    assert!(effects.contains(&Effect::FocusWindow));
    notices.click(true);
    let clicked = futures::executor::block_on(app.services.clone().execute(Effect::Notify {
        notice: crate::application::Notice {
            title: "t".into(),
            body: "b".into(),
        },
        open: Screen::Tasks,
    }));
    assert!(matches!(clicked, Some(Message::NoticeClicked(Screen::Tasks))));
    assert!(futures::executor::block_on(app.services.clone().execute(Effect::FocusWindow)).is_none());

    // A task no longer waiting leaves the set; waiting again, it is told again.
    fake.finish_task(&running, TaskStatus::Running);
    send(&mut app, Message::Tick(tick + Duration::seconds(60)));
    assert!(!app.noticed.contains(&running));
}

#[test]
fn a_teammates_warning_raises_a_notice_that_opens_the_teams() {
    let (mut app, fake) = with_a_team();
    let notices = std::sync::Arc::new(crate::adapters::fake::RecordingNotifier::default());
    app.services.notifier = notices.clone();
    fake.publish(
        "bot.event.emitted",
        "bot",
        serde_json::json!({"name": "stop.halt", "severity": "warning", "message": "a deploy is starting", "to": ["bot-wrx80"]}),
    );
    let tick = app.now + Duration::seconds(6);
    send(&mut app, Message::Tick(tick));
    let shown = notices.shown();
    assert_eq!(shown.len(), 1);
    assert_eq!(shown[0].title, "Warning to the team · team ops");
    assert!(shown[0].body.starts_with("bot → bot-wrx80: a deploy is starting"));
}

#[test]
fn a_kept_artifact_is_saved_and_one_not_kept_says_why() {
    let mut app = connected();
    let done = task_in(&app, TaskStatus::Completed);
    send(&mut app, Message::Go(Screen::Task(done)));
    let run = app.task.as_ref().unwrap().executions[0].id.clone();
    let effects = app.apply(Message::Download(run.clone(), "files.md".into()));
    assert_eq!(effects, vec![Effect::Download(run.clone(), "files.md".into())]);
    assert!(app.downloading);
    assert!(app.apply(Message::Download(run.clone(), "files.md".into())).is_empty());
    drive(&mut app, effects);
    assert!(!app.downloading);
    let banner = app.banner.as_ref().unwrap();
    assert_eq!(
        (banner.title.as_str(), banner.detail.as_str()),
        ("Saved", "/saved/files.md")
    );
    send(&mut app, Message::Download(run, "workspace.tar".into()));
    assert_eq!(app.banner.as_ref().unwrap().title, "Not found");
    app.banner = None;
    send(&mut app, Message::Downloaded(Ok(None)));
    assert!(app.banner.is_none(), "a cancelled save says nothing");
}

#[test]
fn a_running_task_is_followed_live_and_keeps_its_history_once_it_ends() {
    let fake = std::sync::Arc::new(crate::adapters::fake::FakeControlPlane::sample(
        "2026-10-25T10:00:00Z".parse().unwrap(),
    ));
    let mut app = super::view::tests::app_over(fake.clone());
    send(&mut app, Message::SettingsSave);
    let running = task_in(&app, TaskStatus::Running);
    let done = task_in(&app, TaskStatus::Completed);
    send(&mut app, Message::Go(Screen::Task(running.clone())));
    let live = app.live.as_ref().unwrap();
    assert!(live.read && !live.following && !live.settled);
    assert_eq!(live.activity.entries.len(), 2);
    assert_eq!(live.activity.running(), 1);
    assert!(live.words.text().starts_with("I will read the host's clock."));

    // While it runs, the next read waits for what comes.
    let tick = app.now + Duration::seconds(3);
    let effects = app.apply(Message::Tick(tick));
    assert!(effects.iter().any(|e| matches!(
        e,
        Effect::FollowTask { id, wait, .. } if *id == running && *wait == crate::application::LIVE_WAIT_SECONDS
    )));
    assert!(
        app.apply(Message::Tick(tick))
            .iter()
            .all(|e| !matches!(e, Effect::FollowTask { .. }))
    );
    drive(&mut app, effects);
    fake.progress(
        &running,
        serde_json::json!([
            {"kind": "tool.ended", "tool": "axlr_call_tool", "callId": "c2", "status": "ok", "durationMs": 900},
            {"kind": "delegation", "bot": "bot-wrx80", "taskId": done.to_string()},
            {"kind": "text", "text": " It is ten."}
        ]),
    );
    fake.say(&running, "It is ten o'clock.");
    send(&mut app, Message::Tick(tick + Duration::seconds(3)));
    let live = app.live.as_ref().unwrap();
    assert_eq!(live.activity.running(), 0);
    assert_eq!(live.activity.entries.len(), 4);
    assert!(live.words.text().contains("It is ten."));
    send(&mut app, Message::ActivityPick(2));
    send(
        &mut app,
        Message::WordsSelect(iced::widget::text_editor::Action::SelectAll),
    );
    let live = app.live.as_ref().unwrap();
    assert_eq!(live.selected, Some(2));
    assert!(live.words.selection().is_some_and(|s| s.ends_with("It is ten.")));

    // A read for another screen, or that failed, changes nothing.
    assert!(
        app.apply(Message::TaskFollowed(done.clone(), Ok(Default::default())))
            .is_empty()
    );
    assert!(
        app.apply(Message::TaskFollowed(
            running.clone(),
            Err(PanelError::Unreachable("down".into()))
        ))
        .is_empty()
    );

    // It ends: one more read takes what came last, then the following
    // stops and the history stays.
    send(&mut app, Message::AskCancel(running.clone()));
    send(&mut app, Message::ConfirmYes);
    send(&mut app, Message::Tick(tick + Duration::seconds(6)));
    let live = app.live.as_ref().unwrap();
    assert!(live.settled && live.read);
    assert_eq!(live.activity.entries.len(), 4);
    let effects = app.apply(Message::Tick(tick + Duration::seconds(9)));
    assert!(effects.iter().all(|e| !matches!(e, Effect::FollowTask { .. })));

    // Another task starts its own.
    send(&mut app, Message::Go(Screen::Task(done.clone())));
    assert_eq!(app.live.as_ref().unwrap().id, done);
    send(&mut app, Message::Go(Screen::Overview));
    assert!(app.apply(Message::ActivityPick(0)).is_empty());
}

#[test]
fn the_boxes_of_a_running_task_show_their_new_end_until_the_person_scrolls_up() {
    let fake = std::sync::Arc::new(crate::adapters::fake::FakeControlPlane::sample(
        "2026-10-25T10:00:00Z".parse().unwrap(),
    ));
    let mut app = super::view::tests::app_over(fake);
    send(&mut app, Message::SettingsSave);
    let running = task_in(&app, TaskStatus::Running);
    send(&mut app, Message::Go(Screen::Task(running.clone())));
    let grow = |app: &App, id: &str, items: serde_json::Value| {
        let mut feed = app.live.as_ref().unwrap().feed.clone();
        let event: crate::domain::Event = serde_json::from_value(serde_json::json!({
            "eventId": id, "type": "bot.task.progress", "tenantId": "t", "botId": "b",
            "taskId": running.to_string(), "createdAt": "2026-10-25T10:00:00Z", "payload": {"items": items}
        }))
        .unwrap();
        feed.merge(vec![event], crate::application::LIVE_FEED_SIZE);
        feed
    };
    let shown = |effects: &[Effect]| -> Vec<LiveStream> {
        effects
            .iter()
            .filter_map(|e| match e {
                Effect::ShowEnd(stream) => Some(*stream),
                _ => None,
            })
            .collect()
    };
    let feed = grow(
        &app,
        "p1",
        serde_json::json!([{"kind": "thinking", "text": "The log next."}, {"kind": "tool.started", "tool": "local_read", "callId": "c9"}, {"kind": "text", "text": " Reading it."}]),
    );
    let effects = app.apply(Message::TaskFollowed(running.clone(), Ok(feed)));
    assert_eq!(
        shown(&effects),
        [LiveStream::Steps, LiveStream::Thinking, LiveStream::Words]
    );
    assert!(effects.iter().any(|e| matches!(e, Effect::FollowTask { .. })));

    // The person scrolls up in the words to read: they stay put, the
    // steps go on showing their end.
    assert!(
        app.apply(Message::ActivityScrolled(LiveStream::Words, false))
            .is_empty()
    );
    let feed = grow(
        &app,
        "p2",
        serde_json::json!([{"kind": "tool.ended", "tool": "local_read", "callId": "c9"}, {"kind": "tool.started", "tool": "local_list", "callId": "c10"}, {"kind": "text", "text": " More."}]),
    );
    let effects = app.apply(Message::TaskFollowed(running.clone(), Ok(feed)));
    assert_eq!(shown(&effects), [LiveStream::Steps]);
    let words = app.live.as_ref().unwrap();
    assert!(!words.follows(LiveStream::Words) && words.activity.text.ends_with("\n\nMore."));
    send(&mut app, Message::ActivityScrolled(LiveStream::Words, true));
    assert!(app.live.as_ref().unwrap().follows(LiveStream::Words));
    // Back from selecting a part, the formatted words show their end;
    // after scrolling up in them, they stay where the person left them.
    assert!(shown(&app.apply(Message::WordsSelecting(true))).is_empty());
    assert_eq!(shown(&app.apply(Message::WordsSelecting(false))), [LiveStream::Words]);
    send(&mut app, Message::ActivityScrolled(LiveStream::Words, false));
    assert!(shown(&app.apply(Message::WordsSelecting(false))).is_empty());
    send(&mut app, Message::ActivityScrolled(LiveStream::Words, true));

    // Away from the task, nothing is brought into view.
    let feed = grow(&app, "p3", serde_json::json!([{"kind": "text", "text": " Done."}]));
    send(&mut app, Message::Go(Screen::Overview));
    assert!(shown(&app.apply(Message::TaskFollowed(running, Ok(feed)))).is_empty());
    assert!(
        app.apply(Message::ActivityScrolled(LiveStream::Steps, false))
            .is_empty()
    );
}

#[test]
fn esc_keeps_what_a_dialog_would_do_and_enter_does_it() {
    let mut app = connected();
    let id = app.fleet.as_ref().unwrap().waiting()[0].bot_id.clone();
    send(&mut app, Message::Go(Screen::Bot(id.clone())));
    app.apply(Message::AskStopBot(id.clone()));
    assert!(app.apply(Message::Key(Keypress::Escape)).is_empty());
    assert_eq!(app.confirm, None);
    app.apply(Message::AskStopBot(id.clone()));
    let effects = app.apply(Message::Key(Keypress::Enter));
    assert!(matches!(effects.as_slice(), [Effect::StopBot(stopped)] if *stopped == id));
    assert_eq!(app.confirm, None);
    // Without a dialog, Enter does nothing.
    assert!(app.apply(Message::Key(Keypress::Enter)).is_empty());
}

#[test]
fn esc_closes_the_new_task_form() {
    let mut app = connected();
    send(&mut app, Message::SubmitOpen(None));
    assert!(app.submit.open);
    app.apply(Message::Key(Keypress::Escape));
    assert!(!app.submit.open);
}

#[test]
fn another_screen_opens_at_its_top_a_refresh_keeps_its_place() {
    let mut app = connected();
    assert!(app.apply(Message::Go(Screen::Bots)).contains(&Effect::ScrollTop));
    assert!(!app.apply(Message::Go(Screen::Bots)).contains(&Effect::ScrollTop));
    let later = app.now + Duration::seconds(3);
    assert!(!app.apply(Message::Tick(later)).contains(&Effect::ScrollTop));
    assert!(app.apply(Message::Go(Screen::Tasks)).contains(&Effect::ScrollTop));
}

#[test]
fn a_bot_page_holds_readers_that_keep_a_selection_over_a_refresh() {
    let mut app = connected();
    let id = app.fleet.as_ref().unwrap().bots[0].id.clone();
    send(&mut app, Message::Go(Screen::Bot(id)));
    let mission = app.bot.as_ref().unwrap().bot.mission.clone();
    let reader = app.readers.get(&ReaderKey::BotMission).unwrap();
    assert_eq!(reader.source(), mission);
    assert!(app.readers.get(&ReaderKey::BotModel).is_some());
    app.apply(Message::Read(
        ReaderKey::BotMission,
        iced::widget::text_editor::Action::SelectAll,
    ));
    app.apply(Message::Read(
        ReaderKey::BotMission,
        iced::widget::text_editor::Action::Edit(iced::widget::text_editor::Edit::Insert('x')),
    ));
    let later = app.now + Duration::seconds(3);
    send(&mut app, Message::Tick(later));
    let reader = app.readers.get(&ReaderKey::BotMission).unwrap();
    assert_eq!(reader.content().selection().as_deref(), Some(mission.as_str()));
    send(&mut app, Message::Go(Screen::Bots));
    assert!(app.readers.is_empty());
}

#[test]
fn connect_has_nothing_to_press_while_connected_with_the_same_values() {
    let mut app = connected();
    assert!(!app.settings.can_connect());
    assert_eq!(app.settings.connected, Some(config()));
    assert!(app.apply(Message::SettingsSave).is_empty(), "a press does nothing");
    app.apply(Message::SettingsCa("/other.crt".into()));
    assert!(app.settings.can_connect() && app.settings.changed());
    app.apply(Message::SettingsCa("/ca.crt".into()));
    assert!(!app.settings.can_connect() && !app.settings.changed());
    app.apply(Message::SettingsUrl("https://10.9.9.9:7879".into()));
    send(&mut app, Message::SettingsSave);
    assert!(app.settings.can_connect(), "a failed try leaves Connect to press");
    assert_eq!(
        app.settings.connected,
        Some(config()),
        "the connection kept is the old one"
    );
}

#[test]
fn the_look_and_the_new_bot_defaults_are_kept_as_they_change() {
    let mut app = app();
    assert!(!app.settings.saved);
    send(&mut app, Message::ToggleTheme);
    assert_eq!(app.mode, ThemeMode::Dark);
    assert!(app.banner.is_none(), "the theme switch says nothing");
    let (initial, saved) = app.services.connect.initial();
    assert_eq!(
        (initial.theme, saved),
        (ThemeMode::Dark, false),
        "saved without a control plane"
    );
    app.apply(Message::SettingsModel(" codex/gpt-6-luna ".into()));
    app.apply(Message::SettingsModels(
        "codex/gpt-6-luna, anthropic/claude-haiku-5.5,".into(),
    ));
    assert_ne!(app.settings.new_bot(), app.new_bot);
    send(&mut app, Message::ToggleTheme);
    assert_eq!(
        app.new_bot,
        Default::default(),
        "a theme switch commits no half-typed field"
    );
    assert_ne!(
        app.settings.new_bot(),
        app.new_bot,
        "the typed values stay on the screen"
    );
    send(&mut app, Message::ToggleTheme);
    send(&mut app, Message::SettingsSavePreferences);
    assert_eq!(app.new_bot.model, "codex/gpt-6-luna");
    assert_eq!(app.new_bot.choices().len(), 2);
    assert_eq!(app.banner.as_ref().unwrap().title, "Settings saved");
    send(&mut app, Message::RegisterOpen);
    assert_eq!(app.register.form.model, "codex/gpt-6-luna");
    send(&mut app, Message::SettingsSave);
    let kept = app.services.connect.initial().0;
    assert!(kept.control_plane.is_some() && kept.theme == ThemeMode::Dark);
    assert_eq!(kept.new_bot.model, "codex/gpt-6-luna");
}

#[test]
fn the_settings_are_edited_as_json_and_applied() {
    let mut app = connected();
    app.apply(Message::SettingsJsonOpen);
    assert!(app.settings_json.open);
    let text = app.settings_json.text();
    assert!(text.contains("\"controlPlane.apiUrl\": \"https://127.0.0.1:7879\""));
    assert!(text.contains("\"look.theme\": \"light\""));
    let edited = text
        .replace("\"look.theme\": \"light\"", "\"look.theme\": \"dark\"")
        .replace("127.0.0.1:7879", "cp.example:7879");
    app.settings_json.content = iced::widget::text_editor::Content::with_text(&edited);
    send(&mut app, Message::SettingsJsonApply);
    assert!(!app.settings_json.open && app.settings_json.error.is_none());
    assert_eq!(app.mode, ThemeMode::Dark);
    assert_eq!(app.settings.api_url, "https://cp.example:7879");
    assert!(app.settings.changed() && app.settings.can_connect());
    assert!(app.connected(), "the connection stays until Connect");
    let banner = app.banner.as_ref().unwrap();
    assert!(banner.title == "Applied" && banner.detail.contains("Connect"));
    assert_eq!(app.services.connect.initial().0.theme, ThemeMode::Dark);
    assert_eq!(
        app.services.connect.initial().0.control_plane,
        Some(config()),
        "the control plane section waits for Connect"
    );

    app.apply(Message::SettingsJsonOpen);
    app.settings_json.content = iced::widget::text_editor::Content::with_text("{\"look.theme\": \"blue\",\n}");
    send(&mut app, Message::SettingsJsonApply);
    assert!(app.settings_json.open);
    assert!(app.settings_json.error.as_deref().unwrap().contains("line 1"));
    app.apply(Message::SettingsJsonEdit(iced::widget::text_editor::Action::Move(
        iced::widget::text_editor::Motion::End,
    )));
    assert!(app.settings_json.error.is_none(), "editing clears the error");
    app.apply(Message::SettingsJsonClose);
    assert!(!app.settings_json.open);
}

#[test]
fn the_settings_are_exported_imported_and_reset() {
    let (mut app, store) = super::view::tests::app_with_files();
    send(&mut app, Message::SettingsSave);
    assert!(app.connected());
    send(&mut app, Message::SettingsExport);
    let banner = app.banner.as_ref().unwrap();
    assert_eq!(banner.title, "Settings exported");
    assert!(
        banner.detail.ends_with("axlr-bots-panel.settings.json"),
        "{}",
        banner.detail
    );
    let exported = store.saved().into_values().next().unwrap();
    assert_eq!(
        PanelSettings::from_json(std::str::from_utf8(&exported).unwrap()).unwrap(),
        app.current_settings()
    );

    let imported = PanelSettings {
        control_plane: Some(config()),
        theme: ThemeMode::Dark,
        new_bot: crate::application::NewBotDefaults {
            model: "codex/gpt-6-luna".into(),
            models: vec![],
        },
    };
    store.put("/in/settings.json", &imported.to_json());
    store.pick(Some("/in/settings.json"));
    send(&mut app, Message::SettingsImport);
    let banner = app.banner.as_ref().unwrap();
    assert_eq!(banner.title, "Settings imported");
    assert!(banner.detail.contains("kept"), "same control plane: nothing to connect");
    assert_eq!(
        (app.mode, app.new_bot.model.as_str()),
        (ThemeMode::Dark, "codex/gpt-6-luna")
    );
    assert!(!app.settings.changed());

    store.put("/in/look.json", "{\"look.theme\": \"light\"}");
    store.pick(Some("/in/look.json"));
    send(&mut app, Message::SettingsImport);
    assert_eq!(app.settings.config(), config(), "no control plane keys: the three stay");
    assert_eq!((app.mode, app.new_bot.clone()), (ThemeMode::Light, Default::default()));
    assert!(!app.settings.changed());

    store.put("/in/bad.json", "{\"bots\": 1}");
    store.pick(Some("/in/bad.json"));
    send(&mut app, Message::SettingsImport);
    assert_eq!(app.banner.as_ref().unwrap().title, "Check settings");
    store.pick(None);
    send(&mut app, Message::SettingsImport);
    assert_eq!(
        app.banner.as_ref().unwrap().title,
        "Check settings",
        "a cancel says nothing new"
    );

    app.apply(Message::SettingsCa("/elsewhere.crt".into()));
    send(&mut app, Message::SettingsReset);
    assert_eq!(app.settings.config(), config());
    assert_eq!((app.mode, app.new_bot.clone()), (ThemeMode::Light, Default::default()));
    assert_eq!(app.banner.as_ref().unwrap().title, "Back to the defaults");
}

#[test]
fn a_broken_connection_wakes_connect_up_as_reconnect() {
    use crate::adapters::fake::FakeControlPlane;
    let fake = std::sync::Arc::new(FakeControlPlane::sample("2026-10-25T10:00:00Z".parse().unwrap()));
    let mut app = super::view::tests::app_over(fake.clone());
    send(&mut app, Message::SettingsSave);
    assert!(app.connected() && !app.can_connect());
    fake.fail_with(Some(PanelError::Unauthorized("token rotated".into())));
    let now = app.now + Duration::seconds(30);
    send(&mut app, Message::Tick(now));
    assert!(matches!(app.fleet_error, Some(PanelError::Unauthorized(_))));
    assert!(
        app.can_connect() && !app.settings.changed(),
        "the same values, worth trying again"
    );
    fake.fail_with(None);
    send(&mut app, Message::SettingsSave);
    assert!(app.fleet_error.is_none() && !app.can_connect());
    assert_eq!(app.banner.as_ref().unwrap().title, "Connected");
}

#[test]
fn the_first_of_two_approvals_says_nothing_ran() {
    let mut app = connected();
    let waiting = app
        .fleet
        .as_ref()
        .unwrap()
        .tasks
        .iter()
        .find(|t| t.status == TaskStatus::WaitingApproval)
        .unwrap()
        .clone();
    let execution: crate::domain::Execution = serde_json::from_str(
        r#"{"id":"exec-1","taskId":"task-1","botId":"bot-wrx80","startedAt":"2026-10-24T22:32:26Z","outcome":"DecisionRequired","kind":"run"}"#,
    )
    .unwrap();
    app.apply(Message::Decided(Ok(crate::domain::DecidedTask {
        task: waiting,
        execution,
    })));
    let banner = app.banner.as_ref().unwrap();
    assert_eq!(banner.title, "First approval recorded");
    assert!(banner.detail.contains("second, different person"), "{}", banner.detail);
}

/// The sample fleet with `bot` at 1.2.0 and 1.3.0 staged for it, read.
fn with_an_update() -> (App, std::sync::Arc<crate::adapters::fake::FakeControlPlane>) {
    let fake = std::sync::Arc::new(crate::adapters::fake::FakeControlPlane::sample(
        "2026-10-25T10:00:00Z".parse().unwrap(),
    ));
    fake.change_bot("bot", |b| b.version = "1.2.0".into());
    fake.stage_bot("bot", "1.3.0");
    let mut app = super::view::tests::app_over(fake.clone());
    send(&mut app, Message::SettingsSave);
    (app, fake)
}

#[test]
fn a_bot_is_updated_to_its_staged_version_after_the_confirmation() {
    let (mut app, fake) = with_an_update();
    let id = BotId::new("bot").unwrap();
    let listed = app.fleet.as_ref().unwrap().bots.iter().find(|b| b.id == id).unwrap();
    assert_eq!(listed.update().unwrap().tip(), "v1.2.0 → v1.3.0 ready");
    send(&mut app, Message::Go(Screen::Bot(id.clone())));
    assert!(app.apply(Message::AskUpdateBot(id.clone())).is_empty());
    let Some(Confirm::UpdateBot(asked, update)) = app.confirm.clone() else {
        panic!("no confirmation: {:?}", app.confirm);
    };
    assert_eq!((asked, update.act()), (id.clone(), "Update to v1.3.0".to_string()));
    let effects = app.apply(Message::ConfirmYes);
    assert!(matches!(effects.as_slice(), [Effect::ApplyStaged(_)]));
    assert!(app.switching);
    drive(&mut app, effects);
    assert!(!app.switching);
    let banner = app.banner.as_ref().unwrap();
    assert_eq!(
        (banner.title.as_str(), banner.detail.as_str()),
        ("bot updated", "now at v1.3.0")
    );
    let bot = &app.bot.as_ref().unwrap().bot;
    assert_eq!((bot.version.as_str(), bot.update()), ("1.3.0", None));
    let listed = app.fleet.as_ref().unwrap().bots.iter().find(|b| b.id == id).unwrap();
    assert!(listed.staged.is_none(), "the fleet is read again");
    assert!(fake.calls().iter().any(|c| c == "apply_staged bot"));

    // Nothing newer: nothing to ask.
    assert!(app.apply(Message::AskUpdateBot(id)).is_empty());
    assert!(app.confirm.is_none());
}

#[test]
fn an_update_dropped_meanwhile_says_so_in_the_banner() {
    let (mut app, fake) = with_an_update();
    let id = BotId::new("bot").unwrap();
    send(&mut app, Message::Go(Screen::Bots));
    send(&mut app, Message::AskUpdateBot(id.clone()));
    assert!(
        matches!(app.confirm, Some(Confirm::UpdateBot(..))),
        "asked from the list's fleet"
    );
    // Another operator drops it before the click.
    futures::executor::block_on(crate::application::ControlPlane::unstage(fake.as_ref(), id.clone())).unwrap();
    send(&mut app, Message::ConfirmYes);
    let banner = app.banner.as_ref().unwrap();
    assert_eq!(banner.title, "Not now");
    assert!(
        banner.detail.contains("applied or dropped meanwhile"),
        "{}",
        banner.detail
    );
    assert!(!app.switching);
    let listed = app.fleet.as_ref().unwrap().bots.iter().find(|b| b.id == id).unwrap();
    assert!(listed.update().is_none(), "the list no longer offers it");
}

#[test]
fn nodes_behind_the_control_plane_are_known_after_a_refresh() {
    let (mut app, fake) = with_an_update();
    fake.set_build(Some(crate::domain::BuildStatus {
        running: crate::domain::BuildIdentity {
            version: "0.4.0".into(),
            ..Default::default()
        },
        minimum_axlr: "0.15.0".into(),
        ..Default::default()
    }));
    fake.set_node_build("wrx80", "0.3.0", "axlr-tui 0.14.0");
    fake.set_node_build("gx10-ff95", "0.4.0", "axlr-tui 0.15.0");
    let tick = app.now + Duration::seconds(3);
    send(&mut app, Message::Tick(tick));
    let fleet = app.fleet.as_ref().unwrap();
    let marks: Vec<String> = fleet
        .nodes
        .iter()
        .filter_map(|n| fleet.behind(n).map(|b| format!("{}: {}", n.id, b.tip())))
        .collect();
    assert_eq!(
        marks,
        vec![
            "wrx80: Node agent v0.3.0: update it to v0.4.0, the control plane's\n\
             AXLR v0.14.0: update it to v0.15.0 or newer"
                .to_string()
        ]
    );
}
