use super::{List, Selection};
use crate::application::{BulkAction, BulkTarget};
use crate::domain::{Bot, BotId, Incident, Node, NodeId, Task, TaskId, TaskStatus, Team};

/// A bulk act weighed on the picked rows: the rows it fits, which get
/// one call each, and how many were weighed. Its button and its dialog
/// read from it.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BulkPlan {
    pub action: BulkAction,
    /// What the targets are: bots for a team's act.
    pub unit: List,
    pub targets: Vec<BulkTarget>,
    /// How many rows it was weighed on.
    pub weighed: usize,
    /// The teams whose bots it goes to, for a team's act.
    pub teams: Vec<String>,
}

impl BulkPlan {
    /// The picked bots it fits.
    pub fn for_bots(action: BulkAction, picked: &Selection<BotId>, bots: &[Bot]) -> Self {
        let rows: Vec<&Bot> = bots.iter().filter(|b| picked.contains(&b.id)).collect();
        Self {
            action,
            unit: List::Bots,
            targets: fitting_bots(action, &rows),
            weighed: rows.len(),
            teams: Vec::new(),
        }
    }

    /// The bots of the picked teams it fits, each once.
    pub fn for_teams(action: BulkAction, picked: &Selection<String>, teams: &[Team]) -> Self {
        let chosen: Vec<&Team> = teams.iter().filter(|t| picked.contains(&t.name)).collect();
        let mut rows: Vec<&Bot> = Vec::new();
        for bot in chosen.iter().flat_map(|t| &t.members) {
            if !rows.iter().any(|b| b.id == bot.id) {
                rows.push(bot);
            }
        }
        Self {
            action,
            unit: List::Bots,
            targets: fitting_bots(action, &rows),
            weighed: rows.len(),
            teams: chosen.iter().map(|t| t.name.clone()).collect(),
        }
    }

    /// The picked tasks it fits, each with the call it waits on when a
    /// decision needs it.
    pub fn for_tasks(
        action: BulkAction,
        picked: &Selection<TaskId>,
        tasks: &[Task],
        call_of: impl Fn(&TaskId) -> Option<String>,
    ) -> Self {
        let rows: Vec<&Task> = tasks.iter().filter(|t| picked.contains(&t.id)).collect();
        let targets = rows
            .iter()
            .filter(|t| match action {
                BulkAction::Approve | BulkAction::Deny => t.status == TaskStatus::WaitingApproval,
                BulkAction::Cancel => t.status.can_cancel(),
                _ => false,
            })
            .map(|t| BulkTarget::Task {
                id: t.id.clone(),
                call: call_of(&t.id),
                label: format!("{} · {}", t.bot_id, t.headline(32)),
            })
            .collect();
        Self {
            action,
            unit: List::Tasks,
            targets,
            weighed: rows.len(),
            teams: Vec::new(),
        }
    }

    pub fn for_nodes(action: BulkAction, picked: &Selection<NodeId>, nodes: &[Node]) -> Self {
        let rows: Vec<&Node> = nodes.iter().filter(|n| picked.contains(&n.id)).collect();
        let targets = rows
            .iter()
            .filter(|n| action == BulkAction::Revoke && n.status.can_revoke())
            .map(|n| BulkTarget::Node(n.id.clone()))
            .collect();
        Self {
            action,
            unit: List::Nodes,
            targets,
            weighed: rows.len(),
            teams: Vec::new(),
        }
    }

    pub fn for_incidents(action: BulkAction, picked: &Selection<String>, incidents: &[Incident]) -> Self {
        let rows: Vec<&Incident> = incidents.iter().filter(|i| picked.contains(&i.id)).collect();
        let targets = rows
            .iter()
            .filter(|i| action == BulkAction::Resolve && i.status.can_resolve())
            .map(|i| BulkTarget::Incident(i.id.clone()))
            .collect();
        Self {
            action,
            unit: List::Incidents,
            targets,
            weighed: rows.len(),
            teams: Vec::new(),
        }
    }

