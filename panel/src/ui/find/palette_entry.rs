use std::cmp::Reverse;

use chrono::{DateTime, Utc};

use super::{Marks, NO_TEAM, PaletteGlyph, PaletteSection, PaletteTarget, Place, Query, Score, TaskWhat};
use crate::application::Fleet;
use crate::domain::{BotStatus, NodeStatus, Task, TaskStatus, Team};
use crate::ui::format;

/// The fewest characters typed before the tasks are searched: one
/// letter is in every task.
const TASKS_FROM: usize = 2;

/// The fewest characters typed before the revoked nodes are searched.
const REVOKED_FROM: usize = 3;

/// One result of the command palette: the section it goes under, the
/// mark before it, its words with the typed parts to highlight, and
/// what choosing it does.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PaletteEntry {
    pub section: PaletteSection,
    pub glyph: PaletteGlyph,
    pub label: String,
    /// The muted words after the label; empty for none.
    pub sublabel: String,
    /// The parts of the label that answer what was typed.
    pub marks: Marks,
    pub target: PaletteTarget,
    /// How well it answers: within its section, the best come first.
    pub score: Score,
}

impl PaletteEntry {
    /// What the palette shows for what was typed, from the fleet as the
    /// panel last read it: the acts, the bots, the teams, the nodes and
    /// the tasks that answer, each section's best first and cut at its
    /// cap. With nothing typed, the first of each but the tasks. Tasks
    /// are searched from two characters on, among the ones the panel
    /// read; revoked nodes from three. A task's id or trace id typed
    /// whole shows that task alone, or every task of the trace.
    pub fn search(query: &Query, fleet: &Fleet, now: DateTime<Utc>) -> Vec<PaletteEntry> {
        let pasted = Self::pasted(query, fleet, now);
        if !pasted.is_empty() {
            return pasted;
        }
        let mut found = Vec::new();
        for (glyph, label, sublabel, target) in actions() {
            found.extend(Self::answer(
                query,
                PaletteSection::Actions,
                glyph,
                label,
                sublabel,
                target,
                None,
            ));
        }
        for bot in &fleet.bots {
            let team = bot.groups.first().map_or(NO_TEAM, String::as_str);
            let words = format!("{} {} {}", bot.id, bot.groups.join(" "), bot.name);
            found.extend(Self::answer(
                query,
                PaletteSection::Bots,
                PaletteGlyph::Bot(bot.status),
                bot.id.as_str(),
                &format!("{team} · {}", bot_word(bot.status)),
                PaletteTarget::Bot(bot.id.clone()),
                Some(&words),
            ));
        }
        for team in Team::from_bots(&fleet.bots) {
            found.extend(Self::answer(
                query,
                PaletteSection::Teams,
                PaletteGlyph::Team,
                &format!("Team {}", team.name),
                &format::count(team.members.len() as u64, "bot", "bots"),
                PaletteTarget::Team(team.name.clone()),
                None,
            ));
        }
        for node in &fleet.nodes {
            let revoked = node.status == NodeStatus::Revoked;
            if revoked && query.chars() < REVOKED_FROM {
                continue;
            }
            found.extend(Self::answer(
                query,
                PaletteSection::Nodes,
                PaletteGlyph::Node(node.status),
                node.id.as_str(),
                &format!("{} · {}", node.host_name, node.status.name()),
                PaletteTarget::Node {
                    id: node.id.clone(),
                    revoked,
                },
                None,
            ));
        }
        if query.chars() >= TASKS_FROM {
            for task in &fleet.tasks {
                let what = TaskWhat::of(task);
                let words = format!(
                    "{} {} {} {} {}",
                    what.message.unwrap_or_default(),
                    task.objective,
                    task.id,
                    task.correlation_id,
                    task.trigger.describe()
                );
                found.extend(Self::answer(
                    query,
                    PaletteSection::Tasks,
                    PaletteGlyph::Task(task.status),
                    what.title(),
                    &Self::task_sublabel(task, now),
                    PaletteTarget::Task(task.id.clone()),
                    Some(&words),
                ));
            }
        }
        let searching = !query.is_empty();
        let mut shown = Vec::new();
        for section in PaletteSection::ALL {
            let mut of_section: Vec<PaletteEntry> = found.iter().filter(|e| e.section == section).cloned().collect();
            of_section.sort_by_key(|entry| Reverse(entry.score));
            of_section.truncate(section.cap(searching));
            shown.extend(of_section);
        }
        shown
    }

