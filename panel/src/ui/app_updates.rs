//! Updating a bot to the version of its definition staged on the
//! control plane: asked first, applied there, the bot read again.

use super::{App, Banner, Confirm, Effect};
use crate::application::PanelError;
use crate::domain::{Bot, BotId};

impl App {
    /// Asks before updating the bot on screen to its staged version;
    /// nothing to ask when it has no newer one.
    pub(super) fn ask_update(&mut self, id: BotId) -> Vec<Effect> {
        let update = self
            .bot
            .as_ref()
            .map(|v| &v.bot)
            .filter(|b| b.id == id)
            .or_else(|| self.fleet.as_ref()?.bots.iter().find(|b| b.id == id))
            .and_then(Bot::update);
        if let Some(update) = update {
            self.confirm = Some(Confirm::UpdateBot(id, update));
        }
        Vec::new()
    }

    /// The update's answer: the bot at its new version, or why not (a
    /// change applied or dropped meanwhile, one the bot no longer takes),
    /// in the banner. Either way the screen and the fleet are read again.
    pub(super) fn bot_updated(&mut self, result: Result<Bot, PanelError>) -> Vec<Effect> {
        self.switching = false;
        match result {
            Ok(bot) => {
                let detail = match bot.version_label() {
                    Some(version) => format!("now at {version}"),
                    None => "its staged definition is applied".to_string(),
                };
                self.banner = Some(Banner::good(format!("{} updated", bot.id), detail, self.now));
                self.replace_bot(bot);
            }
            Err(error) => self.banner = Some(Banner::error(&error, self.now)),
        }
        let mut effects = self.reload_screen();
        effects.extend(self.refresh_now());
        effects
    }
}
