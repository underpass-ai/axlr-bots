use super::{Query, StepFilter, StepText};

/// The find of a task's activity as a person drives it: what they typed,
/// which chip is on and which of the matching steps they are at. The
/// steps are the lines the activity draws, in order; a step is named by
/// its place among them.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct ActivityFind {
    typed: String,
    current: usize,
    kind: StepFilter,
}

impl ActivityFind {
    /// What its field holds.
    pub fn typed(&self) -> &str {
        &self.typed
    }

    /// The field changed: the find goes back to its first match.
    pub fn type_in(&mut self, typed: impl Into<String>) {
        self.typed = typed.into();
        self.current = 0;
    }

    /// What was typed, as the find reads it: every word in a step, in
    /// any order. Also what marks a line's words.
    pub fn query(&self) -> Query {
        Query::new(&self.typed)
    }

    /// The chip that is on.
    pub fn kind(&self) -> StepFilter {
        self.kind
    }

    /// A press on a chip.
    pub fn show(&mut self, kind: StepFilter) {
        self.kind = kind;
    }

    /// Another task opens: nothing typed, every step shown.
    pub fn reset(&mut self) {
        *self = Self::default();
    }

    /// The steps the chip shows, by their place.
    pub fn shown(&self, steps: &[StepText]) -> Vec<usize> {
        (0..steps.len()).filter(|&at| self.kind.shows(&steps[at])).collect()
    }

    /// The steps shown that answer what was typed, by their place; none
    /// while nothing is typed.
    pub fn hits(&self, steps: &[StepText]) -> Vec<usize> {
        let query = self.query();
        if query.is_empty() {
            return Vec::new();
        }
        self.shown(steps)
            .into_iter()
            .filter(|&at| query.matches(&steps[at].text))
            .collect()
    }

    /// The match the find is at, by its step's place: the one to scroll
    /// to and mark apart. The first when fewer matches are left than
    /// where it was.
    pub fn current(&self, steps: &[StepText]) -> Option<usize> {
        let hits = self.hits(steps);
        self.position(hits.len()).map(|at| hits[at])
    }

    /// To the next match, from the last round to the first.
    pub fn next(&mut self, steps: &[StepText]) {
        let matches = self.hits(steps).len();
        if let Some(at) = self.position(matches) {
            self.current = (at + 1) % matches;
        }
    }

    /// To the match before, from the first round to the last.
    pub fn previous(&mut self, steps: &[StepText]) {
        let matches = self.hits(steps).len();
        if let Some(at) = self.position(matches) {
            self.current = (at + matches - 1) % matches;
        }
    }

    /// Where the find is among its matches, `1/7`; `0/0` when nothing
    /// matches; empty while nothing is typed.
    pub fn label(&self, steps: &[StepText]) -> String {
        if self.query().is_empty() {
            return String::new();
        }
        let matches = self.hits(steps).len();
        match self.position(matches) {
            Some(at) => format!("{}/{matches}", at + 1),
            None => "0/0".to_string(),
        }
    }

