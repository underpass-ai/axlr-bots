//! The list screens' tables: sorting by a header, picking rows, and one
//! act on the picked rows from the bar over the list.

use super::table::{BulkPlan, ColumnRef, List, RowRef};
use super::{App, Banner, Confirm, Effect, Screen};
use crate::application::{BulkAction, BulkOutcome};

impl App {
    /// The list the screen on show is, if it has a table to pick in.
    pub fn list_on_screen(&self) -> Option<List> {
        match self.screen {
            Screen::Nodes => Some(List::Nodes),
            Screen::Bots => Some(List::Bots),
            Screen::Tasks => Some(List::Tasks),
            Screen::Incidents => Some(List::Incidents),
            Screen::Teams => Some(List::Teams),
            _ => None,
        }
    }

    pub(super) fn sort_by(&mut self, column: ColumnRef) -> Vec<Effect> {
        self.tables.sort(column);
        Vec::new()
    }

    pub(super) fn pick(&mut self, row: RowRef, picked: bool) -> Vec<Effect> {
        self.tables.pick(row, picked);
        Vec::new()
    }

    /// The header's box: picks every row the list shows now, or lets
    /// them go.
    pub(super) fn pick_shown(&mut self, list: List, picked: bool) -> Vec<Effect> {
        let rows = self.shown_rows(list);
        self.tables.pick_all(rows, picked);
        Vec::new()
    }

    pub(super) fn clear_picks(&mut self, list: List) -> Vec<Effect> {
        self.tables.clear(list);
        Vec::new()
    }

    /// Weighs the act on the picked rows and asks once; nothing when it
    /// fits none or another bulk act is on its way.
    pub(super) fn ask_bulk(&mut self, list: List, action: BulkAction) -> Vec<Effect> {
        if self.tables.busy.is_some() {
            return Vec::new();
        }
        let plan = self.bulk_plan(list, action);
        if plan.ready() {
            self.confirm = Some(Confirm::Bulk(plan));
        }
        Vec::new()
    }

    /// The dialog's yes: one call per row, a few at a time.
    pub(super) fn run_bulk(&mut self, plan: BulkPlan) -> Vec<Effect> {
        if self.tables.busy.is_some() {
            return Vec::new();
        }
        self.tables.busy = Some(plan.action);
        vec![Effect::Bulk {
            action: plan.action,
            targets: plan.targets,
        }]
    }

    /// One banner for the whole act, then the lists read again. The
    /// rows stay picked, as a console keeps them, for the next act.
    pub(super) fn bulk_done(&mut self, outcome: BulkOutcome) -> Vec<Effect> {
        self.tables.busy = None;
        self.banner = Some(Banner::bulk(&outcome, self.now));
        if self.screen == Screen::Incidents {
            self.incidents.list_loading = false;
        }
        let mut effects = self.reload_screen();
        effects.extend(self.refresh_now());
        effects
    }

    /// The act on the picked rows of a list, weighed on what it shows.
    pub fn bulk_plan(&self, list: List, action: BulkAction) -> BulkPlan {
        let tables = &self.tables;
        let fleet = self.fleet.as_ref();
        match list {
            List::Nodes => BulkPlan::for_nodes(
                action,
                &tables.nodes.selection,
                fleet.map(|f| f.nodes.as_slice()).unwrap_or_default(),
            ),
            List::Bots => BulkPlan::for_bots(
                action,
                &tables.bots.selection,
                fleet.map(|f| f.bots.as_slice()).unwrap_or_default(),
            ),
            List::Tasks => BulkPlan::for_tasks(
                action,
                &tables.tasks.selection,
                fleet.map(|f| f.tasks.as_slice()).unwrap_or_default(),
                |id| fleet.and_then(|f| f.call_of(id)).map(|c| c.call_id.clone()),
            ),
            List::Incidents => BulkPlan::for_incidents(
                action,
                &tables.incidents.selection,
                self.incidents.list.as_deref().unwrap_or_default(),
            ),
            List::Teams => BulkPlan::for_teams(action, &tables.teams.selection, &self.teams()),
        }
    }

