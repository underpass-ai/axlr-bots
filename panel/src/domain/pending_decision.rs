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
        let bare: PendingDecision = serde_json::from_str(r#"{"tool":"t"}"#).unwrap();
        assert!(bare.argument_lines().is_empty());
        let list: PendingDecision = serde_json::from_str(r#"{"tool":"t","arguments":[1,2]}"#).unwrap();
        assert_eq!(
            list.argument_lines(),
            vec![("arguments".to_string(), "[1,2]".to_string())]
        );
    }
}
