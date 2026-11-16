/// The row of a list the keyboard is on, by the row's key: the arrows
/// move it over whatever the list shows now, and it stays on its row
/// while the list is read again, filtered or folded.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RowCursor<K> {
    selected: Option<K>,
}

impl<K> Default for RowCursor<K> {
    fn default() -> Self {
        Self { selected: None }
    }
}

impl<K: Clone + PartialEq> RowCursor<K> {
    /// The row it is on, when it is on one.
    pub fn selected(&self) -> Option<&K> {
        self.selected.as_ref()
    }

    /// Puts it on a row: a click.
    pub fn select(&mut self, key: K) {
        self.selected = Some(key);
    }

    /// Takes it off every row: Esc.
    pub fn clear(&mut self) {
        self.selected = None;
    }

    /// Where its row is among the rows shown; none when it is on no row
    /// or its row is not shown.
    pub fn index(&self, rows: &[K]) -> Option<usize> {
        let selected = self.selected.as_ref()?;
        rows.iter().position(|row| row == selected)
    }

    /// One row down, staying on the last; onto the first row when it is
    /// on none shown. Says the row it is on after. An empty list leaves
    /// it as it is.
    pub fn down(&mut self, rows: &[K]) -> Option<&K> {
        let to = self.index(rows).map_or(0, |at| at + 1);
        self.go(rows, to)
    }

    /// One row up, staying on the first; onto the first row when it is
    /// on none shown.
    pub fn up(&mut self, rows: &[K]) -> Option<&K> {
        let to = self.index(rows).map_or(0, |at| at.saturating_sub(1));
        self.go(rows, to)
    }

    /// Onto the first row: Enter with no row open, or the down arrow
    /// out of the filter.
    pub fn first(&mut self, rows: &[K]) -> Option<&K> {
        self.go(rows, 0)
    }

    /// Onto the last row.
    pub fn last(&mut self, rows: &[K]) -> Option<&K> {
        self.go(rows, rows.len().saturating_sub(1))
    }

    /// After the list changed: it stays on its row when the row is
    /// still shown, and lets go of it when it is gone. Says whether it
    /// is still on a row.
    pub fn keep(&mut self, rows: &[K]) -> bool {
        if self.index(rows).is_none() {
            self.selected = None;
        }
        self.selected.is_some()
    }

    fn go(&mut self, rows: &[K], to: usize) -> Option<&K> {
        if let Some(row) = rows.get(to.min(rows.len().saturating_sub(1))) {
            self.selected = Some(row.clone());
        }
        self.selected.as_ref()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    const ROWS: [&str; 4] = ["a", "b", "c", "d"];

    #[test]
    fn starts_on_no_row_and_the_first_arrow_takes_the_first() {
        let mut cursor = RowCursor::default();
        assert_eq!(cursor.selected(), None);
        assert_eq!(cursor.index(&ROWS), None);
        assert_eq!(cursor.down(&ROWS), Some(&"a"));
        let mut cursor = RowCursor::default();
        assert_eq!(cursor.up(&ROWS), Some(&"a"));
        let mut cursor = RowCursor::default();
        assert_eq!(cursor.first(&ROWS), Some(&"a"));
        assert_eq!(cursor.last(&ROWS), Some(&"d"));
    }

    #[test]
    fn moves_down_and_up_and_stops_at_the_ends() {
        let mut cursor = RowCursor::default();
        cursor.select("c");
        assert_eq!(cursor.index(&ROWS), Some(2));
        assert_eq!(cursor.down(&ROWS), Some(&"d"));
        assert_eq!(cursor.down(&ROWS), Some(&"d"), "it stays on the last row");
        assert_eq!(cursor.up(&ROWS), Some(&"c"));
        cursor.select("a");
        assert_eq!(cursor.up(&ROWS), Some(&"a"), "it stays on the first row");
        cursor.clear();
        assert_eq!(cursor.selected(), None);
    }

    #[test]
    fn stays_on_its_row_when_the_list_changes_around_it() {
        let mut cursor = RowCursor::default();
        cursor.select("c");
        let newer = ["new", "a", "b", "c", "d"];
        assert!(cursor.keep(&newer));
        assert_eq!(cursor.index(&newer), Some(3));
        assert_eq!(cursor.down(&newer), Some(&"d"));
        let shorter = ["d"];
        assert!(cursor.keep(&shorter));
        assert_eq!(cursor.down(&shorter), Some(&"d"));
    }

    #[test]
    fn lets_go_of_a_row_that_is_gone_and_starts_again_from_the_first() {
        let mut cursor = RowCursor::default();
        cursor.select("d");
        let shrunk = ["a", "b"];
        assert_eq!(cursor.index(&shrunk), None);
        let mut moved = cursor.clone();
        assert_eq!(moved.down(&shrunk), Some(&"a"), "a row not shown counts as no row");
        assert_eq!(moved.up(&shrunk), Some(&"a"));
        assert!(!cursor.keep(&shrunk));
        assert_eq!(cursor.selected(), None);
        assert!(!cursor.keep(&shrunk), "on no row, there is nothing to keep");
    }

    #[test]
    fn an_empty_list_moves_nothing() {
        let none: [&str; 0] = [];
        let mut cursor = RowCursor::default();
        assert_eq!(cursor.down(&none), None);
        assert_eq!(cursor.first(&none), None);
        assert_eq!(cursor.last(&none), None);
        cursor.select("b");
        assert_eq!(
            cursor.down(&none),
            Some(&"b"),
            "the open row stays open over an empty list"
        );
        assert_eq!(cursor.up(&none), Some(&"b"));
        assert!(!cursor.keep(&none));
    }
}
