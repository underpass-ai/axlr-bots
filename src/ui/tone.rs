use iced::Color;

use super::{Icon, Tokens};
use crate::domain::{BotStatus, CallVerdict, EventTone, ExecutionOutcome, NodeStatus, TaskStatus};

/// What a status means at a glance: one colour and one icon each.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Tone {
    /// Done, on, healthy.
    Good,
    /// Working right now.
    Busy,
    /// Needs a person.
    Wait,
    /// Failed, off-line, refused.
    Bad,
    /// Stopped, waiting its turn, not started.
    Idle,
}

impl Tone {
    pub fn color(self, tokens: &Tokens) -> Color {
        match self {
            Self::Good => tokens.good,
            Self::Busy => tokens.busy,
            Self::Wait => tokens.wait,
            Self::Bad => tokens.bad,
            Self::Idle => tokens.idle,
        }
    }

    pub fn icon(self) -> Icon {
        match self {
            Self::Good => Icon::Check,
            Self::Busy => Icon::Play,
            Self::Wait => Icon::Alert,
            Self::Bad => Icon::Cross,
            Self::Idle => Icon::Pause,
        }
    }

    /// The icon of an event: a plain dot for one that only happened.
    pub fn event_icon(self) -> Icon {
        match self {
            Self::Idle => Icon::Dot,
            other => other.icon(),
        }
    }

    pub fn of_node(status: NodeStatus) -> Self {
        match status {
            NodeStatus::Online => Self::Good,
            NodeStatus::Registered => Self::Idle,
            NodeStatus::Offline | NodeStatus::Revoked => Self::Bad,
        }
    }

    pub fn of_bot(status: BotStatus) -> Self {
        match status {
            BotStatus::Enabled => Self::Good,
            BotStatus::Paused => Self::Wait,
            BotStatus::Draft | BotStatus::Registered => Self::Idle,
        }
    }

    pub fn of_task(status: TaskStatus) -> Self {
        match status {
            TaskStatus::Completed => Self::Good,
            TaskStatus::Running => Self::Busy,
            TaskStatus::WaitingApproval => Self::Wait,
            TaskStatus::Failed => Self::Bad,
            // Nobody decided in time: a person's miss, not a crash.
            TaskStatus::Expired => Self::Wait,
            TaskStatus::Pending | TaskStatus::Cancelled => Self::Idle,
        }
    }

    pub fn of_outcome(outcome: ExecutionOutcome) -> Self {
        match outcome {
            ExecutionOutcome::Completed => Self::Good,
            ExecutionOutcome::Running => Self::Busy,
            ExecutionOutcome::DecisionRequired => Self::Wait,
            ExecutionOutcome::Failed => Self::Bad,
            ExecutionOutcome::Cancelled => Self::Idle,
        }
    }

    pub fn of_verdict(verdict: CallVerdict) -> Self {
        match verdict {
            CallVerdict::Allowed => Self::Good,
            CallVerdict::Refused => Self::Bad,
            CallVerdict::Asking => Self::Wait,
            CallVerdict::Unknown => Self::Idle,
        }
    }

    pub fn of_event(tone: EventTone) -> Self {
        match tone {
            EventTone::Good => Self::Good,
            EventTone::Bad => Self::Bad,
            EventTone::Waiting => Self::Wait,
            EventTone::Working => Self::Busy,
            EventTone::Neutral => Self::Idle,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn statuses_map_to_tones() {
        assert_eq!(Tone::of_node(NodeStatus::Online), Tone::Good);
        assert_eq!(Tone::of_node(NodeStatus::Registered), Tone::Idle);
        assert_eq!(Tone::of_node(NodeStatus::Revoked), Tone::Bad);
        assert_eq!(Tone::of_bot(BotStatus::Paused), Tone::Wait);
        assert_eq!(Tone::of_bot(BotStatus::Draft), Tone::Idle);
        assert_eq!(Tone::of_bot(BotStatus::Enabled), Tone::Good);
        assert_eq!(Tone::of_task(TaskStatus::WaitingApproval), Tone::Wait);
        assert_eq!(Tone::of_task(TaskStatus::Running), Tone::Busy);
        assert_eq!(Tone::of_task(TaskStatus::Cancelled), Tone::Idle);
        assert_eq!(Tone::of_task(TaskStatus::Failed), Tone::Bad);
        assert_eq!(Tone::of_task(TaskStatus::Expired), Tone::Wait);
        assert_eq!(Tone::of_task(TaskStatus::Completed), Tone::Good);
        assert_eq!(Tone::of_outcome(ExecutionOutcome::DecisionRequired), Tone::Wait);
        assert_eq!(Tone::of_outcome(ExecutionOutcome::Running), Tone::Busy);
        assert_eq!(Tone::of_outcome(ExecutionOutcome::Cancelled), Tone::Idle);
        assert_eq!(Tone::of_outcome(ExecutionOutcome::Failed), Tone::Bad);
        assert_eq!(Tone::of_outcome(ExecutionOutcome::Completed), Tone::Good);
        assert_eq!(Tone::of_verdict(CallVerdict::Allowed), Tone::Good);
        assert_eq!(Tone::of_verdict(CallVerdict::Refused), Tone::Bad);
        assert_eq!(Tone::of_verdict(CallVerdict::Asking), Tone::Wait);
        assert_eq!(Tone::of_verdict(CallVerdict::Unknown), Tone::Idle);
        assert_eq!(Tone::of_event(EventTone::Good), Tone::Good);
        assert_eq!(Tone::of_event(EventTone::Bad), Tone::Bad);
        assert_eq!(Tone::of_event(EventTone::Waiting), Tone::Wait);
        assert_eq!(Tone::of_event(EventTone::Neutral), Tone::Idle);
        assert_eq!(Tone::of_event(EventTone::Working), Tone::Busy);
        assert_eq!(Tone::Idle.event_icon(), Icon::Dot);
        assert_eq!(Tone::Good.event_icon(), Icon::Check);
    }

    #[test]
    fn every_tone_has_its_own_colour_and_icon() {
        let tones = [Tone::Good, Tone::Busy, Tone::Wait, Tone::Bad, Tone::Idle];
        let tokens = Tokens::DARK;
        for (i, a) in tones.iter().enumerate() {
            for b in &tones[i + 1..] {
                assert_ne!(a.color(&tokens), b.color(&tokens));
                assert_ne!(a.icon(), b.icon());
            }
        }
    }
}
