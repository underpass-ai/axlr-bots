use chrono::{DateTime, Duration, Utc};

use super::Tone;
use crate::application::PanelError;

/// The line on top of the content after an act: what happened, in a tone.
#[derive(Debug, Clone, PartialEq)]
pub struct Banner {
    pub tone: Tone,
    pub title: String,
    pub detail: String,
    pub at: DateTime<Utc>,
}

impl Banner {
    pub fn good(title: impl Into<String>, detail: impl Into<String>, at: DateTime<Utc>) -> Self {
        Self {
            tone: Tone::Good,
            title: title.into(),
            detail: detail.into(),
            at,
        }
    }

    pub fn error(error: &PanelError, at: DateTime<Utc>) -> Self {
        Self {
            tone: Tone::Bad,
            title: error.title().into(),
            detail: error.detail().into(),
            at,
        }
    }

    /// The title and the detail on one line, as a copy takes them.
    pub fn copy_text(&self) -> String {
        if self.detail.is_empty() {
            return self.title.clone();
        }
        format!("{}: {}", self.title, self.detail)
    }

    /// A good banner goes after 6 seconds, a bad one after 20.
    pub fn expired(&self, now: DateTime<Utc>) -> bool {
        let shown = match self.tone {
            Tone::Bad => Duration::seconds(20),
            _ => Duration::seconds(6),
        };
        now - self.at > shown
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn good_news_goes_sooner() {
        let at: DateTime<Utc> = "2026-10-25T10:00:00Z".parse().unwrap();
        let good = Banner::good("Sent", "task-1", at);
        assert!(!good.expired(at + Duration::seconds(5)));
        assert!(good.expired(at + Duration::seconds(7)));
        let bad = Banner::error(&PanelError::NotFound("task-1".into()), at);
        assert_eq!((bad.title.as_str(), bad.detail.as_str()), ("Not found", "task-1"));
        assert_eq!(bad.copy_text(), "Not found: task-1");
        assert_eq!(Banner::good("Copied", "", at).copy_text(), "Copied");
        assert!(!bad.expired(at + Duration::seconds(7)));
        assert!(bad.expired(at + Duration::seconds(21)));
    }
}
