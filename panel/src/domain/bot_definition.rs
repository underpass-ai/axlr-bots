use serde::{Deserialize, Serialize};

use super::identifier::{validate_identifier, validate_text};
use super::{AutonomyLevel, Budgets, DomainError, ModelPolicy, NodeId, Placement, ToolRules, Trigger, TriggerType};

/// A bot to register, as a definition file writes it. The panel checks
/// what it can before the control plane checks the rest.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct BotDefinition {
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub id: String,
    pub tenant: String,
    pub name: String,
    pub owner: String,
    pub mission: String,
    #[serde(default)]
    pub allowed_tools: Vec<String>,
    #[serde(default)]
    pub level: AutonomyLevel,
    #[serde(default)]
    pub tool_rules: ToolRules,
    #[serde(default)]
    pub triggers: Vec<Trigger>,
    #[serde(default)]
    pub placement: Placement,
    #[serde(default)]
    pub model: ModelPolicy,
    #[serde(default)]
    pub budgets: Budgets,
    /// How a pending call waits for a person; passed through as written.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub approvals: Option<serde_json::Value>,
    /// The bots this bot may delegate to; passed through as written.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub delegation: Option<serde_json::Value>,
    /// The teams it belongs to: the panel's Team.
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub groups: Vec<String>,
    /// What it may tell its teams; passed through as written.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub events: Option<serde_json::Value>,
    /// What runs its tasks on the node (AXLR when absent, else a Jev
    /// program); passed through as written.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub runtime: Option<serde_json::Value>,
}

/// The few fields of the register form.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct BotForm {
    pub name: String,
    pub mission: String,
    pub level: AutonomyLevel,
    pub tools: String,
    pub model: String,
    pub node: String,
    /// The team, or several separated by commas; empty: none.
    pub team: String,
}

/// The tool a vehicle's bot calls to fly or drive home; the panel lets it
/// run without a person, as returning is safe.
pub const RETURN_HOME_TOOL: &str = "return_home";

/// The team names a form's field holds, without repeats.
pub fn team_names(raw: &str) -> Vec<String> {
    let mut names: Vec<String> = Vec::new();
    for name in raw.split([',', ' ']).map(str::trim).filter(|t| !t.is_empty()) {
        if !names.iter().any(|n| n == name) {
            names.push(name.to_string());
        }
    }
    names
}

impl BotDefinition {
    /// Reads a definition file's JSON and checks it.
    pub fn from_json(text: &str) -> Result<Self, DomainError> {
        let definition: Self =
            serde_json::from_str(text).map_err(|e| DomainError::new("definition", format!("is not valid: {e}")))?;
        definition.validate()?;
        Ok(definition)
    }

    /// Builds a definition from the form: tenant `default`, owner
    /// `operator`, a manual trigger, the id taken from the name.
    pub fn from_form(form: &BotForm) -> Result<Self, DomainError> {
        let id = validate_identifier("bot name", &form.name)?;
        let tools: Vec<String> = form
            .tools
            .split([',', ' '])
            .map(str::trim)
            .filter(|t| !t.is_empty())
            .map(str::to_string)
            .collect();
        let node = form.node.trim();
        let placement = if node.is_empty() {
            Placement::default()
        } else {
            Placement {
                nodes: vec![NodeId::new(node)?],
                ..Default::default()
            }
        };
        let model = form.model.trim();
        let approvals = tools
            .iter()
            .any(|t| t == RETURN_HOME_TOOL)
            .then(|| serde_json::json!({"autoApprove": [{"tool": RETURN_HOME_TOOL}]}));
        let definition = Self {
            id: id.clone(),
            tenant: "default".into(),
            name: id,
            owner: "operator".into(),
            mission: form.mission.trim().to_string(),
            allowed_tools: tools,
            level: form.level,
            tool_rules: ToolRules::default(),
            triggers: vec![Trigger::manual()],
            placement,
            model: ModelPolicy {
                model: model.to_string(),
                routing_policy: if model.is_empty() {
                    String::new()
                } else {
                    "cost-optimized".into()
                },
                allow_remote_providers: !model.is_empty(),
                max_context_tokens: 0,
            },
            budgets: Budgets::default(),
            approvals,
            delegation: None,
            groups: team_names(&form.team),
            events: None,
        };
        definition.validate()?;
        Ok(definition)
    }

