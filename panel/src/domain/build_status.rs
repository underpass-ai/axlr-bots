use serde::Deserialize;

use super::BuildIdentity;

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
    }
}
