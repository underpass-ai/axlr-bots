use super::{Bot, BotPatch, DomainError, ToolAccess, ToolRule, ToolRules, ToolSetting};

/// The AXLR tools every install has; the panel offers them by name.
pub const CORE_TOOLS: [&str; 5] = ["local_list", "local_read", "local_search", "local_write", "local_exec"];

/// The most tools a bot may list here.
const MAX_TOOLS: usize = 64;

/// A bot's tools as the operator edits them: each listed tool and what
/// it may do. It is turned back into the control plane's three lists
/// (`allowedTools`, `toolRules`, `approvals.autoApprove`) against the bot
/// as it is at save time: the rules with argument patterns the panel does
/// not show stay as they were, and every rule about a tool taken off the
/// list goes with it (the control plane refuses a rule about a tool the
/// bot no longer has).
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct ToolPlan {
    pub tools: Vec<ToolSetting>,
}

impl ToolPlan {
    /// The bot's tools as they are.
    pub fn of(bot: &Bot) -> Self {
        let tools = bot
            .allowed_tools
            .iter()
            .map(|tool| ToolSetting {
                tool: tool.clone(),
                access: if whole(&bot.tool_rules.deny, tool) {
                    ToolAccess::Never
                } else if whole(&bot.approvals.auto_approve, tool) {
                    ToolAccess::OnItsOwn
                } else {
                    ToolAccess::AsksFirst
                },
            })
            .collect();
        Self { tools }
    }

    /// Whether the plan lists the tool.
    pub fn has(&self, tool: &str) -> bool {
        self.tools.iter().any(|t| t.tool == tool)
    }

    /// Adds a tool: a name as the control plane takes it, once.
    pub fn add(&mut self, tool: &str, access: ToolAccess) -> Result<(), DomainError> {
        let tool = tool.trim();
        validate_tool(tool)?;
        if self.has(tool) {
            return Err(DomainError::new("tool", format!("{tool} is listed already")));
        }
        if self.tools.len() >= MAX_TOOLS {
            return Err(DomainError::new("tool", "a bot lists at most 64 tools here"));
        }
        self.tools.push(ToolSetting {
            tool: tool.to_string(),
            access,
        });
        Ok(())
    }

    /// Takes a tool off the list.
    pub fn remove(&mut self, index: usize) {
        if index < self.tools.len() {
            self.tools.remove(index);
        }
    }

    /// Changes what a listed tool may do.
    pub fn set(&mut self, index: usize, access: ToolAccess) {
        if let Some(setting) = self.tools.get_mut(index) {
            setting.access = access;
        }
    }

    /// The names it may still add, of the ones offered, in order.
    pub fn offers<'a>(&self, known: impl IntoIterator<Item = &'a str>, typed: &str) -> Vec<String> {
        let typed = typed.trim().to_lowercase();
        let mut out: Vec<String> = Vec::new();
        for name in known {
            if !self.has(name) && name.contains(typed.as_str()) && !out.iter().any(|o| o == name) {
                out.push(name.to_string());
            }
        }
        out
    }

    /// The patch that makes `current` have these tools; `None` when it
    /// has them already.
    pub fn patch(&self, current: &Bot) -> Option<BotPatch> {
        let listed = |tool: &str| self.has(tool);
        let removed = |tool: &str| current.allowed_tools.iter().any(|t| t == tool) && !listed(tool);
        let access = |tool: &str| self.tools.iter().find(|t| t.tool == tool).map(|t| t.access);
        // Rules with argument patterns stay, and whole rules about tools
        // the plan does not list; a whole rule about a listed tool stays
        // while the plan still says so, in its place, and the plan's new
        // ones come after.
        let merged = |rules: &[ToolRule], wanted: ToolAccess| -> Vec<ToolRule> {
            let mut out: Vec<ToolRule> = rules
                .iter()
                .filter(|r| {
                    !removed(&r.tool)
                        && (!r.arguments.is_empty() || !listed(&r.tool) || access(&r.tool) == Some(wanted))
                })
                .cloned()
                .collect();
            for setting in self.tools.iter().filter(|t| t.access == wanted) {
                if !whole(&out, &setting.tool) {
                    out.push(ToolRule::whole(&setting.tool));
                }
            }
            out
        };
        let deny = merged(&current.tool_rules.deny, ToolAccess::Never);
        let auto = merged(&current.approvals.auto_approve, ToolAccess::OnItsOwn);
        let allow: Vec<ToolRule> = current
            .tool_rules
            .allow
            .iter()
            .filter(|r| listed(&r.tool))
            .cloned()
            .collect();
        let tools: Vec<String> = self.tools.iter().map(|t| t.tool.clone()).collect();
        let rules = ToolRules { allow, deny };
        if tools == current.allowed_tools && rules == current.tool_rules && auto == current.approvals.auto_approve {
            return None;
        }
        Some(BotPatch {
            allowed_tools: Some(tools),
            tool_rules: Some(rules),
            auto_approve: Some(auto),
            ..BotPatch::default()
        })
    }
}

/// Whether a rule names the tool as a whole, with no argument pattern.
fn whole(rules: &[ToolRule], tool: &str) -> bool {
    rules.iter().any(|r| r.tool == tool && r.arguments.is_empty())
}

