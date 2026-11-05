use super::SortDirection;

/// What a sorted column's header shows: its arrow, and its place among
/// the sorting columns when there are more than one.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct SortBadge {
    /// 1 for the column that sorts first.
    pub order: usize,
    pub direction: SortDirection,
    /// More than one column sorts the table: the header shows `order`.
    pub several: bool,
}
