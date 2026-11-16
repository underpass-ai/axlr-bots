//! The inspector beside a list: its tabs, the find in a task's activity
//! with its matches, following a running task's newest step, and sending
//! a task again.

use super::find::StepText;
use super::inspector::{Tab, TaskTab};
use super::{App, Effect, Field, Keypress, LiveStream, Screen, SubmitForm};
use crate::domain::TaskId;

impl App {
    /// The kind of tab the inspector shows now, with the one open; none
    /// for a row without tabs (a node) or no row.
    pub fn tab_on_show(&self) -> Option<Tab> {
        match self.screen {
            Screen::Task(_) => Some(Tab::Task(self.tabs.task)),
            Screen::Bot(_) => Some(Tab::Bot(self.tabs.bot)),
            Screen::Team(_) => Some(Tab::Team(self.tabs.team)),
            _ => None,
        }
    }

    /// A press on a tab: it opens, its body from its top, and what it
    /// shows is read.
    pub(super) fn tab_picked(&mut self, tab: Tab) -> Vec<Effect> {
        let before = self.tabs;
        self.tabs.open(tab);
        self.tab_opened(before != self.tabs)
    }

    /// Left or Right with a row inspected: the tab before or after.
    pub(super) fn tab_key(&mut self, forward: bool) -> Vec<Effect> {
        let Some(on_show) = self.tab_on_show() else {
            return Vec::new();
        };
        self.tabs.step(on_show, forward);
        self.tab_opened(true)
    }

    fn tab_opened(&mut self, changed: bool) -> Vec<Effect> {
        let mut effects = if changed {
            vec![Effect::InspectorTop]
        } else {
            Vec::new()
        };
        // The tree of what the task asked is read when its tab opens.
        effects.extend(self.reload_activity());
        effects
    }

    /// Whether the find in the activity is on show: a task inspected on
    /// its Activity.
    pub fn find_shown(&self) -> bool {
        matches!(self.screen, Screen::Task(_)) && self.tabs.task == TaskTab::Activity
    }

    /// Ctrl+F: on a task, its Activity opens with the keyboard in the
    /// find; elsewhere it does nothing.
    pub(super) fn find_focus(&mut self) -> Vec<Effect> {
        if !matches!(self.screen, Screen::Task(_)) {
            return Vec::new();
        }
        let mut effects = if self.tabs.task == TaskTab::Activity {
            Vec::new()
        } else {
            self.tab_picked(Tab::Task(TaskTab::Activity))
        };
        effects.push(Effect::Focus(Field::ActivityFind));
        effects
    }

    /// The steps of the inspected task's activity as the find reads
    /// them, one per line it draws, in order.
    pub fn steps_text(&self) -> Vec<StepText> {
        let Screen::Task(id) = &self.screen else {
            return Vec::new();
        };
        self.live
            .as_ref()
            .filter(|live| &live.id == id)
            .map(|live| live.activity.entries.iter().map(StepText::of).collect())
            .unwrap_or_default()
    }

    /// The find typed into: its first match in sight.
    pub(super) fn find_typed(&mut self, typed: String) -> Vec<Effect> {
        self.typed_in = Some(Field::ActivityFind);
        self.find.type_in(typed);
        self.show_match()
    }

    /// The next match, or the one before, going round; it is brought
    /// into sight.
    pub(super) fn find_step(&mut self, forward: bool) -> Vec<Effect> {
        let steps = self.steps_text();
        if forward {
            self.find.next(&steps);
        } else {
            self.find.previous(&steps);
        }
        self.show_match()
    }

    /// The match the find is at, in sight. A running task stops following
    /// its newest step while a match is shown, or its next read would
    /// take the person away from it.
    fn show_match(&mut self) -> Vec<Effect> {
        let Some(at) = self.find.current(&self.steps_text()) else {
            return Vec::new();
        };
        if let Some(live) = self.live.as_mut() {
            live.scrolled(LiveStream::Steps, false);
        }
        vec![Effect::ShowStep(at)]
    }

    /// Jump to latest: the activity follows the newest step again.
    pub(super) fn jump_to_latest(&mut self) -> Vec<Effect> {
        match self.live.as_mut() {
            Some(live) => {
                live.scrolled(LiveStream::Steps, true);
                vec![Effect::ShowEnd(LiveStream::Steps)]
            }
            None => Vec::new(),
        }
    }

    /// An up or down arrow, once it is known whether the find had the
    /// keyboard: there it goes to the match before or after; anywhere
    /// else it moves the list, and the inspector with it.
    pub(super) fn find_key(&mut self, key: Keypress, in_find: bool) -> Vec<Effect> {
        match key {
            Keypress::Up | Keypress::Down if in_find && self.find_shown() => self.find_step(key == Keypress::Down),
            _ => self.list_or_nothing(key),
        }
    }

