use std::sync::Arc;

use super::{ControlPlane, PanelError};
use crate::domain::{Bot, BotId, BotPatch};

/// Replaces the fields a patch names.
pub struct PatchBot {
    control_plane: Arc<dyn ControlPlane>,
}

impl PatchBot {
    pub fn new(control_plane: Arc<dyn ControlPlane>) -> Self {
        Self { control_plane }
    }

    pub async fn execute(&self, id: BotId, patch: BotPatch) -> Result<Bot, PanelError> {
        patch.validate()?;
        self.control_plane.patch_bot(id, patch).await
    }
}
