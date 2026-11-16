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
    /// palette; the find in a task's activity empties as it leaves it,
    /// when the list's filter has nothing to empty; on a list with no
    /// form open the other field is the filter, which Esc empties.
    pub(super) fn escape_taken(&mut self) -> Vec<Effect> {
        if self.palette.is_open() {
            self.palette.close();
            return Vec::new();
        }
        if self.find_shown() && !self.find.typed().is_empty() && self.tables.query.is_empty() {
            self.find.type_in("");
            return Vec::new();
        }
        if self.list_on_screen().is_some() && !self.form_over_list() && !self.tables.query.is_empty() {
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

    /// A click on a result of the palette, while it is open.
    pub(super) fn palette_picked(&mut self, index: usize) -> Vec<Effect> {
        if !self.palette.is_open() {
            return Vec::new();
        }
        let entries = self.palette_entries();
        match self.palette.pick(&entries, index) {
            Some(target) => self.carry_out(target),
            None => Vec::new(),
        }
    }

    /// Does what a result of the palette says. A bot, a team, a node and
    /// a task open in the inspector beside their list, its cursor on
    /// them; a revoked node shows the revoked ones first.
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
                if revoked {
                    self.tables.nodes_shown.set(true);
                    self.rows_changed();
                }
                self.open_row(RowRef::Node(id))
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

#[cfg(test)]
mod tests {
    use super::super::find::{GroupKey, TaskGrouping};
    use super::super::table::List;
    use super::super::test_runtime::send;
    use super::super::{Keypress, ThemeMode};
    use super::*;
    use crate::domain::{BotId, NodeId, TaskId, TaskStatus};

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

    fn lines(app: &App) -> usize {
        app.task_groups().iter().map(|g| g.count()).sum()
    }

    #[test]
    fn two_hundred_tasks_fold_into_ninety_lines() {
        let app = large(Screen::Tasks);
        assert!(app.tables.fold.is_on(), "folded until a person says otherwise");
        assert_eq!(app.tasks_through().len(), 200);
        assert_eq!(lines(&app), 90);
        let groups = app.task_groups();
        let all: Vec<_> = groups.iter().flat_map(|g| g.lines.iter().cloned()).collect();
        assert_eq!(super::super::find::FoldRepeats::folded(&all), 110);
    }

    #[test]
    fn the_filter_narrows_every_list_from_its_top_and_empties_on_another_screen() {
        let mut app = large(Screen::Tasks);
        assert_eq!(
            app.apply(Message::ListFilterTyped("gate failed".into())),
            vec![Effect::ScrollTop]
        );
        assert_eq!(app.tasks_through().len(), 19, "every word must be found");
        assert!(app.tasks_through().iter().all(|t| t.input.is_some()));
        app.apply(Message::ListFilterTyped("support-triage gate".into()));
        assert_eq!(app.tasks_through().len(), 9);
        send(&mut app, Message::Go(Screen::Bots));
        assert!(app.tables.query.is_empty(), "another list starts unfiltered");
        app.apply(Message::ListFilterTyped("lead".into()));
        assert_eq!(super::super::find::TeamGroup::shown(&app.bot_groups()).len(), 8);
        send(&mut app, Message::Go(Screen::Nodes));
        app.apply(Message::ListFilterTyped("host-a".into()));
        assert_eq!(app.node_rows().len(), 2);
        app.apply(Message::ListFilterTyped("host-1".into()));
        assert!(app.node_rows().is_empty(), "the revoked ones stay put away");
        send(&mut app, Message::ShowRevoked(true));
        assert_eq!(app.node_rows().len(), 6);
        send(&mut app, Message::Go(Screen::Teams));
        app.apply(Message::ListFilterTyped("billing-ledger".into()));
        let teams: Vec<String> = app.team_rows().into_iter().map(|t| t.name).collect();
        assert_eq!(teams, vec!["billing"], "a team answers by its members");
    }

    #[test]
    fn a_filter_lets_go_of_the_picks_it_hides() {
        let mut app = large(Screen::Bots);
        send(&mut app, Message::PickShown(List::Bots, true));
        assert_eq!(app.tables.picked(List::Bots), 13);
        send(&mut app, Message::ListFilterTyped("ops".into()));
        assert_eq!(app.tables.picked(List::Bots), 5);
        send(&mut app, Message::PickShown(List::Bots, true));
        assert_eq!(app.tables.picked(List::Bots), 5);
    }

    #[test]
    fn esc_a_field_took_empties_the_filter_or_closes_the_palette() {
        let mut app = large(Screen::Tasks);
        app.apply(Message::ListFilterTyped("gate".into()));
        assert_eq!(app.apply(Message::EscapeTaken), vec![Effect::ScrollTop]);
        assert!(app.tables.query.is_empty());
        assert!(
            app.apply(Message::EscapeTaken).is_empty(),
            "nothing typed, nothing to do"
        );
        // With a form open the Esc was its field's.
        app.apply(Message::ListFilterTyped("gate".into()));
        app.apply(Message::SubmitOpen(None));
        app.apply(Message::EscapeTaken);
        assert_eq!(app.tables.query, "gate");
        app.apply(Message::SubmitClose);
        app.apply(Message::PaletteOpen);
        app.apply(Message::EscapeTaken);
        assert!(!app.palette.is_open(), "the palette's field closes the palette");
        assert_eq!(app.tables.query, "gate", "and leaves the filter alone");
        // The find in a task's activity empties when the filter has
        // nothing to empty.
        app.apply(Message::ListFilterTyped(String::new()));
        let task = app.task_lines_drawn().keys()[0].clone();
        send(&mut app, Message::RowPicked(super::super::table::RowRef::Task(task)));
        app.apply(Message::FindTyped("round".into()));
        app.apply(Message::EscapeTaken);
        assert_eq!(app.find.typed(), "");
        // Off a list, there is no filter to empty.
        send(&mut app, Message::Go(Screen::Settings));
        app.tables.query = "left".into();
        app.apply(Message::EscapeTaken);
        assert_eq!(app.tables.query, "left");
    }

    #[test]
    fn fold_repeats_and_the_groups_change_the_lines() {
        let mut app = large(Screen::Tasks);
        assert_eq!(app.apply(Message::FoldToggled), vec![Effect::ScrollTop]);
        assert_eq!(lines(&app), 200);
        app.apply(Message::FoldToggled);
        assert_eq!(lines(&app), 90);
        assert_eq!(app.apply(Message::GroupingCycled), vec![Effect::ScrollTop]);
        assert_eq!(app.tables.grouping, TaskGrouping::Trigger);
        let groups = app.task_groups();
        assert_eq!(groups[0].label, "event ops.item.assigned");
        assert_eq!(groups[0].count(), 39);
        let key = groups[0].key.clone();
        drop(groups);
        app.apply(Message::GroupToggled(key.clone()));
        assert!(!app.task_groups()[0].open);
        assert_eq!(app.tasks_on_show().len(), 200 - 39);
        app.apply(Message::GroupToggled(key));
        assert!(app.task_groups()[0].open);
        for grouping in [TaskGrouping::Bot, TaskGrouping::Status, TaskGrouping::None] {
            app.apply(Message::GroupingCycled);
            assert_eq!(app.tables.grouping, grouping);
        }
        assert_eq!(app.task_groups().len(), 1, "ungrouped: one group without a header");
    }

    #[test]
    fn a_teams_header_flips_it_whichever_way_it_was_met() {
        let mut app = large(Screen::Bots);
        let audit = GroupKey::Team("audit".into());
        let open = |app: &App, key: &GroupKey| app.bot_groups().iter().find(|g| &g.key == key).unwrap().open;
        assert!(!open(&app, &audit), "a team with no bot on is met closed");
        app.apply(Message::GroupToggled(audit.clone()));
        assert!(open(&app, &audit));
        let ops = GroupKey::Team("ops".into());
        app.apply(Message::GroupToggled(ops.clone()));
        assert!(!open(&app, &ops));
    }

    #[test]
    fn the_palette_opens_with_the_keyboard_in_it_and_its_shortcut_closes_it() {
        let mut app = large(Screen::Tasks);
        assert_eq!(
            app.apply(Message::Key(Keypress::Palette)),
            vec![Effect::Focus(Field::Palette)]
        );
        assert!(app.palette.is_open());
        app.apply(Message::PaletteTyped("ops".into()));
        assert_eq!(app.palette.typed(), "ops");
        assert!(app.apply(Message::Key(Keypress::Palette)).is_empty());
        assert!(!app.palette.is_open());
        app.apply(Message::PaletteOpen);
        assert_eq!(app.palette.typed(), "", "it opens empty");
        app.apply(Message::PaletteClose);
        assert!(!app.palette.is_open());
        app.apply(Message::PaletteOpen);
        app.apply(Message::Key(Keypress::Escape));
        assert!(!app.palette.is_open());
    }

    #[test]
    fn the_palette_takes_the_keys_while_open_and_brings_its_result_into_sight() {
        let mut app = large(Screen::Tasks);
        let cursor = app.tables.tasks.cursor.clone();
        app.apply(Message::PaletteOpen);
        app.apply(Message::PaletteTyped("ops".into()));
        let entries = app.palette_entries();
        assert_eq!(entries[0].label, "ops-lead");
        let effects = app.apply(Message::Key(Keypress::Down));
        assert_eq!(
            effects,
            vec![Effect::Reveal(Scroller::Palette, RowSpot { rows: 1, headers: 1 })]
        );
        assert_eq!(app.tables.tasks.cursor, cursor, "the list behind does not move");
        for _ in 0..5 {
            app.apply(Message::Key(Keypress::Down));
        }
        assert_eq!(
            app.apply(Message::Key(Keypress::Up)),
            vec![Effect::Reveal(Scroller::Palette, RowSpot { rows: 5, headers: 2 })],
            "the sixth result is the team's, under its own heading"
        );
        assert_eq!(
            app.apply(Message::Key(Keypress::Up)),
            vec![Effect::Reveal(Scroller::Palette, RowSpot { rows: 4, headers: 1 })]
        );
        assert!(app.apply(Message::Key(Keypress::Filter)).is_empty());
        app.apply(Message::Key(Keypress::Enter));
        assert!(!app.palette.is_open());
        assert_eq!(app.screen, Screen::Bot(BotId::new("ops-report").unwrap()));
        assert_eq!(
            app.tables.bots.cursor.selected().map(|b| b.as_str()),
            Some("ops-report")
        );
    }

    #[test]
    fn a_click_on_a_result_does_what_it_says_and_nothing_found_does_nothing() {
        let mut app = large(Screen::Tasks);
        app.apply(Message::PaletteOpen);
        app.apply(Message::PaletteTyped("billing".into()));
        let at = app
            .palette_entries()
            .iter()
            .position(|e| e.label == "Team billing")
            .unwrap();
        send(&mut app, Message::PalettePicked(at));
        assert_eq!(app.screen, Screen::Team("billing".into()));
        app.apply(Message::PaletteOpen);
        app.apply(Message::PaletteTyped("zzzz-nothing".into()));
        assert!(app.palette_entries().is_empty());
        assert!(app.apply(Message::Key(Keypress::Enter)).is_empty());
        assert!(!app.palette.is_open());
        assert!(
            app.apply(Message::PalettePicked(3)).is_empty(),
            "a click that comes after the palette closed does nothing"
        );
    }

    #[test]
    fn every_target_of_the_palette_does_what_it_says() {
        let mut app = large(Screen::Overview);
        for (place, screen) in [
            (Place::Tasks, Screen::Tasks),
            (Place::Bots, Screen::Bots),
            (Place::Teams, Screen::Team("audit".into())),
            (Place::Nodes, Screen::Nodes),
            (Place::Settings, Screen::Settings),
            (Place::Home, Screen::Overview),
        ] {
            send(&mut app, Message::Go(Screen::Tasks));
            app.carry_out(PaletteTarget::Go(place));
            assert_eq!(app.screen, screen, "{place:?}");
        }
        app.carry_out(PaletteTarget::NewTask);
        assert!(app.submit.open && app.screen == Screen::Tasks);
        app.apply(Message::SubmitClose);
        app.carry_out(PaletteTarget::NewBot);
        assert!(app.register.open && app.screen == Screen::Bots);
        app.apply(Message::RegisterClose);
        let effects = app.carry_out(PaletteTarget::SwitchTheme);
        assert_eq!(app.mode, ThemeMode::Dark);
        assert!(matches!(effects.as_slice(), [Effect::SavePreferences { .. }]));
        app.carry_out(PaletteTarget::ShowTasks(TaskStatus::Failed));
        assert_eq!(
            (app.screen.clone(), app.filter.status),
            (Screen::Tasks, Some(TaskStatus::Failed))
        );
        app.carry_out(PaletteTarget::ShowTasks(TaskStatus::WaitingApproval));
        assert_eq!(app.filter.status, Some(TaskStatus::WaitingApproval));
        let bot = BotId::new("ops-lead").unwrap();
        app.carry_out(PaletteTarget::Bot(bot.clone()));
        assert_eq!(app.screen, Screen::Bot(bot));
        app.carry_out(PaletteTarget::Team("ops".into()));
        assert_eq!(app.screen, Screen::Team("ops".into()));
        let task = app.fleet.as_ref().unwrap().tasks[7].id.clone();
        app.carry_out(PaletteTarget::Task(task.clone()));
        assert_eq!(app.screen, Screen::Task(task.clone()));
        assert_eq!(app.tables.tasks.cursor.selected(), Some(&task));
        // A node opens in the inspector beside its list, the revoked
        // shown first when it is one of them, its row brought into sight.
        let old = NodeId::new("old-node-40").unwrap();
        let effects = app.carry_out(PaletteTarget::Node {
            id: old.clone(),
            revoked: true,
        });
        assert_eq!(app.screen, Screen::Node(old.clone()));
        assert!(app.tables.nodes_shown.shows_revoked());
        assert_eq!(
            app.cursor_row(List::Nodes),
            Some(super::super::table::RowRef::Node(old))
        );
        assert!(
            effects
                .iter()
                .any(|e| matches!(e, Effect::Reveal(Scroller::Rows, spot) if spot.rows == 47)),
            "{effects:?}"
        );
        assert!(!effects.contains(&Effect::ScrollTop), "the reveal alone moves the rows");
        let _ = TaskId::new("t");
    }

    #[test]
    fn a_result_is_drawn_under_the_headings_up_to_its_own() {
        let mut app = large(Screen::Tasks);
        app.apply(Message::PaletteOpen);
        let entries = app.palette_entries();
        assert_eq!(palette_spot(&entries, 0), RowSpot { rows: 0, headers: 1 });
        let first_bot = entries
            .iter()
            .position(|e| e.section == super::super::find::PaletteSection::Bots)
            .unwrap();
        assert_eq!(
            palette_spot(&entries, first_bot),
            RowSpot {
                rows: first_bot,
                headers: 2
            }
        );
    }
}
