use std::sync::Arc;

use super::{ControlPlane, PanelError};
use crate::domain::{Bot, BotId};

/// Turns a bot on (it accepts tasks) or puts it on hold.
pub struct SwitchBot {
    control_plane: Arc<dyn ControlPlane>,
}

impl SwitchBot {
    pub fn new(control_plane: Arc<dyn ControlPlane>) -> Self {
        Self { control_plane }
    }

    pub async fn enable(&self, id: BotId) -> Result<Bot, PanelError> {
        self.control_plane.enable_bot(id).await
    }

    pub async fn pause(&self, id: BotId) -> Result<Bot, PanelError> {
        self.control_plane.pause_bot(id).await
    }
}
