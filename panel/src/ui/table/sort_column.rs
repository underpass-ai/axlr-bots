use std::cmp::Ordering;

/// A column a table can be sorted by: its header's words and how two of
/// its rows compare, ascending.
pub trait SortColumn: Copy + Eq + std::fmt::Debug {
    type Row;

    fn label(self) -> &'static str;

    fn compare(self, a: &Self::Row, b: &Self::Row) -> Ordering;
}