/// A tool name as the control plane takes it: dotted segments of
/// lowercase letters, digits, `_` and `-`, at most 128 characters.
pub fn validate_tool(tool: &str) -> Result<(), DomainError> {
    if tool.is_empty() {
        return Err(DomainError::new("tool", "type a tool name"));
    }
    if tool.len() > 128 {
        return Err(DomainError::new("tool", "a tool name is at most 128 characters"));
    }
    let fine = |c: char| c.is_ascii_lowercase() || c.is_ascii_digit() || c == '_' || c == '-';
    if tool
        .split('.')
        .any(|segment| segment.is_empty() || !segment.chars().all(fine))
    {
        return Err(DomainError::new(
            "tool",
            "a tool name uses lowercase letters, digits, _ and -, with dots between parts",
        ));
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::domain::BotApprovals;
    use std::collections::BTreeMap;

    fn rule(tool: &str, arg: Option<(&str, &str)>) -> ToolRule {
        ToolRule {
            tool: tool.into(),
            arguments: arg
                .map(|(k, v)| BTreeMap::from([(k.to_string(), v.to_string())]))
                .unwrap_or_default(),
        }
    }

    fn bot() -> Bot {
        let mut bot: Bot = serde_json::from_str(
            r#"{"id":"worker-1","name":"worker-1","mission":"Do it.","status":"Enabled",
            "createdAt":"2026-11-05T10:00:00Z","updatedAt":"2026-11-05T10:00:00Z"}"#,
        )
        .unwrap();
        bot.allowed_tools = vec!["local_read".into(), "local_write".into(), "local_exec".into()];
        bot.tool_rules = ToolRules {
            allow: vec![rule("local_write", Some(("path", "*.txt")))],
            deny: vec![rule("local_exec", None), rule("local_write", Some(("path", "/etc/*")))],
        };
        bot.approvals = BotApprovals {
            auto_approve: vec![rule("local_read", None), rule("rollback", None)],
        };
        bot
    }

    #[test]
    fn reads_each_tool_in_words() {
        let plan = ToolPlan::of(&bot());
        let words: Vec<(&str, ToolAccess)> = plan.tools.iter().map(|t| (t.tool.as_str(), t.access)).collect();
        assert_eq!(
            words,
            [
                ("local_read", ToolAccess::OnItsOwn),
                ("local_write", ToolAccess::AsksFirst),
                ("local_exec", ToolAccess::Never)
            ]
        );
        assert_eq!(plan.patch(&bot()), None, "nothing changed, nothing to save");
    }

    #[test]
    fn adds_removes_and_changes_a_tool_into_one_patch() {
        let current = bot();
        let mut plan = ToolPlan::of(&current);
        plan.add(" local_list ", ToolAccess::OnItsOwn).unwrap();
        assert!(plan.add("local_list", ToolAccess::Never).is_err());
        assert!(plan.add("Bad Tool", ToolAccess::Never).is_err());
        assert!(plan.add("", ToolAccess::Never).is_err());
        assert!(plan.add("a..b", ToolAccess::Never).is_err());
        assert!(plan.add(&"x".repeat(129), ToolAccess::Never).is_err());
        plan.remove(1); // local_write and both of its rules
        plan.remove(99);
        plan.set(1, ToolAccess::AsksFirst); // local_exec: no deny any more
        plan.set(99, ToolAccess::Never);
        let patch = plan.patch(&current).unwrap();
        assert_eq!(patch.allowed_tools.unwrap(), ["local_read", "local_exec", "local_list"]);
        let rules = patch.tool_rules.unwrap();
        assert!(rules.allow.is_empty() && rules.deny.is_empty(), "{rules:?}");
        // The rule about a tool it does not list stays.
        assert_eq!(
            patch.auto_approve.unwrap(),
            [
                rule("local_read", None),
                rule("rollback", None),
                rule("local_list", None)
            ]
        );
        assert!(patch.mission.is_none());
    }

    #[test]
    fn never_adds_a_whole_deny_and_keeps_the_patterns() {
        let current = bot();
        let mut plan = ToolPlan::of(&current);
        plan.set(0, ToolAccess::Never);
        let patch = plan.patch(&current).unwrap();
        let rules = patch.tool_rules.unwrap();
        assert_eq!(rules.allow, [rule("local_write", Some(("path", "*.txt")))]);
        assert_eq!(
            rules.deny,
            [
                rule("local_exec", None),
                rule("local_write", Some(("path", "/etc/*"))),
                rule("local_read", None)
            ]
        );
        assert_eq!(patch.auto_approve.unwrap(), [rule("rollback", None)]);
    }

    #[test]
    fn offers_the_names_it_does_not_list_yet() {
        let plan = ToolPlan::of(&bot());
        let known = ["local_read", "local_list", "local_search", "docdb.find", "local_list"];
        assert_eq!(plan.offers(known, ""), ["local_list", "local_search", "docdb.find"]);
        assert_eq!(plan.offers(known, "DOC"), ["docdb.find"]);
        assert!(plan.offers(CORE_TOOLS, "nothing like it").is_empty());
    }
}
