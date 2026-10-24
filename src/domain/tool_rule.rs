use std::collections::BTreeMap;

use serde::{Deserialize, Serialize};

/// A tool and the patterns its arguments must match.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct ToolRule {
    pub tool: String,
    #[serde(default, skip_serializing_if = "BTreeMap::is_empty")]
    pub arguments: BTreeMap<String, String>,
}

impl ToolRule {
    /// `tool(arg=pattern, …)`, or the tool alone.
    pub fn describe(&self) -> String {
        if self.arguments.is_empty() {
            return self.tool.clone();
        }
        let args: Vec<String> = self
            .arguments
            .iter()
            .map(|(k, v)| format!("{k}={v}"))
            .collect();
        format!("{}({})", self.tool, args.join(", "))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn describes_the_rule() {
        let mut rule = ToolRule {
            tool: "local_exec".into(),
            arguments: BTreeMap::new(),
        };
        assert_eq!(rule.describe(), "local_exec");
        rule.arguments.insert("program".into(), "journalctl".into());
        assert_eq!(rule.describe(), "local_exec(program=journalctl)");
    }
}
