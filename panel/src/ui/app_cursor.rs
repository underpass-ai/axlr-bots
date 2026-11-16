//! The row cursor of the list on show: the arrows (and `j`, `k`) move
//! it over the rows the list shows and bring its row into sight, Enter
//! opens its row, Esc lets go of it; a click on a row puts it there and
//! opens the row. Each list keeps its own cursor in its table.

use super::find::{DrawnRows, RowCursor, RowSpot};
use super::table::{List, RowRef};
use super::{App, Effect, Field, Keypress, Screen, Scroller};

impl App {
    /// A key for the list on show: the arrows move its cursor, Enter
    /// opens the row under it (or puts it on the first row), Esc lets
    /// go of the cursor, then of the picked rows, then of the filter,
    /// and `/` puts the keyboard in the filter.
    pub(super) fn list_key(&mut self, list: List, key: Keypress) -> Vec<Effect> {
        match key {
            Keypress::Up | Keypress::Down => self.move_cursor(list, key),
            Keypress::Enter => match self.cursor_row(list) {
                Some(row) => self.open_row(row),
                None => self.move_cursor(list, Keypress::Down),
            },
            Keypress::Escape if self.cursor_row(list).is_some() => {
                self.clear_cursor(list);
                Vec::new()
            }
            Keypress::Escape if self.tables.picked(list) > 0 => self.clear_picks(list),
            Keypress::Escape if !self.tables.query.is_empty() => self.clear_filter(),
            Keypress::Filter if self.fleet.is_some() => vec![Effect::Focus(Field::ListFilter)],
            _ => Vec::new(),
        }
    }

    /// The row under the list's cursor, when the list shows it.
    pub fn cursor_row(&self, list: List) -> Option<RowRef> {
        let tables = &self.tables;
        match list {
            List::Tasks => on_show(&tables.tasks.cursor, &self.task_lines_drawn()).map(RowRef::Task),
            List::Bots => on_show(&tables.bots.cursor, &self.bot_lines_drawn()).map(RowRef::Bot),
            List::Nodes => on_show(&tables.nodes.cursor, &self.node_lines_drawn()).map(RowRef::Node),
            List::Teams => on_show(&tables.teams.cursor, &self.team_lines_drawn()).map(RowRef::Team),
        }
    }

    /// One row up or down, onto the first when it is on none; its row
    /// brought into sight.
    fn move_cursor(&mut self, list: List, key: Keypress) -> Vec<Effect> {
        let up = key == Keypress::Up;
        let spot = match list {
            List::Tasks => {
                let drawn = self.task_lines_drawn();
                step(&mut self.tables.tasks.cursor, &drawn, up)
            }
            List::Bots => {
                let drawn = self.bot_lines_drawn();
                step(&mut self.tables.bots.cursor, &drawn, up)
            }
            List::Nodes => {
                let drawn = self.node_lines_drawn();
                step(&mut self.tables.nodes.cursor, &drawn, up)
            }
            List::Teams => {
                let drawn = self.team_lines_drawn();
                step(&mut self.tables.teams.cursor, &drawn, up)
            }
        };
        spot.map(|spot| Effect::Reveal(Scroller::Rows, spot))
            .into_iter()
            .collect()
    }

    fn clear_cursor(&mut self, list: List) {
        match list {
            List::Tasks => self.tables.tasks.cursor.clear(),
            List::Bots => self.tables.bots.cursor.clear(),
            List::Nodes => self.tables.nodes.cursor.clear(),
            List::Teams => self.tables.teams.cursor.clear(),
        }
    }

    /// Puts the row's list cursor on it, wherever the list is.
    pub(super) fn select_row(&mut self, row: RowRef) {
        match row {
            RowRef::Task(id) => self.tables.tasks.cursor.select(id),
            RowRef::Bot(id) => self.tables.bots.cursor.select(id),
            RowRef::Node(id) => self.tables.nodes.cursor.select(id),
            RowRef::Team(name) => self.tables.teams.cursor.select(name),
        }
    }

    /// Opens a row: its list's cursor goes to it, and its page opens. A
    /// node has no page: it stays selected in its list. The one place a
    /// row is opened, by a click, Enter or the palette.
    pub(super) fn open_row(&mut self, row: RowRef) -> Vec<Effect> {
        self.select_row(row.clone());
        match row {
            RowRef::Task(id) => self.go(Screen::Task(id)),
            RowRef::Bot(id) => self.go(Screen::Bot(id)),
            RowRef::Team(name) => self.go(Screen::Team(name)),
            RowRef::Node(_) => Vec::new(),
        }
    }

    /// Where the row under the cursor is drawn in the list on show.
    pub fn cursor_spot(&self, list: List) -> Option<RowSpot> {
        let tables = &self.tables;
        match list {
            List::Tasks => spot(&tables.tasks.cursor, &self.task_lines_drawn()),
            List::Bots => spot(&tables.bots.cursor, &self.bot_lines_drawn()),
            List::Nodes => spot(&tables.nodes.cursor, &self.node_lines_drawn()),
            List::Teams => spot(&tables.teams.cursor, &self.team_lines_drawn()),
        }
    }
}

/// The cursor's row when the list draws it.
fn on_show<K: Clone + PartialEq>(cursor: &RowCursor<K>, drawn: &DrawnRows<K>) -> Option<K> {
    cursor.index(&drawn.keys()).and(cursor.selected().cloned())
}

/// Where the cursor's row is drawn.
fn spot<K: Clone + PartialEq>(cursor: &RowCursor<K>, drawn: &DrawnRows<K>) -> Option<RowSpot> {
    drawn.spot(cursor.selected()?)
}

/// The cursor one row up or down over what is drawn, and where its row
/// is then; none over an empty list.
fn step<K: Clone + PartialEq>(cursor: &mut RowCursor<K>, drawn: &DrawnRows<K>, up: bool) -> Option<RowSpot> {
    let keys = drawn.keys();
    let at = if up { cursor.up(&keys) } else { cursor.down(&keys) }?.clone();
    drawn.spot(&at)
}
