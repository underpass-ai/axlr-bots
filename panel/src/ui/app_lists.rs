//! The lists as the screens show them: the rows the chips, the bot
//! picker and the filter let through, folded, grouped and in the
//! table's order. The views draw them; the picks and the row cursor
//! keep to them.

use std::collections::BTreeMap;

use super::App;
use super::find::{DrawnRows, Query, Searchable, TaskGroup, TeamGroup};
use crate::domain::{BotId, Node, NodeId, Task, TaskId, Team};

impl App {
    /// What the filter field holds, as the lists read it.
    pub fn list_query(&self) -> Query {
        Query::new(&self.tables.query)
    }

    /// The tasks the status chips, the bot picker and the filter let
    /// through, newest first.
    pub fn tasks_through(&self) -> Vec<&Task> {
        let Some(fleet) = self.fleet.as_ref() else {
            return Vec::new();
        };
        self.list_query().filter(self.filter.apply(&fleet.tasks))
    }

    /// The Tasks list as shown: the tasks let through with their
    /// repeats folded into lines, newest first so a line shows its
    /// newest task; the lines in the table's order, in their groups.
    pub fn task_groups(&self) -> Vec<TaskGroup<'_>> {
        let mut lines = self.tables.fold.lines(self.tasks_through());
        let order = self.tables.tasks.sort.order(lines.iter().map(|line| line.task));
        let place: BTreeMap<&TaskId, usize> = order.iter().enumerate().map(|(at, task)| (&task.id, at)).collect();
        lines.sort_by_key(|line| place.get(&line.task.id).copied().unwrap_or(usize::MAX));
        self.tables.grouping.groups(lines, &self.tables.collapsed)
    }

    /// The Bots list as shown: every bot counted in its team, the rows
    /// the chip and the filter let through in the table's order.
    pub fn bot_groups(&self) -> Vec<TeamGroup<'_>> {
        let Some(fleet) = self.fleet.as_ref() else {
            return Vec::new();
        };
        let query = self.list_query();
        let show = self.tables.bot_show;
        let shows = |bot: &crate::domain::Bot| show.takes(bot) && query.matches(&bot.search_text());
        let ordered = self.tables.bots.sort.order(&fleet.bots);
        TeamGroup::of(ordered, shows, !query.is_empty(), &self.tables.collapsed)
    }

    /// The Nodes list as shown: the nodes the filter lets through, the
    /// revoked ones only when asked for, in the table's order.
    pub fn node_rows(&self) -> Vec<&Node> {
        let Some(fleet) = self.fleet.as_ref() else {
            return Vec::new();
        };
        let rows = self.tables.nodes_shown.rows(self.list_query().filter(&fleet.nodes));
        self.tables.nodes.sort.order(rows)
    }

    /// The Teams list as shown: the teams the filter lets through, in
    /// the table's order.
    pub fn team_rows(&self) -> Vec<Team> {
        let teams = self.teams();
        let kept = self.list_query().filter(&teams);
        self.tables.teams.sort.order(kept).into_iter().cloned().collect()
    }

    /// Every task the lines on show hold, the folded ones too: what the
    /// header's box picks, and what a pick keeps to.
    pub fn tasks_on_show(&self) -> Vec<TaskId> {
        let groups = self.task_groups();
        TaskGroup::shown(&groups)
            .into_iter()
            .flat_map(|line| line.alike.iter().map(|task| task.id.clone()))
            .collect()
    }

    /// The tasks of the line on show that holds the task: the task
    /// alone when its line folds nothing, none when no line holds it.
    pub fn line_of(&self, id: &TaskId) -> Option<Vec<TaskId>> {
        let groups = self.task_groups();
        TaskGroup::shown(&groups)
            .into_iter()
            .find(|line| line.holds(id))
            .map(|line| line.alike.iter().map(|task| task.id.clone()).collect())
    }

    /// The Tasks list line after line: a header per group when it is
    /// grouped, a row per line of an open group, each row by the task
    /// that stands for it (the one under the cursor when its line holds
    /// it, so a line stays under the cursor as newer repeats come).
    pub fn task_lines_drawn(&self) -> DrawnRows<TaskId> {
        let groups = self.task_groups();
        let open = self.tables.tasks.cursor.selected();
        let mut drawn = DrawnRows::default();
        for group in &groups {
            if self.tables.grouping.is_grouped() {
                drawn.header();
            }
            if group.open {
                for line in &group.lines {
                    drawn.row(line.key(open));
                }
            }
        }
        drawn
    }

    /// The Bots list line after line: each team's header, then its rows
    /// when it is open.
    pub fn bot_lines_drawn(&self) -> DrawnRows<BotId> {
        let mut drawn = DrawnRows::default();
        for group in self.bot_groups() {
            drawn.header();
            if group.open {
                for bot in group.bots {
                    drawn.row(bot.id.clone());
                }
            }
        }
        drawn
    }

    /// The Nodes list line after line.
    pub fn node_lines_drawn(&self) -> DrawnRows<NodeId> {
        DrawnRows::rows(self.node_rows().into_iter().map(|node| node.id.clone()))
    }

    /// The Teams list line after line.
    pub fn team_lines_drawn(&self) -> DrawnRows<String> {
        DrawnRows::rows(self.team_rows().into_iter().map(|team| team.name))
    }

    /// After the rows a list shows changed (a refresh, the filter, a
    /// chip, a fold, a group): the picks out of sight drop out and each
    /// row cursor stays on its row while the row is still shown.
    pub(super) fn rows_changed(&mut self) {
        self.drop_gone_picks();
        let tasks = self.task_lines_drawn().keys();
        self.tables.tasks.cursor.keep(&tasks);
        let bots = self.bot_lines_drawn().keys();
        self.tables.bots.cursor.keep(&bots);
        let nodes = self.node_lines_drawn().keys();
        self.tables.nodes.cursor.keep(&nodes);
        let teams = self.team_lines_drawn().keys();
        self.tables.teams.cursor.keep(&teams);
    }
}