    /// Run again: the new task's form opens with the task's bot and its
    /// objective, for the person to send.
    pub(super) fn run_again(&mut self, id: TaskId) -> Vec<Effect> {
        let task = self
            .task
            .as_ref()
            .map(|detail| &detail.task)
            .filter(|task| task.id == id)
            .or_else(|| self.fleet.as_ref().and_then(|f| f.tasks.iter().find(|t| t.id == id)));
        let Some(task) = task else {
            return Vec::new();
        };
        let objective = task.objective.clone();
        self.submit = SubmitForm::opened(Some(task.bot_id.clone()), self.services.keys.next_key());
        self.submit.objective = objective;
        Vec::new()
    }
}

#[cfg(test)]
mod tests {
    use super::super::find::StepFilter;
    use super::super::inspector::{BotTab, TeamTab};
    use super::super::table::{List, RowRef};
    use super::super::test_runtime::send;
    use super::super::{Message, Scroller};
    use super::*;
    use crate::domain::{BotId, NodeId};

    fn large(screen: Screen) -> App {
        let fake = std::sync::Arc::new(crate::adapters::fake::FakeControlPlane::large(
            "2026-10-25T10:00:00Z".parse().unwrap(),
        ));
        let mut app = super::super::view::tests::app_over(fake);
        send(&mut app, Message::SettingsSave);
        send(&mut app, Message::Go(screen));
        app
    }

    /// The task of the large fleet with the long run, open on its list.
    fn long_run() -> (App, TaskId) {
        let mut app = large(Screen::Tasks);
        let id = app
            .fleet
            .as_ref()
            .unwrap()
            .tasks
            .iter()
            .find(|t| t.bot_id.as_str() == "ops-deploy")
            .unwrap()
            .id
            .clone();
        send(&mut app, Message::RowPicked(RowRef::Task(id.clone())));
        (app, id)
    }

    #[test]
    fn a_row_opens_beside_its_list_which_keeps_its_filter_and_its_cursor() {
        let mut app = large(Screen::Tasks);
        send(&mut app, Message::ListFilterTyped("ops".into()));
        let first = app.task_lines_drawn().keys()[0].clone();
        let effects = app.apply(Message::RowPicked(RowRef::Task(first.clone())));
        assert_eq!(app.screen, Screen::Task(first.clone()));
        assert!(effects.contains(&Effect::InspectorTop));
        assert!(
            !effects.contains(&Effect::ScrollTop),
            "the list stays where it was scrolled"
        );
        assert!(
            effects
                .iter()
                .any(|e| matches!(e, Effect::LoadTask(id) if *id == first))
        );
        assert_eq!(app.tables.query, "ops", "the filter stays");
        assert_eq!(app.cursor_row(List::Tasks), Some(RowRef::Task(first)));
    }

    #[test]
    fn the_arrows_move_the_cursor_and_the_inspector_follows() {
        let mut app = large(Screen::Tasks);
        let keys = app.task_lines_drawn().keys();
        send(&mut app, Message::RowPicked(RowRef::Task(keys[0].clone())));
        // On the Activity the arrow first asks whether the find has the
        // keyboard; it had not.
        assert_eq!(
            app.apply(Message::Key(Keypress::Down)),
            vec![Effect::AskFind(Keypress::Down)]
        );
        let effects = app.apply(Message::FindKey(Keypress::Down, false));
        assert_eq!(app.screen, Screen::Task(keys[1].clone()));
        assert!(effects.iter().any(|e| matches!(e, Effect::Reveal(Scroller::Rows, _))));
        assert!(
            effects
                .iter()
                .any(|e| matches!(e, Effect::LoadTask(id) if *id == keys[1]))
        );
        send(&mut app, Message::Key(Keypress::Up));
        assert_eq!(app.screen, Screen::Task(keys[0].clone()));
        // Enter on the row inspected opens nothing more.
        assert!(app.apply(Message::Key(Keypress::Enter)).is_empty());
    }

