use super::{Selection, SortColumn, SortSpec};
use crate::ui::find::RowCursor;

/// One list's table as the person left it: its order, its picked rows
/// and the row the keyboard is on.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TableState<C: SortColumn, K: Ord> {
    pub sort: SortSpec<C>,
    pub selection: Selection<K>,
    /// The row the arrows, a click or the palette put the list on; it
    /// stays while the list is read again, as long as its row is shown.
    pub cursor: RowCursor<K>,
}

impl<C: SortColumn, K: Ord> TableState<C, K> {
    /// A table in its own sensible order, nothing picked, on no row.
    pub fn sorted(sort: SortSpec<C>) -> Self {
        Self {
            sort,
            selection: Selection::default(),
            cursor: RowCursor::default(),
        }
    }
}
