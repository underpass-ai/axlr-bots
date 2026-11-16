use super::StepKind;
use crate::application::{ActivityEntry, ToolState};

/// One step of a task's activity as its find reads it: what kind it is,
/// whether it failed, and every word its line shows. The view makes one
/// per line it draws, in order, so a find's step is a line's index.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct StepText {
    pub kind: StepKind,
    pub failed: bool,
    pub text: String,
}

impl StepText {
    /// A tool call: its name, the sentence that says why it was made
    /// when it has one, and its arguments.
    pub fn tool(name: &str, why: &str, arguments: &str, failed: bool) -> Self {
        Self {
            kind: StepKind::Tool,
            failed,
            text: join(&[name, why, arguments]),
        }
    }

    /// An event the task emitted: its name and its message.
    pub fn event(name: &str, message: &str) -> Self {
        Self {
            kind: StepKind::Event,
            failed: false,
            text: join(&[name, message]),
        }
    }

    /// Words: what the task said, or any other line that is neither a
    /// tool call nor an event.
    pub fn note(text: &str) -> Self {
        Self {
            kind: StepKind::Note,
            failed: false,
            text: text.trim().to_string(),
        }
    }

    /// A line of the activity the panel folds from a task's timeline: a
    /// call by its name, why it was made and its arguments, failed when it
    /// ended so; an event by its name, its message and whom it reached;
    /// any other line by its words.
    pub fn of(entry: &ActivityEntry) -> Self {
        match entry {
            ActivityEntry::Tool(call) => Self::tool(
                call.name(),
                &call.why().unwrap_or_default(),
                &call.summary,
                call.state == ToolState::Failed,
            ),
            ActivityEntry::Emitted(event) => Self::event(&event.name, &join(&[&event.message, &event.to.join(" ")])),
            ActivityEntry::YouWrote { text, .. }
            | ActivityEntry::Said { text, .. }
            | ActivityEntry::Words { text, .. } => Self::note(text),
            other => Self::note(&other.headline()),
        }
    }
}

/// The parts that say something, a space between them.
fn join(parts: &[&str]) -> String {
    parts
        .iter()
        .map(|part| part.trim())
        .filter(|part| !part.is_empty())
        .collect::<Vec<_>>()
        .join(" ")
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::application::{DelegationActivity, ToolActivity};

    fn at() -> chrono::DateTime<chrono::Utc> {
        "2026-10-25T09:00:00Z".parse().unwrap()
    }

    #[test]
    fn reads_every_word_a_line_shows() {
        let call = StepText::tool("local_read", "See what the file says", "{\"path\":\"a.txt\"}", false);
        assert_eq!(call.text, "local_read See what the file says {\"path\":\"a.txt\"}");
        assert_eq!((call.kind, call.failed), (StepKind::Tool, false));
        let bare = StepText::tool("local_list", " ", "", true);
        assert_eq!((bare.text.as_str(), bare.failed), ("local_list", true));
        let event = StepText::event("stock.low", "3 left of A-12");
        assert_eq!(
            (event.kind, event.text.as_str()),
            (StepKind::Event, "stock.low 3 left of A-12")
        );
        let said = StepText::note("  There are 3 files. ");
        assert_eq!(
            (said.kind, said.text.as_str(), said.failed),
            (StepKind::Note, "There are 3 files.", false)
        );
    }

    #[test]
    fn reads_the_lines_the_panel_folds_from_a_timeline() {
        let mut call = ToolActivity {
            call_id: "call-1".into(),
            tool: "axlr_call_tool".into(),
            mcp_tool: "list_pods".into(),
            summary: "namespace=web".into(),
            state: ToolState::Failed,
            error: "no such namespace".into(),
            started_at: at(),
            duration_ms: Some(120),
        };
        let failed = StepText::of(&ActivityEntry::Tool(call.clone()));
        assert_eq!(failed, StepText::tool("list_pods", "", "namespace=web", true));
        let mut reasoned = call.clone();
        reasoned.summary = r#"{"why":"See the pods","namespace":"web"}"#.into();
        assert_eq!(
            StepText::of(&ActivityEntry::Tool(reasoned)).text,
            r#"list_pods See the pods {"why":"See the pods","namespace":"web"}"#
        );
        let emitted = StepText::of(&ActivityEntry::Emitted(crate::application::EmittedActivity {
            name: "item.done".into(),
            message: "item-1 read".into(),
            to: vec!["lead".into(), "worker".into()],
            at: at(),
        }));
        assert_eq!(emitted, StepText::event("item.done", "item-1 read lead worker"));
        let words = StepText::of(&ActivityEntry::Words {
            text: "Reading the notes.".into(),
            at: at(),
        });
        assert_eq!(words, StepText::note("Reading the notes."));
        for state in [
            ToolState::Ok,
            ToolState::Denied,
            ToolState::Running,
            ToolState::Unfinished,
        ] {
            call.state = state;
            assert!(!StepText::of(&ActivityEntry::Tool(call.clone())).failed, "{state:?}");
        }
        let said = StepText::of(&ActivityEntry::Said {
            text: "Done.".into(),
            at: at(),
        });
        assert_eq!(said, StepText::note("Done."));
        let wrote = StepText::of(&ActivityEntry::YouWrote {
            text: "Try again".into(),
            at: at(),
        });
        assert_eq!(wrote, StepText::note("Try again"));
        let asked = StepText::of(&ActivityEntry::Delegation(DelegationActivity {
            bot: "lab-reader".into(),
            task: None,
            refused: String::new(),
            at: at(),
        }));
        assert_eq!(asked, StepText::note("Asked lab-reader"));
        let waiting = StepText::of(&ActivityEntry::Waiting {
            tool: "local_write".into(),
            call_id: "call-2".into(),
            at: at(),
            decision: String::new(),
        });
        assert_eq!(waiting, StepText::note("Needs OK to call local_write"));
    }
}
