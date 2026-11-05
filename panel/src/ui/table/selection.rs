use std::collections::BTreeSet;

/// The rows picked for an act, by id: they stay picked while the list is
/// read again, and drop out when their row is gone.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Selection<K: Ord> {
    ids: BTreeSet<K>,
}

impl<K: Ord> Default for Selection<K> {
    fn default() -> Self {
        Self { ids: BTreeSet::new() }
    }
}

impl<K: Ord + Clone> Selection<K> {
    /// Pick a row (`true`) or let it go.
    pub fn set(&mut self, id: K, picked: bool) {
        if picked {
            self.ids.insert(id);
        } else {
            self.ids.remove(&id);
        }
    }

    /// Pick every row shown, or let them all go.
    pub fn set_all(&mut self, shown: impl IntoIterator<Item = K>, picked: bool) {
        if picked {
            self.ids.extend(shown);
        } else {
            for id in shown {
                self.ids.remove(&id);
            }
        }
    }

    pub fn clear(&mut self) {
        self.ids.clear();
    }

    /// Keeps only the ids still listed.
    pub fn retain(&mut self, listed: impl Fn(&K) -> bool) {
        self.ids.retain(|id| listed(id));
    }

    pub fn contains(&self, id: &K) -> bool {
        self.ids.contains(id)
    }

    pub fn len(&self) -> usize {
        self.ids.len()
    }

    pub fn is_empty(&self) -> bool {
        self.ids.is_empty()
    }

    /// Every row shown is picked, and there is one at least: the
    /// header's box is ticked.
    pub fn covers<'a>(&self, shown: impl IntoIterator<Item = &'a K>) -> bool
    where
        K: 'a,
    {
        let mut any = false;
        for id in shown {
            if !self.ids.contains(id) {
                return false;
            }
            any = true;
        }
        any
    }

    pub fn ids(&self) -> impl Iterator<Item = &K> {
        self.ids.iter()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn picks_and_lets_go_by_id() {
        let mut picked = Selection::default();
        assert!(picked.is_empty());
        picked.set("b", true);
        picked.set("a", true);
        picked.set("a", true);
        assert_eq!(picked.len(), 2);
        assert!(picked.contains(&"a"));
        picked.set("a", false);
        assert_eq!(picked.ids().copied().collect::<Vec<_>>(), vec!["b"]);
        picked.clear();
        assert!(picked.is_empty());
    }

    #[test]
    fn the_header_box_picks_every_row_shown() {
        let mut picked = Selection::default();
        let shown = ["a", "b", "c"];
        assert!(!picked.covers(shown.iter()));
        picked.set_all(shown, true);
        assert!(picked.covers(shown.iter()));
        assert!(!picked.covers([].iter()));
        picked.set("d", true);
        picked.set_all(shown, false);
        assert_eq!(picked.ids().copied().collect::<Vec<_>>(), vec!["d"]);
    }

    #[test]
    fn rows_gone_drop_out() {
        let mut picked = Selection::default();
        picked.set_all(["a", "b", "c"], true);
        picked.retain(|id| *id != "b");
        assert_eq!(picked.ids().copied().collect::<Vec<_>>(), vec!["a", "c"]);
    }
}
