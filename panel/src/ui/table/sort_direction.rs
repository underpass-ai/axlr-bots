use std::cmp::Ordering;

/// Which way a sorted column runs.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum SortDirection {
    /// The smallest, the first or the most urgent on top.
    Ascending,
    Descending,
}

impl SortDirection {
    /// A comparison of two rows, read in this direction.
    pub fn apply(self, ordering: Ordering) -> Ordering {
        match self {
            Self::Ascending => ordering,
            Self::Descending => ordering.reverse(),
        }
    }

    /// The words of its tip.
    pub fn describe(self) -> &'static str {
        match self {
            Self::Ascending => "ascending",
            Self::Descending => "descending",
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn descending_reverses_the_order() {
        assert_eq!(SortDirection::Ascending.apply(Ordering::Less), Ordering::Less);
        assert_eq!(SortDirection::Descending.apply(Ordering::Less), Ordering::Greater);
        assert_eq!(SortDirection::Descending.apply(Ordering::Equal), Ordering::Equal);
        assert_eq!(SortDirection::Descending.describe(), "descending");
    }
}
