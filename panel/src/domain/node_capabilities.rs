use serde::{Deserialize, Serialize};

/// What a node's AXLR can do: its version and whether it takes a policy
/// file and the correlation ids; the runtimes it has beside AXLR; and the
/// model providers its bots can reach, by name only.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct NodeCapabilities {
    #[serde(default, rename = "axlrVersion")]
    pub axlr_version: String,
    /// The node agent's own build, as Go recorded it: its version and
    /// commit. A node older than the fields names none.
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub node_version: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub node_revision: String,
    #[serde(default)]
    pub policy_supported: bool,
    #[serde(default)]
    pub correlation_supported: bool,
    /// A node that runs only the bots placed on it by name or label, such
    /// as an agent beside a service in a pod.
    #[serde(default, skip_serializing_if = "std::ops::Not::not")]
    pub dedicated: bool,
    /// The runtimes beside AXLR (`jev`).
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub runtimes: Vec<String>,
    /// The model providers the node's bots can reach: `openrouter`,
    /// `codex`, `jev`, `local:<id>` per local model its AXLR names. A node
    /// older than the field names none, which says nothing.
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub providers: Vec<String>,
}

/// Whether a node can serve a bot's model.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ModelFit {
    /// The node did not say which providers it has.
    Unknown,
    Served,
    /// The node named its providers and none serves the model.
    Missing,
}

impl NodeCapabilities {
    /// The provider a model's name asks for: a local model of the node's
    /// AXLR by its id (`local:<id>`), the Codex proxy for `codex/…`, and
    /// OpenRouter for every other `vendor/model`.
    pub fn provider_for(model: &str) -> String {
        if model.starts_with("codex/") {
            "codex".to_string()
        } else {
            "openrouter".to_string()
        }
    }

    /// Whether this node serves the model: a `local:<model>` it named, or
    /// the provider the model's name asks for.
    pub fn fit(&self, model: &str) -> ModelFit {
        let model = model.trim();
        if self.providers.is_empty() || model.is_empty() {
            return ModelFit::Unknown;
        }
        let local = format!("local:{model}");
        let provider = Self::provider_for(model);
        if self.providers.iter().any(|p| *p == local || *p == provider) {
            ModelFit::Served
        } else {
            ModelFit::Missing
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn with(providers: &[&str]) -> NodeCapabilities {
        NodeCapabilities {
            providers: providers.iter().map(|p| p.to_string()).collect(),
            ..Default::default()
        }
    }

    #[test]
    fn reads_the_providers_and_leaves_them_out_when_none() {
        let caps: NodeCapabilities = serde_json::from_str(
            r#"{"policySupported":true,"correlationSupported":true,"runtimes":["jev"],"providers":["openrouter","codex","jev","local:codex/gpt-6.1-sol"]}"#,
        )
        .unwrap();
        assert_eq!(caps.runtimes, vec!["jev"]);
        assert_eq!(caps.providers.len(), 4);
        let old: NodeCapabilities =
            serde_json::from_str(r#"{"policySupported":true,"correlationSupported":true}"#).unwrap();
        assert!(old.providers.is_empty() && old.runtimes.is_empty());
        let json = serde_json::to_string(&old).unwrap();
        assert!(!json.contains("providers") && !json.contains("runtimes"), "{json}");
        assert!(
            !json.contains("nodeVersion") && !json.contains("nodeRevision"),
            "{json}"
        );
        let built: NodeCapabilities = serde_json::from_str(
            r#"{"axlrVersion":"axlr-tui 0.14.0","nodeVersion":"0.4.0","nodeRevision":"0d1e2f3a4b5c","policySupported":true,"correlationSupported":true}"#,
        )
        .unwrap();
        assert_eq!(
            (built.node_version.as_str(), built.node_revision.as_str()),
            ("0.4.0", "0d1e2f3a4b5c")
        );
    }

    #[test]
    fn tells_whether_a_node_serves_a_model() {
        let old = NodeCapabilities::default();
        assert_eq!(old.fit("anthropic/claude-haiku-5.5"), ModelFit::Unknown);
        let node = with(&["codex", "local:codex/gpt-6.1-sol"]);
        assert_eq!(node.fit(""), ModelFit::Unknown);
        assert_eq!(node.fit("  "), ModelFit::Unknown);
        assert_eq!(node.fit("codex/gpt-6.1-sol"), ModelFit::Served);
        assert_eq!(node.fit(" codex/gpt-6.1-sol "), ModelFit::Served);
        assert_eq!(
            node.fit("codex/gpt-6-luna"),
            ModelFit::Served,
            "the proxy serves any codex model"
        );
        assert_eq!(node.fit("anthropic/claude-haiku-5.5"), ModelFit::Missing);
        let openrouter = with(&["openrouter"]);
        assert_eq!(openrouter.fit("anthropic/claude-haiku-5.5"), ModelFit::Served);
        assert_eq!(openrouter.fit("codex/gpt-6.1-sol"), ModelFit::Missing);
        let local = with(&["local:ollama/qwen"]);
        assert_eq!(local.fit("ollama/qwen"), ModelFit::Served);
        assert_eq!(local.fit("ollama/llama"), ModelFit::Missing);
        assert_eq!(NodeCapabilities::provider_for("codex/x"), "codex");
        assert_eq!(NodeCapabilities::provider_for("openai/gpt-6"), "openrouter");
    }
}
