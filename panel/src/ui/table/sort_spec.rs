use std::cmp::Ordering;

use super::{SortBadge, SortColumn, SortDirection, SortKey};

/// How a table's rows are ordered: the columns that sort it, the first
/// one first. Empty, the rows keep the order they came in.
///
/// A click on a header sorts by that column alone: ascending, then
/// descending, then not at all. A Shift+click adds the column after the
/// others, or turns it the same way where it already is.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SortSpec<C> {
    keys: Vec<SortKey<C>>,
}

impl<C: SortColumn> SortSpec<C> {
    /// Not sorted: the rows as they came.
    pub fn none() -> Self {
        Self { keys: Vec::new() }
    }

    pub fn by(column: C, direction: SortDirection) -> Self {
        Self {
            keys: vec![SortKey::new(column, direction)],
        }
    }

    /// Then by another column, for rows the ones before leave level.
    pub fn then(mut self, column: C, direction: SortDirection) -> Self {
        self.keys.retain(|k| k.column != column);
        self.keys.push(SortKey::new(column, direction));
        self
    }

    pub fn keys(&self) -> &[SortKey<C>] {
        &self.keys
    }

    /// A press on a column's header; `adding` with Shift held.
    pub fn press(&mut self, column: C, adding: bool) {
        if adding {
            self.add(column);
        } else {
            self.click(column);
        }
    }

    /// Sort by this column alone, or turn it when it is the only one.
    pub fn click(&mut self, column: C) {
        let next = match self.keys.as_slice() {
            [only] if only.column == column => turned(only.direction),
            _ => Some(SortDirection::Ascending),
        };
        self.keys = next.map(|d| vec![SortKey::new(column, d)]).unwrap_or_default();
    }

    /// Sort by this column too, after the others; or turn it in place.
    pub fn add(&mut self, column: C) {
        match self.keys.iter().position(|k| k.column == column) {
            Some(at) => match turned(self.keys[at].direction) {
                Some(direction) => self.keys[at].direction = direction,
                None => {
                    self.keys.remove(at);
                }
            },
            None => self.keys.push(SortKey::new(column, SortDirection::Ascending)),
        }
    }

    /// The header's arrow and number, if the column sorts the table.
    pub fn badge(&self, column: C) -> Option<SortBadge> {
        let at = self.keys.iter().position(|k| k.column == column)?;
        Some(SortBadge {
            order: at + 1,
            direction: self.keys[at].direction,
            several: self.keys.len() > 1,
        })
    }

    /// The rows in this order; rows level on every column keep the order
    /// they came in.
    pub fn order<'a>(&self, rows: impl IntoIterator<Item = &'a C::Row>) -> Vec<&'a C::Row>
    where
        C::Row: 'a,
    {
        let mut rows: Vec<&C::Row> = rows.into_iter().collect();
        if !self.keys.is_empty() {
            rows.sort_by(|a, b| self.compare(a, b));
        }
        rows
    }

    fn compare(&self, a: &C::Row, b: &C::Row) -> Ordering {
        self.keys
            .iter()
            .map(|k| k.direction.apply(k.column.compare(a, b)))
            .find(|o| o.is_ne())
            .unwrap_or(Ordering::Equal)
    }
}

/// Ascending, then descending, then off.
fn turned(direction: SortDirection) -> Option<SortDirection> {
    match direction {
        SortDirection::Ascending => Some(SortDirection::Descending),
        SortDirection::Descending => None,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// A row of two numbers, to sort by either.
    #[derive(Debug, Clone, Copy, PartialEq, Eq)]
    enum Col {
        First,
        Second,
    }

    impl SortColumn for Col {
        type Row = (u32, u32);

        fn label(self) -> &'static str {
            match self {
                Col::First => "First",
                Col::Second => "Second",
            }
        }

        fn compare(self, a: &(u32, u32), b: &(u32, u32)) -> Ordering {
            match self {
                Col::First => a.0.cmp(&b.0),
                Col::Second => a.1.cmp(&b.1),
            }
        }
    }

    const ROWS: [(u32, u32); 4] = [(2, 1), (1, 2), (2, 0), (1, 1)];

    fn order(spec: &SortSpec<Col>) -> Vec<(u32, u32)> {
        spec.order(ROWS.iter()).into_iter().copied().collect()
    }

    #[test]
    fn a_click_sorts_up_then_down_then_not_at_all() {
        let mut spec = SortSpec::none();
        assert_eq!(order(&spec), ROWS.to_vec());
        spec.click(Col::First);
        assert_eq!(order(&spec), vec![(1, 2), (1, 1), (2, 1), (2, 0)]);
        assert_eq!(
            spec.badge(Col::First),
            Some(SortBadge {
                order: 1,
                direction: SortDirection::Ascending,
                several: false
            })
        );
        spec.click(Col::First);
        assert_eq!(order(&spec), vec![(2, 1), (2, 0), (1, 2), (1, 1)]);
        spec.click(Col::First);
        assert_eq!(spec, SortSpec::none());
        assert_eq!(spec.badge(Col::First), None);
        assert_eq!(Col::Second.label(), "Second");
    }

    #[test]
    fn a_click_on_another_column_sorts_by_it_alone() {
        let mut spec = SortSpec::by(Col::First, SortDirection::Ascending).then(Col::Second, SortDirection::Descending);
        spec.click(Col::Second);
        assert_eq!(spec, SortSpec::by(Col::Second, SortDirection::Ascending));
        spec.press(Col::First, false);
        assert_eq!(spec, SortSpec::by(Col::First, SortDirection::Ascending));
    }

    #[test]
    fn shift_adds_a_second_column_and_turns_it_in_place() {
        let mut spec = SortSpec::by(Col::First, SortDirection::Ascending);
        spec.press(Col::Second, true);
        assert_eq!(order(&spec), vec![(1, 1), (1, 2), (2, 0), (2, 1)]);
        let second = spec.badge(Col::Second).unwrap();
        assert_eq!((second.order, second.several), (2, true));
        spec.add(Col::Second);
        assert_eq!(order(&spec), vec![(1, 2), (1, 1), (2, 1), (2, 0)]);
        assert_eq!(spec.keys()[1].direction, SortDirection::Descending);
        spec.add(Col::Second);
        assert_eq!(spec, SortSpec::by(Col::First, SortDirection::Ascending));
        spec.add(Col::First);
        spec.add(Col::First);
        assert_eq!(spec, SortSpec::none());
    }

    #[test]
    fn then_moves_a_column_to_the_end() {
        let spec = SortSpec::by(Col::First, SortDirection::Ascending).then(Col::First, SortDirection::Descending);
        assert_eq!(spec.keys().len(), 1);
        assert_eq!(spec.keys()[0].direction, SortDirection::Descending);
    }
}