    /// The tasks whose id or trace id is what was typed, whole and
    /// whatever its case, newest first: a pasted id goes straight to its
    /// task.
    fn pasted(query: &Query, fleet: &Fleet, now: DateTime<Utc>) -> Vec<PaletteEntry> {
        let typed = query.text().to_lowercase();
        if typed.is_empty() {
            return Vec::new();
        }
        fleet
            .tasks
            .iter()
            .filter(|task| task.id.as_str().to_lowercase() == typed || task.correlation_id.to_lowercase() == typed)
            .take(PaletteSection::Tasks.cap(true))
            .map(|task| {
                let label = TaskWhat::of(task).title().to_string();
                PaletteEntry {
                    section: PaletteSection::Tasks,
                    glyph: PaletteGlyph::Task(task.status),
                    marks: query.marks(&label),
                    label,
                    sublabel: Self::task_sublabel(task, now),
                    target: PaletteTarget::Task(task.id.clone()),
                    score: Score::any(),
                }
            })
            .collect()
    }

    /// A result when it answers the query: by its label and sublabel, or
    /// by the `words` given for it. The label's highlight is every place
    /// a typed word is written in it or, when the words were only found
    /// letter by letter, those letters.
    fn answer(
        query: &Query,
        section: PaletteSection,
        glyph: PaletteGlyph,
        label: &str,
        sublabel: &str,
        target: PaletteTarget,
        words: Option<&str>,
    ) -> Option<PaletteEntry> {
        let found = match words {
            Some(words) => query.rank(words),
            None => query.rank(&format!("{label} {sublabel}")),
        }?;
        let mut marks = query.marks(label);
        if marks.is_empty() && !query.is_empty() {
            marks = query.rank(label).map(|found| found.marks).unwrap_or_default();
        }
        Some(PaletteEntry {
            section,
            glyph,
            label: label.to_string(),
            sublabel: sublabel.to_string(),
            marks,
            target,
            score: found.score,
        })
    }

    /// A task's muted words: its bot and how long ago it came.
    fn task_sublabel(task: &Task, now: DateTime<Utc>) -> String {
        format!("{} · {}", task.bot_id, format::age(task.created_at, now))
    }
}

/// What the panel can do from the palette, in the order shown when
/// nothing is typed: the places first.
fn actions() -> Vec<(PaletteGlyph, &'static str, &'static str, PaletteTarget)> {
    let go = |label, place| (PaletteGlyph::Go, label, "", PaletteTarget::Go(place));
    vec![
        go("Go to Home", Place::Home),
        go("Go to Tasks", Place::Tasks),
        go("Go to Bots", Place::Bots),
        go("Go to Teams", Place::Teams),
        go("Go to Nodes", Place::Nodes),
        (
            PaletteGlyph::New,
            "New task",
            "send a task to a bot",
            PaletteTarget::NewTask,
        ),
        (PaletteGlyph::New, "New bot", "", PaletteTarget::NewBot),
        (
            PaletteGlyph::Theme,
            "Switch theme",
            "light or dark",
            PaletteTarget::SwitchTheme,
        ),
        (
            PaletteGlyph::NeedsYou,
            "Show tasks that need you",
            "",
            PaletteTarget::ShowTasks(TaskStatus::WaitingApproval),
        ),
        (
            PaletteGlyph::Failed,
            "Show failed tasks",
            "",
            PaletteTarget::ShowTasks(TaskStatus::Failed),
        ),
        go("Go to Settings", Place::Settings),
    ]
}

/// Where a bot stands in a word: `On` for one that accepts tasks.
fn bot_word(status: BotStatus) -> &'static str {
    match status {
        BotStatus::Enabled => "On",
        other => other.name(),
    }
}

#[cfg(test)]
mod tests {
    use super::super::test_rows::{bot, node, task};
    use super::*;