    /// Which of `matches` it is at; none when there is none.
    fn position(&self, matches: usize) -> Option<usize> {
        match matches {
            0 => None,
            _ if self.current >= matches => Some(0),
            _ => Some(self.current),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn steps() -> Vec<StepText> {
        vec![
            StepText::tool("local_list", "See what files there are", ".", false),
            StepText::tool("local_read", "Read the notes file", "notes.txt", false),
            StepText::note("The notes file says to read the missing file."),
            StepText::tool("local_read", "", "missing.txt", true),
            StepText::event("report.ready", "the notes are read"),
            StepText::tool("local_write", "Write the report", "report.md", false),
        ]
    }

    #[test]
    fn with_nothing_typed_there_is_no_match_and_no_label() {
        let find = ActivityFind::default();
        let steps = steps();
        assert_eq!(find.typed(), "");
        assert_eq!(find.shown(&steps), vec![0, 1, 2, 3, 4, 5]);
        assert!(find.hits(&steps).is_empty());
        assert_eq!(find.current(&steps), None);
        assert_eq!(find.label(&steps), "");
        let mut spaces = ActivityFind::default();
        spaces.type_in("   ");
        assert_eq!(spaces.label(&steps), "");
    }

    #[test]
    fn finds_the_steps_that_answer_by_tool_why_arguments_words_and_events() {
        let steps = steps();
        let mut find = ActivityFind::default();
        find.type_in("notes");
        assert_eq!(find.hits(&steps), vec![1, 2, 4]);
        assert_eq!(find.label(&steps), "1/3");
        assert_eq!(find.current(&steps), Some(1));
        find.type_in("LOCAL_READ");
        assert_eq!(find.hits(&steps), vec![1, 3]);
        find.type_in("file read");
        assert_eq!(find.hits(&steps), vec![1, 2], "every word, in any order");
        find.type_in("report.ready");
        assert_eq!(find.hits(&steps), vec![4]);
        find.type_in("missing.txt");
        assert_eq!(find.hits(&steps), vec![3]);
    }

    #[test]
    fn next_and_previous_go_round() {
        let steps = steps();
        let mut find = ActivityFind::default();
        find.type_in("notes");
        let mut seen = Vec::new();
        for _ in 0..4 {
            seen.push((find.current(&steps), find.label(&steps)));
            find.next(&steps);
        }
        assert_eq!(
            seen,
            vec![
                (Some(1), "1/3".to_string()),
                (Some(2), "2/3".to_string()),
                (Some(4), "3/3".to_string()),
                (Some(1), "1/3".to_string())
            ]
        );
        find.type_in("notes");
        find.previous(&steps);
        assert_eq!((find.current(&steps), find.label(&steps)), (Some(4), "3/3".to_string()));
        find.previous(&steps);
        assert_eq!(find.current(&steps), Some(2));
    }

    #[test]
    fn typing_goes_back_to_the_first_match() {
        let steps = steps();
        let mut find = ActivityFind::default();
        find.type_in("notes");
        find.next(&steps);
        find.next(&steps);
        assert_eq!(find.label(&steps), "3/3");
        find.type_in("note");
        assert_eq!(find.label(&steps), "1/3");
        assert_eq!(find.typed(), "note");
        assert_eq!(find.query(), Query::new("note"));
    }

    #[test]
    fn nothing_matching_is_zero_of_zero_and_the_arrows_do_nothing() {
        let steps = steps();
        let mut find = ActivityFind::default();
        find.type_in("nothing-like-it");
        assert_eq!(find.label(&steps), "0/0");
        find.next(&steps);
        find.previous(&steps);
        assert_eq!(find.current(&steps), None);
        assert_eq!(find.label(&steps), "0/0");
        find.type_in("notes");
        assert_eq!(find.label(&[]), "0/0");
        assert_eq!(find.current(&[]), None);
        assert!(find.shown(&[]).is_empty());
    }

    #[test]
    fn a_chip_narrows_the_steps_and_the_matches() {
        let steps = steps();
        let mut find = ActivityFind::default();
        find.type_in("read");
        assert_eq!(find.hits(&steps), vec![1, 2, 3, 4]);
        find.next(&steps);
        find.next(&steps);
        find.next(&steps);
        assert_eq!(find.label(&steps), "4/4");
        find.show(StepFilter::Tools);
        assert_eq!(find.kind(), StepFilter::Tools);
        assert_eq!(find.shown(&steps), vec![0, 1, 3, 5]);
        assert_eq!(find.hits(&steps), vec![1, 3]);
        assert_eq!(
            (find.current(&steps), find.label(&steps)),
            (Some(1), "1/2".to_string()),
            "fewer matches than where it was: back to the first"
        );
        find.next(&steps);
        assert_eq!(find.current(&steps), Some(3));
        find.show(StepFilter::Failed);
        assert_eq!((find.shown(&steps), find.hits(&steps)), (vec![3], vec![3]));
        find.show(StepFilter::Events);
        assert_eq!(find.current(&steps), Some(4));
        find.reset();
        assert_eq!(find, ActivityFind::default());
    }

    #[test]
    fn follows_the_steps_of_a_task_still_running() {
        let mut steps = steps();
        let mut find = ActivityFind::default();
        find.type_in("report");
        find.next(&steps);
        assert_eq!((find.current(&steps), find.label(&steps)), (Some(5), "2/2".to_string()));
        steps.push(StepText::note("The report is written."));
        assert_eq!((find.current(&steps), find.label(&steps)), (Some(5), "2/3".to_string()));
        find.next(&steps);
        assert_eq!(find.current(&steps), Some(6));
    }
}
