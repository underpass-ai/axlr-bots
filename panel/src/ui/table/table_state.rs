use super::{Selection, SortColumn, SortSpec};

/// One list's table as the person left it: its order and its picked
/// rows.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TableState<C: SortColumn, K: Ord> {
    pub sort: SortSpec<C>,
    pub selection: Selection<K>,
}

impl<C: SortColumn, K: Ord> TableState<C, K> {
    /// A table in its own sensible order, nothing picked.
    pub fn sorted(sort: SortSpec<C>) -> Self {
        Self {
            sort,
            selection: Selection::default(),
        }
    }
}
