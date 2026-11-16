use std::collections::BTreeMap;

use super::GroupKey;

/// The group headers a person closed or opened by hand, and how each
/// header stood when the panel first showed it. A header never touched
/// stands as it was met (a team with no bot on starts closed), so a
/// refresh that pauses a team's last bot never folds it under the hand;
/// once touched it stays as the person left it.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct Collapsed {
    /// Each touched header, and whether it was left closed.
    chosen: BTreeMap<GroupKey, bool>,
    /// Each header met so far, and whether it started closed.
    met: BTreeMap<GroupKey, bool>,
}

impl Collapsed {
    /// Whether the group is closed: as the person left it, or as its
    /// list starts it.
    pub fn is_closed(&self, key: &GroupKey, starts_closed: bool) -> bool {
        self.chosen.get(key).copied().unwrap_or(starts_closed)
    }

    /// A press on the header: the group closes when it was open and
    /// opens when it was closed, whichever way it started.
    pub fn toggle(&mut self, key: GroupKey, starts_closed: bool) {
        let closed = self.is_closed(&key, starts_closed);
        self.chosen.insert(key, !closed);
    }

    /// Leaves the group closed (`true`) or open.
    pub fn set(&mut self, key: GroupKey, closed: bool) {
        self.chosen.insert(key, closed);
    }

    /// The group as the panel first showed it: whether it started
    /// closed. A group met before keeps how it was met.
    pub fn meet(&mut self, key: GroupKey, closed: bool) {
        self.met.entry(key).or_insert(closed);
    }

    /// Whether the group started closed when it was met; none for a
    /// group not met yet.
    pub fn met(&self, key: &GroupKey) -> Option<bool> {
        self.met.get(key).copied()
    }

    /// Forgets every choice and every group met: the groups stand as
    /// their lists start them.
    pub fn clear(&mut self) {
        self.chosen.clear();
        self.met.clear();
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn team(name: &str) -> GroupKey {
        GroupKey::Team(name.into())
    }

    #[test]
    fn an_untouched_header_stands_as_its_list_starts_it() {
        let collapsed = Collapsed::default();
        assert!(!collapsed.is_closed(&team("ops"), false));
        assert!(collapsed.is_closed(&team("ops"), true));
    }

    #[test]
    fn a_press_flips_the_group_whichever_way_it_started() {
        let mut collapsed = Collapsed::default();
        collapsed.toggle(team("idle"), true);
        assert!(!collapsed.is_closed(&team("idle"), true), "it started closed: it opens");
        collapsed.toggle(team("idle"), true);
        assert!(collapsed.is_closed(&team("idle"), true));
        collapsed.toggle(team("ops"), false);
        assert!(collapsed.is_closed(&team("ops"), false));
        assert!(
            collapsed.is_closed(&team("ops"), true) && collapsed.is_closed(&team("idle"), false),
            "a touched header stays as it was left when its list would start it otherwise"
        );
    }

    #[test]
    fn a_group_keeps_how_it_was_met() {
        let mut collapsed = Collapsed::default();
        assert_eq!(collapsed.met(&team("ops")), None);
        collapsed.meet(team("ops"), false);
        collapsed.meet(team("ops"), true);
        assert_eq!(collapsed.met(&team("ops")), Some(false), "the first meeting stands");
        collapsed.meet(team("idle"), true);
        assert_eq!(collapsed.met(&team("idle")), Some(true));
    }

    #[test]
    fn sets_and_forgets() {
        let mut collapsed = Collapsed::default();
        collapsed.set(team("ops"), true);
        collapsed.meet(team("ops"), false);
        assert!(collapsed.is_closed(&team("ops"), false));
        collapsed.set(team("ops"), false);
        assert!(!collapsed.is_closed(&team("ops"), true));
        collapsed.clear();
        assert_eq!(collapsed, Collapsed::default());
    }
}
