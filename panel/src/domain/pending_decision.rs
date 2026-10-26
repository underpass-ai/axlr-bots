use serde::{Deserialize, Serialize};

/// The call a run stopped on until a person decides: the tool and its
/// arguments.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct PendingDecision {
    pub tool: String,
    #[serde(default)]
    pub call_id: String,
    #[serde(default)]
    pub session_id: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub arguments: Option<serde_json::Value>,
}

impl PendingDecision {
    /// The arguments as `name: value` lines, strings unquoted; the JSON
    /// itself when they are not an object.
    pub fn argument_lines(&self) -> Vec<(String, String)> {
        match &self.arguments {
            None | Some(serde_json::Value::Null) => Vec::new(),
            Some(serde_json::Value::Object(map)) => map
                .iter()
                .map(|(k, v)| {
                    let shown = match v {
                        serde_json::Value::String(s) => s.clone(),
                        other => other.to_string(),
                    };
                    (k.clone(), shown)
                })
                .collect(),
            Some(other) => vec![("arguments".to_string(), other.to_string())],
        }
    }

    /// The call in a few words: the tool and its first argument's value,
    /// cut to `max` characters, `local_write · hello`.
    pub fn summary(&self, max: usize) -> String {
        let Some((_, value)) = self.argument_lines().into_iter().next() else {
            return self.tool.clone();
        };
        let value = value.lines().next().unwrap_or_default();
        let value = if value.chars().count() > max {
            format!("{}…", value.chars().take(max.saturating_sub(1)).collect::<String>())
        } else {
            value.to_string()
        };
        format!("{} · {value}", self.tool)
    }

    /// The tool, then its arguments one per line: what a copy of the call
    /// takes.
    pub fn call_text(&self) -> String {
        let mut lines = vec![self.tool.clone()];
        lines.extend(
            self.argument_lines()
                .into_iter()
                .map(|(name, value)| format!("{name}: {value}")),
        );
        lines.join("\n")
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn lists_the_arguments() {
        let pending: PendingDecision = serde_json::from_str(
            r#"{"tool":"local_write","callId":"c1","sessionId":"s1","arguments":{"path":"hello.txt","content":"hello","append":false}}"#,
        )
        .unwrap();
        assert_eq!(
            pending.argument_lines(),
            vec![
                ("append".to_string(), "false".to_string()),
                ("content".to_string(), "hello".to_string()),
                ("path".to_string(), "hello.txt".to_string()),
            ]
        );
        assert_eq!(
            pending.call_text(),
            "local_write\nappend: false\ncontent: hello\npath: hello.txt"
        );
        let bare: PendingDecision = serde_json::from_str(r#"{"tool":"t"}"#).unwrap();
        assert!(bare.argument_lines().is_empty());
        assert_eq!(bare.call_text(), "t");
        assert_eq!(bare.summary(10), "t");
        assert_eq!(pending.summary(10), "local_write · false");
        let long: PendingDecision =
            serde_json::from_str(r#"{"tool":"say","arguments":{"text":"a long line\nand more"}}"#).unwrap();
        assert_eq!(long.summary(6), "say · a lon…");
        assert_eq!(long.summary(40), "say · a long line");
        let list: PendingDecision = serde_json::from_str(r#"{"tool":"t","arguments":[1,2]}"#).unwrap();
        assert_eq!(
            list.argument_lines(),
            vec![("arguments".to_string(), "[1,2]".to_string())]
        );
    }
}
