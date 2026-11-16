//! A fleet of a real size on top of the sample: eight teams, forty
//! bots, forty-eight nodes most of them revoked long ago, and the
//! newest two hundred tasks with long objectives and the same timer
//! round again and again. The screens are judged against it, so a list
//! that reads well with five rows is also seen with two hundred.

use chrono::{DateTime, Duration, Utc};

use super::{FakeControlPlane, FakeState};
use crate::domain::{
    AutonomyLevel, BotId, BotStatus, NodeId, NodeStatus, TaskStatus, Trigger, TriggerRef, TriggerType,
};

/// The teams, each with the roles of its bots; the first role leads
/// its team's rounds.
const TEAMS: [(&str, &[&str]); 8] = [
    ("ops", &["lead", "deploy", "watch", "report", "night"]),
    ("support", &["lead", "triage", "answer", "follow-up", "survey"]),
    ("research", &["lead", "reader", "summariser", "fact-check"]),
    ("billing", &["lead", "invoices", "reminders", "refunds", "ledger"]),
    ("docs", &["lead", "writer", "editor", "translator", "links"]),
    ("release", &["lead", "notes", "changelog", "announce"]),
    ("intake", &["lead", "forms", "router", "dedupe", "archive"]),
    ("audit", &["lead", "access", "spend", "logs"]),
];

/// The teams whose bots are on; the others were paused by their owner.
const TEAMS_ON: [&str; 2] = ["ops", "support"];

/// How many tasks the large fleet adds to the sample's five.
const MORE_TASKS: i64 = 195;

impl FakeControlPlane {
    /// The sample fleet and a larger one beside it: the size a control
    /// plane has after some months of work.
    pub fn large(now: DateTime<Utc>) -> Self {
        let fake = Self::sample(now);
        {
            let mut state = fake.state.lock().unwrap();
            state.add_large_nodes(now);
            state.add_large_bots(now);
            state.add_large_tasks(now);
            state.add_long_activity();
            state.add_live_steps(now);
        }
        fake
    }
}

/// A step of a run as the fake tells it: the tool, the MCP tool a
/// bridged call names, its arguments, how long it took and the error of
/// one that failed.
struct Step {
    tool: &'static str,
    mcp: &'static str,
    arguments: &'static str,
    ms: u64,
    error: &'static str,
}

const fn step(tool: &'static str, arguments: &'static str, ms: u64) -> Step {
    Step {
        tool,
        mcp: "",
        arguments,
        ms,
        error: "",
    }
}

const fn bridged(mcp: &'static str, arguments: &'static str, ms: u64) -> Step {
    Step {
        tool: "axlr_call_tool",
        mcp,
        arguments,
        ms,
        error: "",
    }
}

