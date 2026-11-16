//! Finding things: the filter over the list on show, the Tasks list's
//! folds and groups, and the command palette with what each of its
//! results does.

use super::find::{PaletteEntry, PaletteTarget, Place, RowSpot};
use super::table::RowRef;
use super::{App, Effect, Field, Keypress, Message, Screen, Scroller};
use crate::application::Fleet;

impl App {
    /// The filter typed into: the list shows what answers, from its top.
    pub(super) fn list_filter_typed(&mut self, typed: String) -> Vec<Effect> {
        self.tables.query = typed;
        self.rows_changed();
        vec![Effect::ScrollTop]
    }

    /// The filter emptied: the list shows every row again, from its top.
    pub(super) fn clear_filter(&mut self) -> Vec<Effect> {
        self.list_filter_typed(String::new())
    }

    /// Esc that a field took and left: the palette's closes the
    /// palette; on a list with no form open the only field is the
    /// filter, which Esc empties as it leaves it.
    pub(super) fn escape_taken(&mut self) -> Vec<Effect> {
        if self.palette.is_open() {
            self.palette.close();
            return Vec::new();
        }
        let form = self.submit.open || self.register.open || self.mint.open;
        if self.list_on_screen().is_some() && !form && !self.tables.query.is_empty() {
            return self.clear_filter();
        }
        Vec::new()
    }

    /// Fold repeats on or off; the list from its top.
    pub(super) fn fold_toggled(&mut self) -> Vec<Effect> {
        self.tables.fold.toggle();
        self.rows_changed();
        vec![Effect::ScrollTop]
    }

    /// The next grouping of the Tasks list; the list from its top.
    pub(super) fn grouping_cycled(&mut self) -> Vec<Effect> {
        self.tables.grouping = self.tables.grouping.next();
        self.rows_changed();
        vec![Effect::ScrollTop]
    }

    /// The palette opens empty with the keyboard in its field.
    pub(super) fn palette_open(&mut self) -> Vec<Effect> {
        self.palette.open();
        vec![Effect::Focus(Field::Palette)]
    }

    /// What the palette shows now, from the fleet as last read.
    pub fn palette_entries(&self) -> Vec<PaletteEntry> {
        match self.fleet.as_ref() {
            Some(fleet) => self.palette.entries(fleet, self.now),
            None => self.palette.entries(&Fleet::default(), self.now),
        }
    }

    /// A key while the palette is open: the arrows move over its
    /// results and bring the one they reach into sight, Enter does what
    /// it says, Esc and its shortcut close it.
    pub(super) fn palette_key(&mut self, key: Keypress) -> Vec<Effect> {
        let entries = self.palette_entries();
        match key {
            Keypress::Escape | Keypress::Palette => {
                self.palette.close();
                Vec::new()
            }
            Keypress::Up | Keypress::Down => {
                if key == Keypress::Up {
                    self.palette.up(entries.len());
                } else {
                    self.palette.down(entries.len());
                }
                self.palette
                    .selected(entries.len())
                    .map(|at| Effect::Reveal(Scroller::Palette, palette_spot(&entries, at)))
                    .into_iter()
                    .collect()
            }
            Keypress::Enter => match self.palette.enter(&entries) {
                Some(target) => self.carry_out(target),
                None => Vec::new(),
            },
            _ => Vec::new(),
        }
    }

    /// A click on a result of the palette.
    pub(super) fn palette_picked(&mut self, index: usize) -> Vec<Effect> {
        let entries = self.palette_entries();
        match self.palette.pick(&entries, index) {
            Some(target) => self.carry_out(target),
            None => Vec::new(),
        }
    }

    /// Does what a result of the palette says. A bot, a team and a task
    /// open their page with their list's cursor on them; a node has no
    /// page, so the Nodes list opens on it, the revoked ones shown first
    /// when it is one of them.
    pub(super) fn carry_out(&mut self, target: PaletteTarget) -> Vec<Effect> {
        match target {
            PaletteTarget::Go(place) => self.go(place_screen(place)),
            PaletteTarget::NewTask => self.handle(Message::SubmitOpen(None)),
            PaletteTarget::NewBot => {
                let mut effects = self.go(Screen::Bots);
                effects.extend(self.handle(Message::RegisterOpen));
                effects
            }
            PaletteTarget::SwitchTheme => self.handle(Message::ToggleTheme),
            PaletteTarget::ShowTasks(status) => self.handle(Message::FilterStatus(Some(status))),
            PaletteTarget::Bot(id) => self.open_row(RowRef::Bot(id)),
            PaletteTarget::Team(name) => self.open_row(RowRef::Team(name)),
            PaletteTarget::Task(id) => self.open_row(RowRef::Task(id)),
            PaletteTarget::Node { id, revoked } => {
                let mut effects = self.go(Screen::Nodes);
                if revoked {
                    self.tables.nodes_shown.set(true);
                }
                self.select_row(RowRef::Node(id));
                self.rows_changed();
                if let Some(spot) = self.cursor_spot(super::table::List::Nodes) {
                    effects.push(Effect::Reveal(Scroller::Rows, spot));
                }
                effects
            }
        }
    }
}

/// The screen of a place of the rail.
fn place_screen(place: Place) -> Screen {
    match place {
        Place::Home => Screen::Overview,
        Place::Tasks => Screen::Tasks,
        Place::Bots => Screen::Bots,
        Place::Teams => Screen::Teams,
        Place::Nodes => Screen::Nodes,
        Place::Settings => Screen::Settings,
    }
}

/// Where a result of the palette is drawn: the results before it and
/// the section headings up to its own.
fn palette_spot(entries: &[PaletteEntry], at: usize) -> RowSpot {
    let mut headers = 0;
    let mut last = None;
    for entry in &entries[..=at.min(entries.len().saturating_sub(1))] {
        if last != Some(entry.section) {
            headers += 1;
            last = Some(entry.section);
        }
    }
    RowSpot { rows: at, headers }
}
