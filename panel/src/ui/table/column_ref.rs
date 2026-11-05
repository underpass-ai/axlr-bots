use super::{BotColumn, IncidentColumn, NodeColumn, TaskColumn, TeamColumn};

/// A header pressed: which list's table, which column.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ColumnRef {
    Nodes(NodeColumn),
    Bots(BotColumn),
    Tasks(TaskColumn),
    Incidents(IncidentColumn),
    Teams(TeamColumn),
}
