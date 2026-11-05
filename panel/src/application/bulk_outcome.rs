use super::{BulkAction, BulkFailure, PanelError};

/// How a bulk act went: the rows it went through for and those it did
/// not, in the order they were asked.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BulkOutcome {
    pub action: BulkAction,
    pub done: Vec<String>,
    pub failed: Vec<BulkFailure>,
}

impl BulkOutcome {
    pub fn new(action: BulkAction) -> Self {
        Self {
            action,
            done: Vec::new(),
            failed: Vec::new(),
        }
    }

    /// One row's answer.
    pub fn record(&mut self, name: String, result: Result<(), PanelError>) {
        match result {
            Ok(()) => self.done.push(name),
            Err(error) => self.failed.push(BulkFailure::new(name, &error)),
        }
    }

    /// Not one row went through.
    pub fn all_failed(&self) -> bool {
        self.done.is_empty() && !self.failed.is_empty()
    }

    /// The banner's title: `4 paused · 1 failed`.
    pub fn title(&self) -> String {
        let done = match self.done.len() {
            0 => format!("None {}", self.action.past()),
            n => format!("{n} {}", self.action.past()),
        };
        match self.failed.len() {
            0 => done,
            n => format!("{done} · {n} failed"),
        }
    }

    /// The banner's line: why the first failed, or which went through.
    pub fn detail(&self) -> String {
        match self.failed.as_slice() {
            [] => names(&self.done),
            [only] => format!("{}: {}", only.name, only.reason),
            [first, rest @ ..] => format!("{}: {} (and {} more)", first.name, first.reason, rest.len()),
        }
    }
}

/// The first three names, then how many more.
fn names(all: &[String]) -> String {
    const SHOWN: usize = 3;
    let head = all.iter().take(SHOWN).cloned().collect::<Vec<_>>().join(", ");
    match all.len().saturating_sub(SHOWN) {
        0 => head,
        more => format!("{head} and {more} more"),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn outcome(done: usize, failed: &[(&str, &str)]) -> BulkOutcome {
        let mut outcome = BulkOutcome::new(BulkAction::Pause);
        for n in 0..done {
            outcome.record(format!("worker-{n}"), Ok(()));
        }
        for (name, why) in failed {
            outcome.record(name.to_string(), Err(PanelError::State(why.to_string())));
        }
        outcome
    }

    #[test]
    fn counts_what_went_through_and_says_why_one_failed() {
        let mixed = outcome(4, &[("watcher", "bot is a draft")]);
        assert_eq!(mixed.title(), "4 paused · 1 failed");
        assert_eq!(mixed.detail(), "watcher: bot is a draft");
        assert!(!mixed.all_failed());
        let two = outcome(1, &[("a", "x"), ("b", "y"), ("c", "z")]);
        assert_eq!(two.detail(), "a: x (and 2 more)");
    }

    #[test]
    fn names_the_rows_when_all_went_through() {
        assert_eq!(outcome(2, &[]).title(), "2 paused");
        assert_eq!(outcome(2, &[]).detail(), "worker-0, worker-1");
        assert_eq!(outcome(5, &[]).detail(), "worker-0, worker-1, worker-2 and 2 more");
        let none = outcome(0, &[("a", "x")]);
        assert_eq!(none.title(), "None paused · 1 failed");
        assert!(none.all_failed());
    }
}