    fn now() -> DateTime<Utc> {
        "2026-10-25T09:05:00Z".parse().unwrap()
    }

    fn fleet() -> Fleet {
        Fleet {
            bots: vec![
                bot("ops-watcher", "ops", "Enabled"),
                bot("ops-fixer", "ops", "Paused"),
                bot("lab-reader", "lab", "Enabled"),
                bot("dev-jobs", "", "Enabled"),
                bot("dev-tester", "dev", "Enabled"),
            ],
            nodes: vec![
                node("node-a", "rack-1.example", "Online"),
                node("node-b", "rack-2.example", "Offline"),
                node("node-old", "rack-9.example", "Revoked"),
            ],
            tasks: vec![
                task("task-5", "ops-watcher", "Completed", "timer:clock", None),
                task(
                    "task-4",
                    "ops-fixer",
                    "Failed",
                    "event:stock.low",
                    Some("3 left of A-12"),
                ),
                task("task-3", "lab-reader", "Running", "manual", None),
                task("task-2", "ops-watcher", "Completed", "timer:clock", None),
                task(
                    "task-1",
                    "ops-fixer",
                    "WaitingApproval",
                    "event:stock.low",
                    Some("1 left of B-2"),
                ),
            ],
            ..Fleet::default()
        }
    }

    fn search(typed: &str) -> Vec<PaletteEntry> {
        PaletteEntry::search(&Query::new(typed), &fleet(), now())
    }

    fn labels(entries: &[PaletteEntry], section: PaletteSection) -> Vec<String> {
        entries
            .iter()
            .filter(|e| e.section == section)
            .map(|e| e.label.clone())
            .collect()
    }

    #[test]
    fn with_nothing_typed_shows_the_first_of_each_but_the_tasks() {
        let entries = search("");
        assert_eq!(
            labels(&entries, PaletteSection::Actions),
            vec![
                "Go to Home",
                "Go to Tasks",
                "Go to Bots",
                "Go to Teams",
                "Go to Nodes",
                "New task"
            ]
        );
        assert_eq!(
            labels(&entries, PaletteSection::Bots),
            vec!["ops-watcher", "ops-fixer", "lab-reader", "dev-jobs"]
        );
        assert_eq!(
            labels(&entries, PaletteSection::Teams),
            vec!["Team dev", "Team lab", "Team ops"]
        );
        assert_eq!(labels(&entries, PaletteSection::Nodes), vec!["node-a", "node-b"]);
        assert!(labels(&entries, PaletteSection::Tasks).is_empty());
        assert!(entries.iter().all(|e| e.marks.is_empty()));
        let sections: Vec<PaletteSection> = entries.iter().map(|e| e.section).collect();
        assert!(sections.windows(2).all(|w| w[0] <= w[1]), "the sections come in order");
    }

    #[test]
    fn every_act_is_there_with_what_it_does() {
        let all = actions();
        let targets: Vec<&PaletteTarget> = all.iter().map(|a| &a.3).collect();
        for place in Place::ALL {
            assert!(targets.contains(&&PaletteTarget::Go(place)), "{place:?}");
        }
        let failed = search("failed");
        assert_eq!(labels(&failed, PaletteSection::Actions), vec!["Show failed tasks"]);
        assert_eq!(failed[0].target, PaletteTarget::ShowTasks(TaskStatus::Failed));
        assert_eq!(failed[0].glyph, PaletteGlyph::Failed);
        let need = search("need you");
        assert_eq!(need[0].target, PaletteTarget::ShowTasks(TaskStatus::WaitingApproval));
        assert_eq!(search("theme")[0].target, PaletteTarget::SwitchTheme);
        assert_eq!(
            search("light")[0].label,
            "Switch theme",
            "an act answers by its muted words too"
        );
        assert_eq!(search("new bot")[0].target, PaletteTarget::NewBot);
        assert_eq!(search("send a task")[0].target, PaletteTarget::NewTask);
        assert_eq!(search("settings")[0].target, PaletteTarget::Go(Place::Settings));
    }