/// The work of one item from its first read to the gate gone green:
/// most calls say why they were made, one failed, two did not say why.
const LONG_RUN: [Step; 25] = [
    Step {
        tool: "local_read",
        mcp: "",
        arguments: r#"{"why":"Read the team's playbook before handling the item.","path":"playbook.md"}"#,
        ms: 6,
        error: "no such file: playbook.md",
    },
    step(
        "local_read",
        r#"{"why":"Read the playbook from its new place in the docs.","path":"docs/playbook.md"}"#,
        7,
    ),
    step(
        "local_read",
        r#"{"why":"Read the item's history in the team's notes.","path":"notes/item-4198.md"}"#,
        8,
    ),
    step(
        "local_list",
        r#"{"why":"See which reports the last gate left behind.","path":"reports"}"#,
        5,
    ),
    step(
        "local_exec",
        r#"{"why":"Run the client's unit tests again to see the failure.","program":"make","args":["test-client"]}"#,
        12_300,
    ),
    step(
        "local_read",
        r#"{"why":"Read the failing test's output.","path":"reports/client-tests.log"}"#,
        11,
    ),
    step(
        "local_exec",
        r#"{"why":"Find the commit that changed the failing test.","program":"git","args":["log","-n","5","--","client/tests"]}"#,
        158,
    ),
    bridged("notes_search", r#"{"query":"client tests fixture","limit":5}"#, 672),
    step(
        "local_exec",
        r#"{"why":"Run the one failing test alone to read its error whole.","program":"make","args":["test-one"]}"#,
        2_400,
    ),
    step(
        "local_read",
        r#"{"why":"Read the test's fixture, where the missing file is named.","path":"client/tests/fixture.toml"}"#,
        9,
    ),
    step(
        "local_write",
        r#"{"why":"Write the fixture with the file's new place.","path":"client/tests/fixture.toml"}"#,
        14,
    ),
    step(
        "local_exec",
        r#"{"why":"Run the client's unit tests with the fix.","program":"make","args":["test-client"]}"#,
        11_800,
    ),
    step(
        "local_exec",
        r#"{"why":"Check that the other services' tests did not move.","program":"make","args":["test"]}"#,
        9_100,
    ),
    step(
        "local_read",
        r#"{"why":"Read the deploy checklist before handing it over.","path":"docs/checklist.md"}"#,
        6,
    ),
    step(
        "local_list",
        r#"{"why":"List what changed in the workspace.","path":"."}"#,
        4,
    ),
    step(
        "local_exec",
        r#"{"why":"Show the change as a diff for the report.","program":"git","args":["diff","--stat"]}"#,
        51,
    ),
    step(
        "local_write",
        r#"{"why":"Write the report of the item.","path":"reports/item-4198.md"}"#,
        16,
    ),
    bridged(
        "notes_write",
        r#"{"why":"Note the fix in the team's notes for the next round.","topic":"item-4198"}"#,
        163,
    ),
    step(
        "local_read",
        r#"{"why":"Read the report back to check it says what was done.","path":"reports/item-4198.md"}"#,
        7,
    ),
    step(
        "local_exec",
        r#"{"why":"Run the gate once more before handing the item over.","program":"make","args":["gate"]}"#,
        12_100,
    ),
    step(
        "local_read",
        r#"{"why":"Read the gate's result.","path":"reports/gate.log"}"#,
        5,
    ),
    bridged("queue_next", r#"{"owner":"ops-deploy","state":"assigned"}"#, 65),
    step(
        "local_read",
        r#"{"why":"Read the next item before planning it.","path":"notes/item-4202.md"}"#,
        55,
    ),
    step(
        "local_exec",
        r#"{"why":"Check for other jobs without repeating the round.","program":"make","args":["queue"]}"#,
        51,
    ),
    step(
        "local_read",
        r#"{"why":"Read the plan the lead left for the next item.","path":"notes/plan.md"}"#,
        8,
    ),
];

/// The events the long run emits to its team, after the step they
/// follow: its name, its message and the bots that took it.
const LONG_RUN_EVENTS: [(usize, &str, &str, &[&str]); 3] = [
    (
        7,
        "ops.item.progress",
        "item-4198 · service-3 · tests reproduced",
        &["ops-lead"],
    ),
    (
        11,
        "ops.item.progress",
        "item-4198 · service-3 · fix ready",
        &["ops-lead", "ops-watch"],
    ),
    (24, "ops.item.done", "item-4198 · service-3 · gate green", &["ops-lead"]),
];

/// What the long run says when it is done.
const LONG_RUN_SAID: &str = "item-4198 is fixed: the client's fixture named a file that moved to docs/. The unit tests and the gate are green, the report is in reports/item-4198.md and the next item, item-4202, is read and waits for its plan.";

impl FakeState {
    /// Two more nodes online, four that went silent days ago and forty
    /// revoked ones: the hosts a fleet enrolled and replaced.
    fn add_large_nodes(&mut self, now: DateTime<Utc>) {
        let providers = [
            "openrouter",
            "codex",
            "local:codex/gpt-6.1-sol",
            "local:codex/gpt-6-luna",
        ];
        for (index, status) in [
            NodeStatus::Online,
            NodeStatus::Online,
            NodeStatus::Offline,
            NodeStatus::Offline,
            NodeStatus::Offline,
            NodeStatus::Offline,
        ]
        .into_iter()
        .enumerate()
        {
            let id = format!("node-{:02}", index + 3);
            let host = format!("host-{}", ["a", "a", "b", "c", "c", "d"][index]);
            self.add_node(&id, &host, &[("zone", "eu")], &providers[..2 + index % 3], status, now);
            let node = self.nodes.get_mut(&NodeId::new(&id).unwrap()).unwrap();
            node.capabilities.axlr_version = "0.18.3".into();
            node.capabilities.node_version = if status == NodeStatus::Online { "0.4.7" } else { "0.4.4" }.into();
            if status == NodeStatus::Offline {
                node.last_seen_at = now - Duration::days(2 + index as i64);
            }
        }
        for index in 1..=40 {
            let id = format!("old-node-{index:02}");
            self.add_node(
                &id,
                &format!("host-{}", index % 7),
                &[],
                &["openrouter"],
                NodeStatus::Revoked,
                now,
            );
            let node = self.nodes.get_mut(&NodeId::new(&id).unwrap()).unwrap();
            node.last_seen_at = now - Duration::days(20 + index);
            node.revoked_at = Some(now - Duration::days(19 + index));
        }
    }

    /// Thirty-seven bots in eight teams: two teams at work, six paused.
    /// A lead runs a round on a timer; the others hear their team's
    /// events.
    fn add_large_bots(&mut self, now: DateTime<Utc>) {
        let levels = [
            AutonomyLevel::Observe,
            AutonomyLevel::ActWithinPolicy,
            AutonomyLevel::ActWithApproval,
            AutonomyLevel::Propose,
        ];
        let models = [
            "anthropic/claude-haiku-5.5",
            "codex/gpt-6-luna",
            "codex/gpt-6.1-sol",
            "openrouter/open-model-large-instruct",
        ];
        for (team_index, (team, roles)) in TEAMS.into_iter().enumerate() {
            for (role_index, role) in roles.iter().enumerate() {
                let id = format!("{team}-{role}");
                let nodes: &[&str] = match (team_index + role_index) % 3 {
                    0 => &["gx10-ff95"],
                    1 => &["node-03"],
                    _ => &[],
                };
                self.add_bot(&id, levels[(team_index + role_index) % levels.len()], nodes, now);
                let bot = self.bots.get_mut(&BotId::new(&id).unwrap()).unwrap();
                bot.groups = vec![team.to_string()];
                bot.model.model = models[(team_index * 2 + role_index) % models.len()].into();
                // The last bot of a team at work is paused too: a team's
                // header counts both.
                let on = TEAMS_ON.contains(&team) && role_index + 1 < roles.len();
                bot.status = if on { BotStatus::Enabled } else { BotStatus::Paused };
                bot.triggers = if role_index == 0 {
                    vec![
                        Trigger::manual(),
                        Trigger {
                            trigger_type: TriggerType::Timer,
                            name: "round".into(),
                            every: "30m".into(),
                            ..Trigger::default()
                        },
                    ]
                } else {
                    vec![
                        Trigger::manual(),
                        Trigger {
                            trigger_type: TriggerType::Event,
                            name: format!("{team}.item.assigned"),
                            events: vec![format!("{team}.item.*")],
                            ..Trigger::default()
                        },
                    ]
                };
            }
        }
    }

    /// The work of a day and a half, one task every few minutes: the
    /// leads' timer rounds, each like the last; the events a teammate
    /// sent, each with its own message; and a few tasks by hand.
    fn add_large_tasks(&mut self, now: DateTime<Utc>) {
        for index in 0..MORE_TASKS {
            let at = now - Duration::minutes(40 + index * 11);
            let team = TEAMS_ON[(index % 2) as usize];
            let item = format!("item-{:04}", 4200 - index);
            let service = format!("service-{}", 1 + index % 4);
            let status = match index % 23 {
                7 => TaskStatus::Failed,
                11 | 19 => TaskStatus::Cancelled,
                _ => TaskStatus::Completed,
            };
            match index % 5 {
                0 | 1 => {
                    let bot = format!("{team}-lead");
                    let objective = format!(
                        "Periodic round of the queue (timer fired at {}). Step 1 of the playbook: read the open items of the team, sort them by age, answer the ones that wait for you, hand the rest to the teammate who owns them and report what is left for the next round.",
                        at.format("%Y-%m-%dT%H:%M:%SZ")
                    );
                    let id = self.add_task(&bot, &objective, TaskStatus::Completed, at);
                    self.task_mut(&id).unwrap().trigger = TriggerRef {
                        trigger_type: "timer".into(),
                        name: "round".into(),
                    };
                }
                2 | 3 => {
                    let role = if index % 5 == 2 { "deploy" } else { "triage" };
                    let (team, bot) = if role == "deploy" {
                        ("ops", "ops-deploy".to_string())
                    } else {
                        ("support", "support-triage".to_string())
                    };
                    let name = format!("{team}.item.assigned");
                    let message = format!(
                        "{item} · {service} · {}",
                        [
                            "planned",
                            "waiting for review",
                            "gate failed: the unit tests of the client",
                            "triaged"
                        ][(index % 4) as usize]
                    );
                    let objective = format!(
                        "Your teammate {team}-lead says ({name}, the team's own event): take {item} of {service}, read its history in the team's notes, do the step the playbook names for its state and emit {team}.item.done with a one-line summary when it is finished."
                    );
                    let id = self.add_task(&bot, &objective, status, at);
                    let task = self.task_mut(&id).unwrap();
                    task.trigger = TriggerRef {
                        trigger_type: "event".into(),
                        name,
                    };
                    task.input = Some(serde_json::json!({
                        "from": format!("{team}-lead"), "name": format!("{team}.item.assigned"), "message": message,
                    }));
                }
                _ => {
                    let bot = format!("{team}-watch");
                    let bot = if team == "ops" {
                        bot
                    } else {
                        "support-answer".to_string()
                    };
                    let objective = format!(
                        "Operator check after a deployment of {service}; this is not your usual round. Run exactly these two read-only checks, compare them with yesterday's numbers and say in three lines whether anything moved."
                    );
                    self.add_task(&bot, &objective, status, at);
                }
            }
        }
    }
}

impl FakeState {
    /// The newest task of the bot that deploys gets the long run: its
    /// steps, the events it emitted and its last words, as its node and
    /// the control plane told them, and the run that ended it.
    fn add_long_activity(&mut self) {
        let Some((task, start)) = self
            .tasks
            .iter()
            .filter(|t| t.bot_id.as_str() == "ops-deploy")
            .max_by_key(|t| t.created_at)
            .map(|t| (t.id.clone(), t.created_at))
        else {
            return;
        };
        let mut at = start + Duration::seconds(6);
        for (index, call) in LONG_RUN.iter().enumerate() {
            self.tell_step(&task, at, index, call);
            at += Duration::milliseconds(call.ms as i64) + Duration::seconds(2);
            for (after, name, message, to) in LONG_RUN_EVENTS {
                if after == index {
                    self.emit_on(&task, at, name, message, to);
                    at += Duration::seconds(1);
                }
            }
        }
        self.event(
            "bot.task.progress",
            "ops-deploy",
            &task,
            at,
            serde_json::json!({"items": [{"kind": "text", "text": LONG_RUN_SAID, "at": at}]}),
        );
        self.add_execution(
            &task,
            crate::domain::ExecutionOutcome::Completed,
            Some("gx10-ff95"),
            LONG_RUN_SAID,
            None,
            at - Duration::seconds(5),
        );
    }

    /// The running task of the sample goes on: more steps after the two
    /// it had, each saying why, the last one still running.
    fn add_live_steps(&mut self, now: DateTime<Utc>) {
        let Some(task) = self
            .tasks
            .iter()
            .find(|t| t.status == TaskStatus::Running)
            .map(|t| t.id.clone())
        else {
            return;
        };
        for (index, call) in LONG_RUN.iter().take(22).enumerate() {
            let at = now - Duration::seconds(5) + Duration::milliseconds(200 * index as i64);
            self.tell_step(&task, at, 100 + index, call);
        }
        let at = now - Duration::seconds(1);
        self.event(
            "bot.task.progress",
            "bot",
            &task,
            at,
            serde_json::json!({"items": [{"kind": "tool.started", "tool": "local_exec", "callId": "live-last", "at": at,
                "summary": r#"{"why":"Read the host's clock once more.","program":"date"}"#}]}),
        );
    }

    /// One step of a run as its node hands it over: its start, and its
    /// end with how long it took.
    fn tell_step(&mut self, task: &crate::domain::TaskId, at: DateTime<Utc>, index: usize, call: &Step) {
        let bot = self
            .tasks
            .iter()
            .find(|t| &t.id == task)
            .map(|t| t.bot_id.to_string())
            .unwrap_or_default();
        let id = format!("call-{index}");
        let (status, error) = if call.error.is_empty() {
            ("ok", "")
        } else {
            ("failed", call.error)
        };
        self.event(
            "bot.task.progress",
            &bot,
            task,
            at,
            serde_json::json!({"items": [
                {"kind": "tool.started", "tool": call.tool, "mcpTool": call.mcp, "callId": id, "summary": call.arguments, "at": at},
                {"kind": "tool.ended", "tool": call.tool, "callId": id, "status": status, "error": error, "durationMs": call.ms},
            ]}),
        );
    }

    /// An event a task's run emitted to its team, recorded on the task
    /// as the control plane does, with the bots it reached.
    fn emit_on(&mut self, task: &crate::domain::TaskId, at: DateTime<Utc>, name: &str, message: &str, to: &[&str]) {
        let bot = self
            .tasks
            .iter()
            .find(|t| &t.id == task)
            .map(|t| t.bot_id.to_string())
            .unwrap_or_default();
        let deliveries: Vec<serde_json::Value> = to
            .iter()
            .map(|b| serde_json::json!({"botId": b, "mode": "task", "created": true}))
            .collect();
        self.event(
            "bot.event.emitted",
            &bot,
            task,
            at,
            serde_json::json!({"name": name, "message": message, "from": bot, "deliveries": deliveries}),
        );
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::application::ControlPlane;
    use crate::domain::TaskFilter;

    fn run<T>(future: impl std::future::Future<Output = T>) -> T {
        futures::executor::block_on(future)
    }

    #[test]
    fn the_large_fleet_has_the_size_the_screens_are_judged_at() {
        let now = "2026-10-25T10:00:00Z".parse().unwrap();
        let fake = FakeControlPlane::large(now);
        let nodes = run(fake.list_nodes()).unwrap();
        assert_eq!(nodes.len(), 48);
        assert_eq!(nodes.iter().filter(|n| n.status == NodeStatus::Revoked).count(), 40);
        assert_eq!(nodes.iter().filter(|n| n.status == NodeStatus::Online).count(), 4);
        let bots = run(fake.list_bots()).unwrap();
        assert_eq!(bots.len(), 40);
        let teams = crate::domain::Team::from_bots(&bots);
        assert_eq!(teams.len(), 8);
        assert!(
            teams
                .iter()
                .any(|t| t.members.iter().all(|b| b.status == BotStatus::Paused))
        );
        let tasks = run(fake.list_tasks(TaskFilter::with_limit(500))).unwrap();
        assert_eq!(tasks.len(), 200);
        // The same round again and again, and events with their own words.
        let rounds = tasks.iter().filter(|t| t.trigger.name == "round").count();
        assert!(rounds >= 60, "{rounds}");
        assert!(tasks.iter().any(|t| t.told_by().is_some()));
        assert!(tasks.iter().all(|t| t.objective.len() > 10));
    }

    #[test]
    fn the_newest_task_of_the_deploy_bot_has_a_long_run_and_the_running_task_goes_on() {
        let now = "2026-10-25T10:00:00Z".parse().unwrap();
        let fake = FakeControlPlane::large(now);
        let tasks = run(fake.list_tasks(TaskFilter::with_limit(500))).unwrap();
        let long = tasks.iter().find(|t| t.bot_id.as_str() == "ops-deploy").unwrap();
        let events = run(fake.task_events(long.id.clone(), 500)).unwrap();
        let activity = crate::application::Activity::of(&events, false);
        let tools = activity
            .entries
            .iter()
            .filter(|e| matches!(e, crate::application::ActivityEntry::Tool(_)))
            .count();
        let emitted = events.iter().filter(|e| e.event_type == "bot.event.emitted").count();
        assert_eq!((tools, emitted), (25, 3));
        assert!(activity.text.starts_with("item-4198 is fixed"));
        let running = tasks.iter().find(|t| t.status == TaskStatus::Running).unwrap();
        let live = run(fake.task_events(running.id.clone(), 500)).unwrap();
        assert_eq!(crate::application::Activity::of(&live, true).running(), 2);
    }
}
