use super::TaskWhat;
use crate::domain::{Bot, Node, Task, Team};

/// A row of a list as its filter reads it: the words of every column it
/// shows, and its ids.
pub trait Searchable {
    fn search_text(&self) -> String;
}

/// A task by its event's message, what it was asked, its bot, what
/// started it, its id and its trace id.
impl Searchable for Task {
    fn search_text(&self) -> String {
        [
            TaskWhat::of(self).message.unwrap_or_default(),
            &self.objective,
            self.bot_id.as_str(),
            &self.trigger.describe(),
            self.id.as_str(),
            &self.correlation_id,
        ]
        .join(" ")
    }
}

/// A bot by its id and name, its teams, its model, where it runs and
/// its level, in the words shown and the control plane's.
impl Searchable for Bot {
    fn search_text(&self) -> String {
        [
            self.id.as_str(),
            &self.name,
            &self.groups.join(" "),
            &self.model.model,
            &self.placement.describe(),
            self.level.label(),
            self.level.name(),
        ]
        .join(" ")
    }
}

/// A node by its id, its host, its labels as `key=value` and the model
/// providers it names.
impl Searchable for Node {
    fn search_text(&self) -> String {
        [
            self.id.as_str(),
            &self.host_name,
            &self.label_pairs().join(" "),
            &self.capabilities.providers.join(" "),
        ]
        .join(" ")
    }
}

/// A team by its name and its members' ids and names.
impl Searchable for Team {
    fn search_text(&self) -> String {
        let mut words = vec![self.name.as_str()];
        for member in &self.members {
            words.push(member.id.as_str());
            words.push(&member.name);
        }
        words.join(" ")
    }
}

#[cfg(test)]
mod tests {
    use super::super::Query;
    use super::super::test_rows::{bot, node, task};
    use super::*;

    fn finds(row: &impl Searchable, typed: &str) -> bool {
        Query::new(typed).matches(&row.search_text())
    }

    #[test]
    fn a_task_answers_by_message_objective_bot_trigger_and_ids() {
        let made = task(
            "task-68ed",
            "ops-watcher",
            "Completed",
            "event:stock.low",
            Some("3 left of A-12"),
        );
        for typed in [
            "a-12",
            "then stop",
            "OPS-WATCH",
            "stock.low",
            "event",
            "task-68ed",
            "trace-of-task-68ed",
        ] {
            assert!(finds(&made, typed), "{typed}");
        }
        assert!(!finds(&made, "completed"), "its status has its own chips");
        assert!(!finds(&made, "watcher: 3 left"), "who told it is not searched");
        let by_hand = task("task-1", "ops-watcher", "Failed", "manual", None);
        assert!(finds(&by_hand, "manual") && !finds(&by_hand, "a-12"));
    }

    #[test]
    fn a_bot_answers_by_id_name_team_model_place_and_level() {
        let row = bot("ops-watcher", "ops,lab", "Enabled");
        for typed in [
            "watcher",
            "the ops-watcher bot",
            "lab",
            "small-model",
            "vendor/",
            "host-a",
            "ask first",
            "act-with-approval",
        ] {
            assert!(finds(&row, typed), "{typed}");
        }
        assert!(!finds(&row, "enabled") && !finds(&row, "each task asks"));
        let mut anywhere = bot("free", "", "Paused");
        anywhere.placement = Default::default();
        assert!(finds(&anywhere, "any node") && !finds(&anywhere, "host-a"));
    }

    #[test]
    fn a_node_answers_by_id_host_labels_and_providers() {
        let row = node("node-1", "rack-7.example", "Online");
        for typed in ["node-1", "RACK-7", "zone=eu", "openrouter"] {
            assert!(finds(&row, typed), "{typed}");
        }
        assert!(!finds(&row, "online") && !finds(&row, "0.18.3"));
    }

    #[test]
    fn a_team_answers_by_its_name_and_its_members() {
        let bots = [
            bot("ops-watcher", "ops", "Enabled"),
            bot("ops-fixer", "ops", "Paused"),
            bot("solo", "lab", "Enabled"),
        ];
        let teams = Team::from_bots(&bots);
        let ops = teams.iter().find(|t| t.name == "ops").unwrap();
        assert!(finds(ops, "ops") && finds(ops, "fixer") && finds(ops, "the ops-watcher bot"));
        assert!(!finds(ops, "solo") && !finds(ops, "lab"));
    }
}
