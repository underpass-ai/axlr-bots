use super::Event;

/// How much of a question or an answer a timeline line holds: what the
/// control plane keeps of an answer in its event.
const SAID: usize = 280;

/// A question of the team channel as a timeline tells it, read from a
/// `bot.question.*` event: on the asker's stream it asked or was refused,
/// on both streams it was answered.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum TeamQuestion {
    /// The bot asked `team`; the control plane gave it to `responder`.
    /// `again` is a repeat that found the question already asked.
    Asked {
        team: String,
        question: String,
        responder: String,
        again: bool,
    },
    /// The question came back: the answer, or why there is none.
    /// `partial` says the answering team's memory is split across stores
    /// and the answer read one of them.
    Answered {
        team: String,
        responder: String,
        answered: bool,
        said: String,
        partial: bool,
    },
    /// The control plane refused the question, and why.
    Refused {
        team: String,
        reason: String,
        message: String,
    },
}

impl TeamQuestion {
    /// The question the event tells; none for any other event.
    pub fn of(event: &Event) -> Option<Self> {
        let text = |key: &str| event.payload_text(key).unwrap_or_default();
        let flag = |key: &str| event.payload_text(key).as_deref() == Some("true");
        match event.event_type.as_str() {
            "bot.question.asked" => Some(Self::Asked {
                team: text("team"),
                question: text("question"),
                responder: text("responder"),
                again: flag("deduplicated"),
            }),
            "bot.question.answered" => {
                // Only a false says there is no answer: a newer control
                // plane that left the flag out still shows what it said.
                let answered = event.payload_text("answered").as_deref() != Some("false");
                let said = match event.payload_text("reason") {
                    Some(reason) if !answered => reason,
                    _ => text("answer"),
                };
                Some(Self::Answered {
                    team: text("team"),
                    responder: text("responder"),
                    answered,
                    said,
                    partial: flag("partial"),
                })
            }
            "bot.question.refused" => Some(Self::Refused {
                team: text("team"),
                reason: text("reason"),
                message: text("message"),
            }),
            _ => None,
        }
    }

    /// Whether it came back without an answer.
    pub fn unanswered(&self) -> bool {
        matches!(self, Self::Answered { answered: false, .. })
    }

    /// Whether the answer read one store of several.
    pub fn partial(&self) -> bool {
        matches!(self, Self::Answered { partial: true, .. })
    }

    /// The line under the event: `dev: Which release…? · to dev-a`,
    /// `dev-a (dev): 0.4.2 · partial: one store of several`, `no answer
    /// from dev-a (dev): expired`, `marketing: not_allowed · …`.
    pub fn detail(&self) -> String {
        match self {
            Self::Asked {
                team,
                question,
                responder,
                again,
            } => {
                let mut line = format!("{}: {}", or_unknown(team), said(question));
                if !responder.is_empty() {
                    line.push_str(&format!(" · to {responder}"));
                }
                if *again {
                    line.push_str(" · asked before");
                }
                line
            }
            Self::Answered {
                team,
                responder,
                answered,
                said: words,
                partial,
            } => {
                let who = format!("{} ({})", or_unknown(responder), or_unknown(team));
                let mut line = if *answered {
                    format!("{who}: {}", said(words))
                } else {
                    format!("no answer from {who}: {}", said(words))
                };
                if *partial {
                    line.push_str(" · partial: one store of several");
                }
                line
            }
            Self::Refused { team, reason, message } => {
                let mut line = format!("{}: {}", or_unknown(team), or_unknown(reason));
                if !message.is_empty() {
                    line.push_str(&format!(" · {}", said(message)));
                }
                line
            }
        }
    }
}

/// A name the event left out reads `unknown`.
fn or_unknown(words: &str) -> &str {
    if words.is_empty() { "unknown" } else { words }
}

/// What a line holds of a question or an answer, on one line.
fn said(words: &str) -> String {
    let flat = words.split_whitespace().collect::<Vec<_>>().join(" ");
    if flat.chars().count() <= SAID {
        return flat;
    }
    let cut: String = flat.chars().take(SAID - 1).collect();
    format!("{cut}…")
}

