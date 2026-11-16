use super::RowSpot;

/// A list as it is drawn, line after line: a group's header, or a row
/// by its key. What the row cursor moves over (the rows), and where a
/// row is, to scroll it into sight.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct DrawnRows<K> {
    /// Each line: none for a header, the row's key for a row.
    lines: Vec<Option<K>>,
}

impl<K> Default for DrawnRows<K> {
    fn default() -> Self {
        Self { lines: Vec::new() }
    }
}

impl<K: Clone + PartialEq> DrawnRows<K> {
    /// A list of rows alone, without headers.
    pub fn rows(keys: impl IntoIterator<Item = K>) -> Self {
        Self {
            lines: keys.into_iter().map(Some).collect(),
        }
    }

    /// A group's header comes next.
    pub fn header(&mut self) {
        self.lines.push(None);
    }

    /// A row comes next.
    pub fn row(&mut self, key: K) {
        self.lines.push(Some(key));
    }

    /// The rows' keys in order, without the headers.
    pub fn keys(&self) -> Vec<K> {
        self.lines.iter().flatten().cloned().collect()
    }

    /// Where the row is drawn; none when it is not.
    pub fn spot(&self, key: &K) -> Option<RowSpot> {
        let at = self.lines.iter().position(|line| line.as_ref() == Some(key))?;
        let headers = self.lines[..at].iter().filter(|line| line.is_none()).count();
        Some(RowSpot {
            rows: at - headers,
            headers,
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn rows_alone_count_no_headers() {
        let drawn = DrawnRows::rows(["a", "b", "c"]);
        assert_eq!(drawn.keys(), vec!["a", "b", "c"]);
        assert_eq!(drawn.spot(&"c"), Some(RowSpot { rows: 2, headers: 0 }));
        assert_eq!(drawn.spot(&"z"), None);
        assert!(DrawnRows::<&str>::default().keys().is_empty());
    }

    #[test]
    fn a_row_counts_the_headers_above_it_its_own_included() {
        let mut drawn = DrawnRows::default();
        drawn.header();
        drawn.row("a");
        drawn.row("b");
        drawn.header();
        drawn.header();
        drawn.row("c");
        assert_eq!(drawn.keys(), vec!["a", "b", "c"]);
        assert_eq!(drawn.spot(&"a"), Some(RowSpot { rows: 0, headers: 1 }));
        assert_eq!(drawn.spot(&"b"), Some(RowSpot { rows: 1, headers: 1 }));
        assert_eq!(
            drawn.spot(&"c"),
            Some(RowSpot { rows: 2, headers: 3 }),
            "a closed group's header counts too"
        );
    }
}