    #[test]
    fn typing_keeps_at_most_four_acts_the_best_first() {
        let entries = search("go");
        let acts = labels(&entries, PaletteSection::Actions);
        assert_eq!(acts, vec!["Go to Home", "Go to Bots", "Go to Tasks", "Go to Teams"]);
    }

    #[test]
    fn finds_a_bot_by_id_team_or_name_and_says_where_it_stands() {
        let entries = search("fixer");
        let fixer = entries.iter().find(|e| e.section == PaletteSection::Bots).unwrap();
        assert_eq!(fixer.label, "ops-fixer");
        assert_eq!(fixer.sublabel, "ops · Paused");
        assert_eq!(fixer.glyph, PaletteGlyph::Bot(BotStatus::Paused));
        assert_eq!(
            fixer.target,
            PaletteTarget::Bot(crate::domain::BotId::new("ops-fixer").unwrap())
        );
        assert_eq!(fixer.marks.pieces(&fixer.label), vec![("ops-", false), ("fixer", true)]);
        let by_name = search("the lab-reader bot");
        assert_eq!(labels(&by_name, PaletteSection::Bots), vec!["lab-reader"]);
        let free = search("dev-jobs");
        assert_eq!(
            free.iter().find(|e| e.label == "dev-jobs").unwrap().sublabel,
            "no team · On"
        );
    }

    #[test]
    fn ranks_a_bot_written_so_before_one_found_letter_by_letter() {
        let entries = search("dvj");
        assert_eq!(labels(&entries, PaletteSection::Bots), vec!["dev-jobs"]);
        let found = &entries[0];
        assert!(!found.score.is_exact());
        assert_eq!(
            found.marks.pieces(&found.label),
            vec![
                ("d", true),
                ("e", false),
                ("v", true),
                ("-", false),
                ("j", true),
                ("obs", false)
            ]
        );
        let ops = search("ops");
        assert_eq!(labels(&ops, PaletteSection::Bots), vec!["ops-fixer", "ops-watcher"]);
        assert_eq!(labels(&ops, PaletteSection::Teams), vec!["Team ops"]);
    }

    #[test]
    fn finds_teams_and_nodes_and_the_revoked_only_from_three_letters() {
        let teams = search("team");
        assert_eq!(
            labels(&teams, PaletteSection::Teams),
            vec!["Team dev", "Team lab", "Team ops"]
        );
        let ops = teams.iter().find(|e| e.label == "Team ops").unwrap();
        assert_eq!((ops.sublabel.as_str(), ops.glyph), ("2 bots", PaletteGlyph::Team));
        assert_eq!(ops.target, PaletteTarget::Team("ops".into()));
        assert_eq!(
            search("lab").iter().find(|e| e.label == "Team lab").unwrap().sublabel,
            "1 bot"
        );

        assert_eq!(labels(&search("no"), PaletteSection::Nodes), vec!["node-a", "node-b"]);
        let nodes = search("nod");
        assert_eq!(
            labels(&nodes, PaletteSection::Nodes),
            vec!["node-a", "node-b", "node-old"]
        );
        let old = nodes.iter().find(|e| e.label == "node-old").unwrap();
        assert_eq!(old.sublabel, "rack-9.example · Revoked");
        assert_eq!(old.glyph, PaletteGlyph::Node(NodeStatus::Revoked));
        assert!(matches!(&old.target, PaletteTarget::Node { id, revoked: true } if id.as_str() == "node-old"));
        let by_host = search("rack-1");
        assert!(matches!(&by_host[0].target, PaletteTarget::Node { id, revoked: false } if id.as_str() == "node-a"));
        assert_eq!(labels(&search("offline"), PaletteSection::Nodes), vec!["node-b"]);
    }

