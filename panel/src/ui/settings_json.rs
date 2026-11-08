use iced::widget::text_editor;

/// The settings as JSON text, open to edit on the settings screen.
#[derive(Default)]
pub struct SettingsJson {
    pub open: bool,
    pub content: text_editor::Content,
    /// Why the text does not read as settings, after Apply.
    pub error: Option<String>,
}

impl SettingsJson {
    pub fn open_with(&mut self, text: &str) {
        self.open = true;
        self.content = text_editor::Content::with_text(text);
        self.error = None;
    }

    pub fn close(&mut self) {
        self.open = false;
        self.error = None;
    }

    pub fn text(&self) -> String {
        self.content.text()
    }
}

impl std::fmt::Debug for SettingsJson {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("SettingsJson")
            .field("open", &self.open)
            .field("error", &self.error)
            .finish()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn opens_with_text_and_closes_clean() {
        let mut json = SettingsJson::default();
        assert!(!json.open);
        json.open_with("{}\n");
        assert!(json.open && json.text().starts_with("{}"));
        json.error = Some("x".into());
        json.close();
        assert!(!json.open && json.error.is_none());
        assert!(format!("{json:?}").contains("SettingsJson"));
    }
}
