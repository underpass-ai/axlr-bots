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

#[cfg(test)]
mod tests {
    use super::super::find::TeamGroup;
    use super::super::test_runtime::{drive, send};
    use super::super::{Confirm, Message};
    use super::*;
    use crate::domain::{BotId, NodeId, TaskId};

    fn large(screen: Screen) -> App {
        let fake = std::sync::Arc::new(crate::adapters::fake::FakeControlPlane::large(
            "2026-10-25T10:00:00Z".parse().unwrap(),
        ));
        let mut app = super::super::view::tests::app_over(fake);
        send(&mut app, Message::SettingsSave);
        send(&mut app, Message::Go(screen));
        app
    }

    fn task_key(app: &App, at: usize) -> TaskId {
        app.task_lines_drawn().keys()[at].clone()
    }

    #[test]
    fn the_arrows_move_the_cursor_over_the_rows_and_bring_them_into_sight() {
        let mut app = large(Screen::Tasks);
        assert_eq!(app.cursor_row(List::Tasks), None);
        let effects = app.apply(Message::Key(Keypress::Down));
        assert_eq!(effects, vec![Effect::Reveal(Scroller::Rows, RowSpot::default())]);
        assert_eq!(app.cursor_row(List::Tasks), Some(RowRef::Task(task_key(&app, 0))));
        app.apply(Message::Key(Keypress::Down));
        let effects = app.apply(Message::Key(Keypress::Down));
        assert_eq!(
            effects,
            vec![Effect::Reveal(Scroller::Rows, RowSpot { rows: 2, headers: 0 })]
        );
        app.apply(Message::Key(Keypress::Up));
        assert_eq!(app.cursor_row(List::Tasks), Some(RowRef::Task(task_key(&app, 1))));
        for _ in 0..3 {
            app.apply(Message::Key(Keypress::Up));
        }
        assert_eq!(
            app.cursor_row(List::Tasks),
            Some(RowRef::Task(task_key(&app, 0))),
            "it stays on the first"
        );
    }

    #[test]
    fn under_group_headers_a_row_counts_the_headers_above_it() {
        let mut app = large(Screen::Bots);
        let effects = app.apply(Message::Key(Keypress::Down));
        assert_eq!(
            effects,
            vec![Effect::Reveal(Scroller::Rows, RowSpot { rows: 0, headers: 1 })]
        );
        for _ in 0..5 {
            app.apply(Message::Key(Keypress::Down));
        }
        // The fifth bot of the first team, then the first of the next.
        assert_eq!(app.cursor_spot(List::Bots), Some(RowSpot { rows: 5, headers: 2 }));
        let shown = TeamGroup::keys(&app.bot_groups());
        assert_eq!(app.cursor_row(List::Bots), Some(RowRef::Bot(shown[5].clone())));
    }

    #[test]
    fn enter_puts_the_cursor_on_the_first_row_then_opens_it() {
        let mut app = large(Screen::Tasks);
        app.apply(Message::Key(Keypress::Enter));
        assert_eq!(app.screen, Screen::Tasks);
        let first = task_key(&app, 0);
        assert_eq!(app.cursor_row(List::Tasks), Some(RowRef::Task(first.clone())));
        send(&mut app, Message::Key(Keypress::Enter));
        assert_eq!(app.screen, Screen::Task(first.clone()));
        // Back on the list the cursor is still on its row, in sight.
        let effects = app.apply(Message::Go(Screen::Tasks));
        assert!(effects.contains(&Effect::Reveal(Scroller::Rows, RowSpot::default())));
        assert!(!effects.contains(&Effect::ScrollTop));
        assert_eq!(app.cursor_row(List::Tasks), Some(RowRef::Task(first)));
    }

    #[test]
    fn esc_lets_go_of_the_cursor_then_of_the_picks_then_of_the_filter() {
        let mut app = large(Screen::Bots);
        send(&mut app, Message::ListFilterTyped("ops".into()));
        send(&mut app, Message::PickShown(List::Bots, true));
        app.apply(Message::Key(Keypress::Down));
        app.apply(Message::Key(Keypress::Escape));
        assert_eq!(app.cursor_row(List::Bots), None);
        assert_eq!(app.tables.picked(List::Bots), 5);
        app.apply(Message::Key(Keypress::Escape));
        assert_eq!(app.tables.picked(List::Bots), 0);
        assert_eq!(app.tables.query, "ops");
        assert_eq!(app.apply(Message::Key(Keypress::Escape)), vec![Effect::ScrollTop]);
        assert!(app.tables.query.is_empty());
    }

    #[test]
    fn slash_puts_the_keyboard_in_the_filter_of_a_list() {
        let mut app = large(Screen::Nodes);
        assert_eq!(
            app.apply(Message::Key(Keypress::Filter)),
            vec![Effect::Focus(Field::ListFilter)]
        );
        send(&mut app, Message::Go(Screen::Overview));
        assert!(
            app.apply(Message::Key(Keypress::Filter)).is_empty(),
            "Home has no filter"
        );
    }

