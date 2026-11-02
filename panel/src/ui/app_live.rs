//! The task on screen, live: its timeline followed in the background
//! while it may still run, folded into its Activity.

use super::{App, Effect, LiveTask, Screen};
use crate::application::{EventFeed, LIVE_WAIT_SECONDS, PanelError};
use crate::domain::TaskId;

impl App {
    /// Starts a read of the timeline of the task on screen when none is
    /// on its way: the first takes the newest events, the next wait for
    /// new ones while the task may still run. Once it ended, one more read
    /// takes what came last and the following stops: its history stays.
    pub(super) fn follow_task(&mut self) -> Vec<Effect> {
        let Screen::Task(id) = &self.screen else {
            return Vec::new();
        };
        if !self.connected() {
            return Vec::new();
        }
        let id = id.clone();
        let ended = self.task_ended(&id);
        let live = match self.live.as_mut() {
            Some(live) if live.id == id => live,
            _ => self.live.insert(LiveTask::new(id.clone())),
        };
        if live.following || (ended && live.settled) {
            return Vec::new();
        }
        live.following = true;
        live.asked_ended = ended;
        let wait = if !live.read || ended { 0 } else { LIVE_WAIT_SECONDS };
        vec![Effect::FollowTask {
            id,
            feed: live.feed.clone(),
            wait,
        }]
    }

    /// What a read of the task's timeline brought: the Activity grows, and
    /// when it brought something while the task runs, the next read waits
    /// at once for what comes after; a read that waited and brought
    /// nothing is started again by the next tick.
    pub(super) fn task_followed(&mut self, id: TaskId, result: Result<EventFeed, PanelError>) -> Vec<Effect> {
        let ended = self.task_ended(&id);
        let Some(live) = self.live.as_mut().filter(|l| l.id == id) else {
            return Vec::new();
        };
        live.following = false;
        let Ok(feed) = result else {
            // The next tick tries again; the task's own read says why.
            return Vec::new();
        };
        let brought = feed.new > 0;
        live.update(feed, !ended);
        live.settled = live.asked_ended;
        if !brought || ended || self.screen != Screen::Task(id) {
            return Vec::new();
        }
        self.follow_task()
    }

    /// Whether the task as last read ended: a task not read yet may run.
    fn task_ended(&self, id: &TaskId) -> bool {
        self.task
            .as_ref()
            .filter(|d| &d.task.id == id)
            .is_some_and(|d| d.task.status.is_terminal())
    }

    /// The task's Activity again once its detail says it ended: a call
    /// without an end is then unfinished.
    pub(super) fn settle_live(&mut self) {
        let Some(detail) = self.task.as_ref() else {
            return;
        };
        let ended = detail.task.status.is_terminal();
        if let Some(live) = self.live.as_mut().filter(|l| l.id == detail.task.id && l.read) {
            let feed = live.feed.clone();
            live.update(feed, !ended);
        }
    }
}
