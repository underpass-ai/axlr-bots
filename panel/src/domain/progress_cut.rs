use chrono::{DateTime, Utc};

use super::CutReason;

/// Where an execution's recorded progress stops, and what was done about
/// the rest: the missing steps are read back from the transcript when the
/// control plane keeps one.
#[derive(Debug, Clone, PartialEq)]
pub struct ProgressCut {
    pub reason: CutReason,
    pub events_kept: u32,
    pub bytes_kept: u64,
    pub cut_at: Option<DateTime<Utc>>,
    /// Steps the node said it dropped before it sent them.
    pub dropped: u32,
    /// The steps after the cut come from the transcript.
    pub from_transcript: bool,
    pub transcript_sessions: Vec<String>,
    /// The completion itself was cut short at its bound.
    pub completion_bounded: bool,
}

impl ProgressCut {
    /// One line for the screen: what happened to the steps after the cut.
    pub fn summary(&self) -> String {
        let mut line = format!("Steps cut: {}", self.reason.label());
        if self.from_transcript {
            line.push_str(" · completed from the transcript");
            if self.completion_bounded {
                line.push_str(", not to the end");
            }
        } else {
            line.push_str(" · the rest is not shown");
        }
        if self.dropped > 0 {
            line.push_str(&format!(" · the node dropped {}", self.dropped));
        }
        line
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn cut() -> ProgressCut {
        ProgressCut {
            reason: CutReason::EventLimit,
            events_kept: 400,
            bytes_kept: 1000,
            cut_at: None,
            dropped: 0,
            from_transcript: false,
            transcript_sessions: Vec::new(),
            completion_bounded: false,
        }
    }

    #[test]
    fn says_what_became_of_the_steps_after_the_cut() {
        assert_eq!(
            cut().summary(),
            "Steps cut: event limit reached · the rest is not shown"
        );
        let completed = ProgressCut {
            from_transcript: true,
            completion_bounded: true,
            dropped: 3,
            ..cut()
        };
        assert_eq!(
            completed.summary(),
            "Steps cut: event limit reached · completed from the transcript, not to the end · the node dropped 3"
        );
    }
}