    /// The rows a list shows now: the Tasks list after its filter.
    pub fn shown_rows(&self, list: List) -> Vec<RowRef> {
        let Some(fleet) = self.fleet.as_ref() else {
            return match list {
                List::Incidents => self.incident_rows(),
                _ => Vec::new(),
            };
        };
        match list {
            List::Nodes => fleet.nodes.iter().map(|n| RowRef::Node(n.id.clone())).collect(),
            List::Bots => fleet.bots.iter().map(|b| RowRef::Bot(b.id.clone())).collect(),
            List::Tasks => self
                .filter
                .apply(&fleet.tasks)
                .into_iter()
                .map(|t| RowRef::Task(t.id.clone()))
                .collect(),
            List::Incidents => self.incident_rows(),
            List::Teams => self.teams().into_iter().map(|t| RowRef::Team(t.name)).collect(),
        }
    }

    fn incident_rows(&self) -> Vec<RowRef> {
        self.incidents
            .list
            .iter()
            .flatten()
            .map(|i| RowRef::Incident(i.id.clone()))
            .collect()
    }

    /// Picked rows whose row is gone, or hidden by the Tasks filter,
    /// drop out.
    pub(super) fn drop_gone_picks(&mut self) {
        if let Some(fleet) = self.fleet.as_ref() {
            let tables = &mut self.tables;
            tables
                .nodes
                .selection
                .retain(|id| fleet.nodes.iter().any(|n| &n.id == id));
            tables
                .bots
                .selection
                .retain(|id| fleet.bots.iter().any(|b| &b.id == id));
            let shown = self.filter.apply(&fleet.tasks);
            tables.tasks.selection.retain(|id| shown.iter().any(|t| &t.id == id));
            let teams = crate::domain::Team::from_bots(&fleet.bots);
            tables
                .teams
                .selection
                .retain(|name| teams.iter().any(|t| &t.name == name));
        }
        if let Some(list) = self.incidents.list.as_ref() {
            self.tables
                .incidents
                .selection
                .retain(|id| list.iter().any(|i| &i.id == id));
        }
    }
}

#[cfg(test)]
mod tests {
    use super::super::test_runtime::{connected, send};
    use super::super::{Keypress, Message};
    use super::*;
    use crate::domain::{BotId, BotStatus, TaskId, TaskStatus};
    use crate::ui::table::{BotColumn, TaskColumn};

    fn on(screen: Screen) -> App {
        let mut app = connected();
        send(&mut app, Message::Go(screen));
        app
    }

    fn bot(id: &str) -> RowRef {
        RowRef::Bot(BotId::new(id).unwrap())
    }

    #[test]
    fn a_bulk_pause_asks_once_then_reports_in_one_banner() {
        let mut app = on(Screen::Bots);
        send(&mut app, Message::PickShown(List::Bots, true));
        assert_eq!(app.tables.picked(List::Bots), 3);
        send(&mut app, Message::AskBulk(List::Bots, BulkAction::Pause));
        let Some(Confirm::Bulk(plan)) = app.confirm.clone() else {
            panic!("{:?}", app.confirm)
        };
        assert_eq!(plan.targets.len(), 3);
        assert_eq!(plan.title(), "Pause 3 bots?");
        let effects = app.apply(Message::ConfirmYes);
        assert!(matches!(&effects[..], [Effect::Bulk { action: BulkAction::Pause, targets }] if targets.len() == 3));
        assert_eq!(app.tables.busy, Some(BulkAction::Pause));
        assert!(app.apply(Message::AskBulk(List::Bots, BulkAction::Stop)).is_empty());
        assert!(app.confirm.is_none());
        super::super::test_runtime::drive(&mut app, effects);
        assert_eq!(app.tables.busy, None);
        assert_eq!(app.banner.as_ref().unwrap().title, "3 paused");
        assert!(
            app.fleet
                .as_ref()
                .unwrap()
                .bots
                .iter()
                .all(|b| b.status == BotStatus::Paused)
        );
        // The rows stay picked for the next act, which now fits them all.
        assert_eq!(app.tables.picked(List::Bots), 3);
        assert_eq!(app.bulk_plan(List::Bots, BulkAction::TurnOn).button(), "Turn on");
        assert_eq!(app.bulk_plan(List::Bots, BulkAction::Pause).button(), "Pause 0 of 3");
    }

