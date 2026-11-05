use iced::widget::Id;

/// One part of a running task's Activity that grows at its end while the
/// person watches: its steps, the model's reasoning and its words. Each
/// stands in a box of its own that keeps its newest lines in view until
/// the person scrolls up to read; then it stays where they put it.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum LiveStream {
    Steps,
    Thinking,
    Words,
}

impl LiveStream {
    pub const ALL: [Self; 3] = [Self::Steps, Self::Thinking, Self::Words];

    /// The box's scrollable, to bring its end into view.
    pub fn id(self) -> Id {
        Id::new(match self {
            Self::Steps => "activity-steps",
            Self::Thinking => "activity-thinking",
            Self::Words => "activity-words",
        })
    }

    /// Its place in a list of three.
    pub fn index(self) -> usize {
        match self {
            Self::Steps => 0,
            Self::Thinking => 1,
            Self::Words => 2,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn each_stream_has_its_own_box() {
        let ids: Vec<Id> = LiveStream::ALL.iter().map(|s| s.id()).collect();
        assert!(ids[0] != ids[1] && ids[1] != ids[2] && ids[0] != ids[2]);
        let places: Vec<usize> = LiveStream::ALL.iter().map(|s| s.index()).collect();
        assert_eq!(places, [0, 1, 2]);
    }
}
