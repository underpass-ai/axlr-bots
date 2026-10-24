use std::path::PathBuf;

use super::PortFuture;

/// Reads a bot definition file's text.
pub trait DefinitionReader: Send + Sync {
    fn read(&self, path: PathBuf) -> PortFuture<'_, String>;
}
