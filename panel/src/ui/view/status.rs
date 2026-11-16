//! A status as a list shows it: a glyph and a word. What ended well or
//! was put away on purpose is quiet; what is abnormal or at work keeps
//! its colour.

use super::widgets::{El, icon, status};
use crate::domain::{BotStatus, NodeStatus, TaskStatus};
use crate::ui::{Icon, Tokens, Tone};

/// A task's status: Done and Stopped are quiet.
pub fn task<'a>(state: TaskStatus, tokens: Tokens) -> El<'a> {
    let tone = Tone::of_task(state);
    status(task_glyph(state), tone, state.label(), task_is_quiet(state), tokens)
}

/// A task's status as its glyph alone, with its word on hover, for a
/// list narrowed beside the inspector.
pub fn task_glyph_only<'a>(state: TaskStatus, tokens: Tokens) -> El<'a> {
    let tone = Tone::of_task(state);
    let ink = match (task_is_quiet(state), tone) {
        (true, Tone::Good) | (false, _) => crate::ui::styles::pill_ink(tokens, tone),
        (true, _) => tokens.muted,
    };
    super::sheet::tip(icon(task_glyph(state), 12.0, ink), state.label().to_string(), tokens)
}

/// The glyph of a task's status.
fn task_glyph(state: TaskStatus) -> Icon {
    match state {
        TaskStatus::Cancelled => Icon::Stop,
        TaskStatus::Pending => Icon::Dot,
        _ => Tone::of_task(state).icon(),
    }
}

/// Whether a task's status is nothing to look at: it ended well, or a
/// person stopped it.
pub fn task_is_quiet(state: TaskStatus) -> bool {
    matches!(state, TaskStatus::Completed | TaskStatus::Cancelled)
}

/// A bot's status, `On` for one that is enabled: only that one has a
/// colour.
pub fn bot<'a>(state: BotStatus, tokens: Tokens) -> El<'a> {
    let tone = Tone::of_bot(state);
    let word = if state == BotStatus::Enabled {
        "On"
    } else {
        state.name()
    };
    status(tone.icon(), tone, word, state != BotStatus::Enabled, tokens)
}

/// A node's status: a revoked node, which never comes back, is quiet.
pub fn node<'a>(state: NodeStatus, tokens: Tokens) -> El<'a> {
    let tone = Tone::of_node(state);
    let (glyph, quiet) = match state {
        NodeStatus::Revoked => (Icon::Ban, true),
        NodeStatus::Registered | NodeStatus::Unknown => (Icon::Dot, false),
        _ => (tone.icon(), false),
    };
    status(glyph, tone, state.name(), quiet, tokens)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn only_what_ended_well_or_was_stopped_is_quiet() {
        for state in TaskStatus::ALL {
            let quiet = matches!(state, TaskStatus::Completed | TaskStatus::Cancelled);
            assert_eq!(task_is_quiet(state), quiet, "{state:?}");
        }
    }
}
