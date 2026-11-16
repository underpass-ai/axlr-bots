use crate::domain::Bot;

/// A team's bots in the Bots list, under a header that folds them away:
/// how many are on and paused in the whole team, whether it is open,
/// and the bots the chips let through, in the table's order.
#[derive(Debug, Clone, PartialEq)]
pub struct BotGroup<'a> {
    /// The team's name; empty for the bots in no team.
    pub team: String,
    pub on: usize,
    pub paused: usize,
    pub open: bool,
    pub bots: Vec<&'a Bot>,
}

impl BotGroup<'_> {
    /// The name on its header: the team's, or `no team`.
    pub fn name(&self) -> &str {
        if self.team.is_empty() { "no team" } else { &self.team }
    }

    /// The counts on its header: `3 on · 1 paused`, leaving out a count
    /// of none; `empty` for a team of bots that are neither.
    pub fn counts(&self) -> String {
        let mut parts = Vec::new();
        if self.on > 0 {
            parts.push(format!("{} on", self.on));
        }
        if self.paused > 0 {
            parts.push(format!("{} paused", self.paused));
        }
        if parts.is_empty() {
            "none on".to_string()
        } else {
            parts.join(" · ")
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn names_itself_and_counts_its_bots() {
        let mut group = BotGroup {
            team: "ops".into(),
            on: 3,
            paused: 1,
            open: true,
            bots: Vec::new(),
        };
        assert_eq!((group.name(), group.counts().as_str()), ("ops", "3 on · 1 paused"));
        group.on = 0;
        group.team.clear();
        assert_eq!((group.name(), group.counts().as_str()), ("no team", "1 paused"));
        group.paused = 0;
        assert_eq!(group.counts(), "none on");
    }
}
