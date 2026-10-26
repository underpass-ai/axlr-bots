use std::path::PathBuf;
use std::sync::Arc;

use super::{ControlPlane, DefinitionReader, PanelError};
use crate::domain::{Bot, BotDefinition, BotForm};

/// Registers a bot from a definition file or from the form.
pub struct RegisterBot {
    control_plane: Arc<dyn ControlPlane>,
    reader: Arc<dyn DefinitionReader>,
}

impl RegisterBot {
    pub fn new(control_plane: Arc<dyn ControlPlane>, reader: Arc<dyn DefinitionReader>) -> Self {
        Self { control_plane, reader }
    }

    /// Reads, checks and registers the definition in the file.
    pub async fn from_file(&self, path: PathBuf) -> Result<Bot, PanelError> {
        let text = self.reader.read(path).await?;
        let definition = BotDefinition::from_json(&text)?;
        self.control_plane.register_bot(definition).await
    }

    /// Builds, checks and registers the definition the form describes.
    pub async fn from_form(&self, form: &BotForm) -> Result<Bot, PanelError> {
        let definition = BotDefinition::from_form(form)?;
        self.control_plane.register_bot(definition).await
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::adapters::fake::MemoryFiles;
    use crate::application::test_support::{fleet, run};
    use crate::domain::{AutonomyLevel, BotStatus};

    const DEFINITION: &str = r#"{"id":"helper","tenant":"default","name":"helper","owner":"operator","mission":"Help.","level":"propose","allowedTools":["local_read"],"triggers":[{"type":"manual"}]}"#;

    #[test]
    fn registers_from_a_file() {
        let (control_plane, _) = fleet();
        let files = Arc::new(MemoryFiles::with("/defs/helper.json", DEFINITION));
        let uc = RegisterBot::new(control_plane.clone(), files);
        let bot = run(uc.from_file("/defs/helper.json".into())).unwrap();
        assert_eq!(bot.id.as_str(), "helper");
        assert_eq!(bot.status, BotStatus::Registered);
        assert_eq!(bot.level, AutonomyLevel::Propose);
        assert!(matches!(
            run(uc.from_file("/defs/helper.json".into())),
            Err(PanelError::Conflict(_))
        ));
        assert!(matches!(
            run(uc.from_file("/defs/none.json".into())),
            Err(PanelError::File(_))
        ));
    }

    #[test]
    fn refuses_a_bad_file_before_calling_the_control_plane() {
        let (control_plane, _) = fleet();
        let files = Arc::new(MemoryFiles::with("/bad.json", "{\"name\": 1}"));
        let uc = RegisterBot::new(control_plane.clone(), files);
        assert!(matches!(
            run(uc.from_file("/bad.json".into())),
            Err(PanelError::Invalid(_))
        ));
        assert!(!control_plane.calls().iter().any(|c| c.starts_with("register_bot")));
    }

    #[test]
    fn registers_from_the_form() {
        let (control_plane, _) = fleet();
        let uc = RegisterBot::new(control_plane, Arc::new(MemoryFiles::default()));
        let form = BotForm {
            name: "scribe".into(),
            mission: "Write notes.".into(),
            ..Default::default()
        };
        let bot = run(uc.from_form(&form)).unwrap();
        assert_eq!(bot.id.as_str(), "scribe");
        let bad = BotForm {
            name: "".into(),
            ..Default::default()
        };
        assert!(matches!(run(uc.from_form(&bad)), Err(PanelError::Invalid(_))));
    }
}
