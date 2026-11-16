/// A tab of a team in the inspector: what happens in it, and who is in
/// it.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub enum TeamTab {
    #[default]
    Happens,
    Members,
}

impl TeamTab {
    /// Every tab, in the order shown.
    pub const ALL: [Self; 2] = [Self::Happens, Self::Members];

    /// The tab's words.
    pub fn label(self) -> &'static str {
        match self {
            Self::Happens => "What happens",
            Self::Members => "Who is in it",
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn opens_on_what_happens() {
        assert_eq!(TeamTab::default(), TeamTab::Happens);
        assert_eq!(TeamTab::Members.label(), "Who is in it");
    }
}
