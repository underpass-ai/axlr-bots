use serde::{Deserialize, Serialize};

use super::ToolRule;

/// The calls a bot's policy lets through and the ones it refuses.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct ToolRules {
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub allow: Vec<ToolRule>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub deny: Vec<ToolRule>,
}

impl ToolRules {
    pub fn is_empty(&self) -> bool {
        self.allow.is_empty() && self.deny.is_empty()
    }

    /// The rules about one tool, in words: `allows path=*.txt`,
    /// `forbids path=/etc/*`.
    pub fn about(&self, tool: &str) -> Vec<String> {
        let allows = self
            .allow
            .iter()
            .filter(|r| r.tool == tool)
            .map(|r| format!("allows {}", r.describe()));
        let forbids = self
            .deny
            .iter()
            .filter(|r| r.tool == tool)
            .map(|r| format!("forbids {}", r.describe()));
        allows.chain(forbids).collect()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::BTreeMap;

    #[test]
    fn names_the_rules_about_a_tool() {
        let rule = |tool: &str, arg: &str| ToolRule {
            tool: tool.into(),
            arguments: BTreeMap::from([("path".to_string(), arg.to_string())]),
        };
        let rules = ToolRules {
            allow: vec![rule("local_write", "*.txt"), rule("local_read", "*")],
            deny: vec![rule("local_write", "/etc/*")],
        };
        assert_eq!(
            rules.about("local_write"),
            vec!["allows local_write(path=*.txt)", "forbids local_write(path=/etc/*)"]
        );
        assert!(rules.about("local_list").is_empty());
    }
}
