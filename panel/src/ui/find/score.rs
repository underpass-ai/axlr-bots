use std::cmp::Reverse;

/// How well a text answers a search, to put results in order: the
/// greater, the better. Words found as written beat words found letter
/// by letter; then the tighter find wins, then the earlier one, then the
/// shorter text.
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
pub struct Score {
    exact: bool,
    spread: Reverse<usize>,
    start: Reverse<usize>,
    length: Reverse<usize>,
}

impl Score {
    /// The score of a find: `exact` when every word was found as
    /// written, `spread` the characters the finds cover from their first
    /// to their last, `start` where the earliest starts and `length` how
    /// long the text is, all in characters.
    pub fn of(exact: bool, spread: usize, start: usize, length: usize) -> Self {
        Self {
            exact,
            spread: Reverse(spread),
            start: Reverse(start),
            length: Reverse(length),
        }
    }

    /// What every text scores when nothing is searched: all the same, so
    /// the results keep the order they came in.
    pub fn any() -> Self {
        Self::of(true, 0, 0, 0)
    }

    /// Whether every word was found as written, none letter by letter.
    pub fn is_exact(self) -> bool {
        self.exact
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn as_written_beats_letter_by_letter() {
        assert!(Score::of(true, 3, 90, 200) > Score::of(false, 3, 0, 5));
        assert!(Score::of(true, 3, 0, 5).is_exact());
        assert!(!Score::of(false, 3, 0, 5).is_exact());
    }

    #[test]
    fn then_tighter_then_earlier_then_shorter() {
        assert!(Score::of(false, 3, 9, 50) > Score::of(false, 5, 0, 50));
        assert!(Score::of(true, 3, 2, 50) > Score::of(true, 3, 7, 50));
        assert!(Score::of(true, 3, 2, 20) > Score::of(true, 3, 2, 50));
        assert_eq!(Score::of(true, 3, 2, 20), Score::of(true, 3, 2, 20));
    }

    #[test]
    fn nothing_searched_scores_every_text_the_same() {
        assert_eq!(Score::any(), Score::any());
        assert!(Score::any().is_exact());
    }
}