    #[test]
    fn an_act_that_fits_no_row_asks_nothing() {
        let mut app = on(Screen::Tasks);
        let done = app
            .fleet
            .as_ref()
            .unwrap()
            .tasks
            .iter()
            .find(|t| t.status == TaskStatus::Completed)
            .unwrap()
            .id
            .clone();
        send(&mut app, Message::Pick(RowRef::Task(done), true));
        send(&mut app, Message::AskBulk(List::Tasks, BulkAction::Cancel));
        assert!(app.confirm.is_none());
    }

    #[test]
    fn approves_waiting_tasks_on_their_calls() {
        let mut app = on(Screen::Tasks);
        send(&mut app, Message::PickShown(List::Tasks, true));
        let total = app.fleet.as_ref().unwrap().tasks.len();
        assert_eq!(app.tables.picked(List::Tasks), total);
        let plan = app.bulk_plan(List::Tasks, BulkAction::Approve);
        assert_eq!(plan.button(), format!("Approve 1 of {total}"));
        send(&mut app, Message::AskBulk(List::Tasks, BulkAction::Approve));
        send(&mut app, Message::ConfirmYes);
        assert_eq!(app.banner.as_ref().unwrap().title, "1 approved");
        // The header's box lets every row shown go.
        send(&mut app, Message::PickShown(List::Tasks, false));
        assert_eq!(app.tables.picked(List::Tasks), 0);
    }

    #[test]
    fn picks_drop_out_when_their_row_is_gone_or_filtered_away() {
        let mut app = on(Screen::Tasks);
        send(&mut app, Message::PickShown(List::Tasks, true));
        send(&mut app, Message::FilterStatus(Some(TaskStatus::Running)));
        assert_eq!(app.tables.picked(List::Tasks), 1);
        send(
            &mut app,
            Message::Pick(RowRef::Task(TaskId::new("task-gone").unwrap()), true),
        );
        send(&mut app, Message::Pick(bot("ghost"), true));
        send(&mut app, Message::Pick(RowRef::Team("ghosts".into()), true));
        send(
            &mut app,
            Message::Pick(RowRef::Node(crate::domain::NodeId::new("gone").unwrap()), true),
        );
        let generation = app.refresh_now();
        super::super::test_runtime::drive(&mut app, generation);
        assert_eq!(app.tables.picked(List::Tasks), 1);
        assert_eq!(app.tables.picked(List::Bots), 0);
        assert_eq!(app.tables.picked(List::Teams), 0);
        assert_eq!(app.tables.picked(List::Nodes), 0);
    }

    #[test]
    fn esc_lets_go_and_another_screen_starts_with_none_picked() {
        let mut app = on(Screen::Bots);
        send(&mut app, Message::Pick(bot("bot"), true));
        send(&mut app, Message::Key(Keypress::Escape));
        assert_eq!(app.tables.picked(List::Bots), 0);
        send(&mut app, Message::Pick(bot("bot"), true));
        send(&mut app, Message::ClearPicks(List::Bots));
        assert_eq!(app.tables.picked(List::Bots), 0);
        send(&mut app, Message::Pick(bot("bot"), true));
        send(&mut app, Message::SortBy(ColumnRef::Bots(BotColumn::Status)));
        send(&mut app, Message::Go(Screen::Nodes));
        assert_eq!(app.tables.picked(List::Bots), 0);
        assert_eq!(app.tables.bots.sort.keys()[0].column, BotColumn::Status);
        assert_eq!(app.list_on_screen(), Some(List::Nodes));
        send(&mut app, Message::Go(Screen::Overview));
        assert_eq!(app.list_on_screen(), None);
    }

