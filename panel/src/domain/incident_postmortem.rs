use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

use super::{BotId, TaskId};

/// Where a resolved incident's postmortem is. The incident stays
/// Resolved through all of them.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum PostmortemState {
    Writing,
    Draft,
    SentBack,
    Failed,
    LeftToPerson,
    Approved,
    /// A value this panel does not know: the control plane is newer.
    #[serde(other)]
    Unknown,
}

impl PostmortemState {
    pub const ALL: [PostmortemState; 6] = [
        Self::Writing,
        Self::Draft,
        Self::SentBack,
        Self::Failed,
        Self::LeftToPerson,
        Self::Approved,
    ];

    /// The word the control plane uses.
    pub fn name(self) -> &'static str {
        match self {
            Self::Writing => "Writing",
            Self::Draft => "Draft",
            Self::SentBack => "SentBack",
            Self::Failed => "Failed",
            Self::LeftToPerson => "LeftToPerson",
            Self::Approved => "Approved",
            Self::Unknown => "Unknown",
        }
    }

    /// The state in a person's words.
    pub fn label(self) -> &'static str {
        match self {
            Self::Writing => "being written",
            Self::Draft => "draft, needs review",
            Self::SentBack => "sent back",
            Self::Failed => "not written",
            Self::LeftToPerson => "left to a person",
            Self::Approved => "approved",
            Self::Unknown => "unknown",
        }
    }
}

/// What a resolved incident's postmortem task wrote, and where it stands
/// with the people who review it.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct IncidentPostmortem {
    pub state: PostmortemState,
    #[serde(default)]
    pub cycle: u32,
    pub task_id: TaskId,
    pub bot_id: BotId,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub author_task_id: Option<TaskId>,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub markdown: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub note: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub reviewed_by: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub reviewed_at: Option<DateTime<Utc>>,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub reason: String,
    #[serde(default, skip_serializing_if = "is_zero")]
    pub send_backs: u32,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub approved_by: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub approved_at: Option<DateTime<Utc>>,
}

fn is_zero(n: &u32) -> bool {
    *n == 0
}

impl IncidentPostmortem {
    /// Whether a person may approve its text: a draft, or the last draft
    /// of one left to a person.
    pub fn can_approve(&self) -> bool {
        matches!(self.state, PostmortemState::Draft | PostmortemState::LeftToPerson) && !self.markdown.is_empty()
    }

    /// Whether a person may send it back to be written again.
    pub fn can_send_back(&self) -> bool {
        matches!(
            self.state,
            PostmortemState::Draft | PostmortemState::Failed | PostmortemState::SentBack
        )
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn reads_the_api_json_and_says_what_a_person_may_do() {
        let raw = r##"{"state":"Draft","cycle":2,"taskId":"task-9","botId":"scribe","authorTaskId":"task-9",
            "markdown":"# Postmortem","sendBacks":1,"note":"add the owner","reviewedBy":"tirso"}"##;
        let mut postmortem: IncidentPostmortem = serde_json::from_str(raw).unwrap();
        assert_eq!((postmortem.state, postmortem.cycle), (PostmortemState::Draft, 2));
        assert!(postmortem.can_approve() && postmortem.can_send_back());
        postmortem.state = PostmortemState::LeftToPerson;
        assert!(postmortem.can_approve() && !postmortem.can_send_back());
        postmortem.markdown.clear();
        assert!(!postmortem.can_approve());
        postmortem.state = PostmortemState::Failed;
        assert!(postmortem.can_send_back() && !postmortem.can_approve());
        postmortem.state = PostmortemState::Approved;
        assert!(!postmortem.can_send_back());
        let again: IncidentPostmortem = serde_json::from_str(&serde_json::to_string(&postmortem).unwrap()).unwrap();
        assert_eq!(again, postmortem);
    }

    #[test]
    fn names_match_the_api_and_a_new_state_is_read() {
        for state in PostmortemState::ALL {
            assert_eq!(serde_json::to_string(&state).unwrap(), format!("\"{}\"", state.name()));
            assert!(!state.label().is_empty());
        }
        assert_eq!(
            serde_json::from_str::<PostmortemState>("\"Archived\"").unwrap(),
            PostmortemState::Unknown
        );
        assert_eq!(PostmortemState::Unknown.name(), "Unknown");
        assert_eq!(PostmortemState::Unknown.label(), "unknown");
    }
}
