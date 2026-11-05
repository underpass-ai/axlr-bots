use std::path::Path;

use super::PendingDecision;

/// The names of an argument that carries the text a bot is about to
/// publish, on a command line (`--text`) or in a JSON object (`text`).
const TEXT_KEYS: [&str; 4] = ["text", "message", "body", "post"];
/// The names of an argument that attaches something to it.
const ATTACHMENT_KEYS: [&str; 5] = ["card", "image", "cover", "diagram", "attachment"];

/// What a bot is about to publish somewhere, read from the call it waits
/// on: the words, what goes with them, and which channel they go to.
///
/// The panel knows nothing about any network: a call carries a draft when
/// one of its arguments is named like a message (`--text`, `message`,
/// `body`, `post`), and its attachments are the arguments named like one
/// (`--card`, `--image`, `--cover`, `--diagram`). The pictures themselves
/// are drawn on the node, so each attachment is a line of words here.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PostDraft {
    /// The program or tool that publishes: `x-post`, `mastodon-post`, `say`.
    pub channel: String,
    pub text: String,
    /// One line per attachment: `headline · DeepSeek V4.1 at 524k context`.
    pub attachments: Vec<String>,
}

impl PostDraft {
    /// The draft a waiting call carries, or None for a call that publishes
    /// nothing: a `local_write` whose `content` is a file, a `local_read`.
    pub fn from_call(call: &PendingDecision) -> Option<Self> {
        let arguments = call.arguments.as_ref()?.as_object()?;
        if let Some(args) = arguments.get("args").and_then(|a| a.as_array()) {
            let program = arguments
                .get("program")
                .and_then(|p| p.as_str())
                .map(|p| {
                    Path::new(p)
                        .file_name()
                        .map(|n| n.to_string_lossy().into_owned())
                        .unwrap_or_default()
                })
                .filter(|p| !p.is_empty())
                .unwrap_or_else(|| call.tool.clone());
            return Self::from_command_line(program, args);
        }
        let text = TEXT_KEYS
            .iter()
            .find_map(|key| arguments.get(*key).and_then(|v| v.as_str()))
            .filter(|t| !t.trim().is_empty())?;
        let attachments = ATTACHMENT_KEYS
            .iter()
            .filter_map(|key| arguments.get(*key))
            .flat_map(|value| match value {
                serde_json::Value::Array(items) => items.iter().map(attachment_line).collect::<Vec<_>>(),
                other => vec![attachment_line(other)],
            })
            .collect();
        Some(PostDraft {
            channel: call.tool.clone(),
            text: text.to_string(),
            attachments,
        })
    }

    fn from_command_line(channel: String, args: &[serde_json::Value]) -> Option<Self> {
        let args: Vec<String> = args
            .iter()
            .map(|a| match a {
                serde_json::Value::String(s) => s.clone(),
                other => other.to_string(),
            })
            .collect();
        let mut text = None;
        let mut attachments = Vec::new();
        for (i, arg) in args.iter().enumerate() {
            let Some(flag) = arg.strip_prefix("--") else {
                continue;
            };
            let Some(value) = args.get(i + 1).filter(|v| !v.starts_with("--")) else {
                continue;
            };
            if text.is_none() && TEXT_KEYS.contains(&flag) && !value.trim().is_empty() {
                text = Some(value.clone());
            } else if ATTACHMENT_KEYS.contains(&flag) {
                attachments.push(attachment_words(value));
            }
        }
        Some(PostDraft {
            channel,
            text: text?,
            attachments,
        })
    }

    /// The links in the text, as written.
    pub fn links(&self) -> Vec<&str> {
        self.text
            .split_whitespace()
            .filter(|w| w.starts_with("https://") || w.starts_with("http://"))
            .collect()
    }

    /// How many characters the text has, as a person counts them.
    pub fn characters(&self) -> usize {
        self.text.chars().count()
    }
}

/// An attachment in a few words: a JSON card becomes its template and its
/// first title-like field, a path its file name, anything else itself.
fn attachment_words(raw: &str) -> String {
    match serde_json::from_str::<serde_json::Value>(raw) {
        Ok(value) if value.is_object() => attachment_line(&value),
        _ => Path::new(raw)
            .file_name()
            .map(|n| n.to_string_lossy().into_owned())
            .filter(|n| !n.is_empty())
            .unwrap_or_else(|| raw.to_string()),
    }
}