    /// There is a row to act on.
    pub fn ready(&self) -> bool {
        !self.targets.is_empty()
    }

    /// The bar's button: `Pause`, or `Pause 3 of 5` when it fits only
    /// some; a team's act counts its bots, `Pause 7 bots`.
    pub fn button(&self) -> String {
        let label = self.action.label();
        let fits = self.targets.len();
        match (self.teams.is_empty(), fits == self.weighed) {
            (true, true) => label.to_string(),
            (true, false) => format!("{label} {fits} of {}", self.weighed),
            (false, true) => format!("{label} {}", self.unit.count(fits)),
            (false, false) => format!("{label} {fits} of {}", self.unit.count(self.weighed)),
        }
    }

    /// The dialog's question: `Pause 3 bots?`.
    pub fn title(&self) -> String {
        format!("{} {}?", self.action.label(), self.unit.count(self.targets.len()))
    }

    /// The dialog's lines: which rows, what it does to each, and the
    /// picked rows it leaves alone.
    pub fn lines(&self) -> Vec<String> {
        let mut lines = vec![self.names()];
        if !self.teams.is_empty() {
            lines.push(format!("The bots of {}.", self.teams.join(", ")));
        }
        lines.push(self.action.what_happens().to_string());
        let skipped = self.weighed - self.targets.len();
        if skipped > 0 {
            lines.push(format!(
                "Left alone: {} it does not apply to.",
                self.unit.count(skipped)
            ));
        }
        lines
    }

    /// The dialog's button: `Pause 3`.
    pub fn act(&self) -> String {
        format!("{} {}", self.action.label(), self.targets.len())
    }

    /// The first three names, then how many more.
    fn names(&self) -> String {
        const SHOWN: usize = 3;
        let head: Vec<String> = self.targets.iter().take(SHOWN).map(BulkTarget::name).collect();
        match self.targets.len().saturating_sub(SHOWN) {
            0 => format!("{}.", head.join(", ")),
            more => format!("{} and {more} more.", head.join(", ")),
        }
    }
}

