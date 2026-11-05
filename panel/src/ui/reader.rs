use iced::widget::text_editor::{Action, Content};

/// Words on a detail page a person can select a part of and copy: the
/// text and the read-only editor that holds the selection.
pub struct Reader {
    source: String,
    content: Content,
}

impl Reader {
    pub fn new(source: String) -> Self {
        Self {
            content: Content::with_text(&source),
            source,
        }
    }

    pub fn source(&self) -> &str {
        &self.source
    }

    pub fn content(&self) -> &Content {
        &self.content
    }

    /// The words as the control plane has them now: the editor is built
    /// again only when they changed, so a refresh keeps the selection.
    pub fn set(&mut self, source: &str) {
        if self.source != source {
            *self = Self::new(source.to_string());
        }
    }

    /// Moves the cursor or the selection; an edit is dropped.
    pub fn act(&mut self, action: Action) {
        if !action.is_edit() {
            self.content.perform(action);
        }
    }
}

#[cfg(test)]
mod tests {
    use iced::widget::text_editor::{Edit, Motion};

    use super::*;

    #[test]
    fn selects_but_never_edits() {
        let mut reader = Reader::new("Fix what the watcher reports.".into());
        reader.act(Action::Edit(Edit::Insert('x')));
        reader.act(Action::Edit(Edit::Paste("y".to_string().into())));
        assert_eq!(reader.content().text().trim_end(), "Fix what the watcher reports.");
        reader.act(Action::Move(Motion::DocumentStart));
        for _ in 0..3 {
            reader.act(Action::Select(Motion::Right));
        }
        assert_eq!(reader.content().selection().as_deref(), Some("Fix"));
    }

    #[test]
    fn keeps_the_selection_while_the_words_stay() {
        let mut reader = Reader::new("worker-1".into());
        reader.act(Action::SelectAll);
        reader.set("worker-1");
        assert_eq!(reader.content().selection().as_deref(), Some("worker-1"));
        reader.set("worker-2");
        assert_eq!(reader.source(), "worker-2");
        assert_eq!(reader.content().selection(), None);
    }
}