    #[test]
    fn esc_closes_the_inspector_and_keeps_the_cursor_then_lets_it_go() {
        let mut app = large(Screen::Bots);
        let bot = BotId::new("ops-lead").unwrap();
        send(&mut app, Message::RowPicked(RowRef::Bot(bot.clone())));
        assert_eq!(app.screen, Screen::Bot(bot.clone()));
        send(&mut app, Message::Key(Keypress::Escape));
        assert_eq!(app.screen, Screen::Bots);
        assert_eq!(app.cursor_row(List::Bots), Some(RowRef::Bot(bot)));
        send(&mut app, Message::Key(Keypress::Escape));
        assert_eq!(app.cursor_row(List::Bots), None);
        // A node's inspector closes the same way, and its close button
        // goes back to the list.
        send(&mut app, Message::Go(Screen::Node(NodeId::new("node-03").unwrap())));
        assert_eq!(app.screen.list(), Some(List::Nodes));
        send(&mut app, Message::Key(Keypress::Escape));
        assert_eq!(app.screen, Screen::Nodes);
    }

    #[test]
    fn the_teams_list_opens_on_its_first_team_and_keeps_one_open() {
        let mut app = large(Screen::Teams);
        let first = app.team_lines_drawn().keys()[0].clone();
        assert_eq!(app.screen, Screen::Team(first.clone()));
        send(&mut app, Message::Key(Keypress::Escape));
        assert_eq!(app.screen, Screen::Team(first.clone()), "Esc leaves a team open");
        send(&mut app, Message::Key(Keypress::Down));
        let second = app.team_lines_drawn().keys()[1].clone();
        assert_eq!(app.screen, Screen::Team(second.clone()));
        // Back on Teams, the team under the cursor opens again.
        send(&mut app, Message::Go(Screen::Overview));
        send(&mut app, Message::Go(Screen::Teams));
        assert_eq!(app.screen, Screen::Team(second));
    }

    #[test]
    fn left_and_right_change_the_tab_which_each_kind_keeps() {
        let (mut app, _) = long_run();
        assert_eq!(app.tab_on_show(), Some(Tab::Task(TaskTab::Activity)));
        let effects = app.apply(Message::Key(Keypress::Right));
        assert_eq!(app.tabs.task, TaskTab::Answer);
        assert_eq!(effects, vec![Effect::InspectorTop]);
        send(&mut app, Message::Key(Keypress::Left));
        send(&mut app, Message::Key(Keypress::Left));
        assert_eq!(app.tabs.task, TaskTab::Raw, "round from the first to the last");
        // The next task opens on the same tab.
        send(&mut app, Message::Key(Keypress::Down));
        assert_eq!(app.tab_on_show(), Some(Tab::Task(TaskTab::Raw)));
        // A bot keeps its own.
        send(&mut app, Message::Go(Screen::Bot(BotId::new("ops-lead").unwrap())));
        assert_eq!(app.tab_on_show(), Some(Tab::Bot(BotTab::Now)));
        send(&mut app, Message::Tab(Tab::Bot(BotTab::Listens)));
        send(&mut app, Message::Key(Keypress::Right));
        assert_eq!(app.tabs.bot, BotTab::Now);
        send(&mut app, Message::Go(Screen::Teams));
        send(&mut app, Message::Key(Keypress::Right));
        assert_eq!(app.tabs.team, TeamTab::Members);
        // Without a row inspected, Left and Right do nothing.
        send(&mut app, Message::Go(Screen::Nodes));
        assert!(app.apply(Message::Key(Keypress::Right)).is_empty());
        assert_eq!(app.tab_on_show(), None);
    }

    #[test]
    fn ctrl_f_opens_the_activity_with_the_keyboard_in_its_find() {
        let (mut app, _) = long_run();
        send(&mut app, Message::Tab(Tab::Task(TaskTab::Runs)));
        let effects = app.apply(Message::Key(Keypress::Find));
        assert_eq!(app.tabs.task, TaskTab::Activity);
        assert_eq!(effects.last(), Some(&Effect::Focus(Field::ActivityFind)));
        send(&mut app, Message::Go(Screen::Bots));
        assert!(app.apply(Message::Key(Keypress::Find)).is_empty(), "a bot has no find");
    }

