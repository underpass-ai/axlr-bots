use std::path::PathBuf;

use crate::application::{PanelError, ShortcutSource};
use crate::domain::{Shortcut, ShortcutFile};

/// The operator's buttons in `shortcuts.toml` beside the settings; no
/// file is no buttons.
pub struct TomlShortcuts {
    path: PathBuf,
}

impl TomlShortcuts {
    pub fn new(path: PathBuf) -> Self {
        Self { path }
    }
}

impl ShortcutSource for TomlShortcuts {
    fn load(&self) -> Result<Vec<Shortcut>, PanelError> {
        match std::fs::read_to_string(&self.path) {
            Ok(text) => {
                ShortcutFile::parse(&text).map_err(|e| PanelError::Config(format!("{}: {e}", self.path.display())))
            }
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(Vec::new()),
            Err(e) => Err(PanelError::Config(format!("{}: {e}", self.path.display()))),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn no_file_is_no_buttons_and_a_bad_file_says_where() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("shortcuts.toml");
        assert!(TomlShortcuts::new(path.clone()).load().unwrap().is_empty());
        std::fs::write(
            &path,
            "[[shortcut]]\nteam = \"o\"\nlabel = \"Stop\"\nname = \"stop.all\"\nmessage = \"Stop.\"\n",
        )
        .unwrap();
        assert_eq!(TomlShortcuts::new(path.clone()).load().unwrap()[0].name, "stop.all");
        std::fs::write(&path, "[[shortcut]]\nteam = \"o\"\n").unwrap();
        let err = TomlShortcuts::new(path).load().unwrap_err();
        assert!(matches!(err, PanelError::Config(m) if m.contains("shortcuts.toml")));
    }
}
