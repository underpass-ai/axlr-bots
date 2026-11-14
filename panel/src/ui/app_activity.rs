//! The activity screen: a task with everything under it, read once and
//! then followed through the control plane's stream while any task of
//! the tree may still change.

use super::{ActivityScreen, App, Effect, Screen};
use crate::application::{ACTIVITY_WAIT_SECONDS, PanelError};
use crate::domain::{ActivityBatch, TaskActivity, TaskId};

impl App {
    /// What the activity screen asks for: the tree when it has none, the
    /// stream while the tree may change.
    pub(super) fn reload_activity(&mut self) -> Vec<Effect> {
        let Screen::Activity(id) = &self.screen else {
            return Vec::new();
        };
        let id = id.clone();
        if self.activity.as_ref().is_none_or(|a| a.id != id) {
            self.activity = Some(ActivityScreen::new(id.clone()));
        }
        let Some(screen) = self.activity.as_mut() else {
            return Vec::new();
        };
        let mut effects = Vec::new();
        if screen.tree.is_none() && !screen.loading {
            screen.loading = true;
            effects.push(Effect::LoadActivity(id.clone()));
        }
        if screen.tree.is_some() && !screen.finished() && !screen.following {
            screen.following = true;
            effects.push(Effect::FollowActivity {
                id,
                cursor: screen.cursor.clone(),
                wait: ACTIVITY_WAIT_SECONDS,
            });
        }
        effects
    }

    pub(super) fn activity_loaded(&mut self, id: TaskId, result: Result<TaskActivity, PanelError>) -> Vec<Effect> {
        let Some(screen) = self.activity.as_mut().filter(|a| a.id == id) else {
            return Vec::new();
        };
        screen.loading = false;
        match result {
            Ok(tree) => screen.loaded(tree),
            Err(error) => {
                self.banner = Some(super::Banner::error(&error, self.now));
                return Vec::new();
            }
        }
        self.reload_activity()
    }

    pub(super) fn activity_followed(&mut self, id: TaskId, result: Result<ActivityBatch, PanelError>) -> Vec<Effect> {
        let on_screen = self.screen == Screen::Activity(id.clone());
        let Some(screen) = self.activity.as_mut().filter(|a| a.id == id) else {
            return Vec::new();
        };
        screen.following = false;
        // A stream that failed is tried again by the next tick; a control
        // plane older than the stream answers not found, and the tree read
        // stays as it is.
        let Ok(batch) = result else {
            return Vec::new();
        };
        let brought = !batch.updates.is_empty();
        screen.followed(batch);
        if brought && on_screen {
            return self.reload_activity();
        }
        Vec::new()
    }
}
