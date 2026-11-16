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
        }
        fake
    }
}

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
}
