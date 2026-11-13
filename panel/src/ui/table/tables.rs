use super::{
    BotColumn, ColumnRef, List, NodeColumn, RowRef, SortDirection, SortSpec, TableState, TaskColumn, TeamColumn,
};
use crate::application::BulkAction;
use crate::domain::{BotId, NodeId, TaskId};

/// The list screens' tables: how each is sorted and what is picked in
/// it, kept while the lists are read again.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Tables {
    pub nodes: TableState<NodeColumn, NodeId>,
    pub bots: TableState<BotColumn, BotId>,
    pub tasks: TableState<TaskColumn, TaskId>,
    pub teams: TableState<TeamColumn, String>,
    /// Shift is held: a header press adds its column to the order.
    pub shift: bool,
    /// The bulk act on its way, if one is: the bars wait for it.
    pub busy: Option<BulkAction>,
}

impl Default for Tables {
    /// Each list in the order a person reads it first: nodes up first,
    /// bots by name, the newest task on top, teams
    /// by name.
    fn default() -> Self {
        use SortDirection::Ascending;
        Self {
            nodes: TableState::sorted(SortSpec::by(NodeColumn::Status, Ascending).then(NodeColumn::Node, Ascending)),
            bots: TableState::sorted(SortSpec::by(BotColumn::Bot, Ascending)),
            tasks: TableState::sorted(SortSpec::by(TaskColumn::Age, Ascending)),
            teams: TableState::sorted(SortSpec::by(TeamColumn::Team, Ascending)),
            shift: false,
            busy: None,
        }
    }
}

impl Tables {
    /// A header pressed: sort by it alone, or with Shift add it.
    pub fn sort(&mut self, column: ColumnRef) {
        let adding = self.shift;
        match column {
            ColumnRef::Nodes(c) => self.nodes.sort.press(c, adding),
            ColumnRef::Bots(c) => self.bots.sort.press(c, adding),
            ColumnRef::Tasks(c) => self.tasks.sort.press(c, adding),
            ColumnRef::Teams(c) => self.teams.sort.press(c, adding),
        }
    }

    /// A row's box ticked or not.
    pub fn pick(&mut self, row: RowRef, picked: bool) {
        match row {
            RowRef::Node(id) => self.nodes.selection.set(id, picked),
            RowRef::Bot(id) => self.bots.selection.set(id, picked),
            RowRef::Task(id) => self.tasks.selection.set(id, picked),
            RowRef::Team(name) => self.teams.selection.set(name, picked),
        }
    }

    /// The header's box: every row shown picked, or none.
    pub fn pick_all(&mut self, rows: Vec<RowRef>, picked: bool) {
        for row in rows {
            self.pick(row, picked);
        }
    }

    /// How many rows of the list are picked.
    pub fn picked(&self, list: List) -> usize {
        match list {
            List::Nodes => self.nodes.selection.len(),
            List::Bots => self.bots.selection.len(),
            List::Tasks => self.tasks.selection.len(),
            List::Teams => self.teams.selection.len(),
        }
    }

    pub fn clear(&mut self, list: List) {
        match list {
            List::Nodes => self.nodes.selection.clear(),
            List::Bots => self.bots.selection.clear(),
            List::Tasks => self.tasks.selection.clear(),
            List::Teams => self.teams.selection.clear(),
        }
    }

    /// Lets go of every list's rows; the orders stay.
    pub fn clear_all(&mut self) {
        for list in [List::Nodes, List::Bots, List::Tasks, List::Teams] {
            self.clear(list);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::ui::table::{SortBadge, SortKey};

    #[test]
    fn each_list_starts_in_its_own_order() {
        let tables = Tables::default();
        assert_eq!(
            tables.nodes.sort.keys(),
            &[
                SortKey::new(NodeColumn::Status, SortDirection::Ascending),
                SortKey::new(NodeColumn::Node, SortDirection::Ascending)
            ]
        );
        assert_eq!(tables.tasks.sort.keys()[0].column, TaskColumn::Age);
        assert_eq!(tables.teams.sort.keys()[0].column, TeamColumn::Team);
        assert_eq!(tables.busy, None);
    }

    #[test]
    fn shift_held_adds_the_column() {
        let mut tables = Tables::default();
        tables.sort(ColumnRef::Bots(BotColumn::Status));
        assert_eq!(tables.bots.sort.keys().len(), 1);
        tables.shift = true;
        tables.sort(ColumnRef::Bots(BotColumn::Level));
        assert_eq!(
            tables.bots.sort.badge(BotColumn::Level),
            Some(SortBadge {
                order: 2,
                direction: SortDirection::Ascending,
                several: true
            })
        );
        tables.sort(ColumnRef::Nodes(NodeColumn::Axlr));
        tables.sort(ColumnRef::Tasks(TaskColumn::Bot));
        tables.sort(ColumnRef::Teams(TeamColumn::Bots));
        assert_eq!(tables.nodes.sort.keys().len(), 3);
        assert_eq!(tables.tasks.sort.keys().len(), 2);
        assert_eq!(tables.teams.sort.keys().len(), 2);
    }

    #[test]
    fn picks_rows_per_list_and_lets_them_go() {
        let mut tables = Tables::default();
        tables.pick_all(
            vec![
                RowRef::Bot(BotId::new("a").unwrap()),
                RowRef::Bot(BotId::new("b").unwrap()),
            ],
            true,
        );
        tables.pick(RowRef::Task(TaskId::new("t").unwrap()), true);
        tables.pick(RowRef::Node(NodeId::new("n").unwrap()), true);
        tables.pick(RowRef::Team("ops".into()), true);
        assert_eq!(tables.picked(List::Bots), 2);
        for list in [List::Tasks, List::Nodes, List::Teams] {
            assert_eq!(tables.picked(list), 1, "{list:?}");
        }
        tables.clear(List::Bots);
        assert_eq!(tables.picked(List::Bots), 0);
        tables.clear_all();
        assert_eq!(tables.picked(List::Teams), 0);
        assert_eq!(tables.tasks.sort.keys()[0].column, TaskColumn::Age);
    }
}
