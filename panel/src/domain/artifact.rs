use serde::{Deserialize, Serialize};

/// A file an execution produced, such as the task's report. `stored`
/// says the control plane keeps its bytes, so it can be downloaded; one
/// it does not keep is only listed.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Artifact {
    pub name: String,
    #[serde(default)]
    pub media_type: String,
    #[serde(default)]
    pub reference: String,
    #[serde(default, skip_serializing_if = "is_zero")]
    pub size: u64,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub sha256: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub description: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub source: String,
    #[serde(default, skip_serializing_if = "std::ops::Not::not")]
    pub stored: bool,
}

fn is_zero(n: &u64) -> bool {
    *n == 0
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn reads_what_a_delivery_lists_and_what_an_older_runtime_does() {
        let artifact: Artifact = serde_json::from_str(
            r#"{"name":"report.md","mediaType":"text/markdown","reference":"out/report.md","size":2048,
            "sha256":"ab","description":"The weekly report","source":"axlr_deliver","stored":true,"callId":"c"}"#,
        )
        .unwrap();
        assert_eq!((artifact.size, artifact.stored), (2048, true));
        let again: Artifact = serde_json::from_str(&serde_json::to_string(&artifact).unwrap()).unwrap();
        assert_eq!(again, artifact);
        let older: Artifact = serde_json::from_str(r#"{"name":"report.md"}"#).unwrap();
        assert!(!older.stored && older.size == 0 && older.media_type.is_empty());
    }
}
