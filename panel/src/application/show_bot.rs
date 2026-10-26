use std::sync::Arc;

use super::{ControlPlane, PanelError};
use crate::domain::{Bot, BotId, Event};

/// How many of a bot's newest events its page shows.
pub const BOT_EVENTS: u32 = 50;

/// A bot and its newest events.
pub struct ShowBot {
    control_plane: Arc<dyn ControlPlane>,
}

impl ShowBot {
    pub fn new(control_plane: Arc<dyn ControlPlane>) -> Self {
        Self { control_plane }
    }

    pub async fn execute(&self, id: BotId) -> Result<(Bot, Vec<Event>), PanelError> {
        futures::try_join!(
            self.control_plane.get_bot(id.clone()),
            self.control_plane.bot_events(id, BOT_EVENTS)
        )
    }
}
