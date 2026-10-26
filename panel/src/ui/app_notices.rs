//! Desktop notices: a bot that asks for an OK, or a teammate that warns,
//! reaches the person even when the panel is behind other windows; a
//! click on the notice opens the place it is about.

use super::{App, Effect, Screen};
use crate::application::{Fleet, Notice};

impl App {
    /// The notices a new read of the fleet calls for: one per task that
    /// began waiting for a person since the last read. The first read
    /// after connecting only learns what already waits.
    pub(super) fn notice_waiting(&mut self, fleet: &Fleet, first: bool) -> Vec<Effect> {
        let waiting: Vec<_> = fleet.waiting().into_iter().collect();
        self.noticed.retain(|id| waiting.iter().any(|t| &t.id == id));
        let mut effects = Vec::new();
        for task in waiting {
            if !self.noticed.insert(task.id.clone()) || first {
                continue;
            }
            let wants = fleet
                .call_of(&task.id)
                .map(|call| format!("wants {}", call.summary(60)))
                .unwrap_or_else(|| task.headline(60));
            effects.push(Effect::Notify {
                notice: Notice {
                    title: format!("{} asks for your OK", task.bot_id),
                    body: self.with_endpoint(&wants),
                },
                open: Screen::Task(task.id.clone()),
            });
        }
        effects
    }

    /// A teammate's warning, as a notice that opens the teams.
    pub(super) fn notice_warning(&self, title: String, detail: String) -> Effect {
        Effect::Notify {
            notice: Notice {
                title,
                body: self.with_endpoint(&detail),
            },
            open: Screen::Teams,
        }
    }

    /// A notice's line, with the control plane it comes from: two panels
    /// on two control planes are told apart.
    fn with_endpoint(&self, words: &str) -> String {
        match &self.endpoint {
            Some(endpoint) => format!("{words}\n{endpoint}"),
            None => words.to_string(),
        }
    }

    /// The person clicked a notice: the panel opens its place and comes
    /// to the front.
    pub(super) fn notice_clicked(&mut self, screen: Screen) -> Vec<Effect> {
        let mut effects = self.go(screen);
        effects.push(Effect::FocusWindow);
        effects
    }
}