fn fitting_bots(action: BulkAction, rows: &[&Bot]) -> Vec<BulkTarget> {
    rows.iter()
        .filter(|b| match action {
            BulkAction::TurnOn => b.status.can_enable(),
            BulkAction::Pause => b.status.can_pause(),
            BulkAction::Stop | BulkAction::Delete => true,
            _ => false,
        })
        .map(|b| BulkTarget::Bot(b.id.clone()))
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::application::ControlPlane;
    use crate::application::test_support::{fleet, run};
    use crate::domain::{BotStatus, IncidentStatus, NodeStatus, TaskFilter};

    fn bots() -> Vec<Bot> {
        let (cp, _) = fleet();
        let mut bots = run(cp.list_bots()).unwrap();
        for (i, bot) in bots.iter_mut().enumerate() {
            bot.status = if i == 0 { BotStatus::Paused } else { BotStatus::Enabled };
        }
        bots
    }

    fn all_bots(bots: &[Bot]) -> Selection<BotId> {
        let mut picked = Selection::default();
        picked.set_all(bots.iter().map(|b| b.id.clone()), true);
        picked
    }

    #[test]
    fn says_when_an_act_fits_only_some_rows() {
        let bots = bots();
        let picked = all_bots(&bots);
        let pause = BulkPlan::for_bots(BulkAction::Pause, &picked, &bots);
        assert_eq!(pause.button(), "Pause 2 of 3");
        assert_eq!(pause.title(), "Pause 2 bots?");
        assert_eq!(pause.act(), "Pause 2");
        assert!(pause.lines().last().unwrap().starts_with("Left alone: 1 bot it"));
        let stop = BulkPlan::for_bots(BulkAction::Stop, &picked, &bots);
        assert_eq!(stop.button(), "Stop");
        assert_eq!(stop.lines().len(), 2);
        let on = BulkPlan::for_bots(BulkAction::TurnOn, &picked, &bots);
        assert_eq!((on.button().as_str(), on.ready()), ("Turn on 1 of 3", true));
        let none = BulkPlan::for_bots(BulkAction::Approve, &picked, &bots);
        assert!(!none.ready());
    }

    #[test]
    fn names_the_first_three_and_counts_the_rest() {
        let mut bots = bots();
        let mut more = bots[1].clone();
        for name in ["d", "e"] {
            more.id = BotId::new(name).unwrap();
            bots.push(more.clone());
        }
        let picked = all_bots(&bots);
        let stop = BulkPlan::for_bots(BulkAction::Stop, &picked, &bots);
        assert_eq!(
            stop.lines()[0],
            format!("{}, {}, {} and 2 more.", bots[0].id, bots[1].id, bots[2].id)
        );
    }

    #[test]
    fn a_teams_act_goes_to_each_of_its_bots_once() {
        let mut bots = bots();
        bots[0].groups = vec!["ops".into(), "qa".into()];
        bots[1].groups = vec!["ops".into()];
        bots[2].groups = vec!["lab".into()];
        let teams = Team::from_bots(&bots);
        let mut picked = Selection::default();
        picked.set_all(["ops".to_string(), "qa".to_string()], true);
        let stop = BulkPlan::for_teams(BulkAction::Stop, &picked, &teams);
        assert_eq!(stop.targets.len(), 2);
        assert_eq!(stop.button(), "Stop 2 bots");
        assert_eq!(stop.lines()[1], "The bots of ops, qa.");
        let pause = BulkPlan::for_teams(BulkAction::Pause, &picked, &teams);
        assert_eq!(pause.button(), "Pause 1 of 2 bots");
    }

    #[test]
    fn decides_only_waiting_tasks_and_stops_only_open_ones() {
        let (cp, _) = fleet();
        let tasks = run(cp.list_tasks(TaskFilter::with_limit(50))).unwrap();
        let mut picked = Selection::default();
        picked.set_all(tasks.iter().map(|t| t.id.clone()), true);
        let approve = BulkPlan::for_tasks(BulkAction::Approve, &picked, &tasks, |_| Some("call-1".into()));
        assert_eq!(approve.button(), format!("Approve 1 of {}", tasks.len()));
        assert!(matches!(&approve.targets[0], BulkTarget::Task { call: Some(c), .. } if c == "call-1"));
        let cancel = BulkPlan::for_tasks(BulkAction::Cancel, &picked, &tasks, |_| None);
        assert_eq!(cancel.targets.len(), 2);
        assert!(!BulkPlan::for_tasks(BulkAction::Pause, &picked, &tasks, |_| None).ready());
    }

    #[test]
    fn revokes_nodes_not_revoked_and_resolves_open_incidents() {
        let (cp, _) = fleet();
        let mut nodes = run(cp.list_nodes()).unwrap();
        nodes[0].status = NodeStatus::Revoked;
        let mut picked = Selection::default();
        picked.set_all(nodes.iter().map(|n| n.id.clone()), true);
        let revoke = BulkPlan::for_nodes(BulkAction::Revoke, &picked, &nodes);
        assert_eq!(
            (revoke.button().as_str(), revoke.title().as_str()),
            ("Revoke 1 of 2", "Revoke 1 node?")
        );
        let mut incidents = run(cp.list_incidents(None, None, 50)).unwrap();
        incidents[0].status = IncidentStatus::Resolved;
        let mut picked = Selection::default();
        picked.set_all(incidents.iter().map(|i| i.id.clone()), true);
        let resolve = BulkPlan::for_incidents(BulkAction::Resolve, &picked, &incidents);
        assert!(resolve.ready());
        assert!(resolve.targets.len() < incidents.len());
        assert!(!BulkPlan::for_incidents(BulkAction::Pause, &picked, &incidents).ready());
        assert!(!BulkPlan::for_nodes(BulkAction::Pause, &picked_nodes(&nodes), &nodes).ready());
    }

    fn picked_nodes(nodes: &[Node]) -> Selection<NodeId> {
        let mut picked = Selection::default();
        picked.set_all(nodes.iter().map(|n| n.id.clone()), true);
        picked
    }
}
