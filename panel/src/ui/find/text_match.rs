use super::{Marks, Score};

/// A text a search found: how well it answers, to rank it, and the parts
/// to highlight.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TextMatch {
    pub score: Score,
    pub marks: Marks,
}

impl TextMatch {
    /// What any text is when nothing is searched: found, nothing marked.
    pub fn any() -> Self {
        Self {
            score: Score::any(),
            marks: Marks::none(),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn nothing_searched_marks_nothing() {
        let found = TextMatch::any();
        assert_eq!(found.score, Score::any());
        assert!(found.marks.is_empty());
    }
}
