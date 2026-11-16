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
