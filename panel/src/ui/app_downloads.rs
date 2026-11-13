//! Saving a task's artifact to a file.

use std::path::PathBuf;

use super::{App, Banner, Effect};
use crate::application::PanelError;
use crate::domain::ExecutionId;

impl App {
    pub(super) fn download(&mut self, execution: ExecutionId, name: String) -> Vec<Effect> {
        if self.downloading {
            return Vec::new();
        }
        self.downloading = true;
        vec![Effect::Download(execution, name)]
    }

    pub(super) fn downloaded(&mut self, result: Result<Option<PathBuf>, PanelError>) -> Vec<Effect> {
        self.downloading = false;
        match result {
            Ok(Some(path)) => self.banner = Some(Banner::good("Saved", path.display().to_string(), self.now)),
            Ok(None) => {}
            Err(error) => self.banner = Some(Banner::error(&error, self.now)),
        }
        Vec::new()
    }
}
