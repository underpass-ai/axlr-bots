use serde::Deserialize;

use super::{BuildIdentity, Version};

/// GET /v1/build: the build the control plane runs, the one installed on
/// its disk, and whether the running one is stale (a newer binary was
/// installed and the service was not restarted).
#[derive(Debug, Clone, Default, PartialEq, Eq, Deserialize)]
pub struct BuildStatus {
    #[serde(default)]
    pub running: BuildIdentity,
    #[serde(default)]
    pub installed: BuildIdentity,
    #[serde(default)]
    pub stale: bool,
    #[serde(default)]
    pub reason: String,
    /// The least AXLR the operator wants on the nodes; empty: no minimum.
    #[serde(default, rename = "minimumAxlr")]
    pub minimum_axlr: String,
}

impl BuildStatus {
    /// The version the control plane runs: the reference for the nodes'
    /// own builds. None for a build the panel cannot place.
    pub fn server_version(&self) -> Option<Version> {
        Version::parse(&self.running.version)
    }

    /// The least AXLR the nodes should run, when there is one.
    pub fn minimum_axlr(&self) -> Option<Version> {
        Version::parse(&self.minimum_axlr)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn reads_the_control_plane_answer() {
        let status: BuildStatus = serde_json::from_str(
            r#"{"running":{"revision":"aaa","modified":false},"installed":{"revision":"bbb","time":"t","modified":true},"stale":true}"#,
        )
        .unwrap();
        assert!(status.stale);
        assert_eq!(status.running.revision, "aaa");
        assert_eq!(status.installed.describe(), "bbb (t) with changes");
        assert_eq!(status.reason, "");
        assert_eq!((status.server_version(), status.minimum_axlr()), (None, None));
        let versioned: BuildStatus = serde_json::from_str(
            r#"{"running":{"version":"0.4.0"},"installed":{},"stale":false,"minimumAxlr":"0.14.0"}"#,
        )
        .unwrap();
        assert_eq!(versioned.server_version().unwrap().label(), "v0.4.0");
        assert_eq!(versioned.minimum_axlr().unwrap().label(), "v0.14.0");
    }
}
