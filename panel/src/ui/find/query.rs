use super::{Folded, Marks, Matcher, Searchable, TextMatch};

/// What a person typed to find something: trimmed, read without case,
/// its words apart. A text answers when every word is in it, in any
/// order.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct Query {
    text: String,
    words: Vec<Vec<char>>,
}

impl Query {
    pub fn new(raw: &str) -> Self {
        let text = raw.trim().to_string();
        let words = text.split_whitespace().map(Folded::needle).collect();
        Self { text, words }
    }

    /// Nothing typed but spaces: every row answers.
    pub fn is_empty(&self) -> bool {
        self.words.is_empty()
    }

    /// As typed, without the spaces around.
    pub fn text(&self) -> &str {
        &self.text
    }

    /// How many characters were typed, for a search that waits for two
    /// or three before it looks further.
    pub fn chars(&self) -> usize {
        self.text.chars().count()
    }

    /// Its words, each folded as the texts are.
    pub(super) fn needles(&self) -> &[Vec<char>] {
        &self.words
    }

    /// Whether every word is written in the text: a list's filter.
    pub fn matches(&self, text: &str) -> bool {
        Matcher::strict(self).matches(text)
    }

    /// Every place its words are written in the text, to highlight.
    pub fn marks(&self, text: &str) -> Marks {
        Matcher::strict(self).marks(text)
    }

    /// The rows of a list that answer, in the order they came.
    pub fn filter<'a, T: Searchable>(&self, rows: impl IntoIterator<Item = &'a T>) -> Vec<&'a T> {
        rows.into_iter()
            .filter(|row| self.is_empty() || self.matches(&row.search_text()))
            .collect()
    }

    /// The text scored for a ranked search, words found letter by letter
    /// too; none when it does not answer.
    pub fn rank(&self, text: &str) -> Option<TextMatch> {
        Matcher::loose(self).find(text)
    }
}

#[cfg(test)]
mod tests {
    use super::super::test_rows::task;
    use super::*;

    #[test]
    fn reads_what_was_typed_trimmed_and_in_words() {
        let query = Query::new("  Web   DEPLOY ");
        assert!(!query.is_empty());
        assert_eq!(query.text(), "Web   DEPLOY");
        assert_eq!(query.chars(), 12);
        assert_eq!(query.needles().len(), 2);
        assert_eq!(query.needles()[1], vec!['d', 'e', 'p', 'l', 'o', 'y']);
        assert_eq!(Query::new("ÑU").chars(), 2);
    }

    #[test]
    fn nothing_typed_is_empty_and_every_text_answers() {
        for raw in ["", "   ", "\t\n"] {
            let query = Query::new(raw);
            assert!(query.is_empty());
            assert_eq!(query.chars(), 0);
            assert!(query.matches("anything") && query.matches(""));
            assert!(query.marks("anything").is_empty());
            assert!(query.rank("anything").is_some());
        }
        assert_eq!(Query::default(), Query::new(" "));
    }

    #[test]
    fn filters_by_every_word_in_any_order() {
        let query = Query::new("timer ops");
        assert!(query.matches("ops-watcher · timer clock"));
        assert!(query.matches("OPS TIMER"));
        assert!(!query.matches("ops-watcher · manual"));
        assert_eq!(query.marks("ops timer ops").ranges(), &[0..3, 4..9, 10..13]);
    }

    #[test]
    fn keeps_the_rows_of_a_list_that_answer_in_their_order() {
        let tasks = [
            task("task-3", "ops-watcher", "Completed", "timer:clock", None),
            task("task-2", "lab-reader", "Failed", "manual", None),
            task(
                "task-1",
                "ops-fixer",
                "Completed",
                "event:stock.low",
                Some("3 left of A-12"),
            ),
        ];
        let ids = |typed: &str| -> Vec<&str> {
            Query::new(typed)
                .filter(&tasks)
                .into_iter()
                .map(|t| t.id.as_str())
                .collect()
        };
        assert_eq!(ids("ops"), vec!["task-3", "task-1"]);
        assert_eq!(ids("ops a-12"), vec!["task-1"]);
        assert_eq!(ids(""), vec!["task-3", "task-2", "task-1"]);
        assert!(ids("nothing-like-it").is_empty());
        assert!(Query::new("ops").filter(&[] as &[crate::domain::Task]).is_empty());
    }

    #[test]
    fn ranks_loosely_and_filters_strictly() {
        let query = Query::new("wtch");
        assert!(!query.matches("ops-watcher"));
        let found = query.rank("ops-watcher").unwrap();
        assert!(!found.score.is_exact());
        assert!(query.rank("ops-reader").is_none());
    }
}
