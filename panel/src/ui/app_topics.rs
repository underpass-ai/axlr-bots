//! A bot's topics and its asks, a team's topics, and the operator's
//! events on a team.

use super::{App, Banner, Effect, ListenForm, PublishForm};
use crate::application::{PanelError, SubscriptionChange};
use crate::domain::{Bot, BotId, Published, Subscription, Topic};

impl App {
    /// Pending asks of every bot, from the last refresh: they need a
    /// person as a waiting call does.
    pub fn pending_asks(&self) -> u32 {
        self.fleet
            .as_ref()
            .map(|f| f.bots.iter().map(|b| b.pending_asks().count() as u32).sum())
            .unwrap_or(0)
    }

    pub(super) fn listen_save(&mut self) -> Vec<Effect> {
        let Some(bot) = self.listen.bot.clone() else {
            return Vec::new();
        };
        if self.listen.busy {
            return Vec::new();
        }
        match Subscription::new(&self.listen.topic, self.listen.mode(), &self.listen.from) {
            Ok(sub) => {
                self.listen.busy = true;
                vec![Effect::SaveSubscriptions(bot, SubscriptionChange::Listen(sub))]
            }
            Err(error) => {
                self.banner = Some(Banner::error(&PanelError::from(error), self.now));
                Vec::new()
            }
        }
    }

    pub(super) fn change_subscriptions(&mut self, bot: BotId, change: SubscriptionChange) -> Vec<Effect> {
        self.switch(Effect::SaveSubscriptions(bot, change))
    }

    pub(super) fn subscriptions_saved(
        &mut self,
        change: SubscriptionChange,
        result: Result<Bot, PanelError>,
    ) -> Vec<Effect> {
        self.switching = false;
        self.listen.busy = false;
        match result {
            Ok(bot) => {
                self.banner = Some(Banner::good(change.done(&bot), "From its next event", self.now));
                if matches!(change, SubscriptionChange::Listen(_)) {
                    self.listen = ListenForm::default();
                }
                if let Some(fleet) = self.fleet.as_mut()
                    && let Some(known) = fleet.bots.iter_mut().find(|b| b.id == bot.id)
                {
                    *known = bot.clone();
                }
                self.replace_bot(bot);
            }
            Err(error) => self.banner = Some(Banner::error(&error, self.now)),
        }
        let mut effects = self.reload_screen();
        effects.extend(self.refresh_now());
        effects
    }

    pub(super) fn topics_loaded(
        &mut self,
        team: String,
        result: Result<Option<Vec<Topic>>, PanelError>,
    ) -> Vec<Effect> {
        self.watch.loading_topics.remove(&team);
        match result {
            Ok(topics) => {
                self.watch.topics.insert(team, topics);
            }
            Err(error) => self.banner = Some(Banner::error(&error, self.now)),
        }
        Vec::new()
    }

    pub(super) fn publish_open(&mut self, team: String) -> Vec<Effect> {
        self.publish = PublishForm::opened(team, self.services.keys.next_key());
        Vec::new()
    }

    pub(super) fn publish_send(&mut self) -> Vec<Effect> {
        let (Some(team), Some(key)) = (self.publish.team.clone(), self.publish.key.clone()) else {
            return Vec::new();
        };
        if !self.publish.ready() {
            return Vec::new();
        }
        self.publish.busy = true;
        vec![Effect::Publish {
            team,
            name: self.publish.name.clone(),
            message: self.publish.message.clone(),
            to: self.publish.to.clone(),
            data: None,
            key,
            shortcut: false,
        }]
    }

    /// One press of a shortcut: a new event, with a key of its own.
    pub(super) fn shortcut(&mut self, index: usize) -> Vec<Effect> {
        let Some(s) = self.shortcuts.get(index).cloned() else {
            return Vec::new();
        };
        vec![Effect::Publish {
            team: s.team,
            name: s.name,
            message: s.message,
            to: String::new(),
            data: s.data,
            key: self.services.keys.next_key(),
            shortcut: true,
        }]
    }

    pub(super) fn published(&mut self, result: Result<Published, PanelError>) -> Vec<Effect> {
        self.publish.busy = false;
        if result.is_ok() {
            self.publish = PublishForm::default();
        }
        self.shortcut_published(result)
    }

    /// The banner an event's answer gives, and the reloads after it.
    pub(super) fn shortcut_published(&mut self, result: Result<Published, PanelError>) -> Vec<Effect> {
        match result {
            Ok(published) => {
                let heard = match published.deliveries.len() {
                    0 => "nobody heard it".to_string(),
                    1 => "1 bot heard it".to_string(),
                    n => format!("{n} bots heard it"),
                };
                let title = if published.created {
                    format!("Published {}", published.name)
                } else {
                    format!("Already published {}", published.name)
                };
                self.banner = Some(Banner::good(title, heard, self.now));
            }
            Err(error) => self.banner = Some(Banner::error(&error, self.now)),
        }
        let mut effects = self.reload_screen();
        effects.extend(self.refresh_now());
        effects.extend(self.follow_teams());
        effects
    }
}
