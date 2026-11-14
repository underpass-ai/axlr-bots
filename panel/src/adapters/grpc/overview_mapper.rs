use serde_json::{Value, json};

use super::proto;
use super::wire::Wire;
use crate::application::PanelError;
use crate::domain::{BuildStatus, Overview};

/// The dashboard's counts and the build, into the panel's.
pub(super) struct OverviewMapper;

impl OverviewMapper {
    pub fn overview(answer: &proto::GetOverviewResponse) -> Result<Overview, PanelError> {
        let counts = |map: &std::collections::HashMap<String, i32>| -> Value {
            Value::Object(map.iter().map(|(k, v)| (k.clone(), json!((*v).max(0)))).collect())
        };
        Wire::read(
            json!({
                "since": Wire::time(answer.since.as_ref()),
                "nodes": counts(&answer.nodes),
                "bots": counts(&answer.bots),
                "tasks": counts(&answer.tasks),
                "tasksSince": counts(&answer.tasks_since),
                "executionsSince": answer.executions_since.max(0),
                "costUsdSince": answer.cost_usd_since,
                "costUnknownSince": answer.cost_unknown_since.max(0),
            }),
            "overview",
        )
    }

    pub fn build(answer: &proto::GetBuildResponse) -> Result<BuildStatus, PanelError> {
        let identity = |build: &Option<proto::BuildIdentity>| -> Value {
            let b = build.clone().unwrap_or_default();
            json!({"version": b.version, "revision": b.revision, "time": b.time, "modified": b.modified})
        };
        Wire::read(
            json!({
                "running": identity(&answer.running),
                "installed": identity(&answer.installed),
                "stale": answer.stale,
                "reason": answer.reason,
                "minimumAxlr": answer.minimum_axlr,
            }),
            "build",
        )
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn reads_the_counts_and_the_cost() {
        let overview = OverviewMapper::overview(&proto::GetOverviewResponse {
            since: Some(Wire::timestamp("2026-10-25T00:00:00Z".parse().unwrap())),
            tasks: [("Completed".to_string(), 4)].into_iter().collect(),
            executions_since: 5,
            cost_usd_since: 0.25,
            cost_unknown_since: 1,
            ..Default::default()
        })
        .unwrap();
        assert_eq!(
            (
                overview.tasks["Completed"],
                overview.executions_since,
                overview.cost_unknown_since
            ),
            (4, 5, 1)
        );
        assert!((overview.cost_usd_since - 0.25).abs() < 1e-9);
    }

    #[test]
    fn reads_the_build_and_whether_it_is_stale() {
        let build = OverviewMapper::build(&proto::GetBuildResponse {
            running: Some(proto::BuildIdentity {
                version: "0.16.0".into(),
                ..Default::default()
            }),
            installed: Some(proto::BuildIdentity {
                version: "0.17.0".into(),
                ..Default::default()
            }),
            stale: true,
            reason: "newer binary".into(),
            ..Default::default()
        })
        .unwrap();
        assert!(build.stale);
        assert_eq!(build.installed.version, "0.17.0");
    }
}
