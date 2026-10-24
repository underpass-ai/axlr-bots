use std::collections::BTreeMap;

use chrono::{DateTime, Duration, Utc};
use serde::{Deserialize, Serialize};

use super::{NodeCapabilities, NodeId, NodeStatus};

/// A host the node agent runs on, as the control plane reports it.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Node {
    pub id: NodeId,
    pub host_name: String,
    #[serde(default)]
    pub labels: BTreeMap<String, String>,
    #[serde(default)]
    pub capabilities: NodeCapabilities,
    pub status: NodeStatus,
    pub registered_at: DateTime<Utc>,
    pub last_seen_at: DateTime<Utc>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub certificate_serial: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub certificate_not_after: Option<DateTime<Utc>>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub revoked_at: Option<DateTime<Utc>>,
}

/// How soon a certificate runs out, in the words of a pill.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum CertificateHealth {
    /// The control plane has not seen the certificate's expiry yet.
    Unknown,
    /// More than 30 days left.
    Good(i64),
    /// 30 days or fewer left.
    Soon(i64),
    Expired,
}

impl Node {
    /// The days left on the node's certificate, and whether that is soon.
    pub fn certificate_health(&self, now: DateTime<Utc>) -> CertificateHealth {
        let Some(not_after) = self.certificate_not_after else {
            return CertificateHealth::Unknown;
        };
        if not_after <= now {
            return CertificateHealth::Expired;
        }
        let days = (not_after - now).num_days();
        if days <= 30 {
            CertificateHealth::Soon(days)
        } else {
            CertificateHealth::Good(days)
        }
    }

    /// How long ago the node was last heard.
    pub fn seen_ago(&self, now: DateTime<Utc>) -> Duration {
        (now - self.last_seen_at).max(Duration::zero())
    }

    /// The labels as `key=value`, in key order.
    pub fn label_pairs(&self) -> Vec<String> {
        self.labels.iter().map(|(k, v)| format!("{k}={v}")).collect()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn sample() -> Node {
        serde_json::from_str(
            r#"{"id":"wrx80","hostName":"wrx80-node1","labels":{"host":"wrx80-node1","gpu":"rtx3090"},
            "capabilities":{"axlrVersion":"0.14.0","policySupported":true,"correlationSupported":true},
            "status":"Online","registeredAt":"2026-10-24T22:32:07Z","lastSeenAt":"2026-10-24T22:41:08Z",
            "certificateSerial":"cef8","certificateNotAfter":"2027-10-24T22:32:07Z"}"#,
        )
        .unwrap()
    }

    #[test]
    fn reads_the_api_json() {
        let node = sample();
        assert_eq!(node.id.as_str(), "wrx80");
        assert_eq!(node.status, NodeStatus::Online);
        assert_eq!(node.capabilities.axlr_version, "0.14.0");
        assert_eq!(node.label_pairs(), vec!["gpu=rtx3090", "host=wrx80-node1"]);
        assert_eq!(node.certificate_serial.as_deref(), Some("cef8"));
        let minimal: Node = serde_json::from_str(
            r#"{"id":"n","hostName":"h","capabilities":{"policySupported":false,"correlationSupported":false},"status":"Registered","registeredAt":"2026-10-24T00:00:00Z","lastSeenAt":"2026-10-24T00:00:00Z"}"#,
        )
        .unwrap();
        assert!(minimal.labels.is_empty() && minimal.certificate_not_after.is_none());
        assert!(serde_json::from_str::<Node>(r#"{"id":"Bad Id"}"#).is_err());
    }

    #[test]
    fn tells_the_certificate_health_and_the_last_heartbeat() {
        let node = sample();
        let now: DateTime<Utc> = "2026-10-25T00:00:00Z".parse().unwrap();
        assert_eq!(node.certificate_health(now), CertificateHealth::Good(364));
        let late: DateTime<Utc> = "2027-10-10T00:00:00Z".parse().unwrap();
        assert_eq!(node.certificate_health(late), CertificateHealth::Soon(14));
        let after: DateTime<Utc> = "2027-11-01T00:00:00Z".parse().unwrap();
        assert_eq!(node.certificate_health(after), CertificateHealth::Expired);
        let mut unknown = node.clone();
        unknown.certificate_not_after = None;
        assert_eq!(unknown.certificate_health(now), CertificateHealth::Unknown);
        assert_eq!(node.seen_ago(now).num_minutes(), 78);
        let before: DateTime<Utc> = "2026-10-24T00:00:00Z".parse().unwrap();
        assert_eq!(node.seen_ago(before), Duration::zero());
    }
}
