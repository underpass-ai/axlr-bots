use iced::widget::text_editor;

use super::LiveStream;
use crate::application::{Activity, EventFeed};
use crate::domain::TaskId;

/// The task on screen as it is followed: its timeline as read so far,
/// whether a read is on its way, the Activity folded from it, the step
/// whose details are open, the model's words as text to select, and
/// which of its boxes keep their newest lines in view.
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
    /// While they select, it keeps the words as they were, so a new word
    /// never takes their selection away; the read view shows them all.
    pub words: text_editor::Content,
    /// The person selects a part of the words.
    pub selecting: bool,
    /// The boxes the person scrolled up in, by `LiveStream::index`: they
    /// stay where they are while the run writes on.
    held: [bool; 3],
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
            selecting: false,
            held: [false; 3],
        }
    }

    /// Takes a new read: the Activity again, and the words when they grew.
    /// It answers the boxes that grew and follow their end, to bring it
    /// into view.
    pub fn update(&mut self, feed: EventFeed, live: bool) -> Vec<LiveStream> {
        self.feed = feed;
        self.read = true;
        let activity = Activity::of(&self.feed.events, live);
        let grew = [
            activity.entries.len() > self.activity.entries.len(),
            activity.thinking.len() > self.activity.thinking.len(),
            activity.text.len() > self.activity.text.len(),
        ];
        if activity.text != self.activity.text && !self.selecting {
            self.words = text_editor::Content::with_text(&activity.text);
        }
        if self.selected.is_some_and(|i| i >= activity.entries.len()) {
            self.selected = None;
        }
        self.activity = activity;
        LiveStream::ALL
            .into_iter()
            .filter(|stream| grew[stream.index()] && self.follows(*stream))
            .collect()
    }

    /// The person scrolled a box: at its end it follows the run again,
    /// anywhere else it stays put.
    pub fn scrolled(&mut self, stream: LiveStream, at_end: bool) {
        self.held[stream.index()] = !at_end;
    }

    /// Whether the box keeps its newest lines in view.
    pub fn follows(&self, stream: LiveStream) -> bool {
        !self.held[stream.index()]
    }

    /// Opens an entry's details, or closes them when they are open.
    pub fn pick(&mut self, index: usize) {
        self.selected = if self.selected == Some(index) || index >= self.activity.entries.len() {
            None
        } else {
            Some(index)
        };
    }

    /// The words as text to select (`true`), with all of them as they
    /// are now, or back to reading them as they come.
    pub fn selecting(&mut self, selecting: bool) {
        if selecting && !self.selecting {
            self.words = text_editor::Content::with_text(&self.activity.text);
        }
        self.selecting = selecting;
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
        assert_eq!(live.update(feed.clone(), true), [LiveStream::Steps, LiveStream::Words]);
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
        live.selecting(true);
        live.select(Action::Edit(Edit::Insert('x')));
        live.select(Action::SelectAll);
        assert_eq!(live.words.selection().as_deref(), Some("Reading."));
        // New words while the person selects leave the selection alone.
        let mut more = feed.clone();
        more.merge(
            vec![progress(
                "e9",
                serde_json::json!([{"kind": "text", "text": " Then the log."}]),
            )],
            10,
        );
        live.update(more, true);
        assert_eq!(live.words.selection().as_deref(), Some("Reading."));
        assert_eq!(live.activity.text, "Reading. Then the log.");
        live.selecting(false);
        live.selecting(true);
        assert_eq!(live.words.text().trim_end(), "Reading. Then the log.");
        live.selecting(false);
        // The task ended: the call without an end is unfinished, and a
        // selection past the list closes.
        live.update(EventFeed::default(), false);
        assert_eq!(live.selected, None);
        assert!(live.words.text().trim().is_empty());
        live.update(feed, false);
        assert_eq!(live.activity.running(), 0);
    }

    #[test]
    fn a_box_the_person_scrolled_up_in_stays_put() {
        let mut live = LiveTask::new(TaskId::new("task-1").unwrap());
        let mut feed = EventFeed::default();
        feed.merge(
            vec![progress(
                "e1",
                serde_json::json!([{"kind": "thinking", "text": "Hm, "}]),
            )],
            10,
        );
        assert_eq!(live.update(feed.clone(), true), [LiveStream::Thinking]);
        live.scrolled(LiveStream::Thinking, false);
        live.scrolled(LiveStream::Words, false);
        assert!(!live.follows(LiveStream::Thinking) && live.follows(LiveStream::Steps));
        feed.merge(
            vec![progress(
                "e2",
                serde_json::json!([{"kind": "thinking", "text": "the log."}, {"kind": "text", "text": "Done."}]),
            )],
            10,
        );
        assert!(live.update(feed.clone(), true).is_empty());
        assert_eq!(live.activity.thinking, "Hm, the log.");
        live.scrolled(LiveStream::Words, true);
        feed.merge(
            vec![progress("e3", serde_json::json!([{"kind": "text", "text": " More."}]))],
            10,
        );
        assert_eq!(live.update(feed.clone(), true), [LiveStream::Words]);
        assert!(live.update(feed, true).is_empty());
    }
}
