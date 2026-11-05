use super::SortDirection;

/// One column a table is sorted by, and which way.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub struct SortKey<C> {
    pub column: C,
    pub direction: SortDirection,
}

impl<C> SortKey<C> {
    pub fn new(column: C, direction: SortDirection) -> Self {
        Self { column, direction }
    }
}
