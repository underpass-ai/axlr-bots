use iced::widget::text_editor;

use crate::application::{Activity, EventFeed};
use crate::domain::TaskId;

/// The task on screen as it is followed: its timeline as read so far,
/// whether a read is on its way, the Activity folded from it, the step
/// whose details are open, and the model's words as text to select.
pub struct LiveTask {
    pub id: TaskId,
    pub feed: EventFeed,
    /// The first read came back.
    pub read: bool,
    pub following: bool,
    /// The read on its way was asked once the task had ended; once it is
    /// back the timeline is whole and the following stops.
    pub asked_ended: bool,
    pub settled: bool,
    pub activity: Activity,
    /// The Activity entry whose details are open.
    pub selected: Option<usize>,
    /// The model's words, read only: a person selects a part and copies.
    pub words: text_editor::Content,
}

impl LiveTask {
    pub fn new(id: TaskId) -> Self {
        Self {
            id,
            feed: EventFeed::default(),
            read: false,
            following: false,
            asked_ended: false,
            settled: false,
            activity: Activity::default(),
            selected: None,
            words: text_editor::Content::new(),
        }
    }

    /// Takes a new read: the Activity again, and the words when they grew.
    pub fn update(&mut self, feed: EventFeed, live: bool) {
        self.feed = feed;
        self.read = true;
        let activity = Activity::of(&self.feed.events, live);
        if activity.text != self.activity.text {
            self.words = text_editor::Content::with_text(&activity.text);
        }
        if self.selected.is_some_and(|i| i >= activity.entries.len()) {
            self.selected = None;
        }
        self.activity = activity;
    }

    /// Opens an entry's details, or closes them when they are open.
    pub fn pick(&mut self, index: usize) {
        self.selected = if self.selected == Some(index) || index >= self.activity.entries.len() {
            None
        } else {
            Some(index)
        };
    }

    /// Moves the cursor or the selection in the words; an edit is
    /// dropped, they are read only.
    pub fn select(&mut self, action: text_editor::Action) {
        if !action.is_edit() {
            self.words.perform(action);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use iced::widget::text_editor::{Action, Edit};

    fn progress(id: &str, items: serde_json::Value) -> crate::domain::Event {
        serde_json::from_value(serde_json::json!({
            "eventId": id, "type": "bot.task.progress", "tenantId": "t", "botId": "b", "taskId": "task-1",
            "createdAt": "2026-11-02T10:00:00Z", "payload": {"items": items}
        }))
        .unwrap()
    }

    #[test]
    fn follows_the_words_and_the_steps() {
        let mut live = LiveTask::new(TaskId::new("task-1").unwrap());
        let mut feed = EventFeed::default();
        feed.merge(
            vec![progress(
                "e1",
                serde_json::json!([{"kind": "text", "text": "Reading."}, {"kind": "tool.started", "tool": "local_read", "callId": "c1"}]),
            )],
            10,
        );
        live.update(feed.clone(), true);
        assert!(live.read);
        assert_eq!(live.words.text().trim_end(), "Reading.");
        assert_eq!(live.activity.running(), 1);
        live.pick(0);
        assert_eq!(live.selected, Some(0));
        live.pick(0);
        assert_eq!(live.selected, None);
        live.pick(7);
        assert_eq!(live.selected, None);
        live.pick(0);
        live.select(Action::Edit(Edit::Insert('x')));
        live.select(Action::SelectAll);
        assert_eq!(live.words.selection().as_deref(), Some("Reading."));
        // The task ended: the call without an end is unfinished, and a
        // selection past the list closes.
        live.update(EventFeed::default(), false);
        assert_eq!(live.selected, None);
        assert!(live.words.text().trim().is_empty());
        live.update(feed, false);
        assert_eq!(live.activity.running(), 0);
    }
}
