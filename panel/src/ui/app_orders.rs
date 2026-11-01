//! An urgent order: the operator stops what a bot is doing and tells it,
//! in words of their own, what to do instead.

use super::{App, Banner, Effect, OrderForm, Screen};
use crate::application::PanelError;
use crate::domain::{BotId, SubmittedTask};

impl App {
    /// Opens the order form on the bot's page, with a key of its own;
    /// from a team's page it goes to the bot first.
    pub(super) fn order_open(&mut self, bot: BotId) -> Vec<Effect> {
        let screen = Screen::Bot(bot.clone());
        let effects = if self.screen == screen {
            Vec::new()
        } else {
            self.go(screen)
        };
        self.order = OrderForm::opened(bot, self.services.keys.next_key());
        effects
    }

    /// Sends the order as an urgent task: it goes first and cancels what
    /// the bot was doing. Without words there is nothing to send.
    pub(super) fn order_send(&mut self) -> Vec<Effect> {
        let (Some(bot), Some(key)) = (self.order.bot.clone(), self.order.key.clone()) else {
            return Vec::new();
        };
        if !self.order.ready() {
            return Vec::new();
        }
        self.switch(Effect::Order {
            bot,
            objective: self.order.text.clone(),
            key,
        })
    }

    /// The order's answer: its task on screen, or the reason with the
    /// form still open and the words kept.
    pub(super) fn ordered(&mut self, result: Result<SubmittedTask, PanelError>) -> Vec<Effect> {
        self.switching = false;
        match result {
            Ok(submitted) => {
                self.banner = Some(Banner::good(
                    format!("{} got an urgent order", submitted.task.bot_id),
                    "It stops what it was doing and does it",
                    self.now,
                ));
                self.order = OrderForm::default();
                let mut effects = self.go(Screen::Task(submitted.task.id));
                effects.extend(self.refresh_now());
                effects
            }
            Err(error) => {
                self.banner = Some(Banner::error(&error, self.now));
                Vec::new()
            }
        }
    }
}