    #[test]
    fn searches_the_tasks_only_from_two_letters() {
        assert!(labels(&search("a"), PaletteSection::Tasks).is_empty());
        let entries = search("a-12");
        let tasks: Vec<&PaletteEntry> = entries.iter().filter(|e| e.section == PaletteSection::Tasks).collect();
        assert_eq!(tasks.len(), 1);
        assert_eq!(tasks[0].label, "3 left of A-12");
        assert_eq!(tasks[0].sublabel, "ops-fixer · 5m");
        assert_eq!(tasks[0].glyph, PaletteGlyph::Task(TaskStatus::Failed));
        assert_eq!(
            tasks[0].marks.pieces(&tasks[0].label),
            vec![("3 left of ", false), ("A-12", true)]
        );
        let by_trigger = search("stock.low");
        assert_eq!(
            labels(&by_trigger, PaletteSection::Tasks),
            vec!["1 left of B-2", "3 left of A-12"],
            "the find that comes earlier in a task's words ranks first"
        );
        assert!(
            by_trigger.iter().all(|e| e.marks.is_empty()),
            "the label does not say the trigger"
        );
        let by_objective = search("then stop");
        assert_eq!(labels(&by_objective, PaletteSection::Tasks).len(), 5);
        assert!(
            labels(&by_objective, PaletteSection::Tasks).contains(&"Do the work of ops-watcher.".to_string()),
            "a task without a message reads by its ask"
        );
    }

    #[test]
    fn shows_at_most_eight_tasks_and_seven_bots() {
        let mut fleet = fleet();
        for n in 0..20 {
            fleet.tasks.push(task(
                &format!("task-x{n}"),
                "ops-watcher",
                "Completed",
                "timer:clock",
                None,
            ));
            fleet.bots.push(bot(&format!("ops-extra-{n}"), "ops", "Enabled"));
        }
        let entries = PaletteEntry::search(&Query::new("ops"), &fleet, now());
        assert_eq!(labels(&entries, PaletteSection::Tasks).len(), 8);
        assert_eq!(labels(&entries, PaletteSection::Bots).len(), 7);
        let none = PaletteEntry::search(&Query::new(""), &fleet, now());
        assert_eq!(labels(&none, PaletteSection::Bots).len(), 4);
    }

    #[test]
    fn a_pasted_task_id_shows_that_task_alone() {
        for typed in ["task-3", "  TASK-3 "] {
            let entries = search(typed);
            assert_eq!(entries.len(), 1, "{typed}");
            assert_eq!(
                entries[0].target,
                PaletteTarget::Task(crate::domain::TaskId::new("task-3").unwrap())
            );
            assert_eq!(entries[0].section, PaletteSection::Tasks);
            assert_eq!(entries[0].sublabel, "lab-reader · 5m");
        }
        let part = search("task-");
        assert!(part.len() > 1, "part of an id is a search like any other");
    }

    #[test]
    fn a_pasted_trace_id_shows_every_task_of_the_trace_newest_first() {
        let mut fleet = fleet();
        fleet.tasks[3].correlation_id = "trace-of-task-5".into();
        let entries = PaletteEntry::search(&Query::new("Trace-Of-Task-5"), &fleet, now());
        let ids: Vec<&PaletteTarget> = entries.iter().map(|e| &e.target).collect();
        assert_eq!(
            ids,
            vec![
                &PaletteTarget::Task(crate::domain::TaskId::new("task-5").unwrap()),
                &PaletteTarget::Task(crate::domain::TaskId::new("task-2").unwrap())
            ]
        );
        fleet.tasks[0].correlation_id = String::new();
        let empty = PaletteEntry::search(&Query::new(""), &fleet, now());
        assert!(
            empty.iter().all(|e| e.section != PaletteSection::Tasks),
            "nothing typed is not the id of a task without a trace"
        );
    }

    #[test]
    fn nothing_found_and_nothing_to_search_are_empty() {
        assert!(search("zzzz-nothing-like-it").is_empty());
        let acts_only = PaletteEntry::search(&Query::new(""), &Fleet::default(), now());
        assert_eq!(acts_only.len(), 6);
        assert!(acts_only.iter().all(|e| e.section == PaletteSection::Actions));
        assert!(PaletteEntry::search(&Query::new("task-1"), &Fleet::default(), now()).is_empty());
    }

    #[test]
    fn a_bot_on_reads_on_and_any_other_by_its_status() {
        assert_eq!(bot_word(BotStatus::Enabled), "On");
        assert_eq!(bot_word(BotStatus::Paused), "Paused");
        assert_eq!(bot_word(BotStatus::Registered), "Registered");
    }
}
