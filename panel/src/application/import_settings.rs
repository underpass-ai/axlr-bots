use std::sync::Arc;

use super::{DefinitionReader, FilePicker, PanelError, PanelSettings};

/// Reads settings from a file the operator picks and checks them; what
/// to do with them (save the preferences, try the control plane) is the
/// panel's.
pub struct ImportSettings {
    picker: Arc<dyn FilePicker>,
    reader: Arc<dyn DefinitionReader>,
}

impl ImportSettings {
    pub fn new(picker: Arc<dyn FilePicker>, reader: Arc<dyn DefinitionReader>) -> Self {
        Self { picker, reader }
    }

    /// The settings read; `None` when the operator cancelled.
    pub async fn execute(&self) -> Result<Option<PanelSettings>, PanelError> {
        let Some(path) = self.picker.pick_settings().await? else {
            return Ok(None);
        };
        let text = self.reader.read(path.clone()).await?;
        PanelSettings::from_json(&text)
            .map(Some)
            .map_err(|e| PanelError::Config(format!("{}: {e}", path.display())))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::adapters::fake::MemoryFiles;
    use crate::application::Theme;
    use crate::application::test_support::run;

    #[test]
    fn reads_and_checks_the_picked_file() {
        let files = Arc::new(MemoryFiles::with("/in/s.json", "{\"look.theme\": \"dark\"}"));
        let import = ImportSettings::new(files.clone(), files.clone());
        assert_eq!(run(import.execute()).unwrap(), None);
        files.pick(Some("/in/s.json"));
        let settings = run(import.execute()).unwrap().unwrap();
        assert_eq!((settings.theme, settings.control_plane), (Theme::Dark, None));
        files.pick(Some("/in/missing.json"));
        assert!(matches!(run(import.execute()), Err(PanelError::File(_))));
        let bad = Arc::new(MemoryFiles::with("/in/bad.json", "{\"look.theme\": 3}"));
        bad.pick(Some("/in/bad.json"));
        let error = run(ImportSettings::new(bad.clone(), bad).execute()).unwrap_err();
        assert!(
            matches!(&error, PanelError::Config(m) if m.contains("/in/bad.json")),
            "{error:?}"
        );
    }
}
