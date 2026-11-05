use std::collections::HashMap;

use iced::widget::text_editor::Action;

use super::{Reader, ReaderKey};

/// The readers of the screen on show, one per passage.
#[derive(Default)]
pub struct Readers(HashMap<ReaderKey, Reader>);

impl Readers {
    pub fn get(&self, key: &ReaderKey) -> Option<&Reader> {
        self.0.get(key)
    }

    /// Holds exactly the passages given: a passage whose words did not
    /// change keeps its reader and its selection, the others start anew,
    /// and the readers of passages gone off screen are dropped.
    pub fn sync(&mut self, passages: Vec<(ReaderKey, String)>) {
        let mut next = HashMap::with_capacity(passages.len());
        for (key, words) in passages {
            let reader = match self.0.remove(&key) {
                Some(mut reader) => {
                    reader.set(&words);
                    reader
                }
                None => Reader::new(words),
            };
            next.insert(key, reader);
        }
        self.0 = next;
    }

    pub fn act(&mut self, key: &ReaderKey, action: Action) {
        if let Some(reader) = self.0.get_mut(key) {
            reader.act(action);
        }
    }

    pub fn len(&self) -> usize {
        self.0.len()
    }

    pub fn is_empty(&self) -> bool {
        self.0.is_empty()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn holds_the_passages_on_screen_and_keeps_a_selection() {
        let mut readers = Readers::default();
        readers.sync(vec![
            (ReaderKey::BotMission, "Watch the hosts.".into()),
            (ReaderKey::BotModel, "local".into()),
        ]);
        assert_eq!(readers.len(), 2);
        readers.act(&ReaderKey::BotMission, Action::SelectAll);
        readers.act(&ReaderKey::IncidentTitle, Action::SelectAll);
        readers.sync(vec![(ReaderKey::BotMission, "Watch the hosts.".into())]);
        assert_eq!(readers.len(), 1);
        let mission = readers.get(&ReaderKey::BotMission).unwrap();
        assert_eq!(mission.content().selection().as_deref(), Some("Watch the hosts."));
        readers.sync(Vec::new());
        assert!(readers.is_empty());
    }
}
