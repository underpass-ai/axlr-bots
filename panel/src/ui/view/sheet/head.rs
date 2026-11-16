use crate::ui::table::{ColumnRef, List, SortBadge};

/// What stands over a column of a list.
#[derive(Debug, Clone, Copy)]
pub enum Head<'a> {
    /// Nothing: the row's acts.
    None,
    /// Its name alone.
    Plain(&'a str),
    /// Its name, which sorts the list when pressed, with the arrow and
    /// the place it has among the sorting columns.
    Sort {
        label: &'a str,
        badge: Option<SortBadge>,
        column: ColumnRef,
    },
    /// The box for every row shown; `all` when they are all picked.
    Check { all: bool, list: List },
}