#[cfg(test)]
mod tests {
    use super::*;

    fn event(kind: &str, payload: serde_json::Value) -> Event {
        serde_json::from_value(serde_json::json!({
            "eventId": "evt-1", "type": kind, "tenantId": "default", "botId": "qa-jefe", "taskId": "task-1",
            "createdAt": "2026-11-11T10:00:00Z", "payload": payload
        }))
        .unwrap()
    }

    #[test]
    fn an_ask_says_the_team_the_question_and_who_got_it() {
        let asked = event(
            "bot.question.asked",
            serde_json::json!({"team": "dev", "question": "Which release\nfixed the importer?",
                "childTaskId": "task-9", "responder": "dev-a", "deduplicated": false}),
        );
        let question = TeamQuestion::of(&asked).unwrap();
        assert_eq!(question.detail(), "dev: Which release fixed the importer? · to dev-a");
        assert!(!question.unanswered() && !question.partial());
        let again = event(
            "bot.question.asked",
            serde_json::json!({"team": "dev", "question": "Same?", "deduplicated": true}),
        );
        assert_eq!(TeamQuestion::of(&again).unwrap().detail(), "dev: Same? · asked before");
    }

    #[test]
    fn an_answer_says_who_answered_what_and_whether_it_is_partial() {
        let answered = event(
            "bot.question.answered",
            serde_json::json!({"questionTaskId": "task-9", "team": "dev", "responder": "dev-a", "status": "Completed",
                "answered": true, "store": "kmp/dev", "partial": true, "node": "dev-node", "answer": "0.4.2",
                "delivery": {"mode": "message"}}),
        );
        let question = TeamQuestion::of(&answered).unwrap();
        assert_eq!(question.detail(), "dev-a (dev): 0.4.2 · partial: one store of several");
        assert!(question.partial() && !question.unanswered());
        let whole = event(
            "bot.question.answered",
            serde_json::json!({"team": "dev", "responder": "dev-a", "answered": true, "partial": false, "answer": "0.4.2"}),
        );
        assert_eq!(TeamQuestion::of(&whole).unwrap().detail(), "dev-a (dev): 0.4.2");
    }

    #[test]
    fn no_answer_says_why() {
        let expired = event(
            "bot.question.answered",
            serde_json::json!({"team": "dev", "responder": "dev-a", "status": "Expired", "answered": false,
                "reason": "nobody answered within two hours", "answer": "nobody answered within two hours"}),
        );
        let question = TeamQuestion::of(&expired).unwrap();
        assert!(question.unanswered());
        assert_eq!(
            question.detail(),
            "no answer from dev-a (dev): nobody answered within two hours"
        );
        // A newer control plane without the flag still shows what it said;
        // a name it left out reads unknown.
        let bare = event("bot.question.answered", serde_json::json!({"answer": "yes"}));
        let question = TeamQuestion::of(&bare).unwrap();
        assert!(!question.unanswered());
        assert_eq!(question.detail(), "unknown (unknown): yes");
    }

    #[test]
    fn a_refusal_says_the_reason_and_a_long_question_is_cut() {
        let refused = event(
            "bot.question.refused",
            serde_json::json!({"team": "marketing", "reason": "not_allowed", "message": "ask.teams does not name marketing"}),
        );
        assert_eq!(
            TeamQuestion::of(&refused).unwrap().detail(),
            "marketing: not_allowed · ask.teams does not name marketing"
        );
        let bare = event("bot.question.refused", serde_json::json!({}));
        assert_eq!(TeamQuestion::of(&bare).unwrap().detail(), "unknown: unknown");
        let long = event(
            "bot.question.asked",
            serde_json::json!({"team": "dev", "question": "x".repeat(2000)}),
        );
        let detail = TeamQuestion::of(&long).unwrap().detail();
        assert_eq!(detail.chars().count(), "dev: ".len() + SAID);
        assert!(detail.ends_with('…'));
        assert!(TeamQuestion::of(&event("bot.task.started", serde_json::json!({}))).is_none());
    }
}
