use chrono::{DateTime, Utc};

use super::{PaletteEntry, PaletteTarget, Query};
use crate::application::Fleet;

/// The command palette as a person drives it: open or not, what they
/// typed and which result the arrows are on. Its results are read from
/// the fleet each time, so they are never older than the lists.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct Palette {
    open: bool,
    typed: String,
    selected: usize,
}

impl Palette {
    pub fn is_open(&self) -> bool {
        self.open
    }

    /// Opens it empty, on its first result.
    pub fn open(&mut self) {
        *self = Self {
            open: true,
            ..Self::default()
        };
    }

    /// Closes it and forgets what was typed.
    pub fn close(&mut self) {
        *self = Self::default();
    }

    /// Its shortcut: opens it when closed, closes it when open.
    pub fn toggle(&mut self) {
        if self.open {
            self.close();
        } else {
            self.open();
        }
    }

    /// What its field holds.
    pub fn typed(&self) -> &str {
        &self.typed
    }

    /// The field changed: the arrows go back to the first result.
    pub fn type_in(&mut self, typed: impl Into<String>) {
        self.typed = typed.into();
        self.selected = 0;
    }

    /// What was typed, as the search reads it.
    pub fn query(&self) -> Query {
        Query::new(&self.typed)
    }

    /// Its results now, in the order shown.
    pub fn entries(&self, fleet: &Fleet, now: DateTime<Utc>) -> Vec<PaletteEntry> {
        PaletteEntry::search(&self.query(), fleet, now)
    }

    /// The result the arrows are on among `count` shown: the last one
    /// when a refresh left fewer; none when there is none.
    pub fn selected(&self, count: usize) -> Option<usize> {
        count.checked_sub(1).map(|last| self.selected.min(last))
    }

    /// One result down, staying on the last.
    pub fn down(&mut self, count: usize) {
        self.selected = self.selected(count).map_or(0, |at| (at + 1).min(count - 1));
    }

    /// One result up, staying on the first.
    pub fn up(&mut self, count: usize) {
        self.selected = self.selected(count).map_or(0, |at| at.saturating_sub(1));
    }

    /// Enter: closes the palette and says what the result the arrows are
    /// on does; nothing when nothing is shown.
    pub fn enter(&mut self, entries: &[PaletteEntry]) -> Option<PaletteTarget> {
        let at = self.selected(entries.len());
        self.choose(entries, at)
    }

    /// A click on a result: closes the palette and says what it does.
    pub fn pick(&mut self, entries: &[PaletteEntry], index: usize) -> Option<PaletteTarget> {
        self.choose(entries, Some(index))
    }

    fn choose(&mut self, entries: &[PaletteEntry], at: Option<usize>) -> Option<PaletteTarget> {
        let target = at.and_then(|at| entries.get(at)).map(|entry| entry.target.clone());
        self.close();
        target
    }
}

#[cfg(test)]
mod tests {
    use super::super::Place;
    use super::super::test_rows::{bot, task};
    use super::*;
    use crate::domain::{BotId, TaskId};

    fn now() -> DateTime<Utc> {
        "2026-10-25T09:05:00Z".parse().unwrap()
    }

    fn fleet() -> Fleet {
        Fleet {
            bots: vec![bot("ops-watcher", "ops", "Enabled"), bot("ops-fixer", "ops", "Paused")],
            tasks: vec![task("task-1", "ops-fixer", "Failed", "manual", None)],
            ..Fleet::default()
        }
    }

    #[test]
    fn opens_empty_and_its_shortcut_closes_it_again() {
        let mut palette = Palette::default();
        assert!(!palette.is_open());
        palette.toggle();
        assert!(palette.is_open());
        palette.type_in("ops");
        palette.down(5);
        palette.toggle();
        assert_eq!(palette, Palette::default());
        palette.open();
        assert!(palette.is_open() && palette.typed().is_empty());
        assert_eq!(palette.selected(3), Some(0));
        palette.type_in("x");
        palette.open();
        assert_eq!(palette.typed(), "", "opened again, it starts empty");
        palette.close();
        assert!(!palette.is_open());
    }

    #[test]
    fn the_arrows_stop_at_the_ends_and_typing_goes_back_to_the_first() {
        let mut palette = Palette::default();
        palette.open();
        palette.up(3);
        assert_eq!(palette.selected(3), Some(0));
        palette.down(3);
        palette.down(3);
        palette.down(3);
        assert_eq!(palette.selected(3), Some(2));
        palette.up(3);
        assert_eq!(palette.selected(3), Some(1));
        palette.type_in("o");
        assert_eq!(palette.selected(3), Some(0));
        assert_eq!(palette.query(), Query::new("o"));
    }

    #[test]
    fn stays_on_a_result_when_fewer_are_left() {
        let mut palette = Palette::default();
        palette.open();
        for _ in 0..4 {
            palette.down(5);
        }
        assert_eq!(palette.selected(5), Some(4));
        assert_eq!(palette.selected(2), Some(1), "the list shrank under it");
        palette.up(2);
        assert_eq!(palette.selected(2), Some(0));
        assert_eq!(palette.selected(0), None);
        palette.down(0);
        palette.up(0);
        assert_eq!(palette.selected(1), Some(0));
    }

    #[test]
    fn enter_closes_it_and_says_what_the_result_does() {
        let fleet = fleet();
        let mut palette = Palette::default();
        palette.open();
        palette.type_in("ops");
        let entries = palette.entries(&fleet, now());
        palette.down(entries.len());
        assert_eq!(
            palette.enter(&entries),
            Some(PaletteTarget::Bot(BotId::new("ops-watcher").unwrap()))
        );
        assert!(!palette.is_open() && palette.typed().is_empty());

        palette.open();
        let entries = palette.entries(&fleet, now());
        assert_eq!(palette.enter(&entries), Some(PaletteTarget::Go(Place::Home)));

        palette.open();
        palette.type_in("task-1");
        let entries = palette.entries(&fleet, now());
        assert_eq!(
            palette.enter(&entries),
            Some(PaletteTarget::Task(TaskId::new("task-1").unwrap())),
            "a pasted id goes to its task"
        );
    }

    #[test]
    fn enter_on_nothing_closes_it_and_does_nothing() {
        let mut palette = Palette::default();
        palette.open();
        palette.type_in("zzzz-nothing-like-it");
        let entries = palette.entries(&fleet(), now());
        assert!(entries.is_empty());
        assert_eq!(palette.enter(&entries), None);
        assert!(!palette.is_open());
    }

    #[test]
    fn a_click_chooses_that_result() {
        let fleet = fleet();
        let mut palette = Palette::default();
        palette.open();
        let entries = palette.entries(&fleet, now());
        assert_eq!(palette.pick(&entries, 1), Some(PaletteTarget::Go(Place::Tasks)));
        assert!(!palette.is_open());
        palette.open();
        assert_eq!(palette.pick(&entries, 99), None);
        assert!(!palette.is_open());
    }
}
