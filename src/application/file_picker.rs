use std::path::PathBuf;

use super::PortFuture;

/// Asks the operator for a bot definition file; `None` when they cancel.
pub trait FilePicker: Send + Sync {
    fn pick_definition(&self) -> PortFuture<'_, Option<PathBuf>>;
}
