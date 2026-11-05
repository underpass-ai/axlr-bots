/// What a bot may do with one of its tools, in a person's words. The
/// control plane keeps it as rules: a tool the bot's `autoApprove` names
/// whole runs without asking, a tool a whole deny rule names never runs,
/// any other listed tool asks a person first.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum ToolAccess {
    OnItsOwn,
    AsksFirst,
    Never,
}

impl ToolAccess {
    pub const ALL: [ToolAccess; 3] = [Self::OnItsOwn, Self::AsksFirst, Self::Never];

    /// The words on the choice.
    pub fn label(self) -> &'static str {
        match self {
            Self::OnItsOwn => "Runs on its own",
            Self::AsksFirst => "Asks you first",
            Self::Never => "Never runs",
        }
    }
}

impl std::fmt::Display for ToolAccess {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(self.label())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn says_each_choice_in_words() {
        let words: Vec<String> = ToolAccess::ALL.iter().map(ToString::to_string).collect();
        assert_eq!(words, ["Runs on its own", "Asks you first", "Never runs"]);
    }
}