    #[test]
    fn the_find_goes_from_match_to_match_and_brings_each_into_sight() {
        let (mut app, _) = long_run();
        let effects = app.apply(Message::FindTyped("tests".into()));
        let steps = app.steps_text();
        let hits = app.find.hits(&steps);
        assert!(hits.len() > 3, "{hits:?}");
        assert_eq!(effects, vec![Effect::ShowStep(hits[0])]);
        assert_eq!(app.apply(Message::FindNext), vec![Effect::ShowStep(hits[1])]);
        assert_eq!(app.apply(Message::FindPrevious), vec![Effect::ShowStep(hits[0])]);
        assert_eq!(
            app.apply(Message::FindPrevious),
            vec![Effect::ShowStep(*hits.last().unwrap())],
            "round from the first to the last"
        );
        // Enter in the field goes on; Shift+Enter goes back.
        assert_eq!(app.apply(Message::FindSubmit), vec![Effect::ShowStep(hits[0])]);
        app.apply(Message::Modifiers(iced::keyboard::Modifiers::SHIFT));
        assert_eq!(
            app.apply(Message::FindSubmit),
            vec![Effect::ShowStep(*hits.last().unwrap())]
        );
        app.apply(Message::Modifiers(iced::keyboard::Modifiers::empty()));
        // A chip narrows the matches.
        send(&mut app, Message::FindShow(StepFilter::Events));
        let events = app.find.hits(&app.steps_text());
        assert!(events.len() < hits.len() && !events.is_empty());
        // Nothing found: no match to show.
        assert!(app.apply(Message::FindTyped("zzzz-nothing".into())).is_empty());
        assert_eq!(app.find.label(&app.steps_text()), "0/0");
        // Another task starts with an empty find.
        send(&mut app, Message::Key(Keypress::Down));
        assert_eq!(app.find.typed(), "");
    }

    #[test]
    fn an_arrow_in_the_find_goes_through_its_matches_and_elsewhere_moves_the_list() {
        let (mut app, id) = long_run();
        send(&mut app, Message::FindTyped("tests".into()));
        let hits = app.find.hits(&app.steps_text());
        assert_eq!(
            app.apply(Message::Key(Keypress::Down)),
            vec![Effect::AskFind(Keypress::Down)]
        );
        assert_eq!(
            app.apply(Message::FindKey(Keypress::Down, true)),
            vec![Effect::ShowStep(hits[1])]
        );
        assert_eq!(app.screen, Screen::Task(id.clone()), "the list did not move");
        app.apply(Message::FindKey(Keypress::Down, false));
        assert_ne!(app.screen, Screen::Task(id), "outside the find it moves the list");
    }

    #[test]
    fn a_running_task_follows_its_newest_step_until_the_person_scrolls_up() {
        let mut app = large(Screen::Tasks);
        let running = app
            .fleet
            .as_ref()
            .unwrap()
            .tasks
            .iter()
            .find(|t| t.status == crate::domain::TaskStatus::Running)
            .unwrap()
            .id
            .clone();
        send(&mut app, Message::Go(Screen::Task(running)));
        let live = app.live.as_ref().unwrap();
        assert!(live.follows(LiveStream::Steps));
        send(&mut app, Message::ActivityScrolled(LiveStream::Steps, false));
        assert!(!app.live.as_ref().unwrap().follows(LiveStream::Steps));
        assert_eq!(
            app.apply(Message::JumpToLatest),
            vec![Effect::ShowEnd(LiveStream::Steps)]
        );
        assert!(app.live.as_ref().unwrap().follows(LiveStream::Steps));
        // A match shown holds the steps where it is.
        send(&mut app, Message::FindTyped("playbook".into()));
        assert!(!app.live.as_ref().unwrap().follows(LiveStream::Steps));
    }

    #[test]
    fn a_task_that_ended_opens_at_its_first_step() {
        let mut app = large(Screen::Tasks);
        let id = app
            .fleet
            .as_ref()
            .unwrap()
            .tasks
            .iter()
            .find(|t| t.bot_id.as_str() == "ops-deploy")
            .unwrap()
            .id
            .clone();
        let opened = app.apply(Message::RowPicked(RowRef::Task(id.clone())));
        let read = opened
            .into_iter()
            .find(|e| matches!(e, Effect::FollowTask { .. }))
            .expect("its timeline is read");
        let effects = super::super::test_runtime::drive(&mut app, vec![read]);
        assert!(effects.is_empty());
        assert!(app.live.as_ref().is_some_and(|l| l.read && !l.activity.is_empty()));
        let feed = app.live.as_ref().unwrap().feed.clone();
        let again = app.task_followed(id, Ok(feed));
        assert!(!again.iter().any(|e| matches!(e, Effect::ShowEnd(_))), "{again:?}");
    }

    #[test]
    fn run_again_opens_the_new_task_form_with_its_bot_and_objective() {
        let (mut app, id) = long_run();
        send(&mut app, Message::RunAgain(id.clone()));
        let task = app.task.as_ref().unwrap().task.clone();
        assert!(app.submit.open);
        assert_eq!(app.submit.bot, Some(task.bot_id));
        assert_eq!(app.submit.objective, task.objective);
        assert_eq!(app.screen, Screen::Task(id), "over the list, the task still open");
        app.apply(Message::SubmitClose);
        send(&mut app, Message::RunAgain(TaskId::new("gone").unwrap()));
        assert!(!app.submit.open, "a task not known opens nothing");
    }
}
