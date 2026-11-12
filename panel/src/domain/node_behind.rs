use super::{BuildStatus, NodeCapabilities, Version};

/// Why a node runs older code than the fleet wants: its agent is older
/// than the control plane (`nodeVersion` against `running.version`),
/// its AXLR is below the operator's minimum (`axlrVersion` against
/// `minimumAxlr`), or both. A version the panel cannot read says
/// nothing.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct NodeBehind {
    /// The node agent's version and the control plane's.
    pub agent: Option<(Version, Version)>,
    /// The node's AXLR and the least the operator wants.
    pub axlr: Option<(Version, Version)>,
}

impl NodeBehind {
    /// What the node is behind on, if anything.
    pub fn of(capabilities: &NodeCapabilities, build: &BuildStatus) -> Option<Self> {
        let agent = match (Version::parse(&capabilities.node_version), build.server_version()) {
            (Some(node), Some(server)) if node < server => Some((node, server)),
            _ => None,
        };
        let axlr = match (Version::find_in(&capabilities.axlr_version), build.minimum_axlr()) {
            (Some(axlr), Some(least)) if axlr < least => Some((axlr, least)),
            _ => None,
        };
        (agent.is_some() || axlr.is_some()).then_some(Self { agent, axlr })
    }

    /// What the mark says on hover, one reason a line.
    pub fn tip(&self) -> String {
        let mut lines = Vec::new();
        if let Some((node, server)) = &self.agent {
            lines.push(format!(
                "Node agent {}: update it to {}, the control plane's",
                node.label(),
                server.label()
            ));
        }
        if let Some((axlr, least)) = &self.axlr {
            lines.push(format!(
                "AXLR {}: update it to {} or newer",
                axlr.label(),
                least.label()
            ));
        }
        lines.join("\n")
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::domain::BuildIdentity;

    fn build(server: &str, minimum: &str) -> BuildStatus {
        BuildStatus {
            running: BuildIdentity {
                version: server.into(),
                ..Default::default()
            },
            minimum_axlr: minimum.into(),
            ..Default::default()
        }
    }

    fn node(agent: &str, axlr: &str) -> NodeCapabilities {
        NodeCapabilities {
            node_version: agent.into(),
            axlr_version: axlr.into(),
            ..Default::default()
        }
    }

    #[test]
    fn flags_an_older_agent_or_an_axlr_below_the_minimum() {
        let table = [
            // agent, AXLR line, server, minimum → agent behind, AXLR behind
            ("0.3.0", "axlr-tui 0.14.0", "0.4.0", "0.14.0", Some((true, false))),
            ("v0.3.0", "axlr-tui 0.14.0", "0.4.0", "", Some((true, false))),
            ("0.4.0", "axlr-tui 0.13.2", "0.4.0", "0.14.0", Some((false, true))),
            ("0.3.9", "nanobot-0.1.0", "0.4.0", "0.2.0", Some((true, true))),
            ("0.4.0", "axlr-tui 0.14.0-dev", "0.4.0", "0.14.0", Some((false, true))),
            ("0.4.0", "axlr-tui 0.14.0", "0.4.0", "0.14.0", None),
            ("0.5.0", "axlr-tui 0.20.0", "0.4.0", "0.14.0", None),
            // Nothing to compare: an older node names no version, a dev
            // build has none the panel can read, no minimum was set.
            ("", "", "0.4.0", "0.14.0", None),
            ("0.3.0", "dev", "", "0.14.0", None),
            (
                "0.0.0-20261112223000-4924ad731450",
                "axlr-tui 0.14.0",
                "0.4.0",
                "",
                None,
            ),
            ("0.3.0", "axlr-tui 0.10.0", "(devel)", "", None),
        ];
        for (agent, axlr, server, minimum, want) in table {
            let behind = NodeBehind::of(&node(agent, axlr), &build(server, minimum));
            assert_eq!(
                behind.as_ref().map(|b| (b.agent.is_some(), b.axlr.is_some())),
                want,
                "{agent} {axlr} against {server} {minimum}"
            );
        }
    }

    #[test]
    fn says_which_and_the_target() {
        let both = NodeBehind::of(&node("0.3.0", "axlr-tui 0.13.0"), &build("0.4.0", "0.14.0")).unwrap();
        assert_eq!(
            both.tip(),
            "Node agent v0.3.0: update it to v0.4.0, the control plane's\nAXLR v0.13.0: update it to v0.14.0 or newer"
        );
    }
}
