use iced::widget::{markdown, text_editor};

/// A task's answer, parsed once: as Markdown to read, and as plain text
/// to select and copy a part of.
pub struct Answer {
    pub source: String,
    pub markdown: markdown::Content,
    pub plain: text_editor::Content,
    /// The plain text is on screen instead of the Markdown.
    pub selecting: bool,
}

impl Answer {
    pub fn new(source: String) -> Self {
        Self {
            markdown: markdown::Content::parse(&source),
            plain: text_editor::Content::with_text(&source),
            source,
            selecting: false,
        }
    }

    /// Moves the cursor or the selection; an edit is dropped, the answer
    /// is read only.
    pub fn select(&mut self, action: text_editor::Action) {
        if !action.is_edit() {
            self.plain.perform(action);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use iced::widget::text_editor::{Action, Edit};

    #[test]
    fn selects_but_never_edits() {
        let mut answer = Answer::new("There are **3** files.".into());
        assert!(!answer.selecting);
        answer.select(Action::Edit(Edit::Insert('x')));
        answer.select(Action::Edit(Edit::Delete));
        assert_eq!(answer.plain.text().trim_end(), "There are **3** files.");
        answer.select(Action::SelectAll);
        assert_eq!(answer.plain.selection().as_deref(), Some("There are **3** files."));
    }
}
