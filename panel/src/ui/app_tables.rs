//! The list screens' tables: sorting by a header, picking rows, and one
//! act on the picked rows from the bar over the list.

use std::collections::BTreeSet;

use super::find::{GroupKey, TeamGroup};
use super::table::{BotShow, BulkPlan, ColumnRef, List, RowRef};
use super::{App, Banner, Confirm, Effect};
use crate::application::{BulkAction, BulkOutcome};
use crate::domain::{BotId, NodeId, TaskId};

impl App {
    /// The list the screen on show is, if it has a table to pick in.
    pub fn list_on_screen(&self) -> Option<List> {
        self.screen.list()
    }

    pub(super) fn sort_by(&mut self, column: ColumnRef) -> Vec<Effect> {
        self.tables.sort(column);
        Vec::new()
    }

    /// A row's box. On a task line that folds repeats, the box picks
    /// or lets go every task the line holds, as its tip says.
    pub(super) fn pick(&mut self, row: RowRef, picked: bool) -> Vec<Effect> {
        let rows = match &row {
            RowRef::Task(id) => self
                .line_of(id)
                .map_or_else(|| vec![row.clone()], |ids| ids.into_iter().map(RowRef::Task).collect()),
            _ => vec![row],
        };
        self.tables.pick_all(rows, picked);
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

    /// A chip over the Bots list; the picked rows it hides are let go.
    pub(super) fn show_bots(&mut self, show: BotShow) -> Vec<Effect> {
        self.tables.bot_show = show;
        self.rows_changed();
        Vec::new()
    }

    /// A group's header pressed, a team's or a task group's: it opens
    /// when it was closed and closes when it was open, whichever way it
    /// started; the picked rows a closed group hides are let go.
    pub(super) fn group_toggled(&mut self, key: GroupKey) -> Vec<Effect> {
        let starts_closed = match &key {
            GroupKey::Team(_) => self
                .bot_groups()
                .iter()
                .find(|group| group.key == key)
                .is_some_and(|group| group.starts_closed),
            GroupKey::Tasks(..) => false,
        };
        self.tables.collapsed.toggle(key, starts_closed);
        self.rows_changed();
        Vec::new()
    }

    /// The revoked nodes listed or put away; put away, the picked ones
    /// are let go.
    pub(super) fn show_revoked(&mut self, revoked: bool) -> Vec<Effect> {
        self.tables.nodes_shown.set(revoked);
        self.rows_changed();
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
            List::Teams => BulkPlan::for_teams(action, &tables.teams.selection, &self.teams()),
        }
    }

    /// The rows a list shows now, as `app_lists` reads them: every task
    /// the task lines on show hold, the folded ones too; the nodes the
    /// filter lets through without the revoked ones put away; the bots
    /// of the chip and the filter in the teams that are open; the teams
    /// the filter lets through.
    pub fn shown_rows(&self, list: List) -> Vec<RowRef> {
        match list {
            List::Nodes => self
                .node_rows()
                .into_iter()
                .map(|n| RowRef::Node(n.id.clone()))
                .collect(),
            List::Bots => TeamGroup::shown(&self.bot_groups())
                .into_iter()
                .map(|b| RowRef::Bot(b.id.clone()))
                .collect(),
            List::Tasks => self.tasks_on_show().into_iter().map(RowRef::Task).collect(),
            List::Teams => self.team_rows().into_iter().map(|t| RowRef::Team(t.name)).collect(),
        }
    }

    /// Picked rows whose row is gone, or hidden by a list's filter, its
    /// chips or a closed group, drop out: an act never reaches a row out
    /// of sight.
    pub(super) fn drop_gone_picks(&mut self) {
        if self.fleet.is_none() {
            return;
        }
        let nodes: BTreeSet<NodeId> = self.node_rows().into_iter().map(|n| n.id.clone()).collect();
        let bots: BTreeSet<BotId> = TeamGroup::keys(&self.bot_groups()).into_iter().collect();
        let tasks: BTreeSet<TaskId> = self.tasks_on_show().into_iter().collect();
        let teams: BTreeSet<String> = self.team_rows().into_iter().map(|t| t.name).collect();
        let tables = &mut self.tables;
        tables.nodes.selection.retain(|id| nodes.contains(id));
        tables.bots.selection.retain(|id| bots.contains(id));
        tables.tasks.selection.retain(|id| tasks.contains(id));
        tables.teams.selection.retain(|name| teams.contains(name));
    }
}

#[cfg(test)]
mod tests {
    use super::super::test_runtime::{connected, send};
    use super::super::{Keypress, Message, Screen};
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

    /// The fleet of a real size, on `screen`.
    fn large(screen: Screen) -> App {
        let fake = std::sync::Arc::new(crate::adapters::fake::FakeControlPlane::large(
            "2026-10-25T10:00:00Z".parse().unwrap(),
        ));
        let mut app = super::super::view::tests::app_over(fake);
        send(&mut app, Message::SettingsSave);
        send(&mut app, Message::Go(screen));
        app
    }

    #[test]
    fn the_box_for_all_never_picks_a_node_put_away() {
        let mut app = large(Screen::Nodes);
        send(&mut app, Message::PickShown(List::Nodes, true));
        assert_eq!(app.tables.picked(List::Nodes), 8, "the revoked ones are out of sight");
        send(&mut app, Message::ShowRevoked(true));
        send(&mut app, Message::PickShown(List::Nodes, true));
        assert_eq!(app.tables.picked(List::Nodes), 48);
        assert_eq!(
            app.bulk_plan(List::Nodes, BulkAction::Revoke).button(),
            "Revoke 8 of 48"
        );
        send(&mut app, Message::ShowRevoked(false));
        assert_eq!(app.tables.picked(List::Nodes), 8, "put away, they are let go");
    }

    #[test]
    fn the_box_for_all_picks_the_bots_on_show_and_a_fold_or_a_chip_lets_go() {
        let mut app = large(Screen::Bots);
        send(&mut app, Message::PickShown(List::Bots, true));
        // Two teams at work of five bots each and three bots in no team;
        // the six teams with no bot on were met closed.
        assert_eq!(app.tables.picked(List::Bots), 13);
        send(&mut app, Message::GroupToggled(GroupKey::Team("ops".into())));
        assert_eq!(app.tables.picked(List::Bots), 8, "a closed team's rows are let go");
        send(&mut app, Message::ShowBots(BotShow::Paused));
        assert_eq!(
            app.tables.picked(List::Bots),
            1,
            "only support's paused bot is in sight"
        );
        send(&mut app, Message::GroupToggled(GroupKey::Team("audit".into())));
        send(&mut app, Message::PickShown(List::Bots, true));
        assert_eq!(app.tables.picked(List::Bots), 5);
        // A pause does not fold a team under the hand: its rows stay
        // picked for the next act.
        send(&mut app, Message::ShowBots(BotShow::All));
        send(&mut app, Message::GroupToggled(GroupKey::Team("audit".into())));
        send(&mut app, Message::ClearPicks(List::Bots));
        send(&mut app, Message::PickShown(List::Bots, true));
        assert_eq!(app.tables.picked(List::Bots), 8);
        send(&mut app, Message::AskBulk(List::Bots, BulkAction::Pause));
        send(&mut app, Message::ConfirmYes);
        assert_eq!(app.tables.picked(List::Bots), 8);
        assert_eq!(app.shown_rows(List::Bots).len(), 8);
    }

    #[test]
    fn nodes_revoke_in_bulk() {
        let mut app = on(Screen::Nodes);
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
