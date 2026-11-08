use std::path::PathBuf;
use std::sync::Arc;

use super::{FilePicker, PanelError, PanelSettings};

/// The name offered for an exported settings file.
pub const EXPORT_NAME: &str = "axlr-bots-panel.settings.json";

/// Writes the settings to a file the operator picks, as the settings
/// file itself reads: paths, never the token.
pub struct ExportSettings {
    picker: Arc<dyn FilePicker>,
}

impl ExportSettings {
    pub fn new(picker: Arc<dyn FilePicker>) -> Self {
        Self { picker }
    }

    /// Where the file went; `None` when the operator cancelled.
    pub async fn execute(&self, settings: &PanelSettings) -> Result<Option<PathBuf>, PanelError> {
        self.picker
            .save_as(EXPORT_NAME.to_string(), settings.to_json().into_bytes())
            .await
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::adapters::fake::MemoryFiles;
    use crate::application::test_support::run;

    #[test]
    fn writes_the_settings_where_the_operator_says() {
        let files = Arc::new(MemoryFiles::default());
        let export = ExportSettings::new(files.clone());
        let path = run(export.execute(&PanelSettings::default())).unwrap().unwrap();
        assert_eq!(path, PathBuf::from("/saved").join(EXPORT_NAME));
        let text = String::from_utf8(files.saved()[&path].clone()).unwrap();
        assert_eq!(PanelSettings::from_json(&text).unwrap(), PanelSettings::default());
        files.cancel_saves();
        assert_eq!(run(export.execute(&PanelSettings::default())).unwrap(), None);
    }
}
