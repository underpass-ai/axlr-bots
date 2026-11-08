use std::path::PathBuf;

use super::PortFuture;

/// Asks the operator for a file: a bot definition or a settings file to
/// read, or where to save a download or an export. `None` when they
/// cancel.
pub trait FilePicker: Send + Sync {
    fn pick_definition(&self) -> PortFuture<'_, Option<PathBuf>>;
    /// A settings file to import.
    fn pick_settings(&self) -> PortFuture<'_, Option<PathBuf>>;
    /// Asks where to save `bytes`, offering `name`, and writes them there.
    fn save_as(&self, name: String, bytes: Vec<u8>) -> PortFuture<'_, Option<PathBuf>>;
}
