use super::TaskGrouping;

/// A group's header in a list, by what it groups: the tasks of one
/// trigger, bot or status, or the bots of one team. What a press on the
/// header names, and what the panel remembers it by.
#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord)]
pub enum GroupKey {
    /// A group of the task list: how the list is grouped and the group's
    /// label, so `Failed` by status is not a bot named so.
    Tasks(TaskGrouping, String),
    /// The bots of a team, by its name.
    Team(String),
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_same_label_under_another_grouping_is_another_group() {
        let by_bot = GroupKey::Tasks(TaskGrouping::Bot, "ops".into());
        let by_trigger = GroupKey::Tasks(TaskGrouping::Trigger, "ops".into());
        assert_ne!(by_bot, by_trigger);
        assert_ne!(by_bot, GroupKey::Team("ops".into()));
        assert_eq!(by_bot, GroupKey::Tasks(TaskGrouping::Bot, "ops".into()));
    }
}
