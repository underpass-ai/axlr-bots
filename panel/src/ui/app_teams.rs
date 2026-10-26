//! The panel's teams: following each team's timeline in the background,
//! the warnings it raises, a bot's team, and the recall of a vehicle.

use super::{App, Banner, Effect, Screen, Tone};
use crate::application::{EventFeed, PanelError, TEAM_WAIT_SECONDS};
use crate::domain::{Bot, BotId, SubmittedTask, Team, team_names};

/// The objective of a recall: what a vehicle's bot does when the operator
/// brings it home.
pub const RECALL_OBJECTIVE: &str = "Return home now with return_home: the operator recalled you from the panel. Stop what you are doing and say where you are.";

impl App {
    /// The teams the fleet's bots name, as of the last refresh.
    pub fn teams(&self) -> Vec<Team> {
        self.fleet
            .as_ref()
            .map(|f| Team::from_bots(&f.bots))
            .unwrap_or_default()
    }

    /// Starts following every team that has no read on its way: the first
    /// read takes the newest events, the next ones wait for new ones.
    pub(super) fn follow_teams(&mut self) -> Vec<Effect> {
        if !self.connected() {
            return Vec::new();
        }
        let mut effects = Vec::new();
        for team in self.teams() {
            if self.watch.following.insert(team.name.clone()) {
                let feed = self.watch.feeds.get(&team.name).cloned().unwrap_or_default();
                let wait = if self.watch.feeds.contains_key(&team.name) {
                    TEAM_WAIT_SECONDS
                } else {
                    0
                };
                effects.push(Effect::FollowTeam {
                    name: team.name,
                    feed,
                    wait,
                });
            }
        }
        effects
    }

    /// What a team's read brought: the timeline grows, a teammate's
    /// warning raises a banner wherever the operator is, and when it
    /// brought something the next read waits at once for what comes
    /// after; a read that waited and brought nothing is started again by
    /// the next refresh.
    pub(super) fn team_loaded(&mut self, name: String, result: Result<EventFeed, PanelError>) -> Vec<Effect> {
        self.watch.following.remove(&name);
        let feed = match result {
            Ok(feed) => feed,
            // The next refresh starts it again; a team gone stays gone.
            Err(_) => return Vec::new(),
        };
        let first = !self.watch.feeds.contains_key(&name);
        if !first {
            let fresh = &feed.events[feed.events.len() - feed.new..];
            if let Some(alert) = fresh.iter().rev().find(|e| e.needs_attention()) {
                self.banner = Some(Banner {
                    tone: Tone::Wait,
                    title: format!("{} · team {name}", alert.label()),
                    detail: alert.detail(),
                    at: self.now,
                });
            }
        }
        let brought = feed.new > 0;
        self.watch.feeds.insert(name.clone(), feed);
        if !brought || !self.teams().iter().any(|t| t.name == name) {
            return Vec::new();
        }
        self.follow_teams()
    }

    /// Opens a bot's team field with what it has now.
    pub(super) fn team_edit(&mut self, id: BotId, words: String) -> Vec<Effect> {
        self.watch.editing = Some((id, words));
        Vec::new()
    }

    /// Saves the team field: the bot's whole list of teams.
    pub(super) fn team_save(&mut self) -> Vec<Effect> {
        let Some((id, words)): Option<(BotId, String)> = self.watch.editing.clone() else {
            return Vec::new();
        };
        self.switch(Effect::SetTeams(id, team_names(&words)))
    }

    pub(super) fn team_saved(&mut self, result: Result<Bot, PanelError>) -> Vec<Effect> {
        self.switching = false;
        match result {
            Ok(bot) => {
                let words = match bot.groups.as_slice() {
                    [] => "in no team".to_string(),
                    [one] => format!("in team {one}"),
                    many => format!("in teams {}", many.join(", ")),
                };
                self.banner = Some(Banner::good(
                    format!("{} is {words}", bot.id),
                    "Its teammates know it from their next task",
                    self.now,
                ));
                self.watch.editing = None;
                self.replace_bot(bot);
            }
            Err(error) => self.banner = Some(Banner::error(&error, self.now)),
        }
        let mut effects = self.reload_screen();
        effects.extend(self.refresh_now());
        effects
    }

    /// Brings a vehicle's bot home at once: an urgent task that cancels
    /// what it was doing; return_home runs without a person.
    pub(super) fn recall(&mut self, bot: BotId) -> Vec<Effect> {
        let key = self.services.keys.next_key();
        self.switch(Effect::Recall { bot, key })
    }

    pub(super) fn recalled(&mut self, result: Result<SubmittedTask, PanelError>) -> Vec<Effect> {
        self.switching = false;
        match result {
            Ok(submitted) => {
                self.banner = Some(Banner::good(
                    format!("{} recalled", submitted.task.bot_id),
                    "It stops what it was doing and comes home",
                    self.now,
                ));
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