fn attachment_line(value: &serde_json::Value) -> String {
    let Some(map) = value.as_object() else {
        return match value {
            serde_json::Value::String(s) => attachment_words(s),
            other => other.to_string(),
        };
    };
    let kind = map
        .get("template")
        .or(map.get("kind"))
        .and_then(|v| v.as_str())
        .unwrap_or("attachment");
    let title = ["headline", "title", "name", "quote", "caption", "alt"]
        .iter()
        .find_map(|k| map.get(*k).and_then(|v| v.as_str()))
        .unwrap_or_default();
    if title.is_empty() {
        kind.to_string()
    } else {
        format!("{kind} · {title}")
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn call(tool: &str, arguments: serde_json::Value) -> PendingDecision {
        PendingDecision {
            tool: tool.into(),
            call_id: "c1".into(),
            session_id: "s1".into(),
            arguments: Some(arguments),
        }
    }

    #[test]
    fn reads_a_post_from_a_command_line() {
        let draft = PostDraft::from_call(&call(
            "local_exec",
            serde_json::json!({
                "program": "/home/me/x-ai-news/bin/x-post",
                "args": ["--topic", "hardware", "--text", "DeepSeek on four 3090s. https://example.com/a",
                         "--card", "{\"template\":\"headline\",\"headline\":\"DeepSeek V4.1 at 524k\"}",
                         "--card", "{\"template\":\"flow\",\"title\":\"How Maya runs it\",\"steps\":[]}",
                         "--cover", "/tmp/pictures/cover.png", "--long"],
                "timeout_ms": 120000
            }),
        ))
        .unwrap();
        assert_eq!(draft.channel, "x-post");
        assert_eq!(draft.text, "DeepSeek on four 3090s. https://example.com/a");
        assert_eq!(
            draft.attachments,
            vec![
                "headline · DeepSeek V4.1 at 524k",
                "flow · How Maya runs it",
                "cover.png"
            ]
        );
        assert_eq!(draft.links(), vec!["https://example.com/a"]);
        assert_eq!(draft.characters(), 45);
    }

    #[test]
    fn reads_a_post_from_an_object() {
        let draft = PostDraft::from_call(&call(
            "say",
            serde_json::json!({"message": "Hello fleet", "image": ["a.png", {"kind": "chart", "title": "Cost"}]}),
        ))
        .unwrap();
        assert_eq!(draft.channel, "say");
        assert_eq!(draft.text, "Hello fleet");
        assert_eq!(draft.attachments, vec!["a.png", "chart · Cost"]);
        assert!(draft.links().is_empty());
    }

    #[test]
    fn a_call_that_publishes_nothing_has_no_draft() {
        assert_eq!(
            PostDraft::from_call(&call(
                "local_write",
                serde_json::json!({"path": "hello.txt", "content": "hello"})
            )),
            None
        );
        assert_eq!(
            PostDraft::from_call(&call(
                "local_exec",
                serde_json::json!({"program": "ls", "args": ["--text"]})
            )),
            None
        );
        assert_eq!(
            PostDraft::from_call(&call("say", serde_json::json!({"text": "  "}))),
            None
        );
        assert_eq!(PostDraft::from_call(&call("say", serde_json::json!([1, 2]))), None);
        assert_eq!(
            PostDraft::from_call(&PendingDecision {
                tool: "t".into(),
                call_id: String::new(),
                session_id: String::new(),
                arguments: None
            }),
            None
        );
    }

    #[test]
    fn a_program_without_a_path_names_the_tool() {
        let draft = PostDraft::from_call(&call("local_exec", serde_json::json!({"args": ["--body", "hi"]}))).unwrap();
        assert_eq!(draft.channel, "local_exec");
        let draft = PostDraft::from_call(&call(
            "local_exec",
            serde_json::json!({"program": "", "args": ["--post", "hi", "--image", "{not json"]}),
        ))
        .unwrap();
        assert_eq!(
            (draft.channel.as_str(), draft.attachments),
            ("local_exec", vec!["{not json".to_string()])
        );
    }
}