    #[test]
    fn a_dialog_keeps_the_keys_from_the_list_behind_it() {
        let mut app = large(Screen::Bots);
        app.apply(Message::AskStopBot(BotId::new("ops-lead").unwrap()));
        assert!(matches!(app.confirm, Some(Confirm::StopBot(_))));
        assert!(app.apply(Message::Key(Keypress::Down)).is_empty());
        assert!(app.apply(Message::Key(Keypress::Filter)).is_empty());
        assert_eq!(app.cursor_row(List::Bots), None);
    }

    #[test]
    fn a_form_open_over_a_list_keeps_the_arrows_and_enter_from_its_rows() {
        let mut app = large(Screen::Bots);
        app.apply(Message::RegisterOpen);
        assert!(app.apply(Message::Key(Keypress::Down)).is_empty());
        assert!(app.apply(Message::Key(Keypress::Enter)).is_empty());
        assert!(app.apply(Message::Key(Keypress::Filter)).is_empty());
        assert_eq!(app.cursor_row(List::Bots), None);
        assert_eq!(app.screen, Screen::Bots);
        app.apply(Message::RegisterClose);
        send(&mut app, Message::Go(Screen::Nodes));
        app.apply(Message::MintOpen);
        assert!(app.apply(Message::Key(Keypress::Down)).is_empty());
        app.apply(Message::MintClose);
        send(&mut app, Message::Go(Screen::Tasks));
        app.apply(Message::SubmitOpen(None));
        assert!(app.apply(Message::Key(Keypress::Up)).is_empty());
        app.apply(Message::Key(Keypress::Escape));
        assert!(!app.submit.open, "Esc still closes the form");
    }

    #[test]
    fn ctrl_k_under_a_dialog_opens_nothing_behind_it() {
        let mut app = large(Screen::Bots);
        app.apply(Message::AskStopBot(BotId::new("ops-lead").unwrap()));
        assert!(app.apply(Message::Key(Keypress::Palette)).is_empty());
        assert!(!app.palette.is_open());
        assert!(app.confirm.is_some(), "the dialog still waits for its answer");
    }

    #[test]
    fn a_click_on_a_row_puts_the_cursor_there_and_opens_it() {
        let mut app = large(Screen::Bots);
        let bot = BotId::new("support-lead").unwrap();
        send(&mut app, Message::RowPicked(RowRef::Bot(bot.clone())));
        assert_eq!(app.screen, Screen::Bot(bot.clone()));
        assert_eq!(app.tables.bots.cursor.selected(), Some(&bot));
        send(&mut app, Message::Go(Screen::Teams));
        send(&mut app, Message::RowPicked(RowRef::Team("ops".into())));
        assert_eq!(app.screen, Screen::Team("ops".into()));
        send(&mut app, Message::Go(Screen::Nodes));
        let node = NodeId::new("node-03").unwrap();
        assert!(send(&mut app, Message::RowPicked(RowRef::Node(node.clone()))).is_empty());
        assert_eq!(app.screen, Screen::Nodes, "a node has no page yet: it stays selected");
        assert_eq!(app.cursor_row(List::Nodes), Some(RowRef::Node(node)));
    }

    #[test]
    fn the_cursor_stays_on_its_row_through_a_refresh_and_lets_go_of_a_row_gone() {
        let mut app = large(Screen::Tasks);
        for _ in 0..6 {
            app.apply(Message::Key(Keypress::Down));
        }
        let on = app.cursor_row(List::Tasks).unwrap();
        let refresh = app.refresh_now();
        drive(&mut app, refresh);
        assert_eq!(app.cursor_row(List::Tasks), Some(on.clone()));
        // A line that folds a timer's runs stays under the cursor as a
        // newer run comes: the key is the task the cursor is on.
        let RowRef::Task(id) = on else { panic!() };
        assert!(app.line_of(&id).unwrap().len() > 1);
        send(&mut app, Message::ListFilterTyped("zzzz-nothing".into()));
        assert_eq!(app.cursor_row(List::Tasks), None);
        assert_eq!(app.tables.tasks.cursor.selected(), None, "its row went away");
    }

    #[test]
    fn a_folded_lines_box_picks_every_task_it_holds() {
        let mut app = large(Screen::Tasks);
        let groups = app.task_groups();
        let folded = groups[0].lines.iter().find(|l| l.count() > 1).unwrap();
        let (id, count) = (folded.task.id.clone(), folded.count());
        drop(groups);
        send(&mut app, Message::Pick(RowRef::Task(id.clone()), true));
        assert_eq!(app.tables.picked(List::Tasks), count);
        send(&mut app, Message::Pick(RowRef::Task(id), false));
        assert_eq!(app.tables.picked(List::Tasks), 0);
        send(&mut app, Message::PickShown(List::Tasks, true));
        assert_eq!(
            app.tables.picked(List::Tasks),
            200,
            "the box for all covers the folded tasks"
        );
        let refresh = app.refresh_now();
        drive(&mut app, refresh);
        assert_eq!(app.tables.picked(List::Tasks), 200, "and a refresh keeps them");
    }
}