    /// The checks the panel can make on its own.
    pub fn validate(&self) -> Result<(), DomainError> {
        if !self.id.is_empty() {
            validate_identifier("bot id", &self.id)?;
        }
        validate_identifier("tenant", &self.tenant)?;
        validate_text("bot name", &self.name, 120)?;
        validate_text("owner", &self.owner, 120)?;
        validate_text("mission", &self.mission, 2000)?;
        for tool in &self.allowed_tools {
            if tool.trim().is_empty() || tool.contains(char::is_whitespace) {
                return Err(DomainError::new("allowed tools", "must be names without spaces"));
            }
        }
        if self.level == AutonomyLevel::Unknown {
            return Err(DomainError::new(
                "autonomy level",
                "must be observe, propose, act-with-approval or act-within-policy",
            ));
        }
        if self.triggers.iter().any(|t| t.trigger_type == TriggerType::Unknown) {
            return Err(DomainError::new(
                "trigger type",
                "must be manual, webhook, timer, delegation or event",
            ));
        }
        if self.groups.len() > 8 {
            return Err(DomainError::new("team", "a bot is in at most 8 teams"));
        }
        for team in &self.groups {
            validate_identifier("team", team)?;
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn keeps_the_approvals_and_the_delegation_of_a_definition() {
        let text = r#"{"tenant": "default", "name": "dispatcher", "owner": "ops", "mission": "Send the vehicles.",
          "triggers": [{"type": "manual"}, {"type": "delegation"}],
          "approvals": {"ttl": "20m", "onExpire": "deny"},
          "delegation": {"to": ["pilot-quad-0"], "maxDepth": 1}}"#;
        let definition = BotDefinition::from_json(text).unwrap();
        assert_eq!(
            definition.triggers[1].trigger_type,
            super::super::TriggerType::Delegation
        );
        assert_eq!(definition.delegation.as_ref().unwrap()["to"][0], "pilot-quad-0");
        let out = serde_json::to_value(&definition).unwrap();
        assert_eq!(out["approvals"]["ttl"], "20m");
    }

    const EXAMPLE: &str = r#"{
      "id": "bot-act-with-approval", "tenant": "default", "name": "bot-act-with-approval", "owner": "operator",
      "mission": "Do what each task asks.", "level": "act-with-approval",
      "allowedTools": ["local_read", "local_write"], "triggers": [{"type": "manual"}],
      "model": {"model": "anthropic/claude-haiku-5.5", "routingPolicy": "cost-optimized", "allowRemoteProviders": true, "maxContextTokens": 32000},
      "budgets": {"maxModelCallsPerTask": 10, "maxToolCallsPerTask": 20, "maxDelegationDepth": 0, "taskTimeoutSeconds": 300}
    }"#;

    #[test]
    fn reads_a_definition_file() {
        let definition = BotDefinition::from_json(EXAMPLE).unwrap();
        assert_eq!(definition.level, AutonomyLevel::ActWithApproval);
        assert_eq!(definition.allowed_tools.len(), 2);
        assert_eq!(definition.budgets.task_timeout_seconds, 300);
        let back: serde_json::Value = serde_json::to_value(&definition).unwrap();
        assert_eq!(back["level"], "act-with-approval");
        assert_eq!(back["model"]["maxContextTokens"], 32000);
    }