    #[test]
    fn shift_held_adds_a_column_to_the_order() {
        let mut app = on(Screen::Tasks);
        send(&mut app, Message::SortBy(ColumnRef::Tasks(TaskColumn::Bot)));
        send(&mut app, Message::Modifiers(iced::keyboard::Modifiers::SHIFT));
        send(&mut app, Message::SortBy(ColumnRef::Tasks(TaskColumn::Status)));
        send(&mut app, Message::Modifiers(iced::keyboard::Modifiers::empty()));
        let keys: Vec<TaskColumn> = app.tables.tasks.sort.keys().iter().map(|k| k.column).collect();
        assert_eq!(keys, vec![TaskColumn::Bot, TaskColumn::Status]);
        assert!(!app.tables.shift);
    }

    #[test]
    fn a_teams_stop_goes_to_its_bots() {
        let fake = std::sync::Arc::new(crate::adapters::fake::FakeControlPlane::sample(
            "2026-10-25T10:00:00Z".parse().unwrap(),
        ));
        fake.set_groups("bot", &["ops"]);
        fake.set_groups("bot-wrx80", &["ops", "lab"]);
        let mut app = super::super::view::tests::app_over(fake.clone());
        send(&mut app, Message::SettingsSave);
        send(&mut app, Message::Go(Screen::Teams));
        send(&mut app, Message::PickShown(List::Teams, true));
        assert_eq!(app.tables.picked(List::Teams), 2);
        let plan = app.bulk_plan(List::Teams, BulkAction::Stop);
        assert_eq!(plan.button(), "Stop 2 bots");
        send(&mut app, Message::AskBulk(List::Teams, BulkAction::Stop));
        send(&mut app, Message::ConfirmYes);
        assert_eq!(app.banner.as_ref().unwrap().title, "2 stopped");
        assert!(fake.calls().iter().filter(|c| c.starts_with("stop_bot")).count() == 2);
    }

    #[test]
    fn incidents_resolve_and_nodes_revoke_in_bulk() {
        let mut app = on(Screen::Incidents);
        send(&mut app, Message::PickShown(List::Incidents, true));
        assert!(app.bulk_plan(List::Incidents, BulkAction::Resolve).ready());
        send(&mut app, Message::AskBulk(List::Incidents, BulkAction::Resolve));
        send(&mut app, Message::ConfirmYes);
        assert!(app.banner.as_ref().unwrap().title.ends_with("resolved"));
        send(&mut app, Message::Go(Screen::Nodes));
        send(&mut app, Message::PickShown(List::Nodes, true));
        send(&mut app, Message::AskBulk(List::Nodes, BulkAction::Revoke));
        assert!(app.confirm.as_ref().is_some_and(|c| c.destructive()));
        send(&mut app, Message::ConfirmYes);
        assert_eq!(app.banner.as_ref().unwrap().title, "2 revoked");
    }

    #[test]
    fn esc_closes_a_dialog_then_the_bots_cards_then_lets_go_of_the_picks() {
        let mut app = on(Screen::Bots);
        send(&mut app, Message::Pick(bot("bot"), true));
        send(&mut app, Message::AskStopBot(BotId::new("bot").unwrap()));
        send(&mut app, Message::Key(Keypress::Escape));
        assert!(app.confirm.is_none());
        assert_eq!(app.tables.picked(List::Bots), 1, "the dialog goes first");
        send(&mut app, Message::Key(Keypress::Escape));
        assert_eq!(app.tables.picked(List::Bots), 0);

        let mut page = on(Screen::Bot(BotId::new("bot").unwrap()));
        send(&mut page, Message::DeleteOpen(BotId::new("bot").unwrap()));
        assert!(page.delete.bot.is_some());
        send(&mut page, Message::Key(Keypress::Escape));
        assert!(page.delete.bot.is_none());
        send(&mut page, Message::EditOpen(BotId::new("bot").unwrap()));
        assert!(page.edit.bot.is_some());
        send(&mut page, Message::Key(Keypress::Escape));
        assert!(page.edit.bot.is_none());
    }
}