    #[test]
    fn refuses_bad_files() {
        assert!(BotDefinition::from_json("{").is_err());
        assert!(BotDefinition::from_json(&EXAMPLE.replace("act-with-approval", "act-boldly")).is_err());
        assert!(BotDefinition::from_json(&EXAMPLE.replace(r#"{"type": "manual"}"#, r#"{"type": "cron"}"#)).is_err());
        assert!(BotDefinition::from_json(&EXAMPLE.replace("\"owner\"", "\"ownerx\"")).is_err());
        assert!(
            BotDefinition::from_json(&EXAMPLE.replace("\"tenant\": \"default\"", "\"tenant\": \"Bad Tenant\""))
                .is_err()
        );
        assert!(BotDefinition::from_json(&EXAMPLE.replace("Do what each task asks.", " ")).is_err());
        assert!(BotDefinition::from_json(&EXAMPLE.replace("\"local_read\"", "\"local read\"")).is_err());
        assert!(
            BotDefinition::from_json(&EXAMPLE.replace("\"id\": \"bot-act-with-approval\"", "\"id\": \"Bad\"")).is_err()
        );
        let mut long = BotDefinition::from_json(EXAMPLE).unwrap();
        long.owner = "o".repeat(121);
        assert!(long.validate().is_err());
    }

    #[test]
    fn a_form_with_a_team_and_return_home() {
        let form = BotForm {
            name: "pilot-quad-1".into(),
            mission: "Fly quad-1.".into(),
            level: AutonomyLevel::ActWithApproval,
            tools: "local_read, run_mission, return_home".into(),
            team: " port, sea port ".into(),
            ..BotForm::default()
        };
        let definition = BotDefinition::from_form(&form).unwrap();
        assert_eq!(definition.groups, vec!["port", "sea"]);
        assert_eq!(
            definition.approvals.as_ref().unwrap()["autoApprove"][0]["tool"],
            "return_home"
        );
        let out = serde_json::to_value(&definition).unwrap();
        assert_eq!(out["groups"][1], "sea");
        let plain = BotDefinition::from_form(&BotForm {
            name: "helper".into(),
            mission: "Help.".into(),
            ..BotForm::default()
        })
        .unwrap();
        assert!(plain.groups.is_empty() && plain.approvals.is_none());
        assert!(!serde_json::to_string(&plain).unwrap().contains("groups"));
        let bad = BotForm {
            team: "Port".into(),
            ..form
        };
        assert!(BotDefinition::from_form(&bad).is_err());
    }

    #[test]
    fn reads_a_definition_with_teams_and_events() {
        let text = r#"{"tenant": "default", "name": "plane", "owner": "ops", "mission": "Fly.",
          "groups": ["port"], "events": {"perMinute": 3},
          "triggers": [{"type": "manual"}, {"type": "event", "events": ["ship.*"], "interrupt": true}]}"#;
        let definition = BotDefinition::from_json(text).unwrap();
        assert_eq!(definition.groups, vec!["port"]);
        assert_eq!(definition.events.as_ref().unwrap()["perMinute"], 3);
        assert!(definition.triggers[1].interrupt);
        let many = r#"{"tenant": "default", "name": "x", "owner": "o", "mission": "m", "groups": ["a","b","c","d","e","f","g","h","i"]}"#;
        assert!(BotDefinition::from_json(many).is_err());
    }

    #[test]
    fn builds_a_definition_from_the_form() {
        let form = BotForm {
            name: "helper".into(),
            mission: " Summarise the logs ".into(),
            level: AutonomyLevel::Propose,
            tools: "local_read, local_list local_search".into(),
            model: "anthropic/claude-haiku-5.5".into(),
            node: "wrx80".into(),
            team: String::new(),
        };
        let definition = BotDefinition::from_form(&form).unwrap();
        assert_eq!(definition.id, "helper");
        assert_eq!(definition.mission, "Summarise the logs");
        assert_eq!(
            definition.allowed_tools,
            vec!["local_read", "local_list", "local_search"]
        );
        assert_eq!(definition.placement.describe(), "wrx80");
        assert_eq!(definition.triggers, vec![Trigger::manual()]);
        assert!(definition.model.allow_remote_providers);
        let bare = BotDefinition::from_form(&BotForm {
            name: "b".into(),
            mission: "m".into(),
            ..Default::default()
        })
        .unwrap();
        assert!(bare.model.model.is_empty() && !bare.model.allow_remote_providers);
        assert_eq!(bare.placement.describe(), "any node");
        assert!(
            BotDefinition::from_form(&BotForm {
                name: "Bad Name".into(),
                mission: "m".into(),
                ..Default::default()
            })
            .is_err()
        );
        assert!(
            BotDefinition::from_form(&BotForm {
                name: "b".into(),
                mission: "".into(),
                ..Default::default()
            })
            .is_err()
        );
        assert!(
            BotDefinition::from_form(&BotForm {
                name: "b".into(),
                mission: "m".into(),
                node: "Bad Node".into(),
                ..Default::default()
            })
            .is_err()
        );
    }
}
