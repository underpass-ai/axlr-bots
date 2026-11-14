use serde_json::{Value, json};

use super::proto;
use super::wire::Wire;
use crate::application::PanelError;
use crate::domain::{EnrollmentRequest, EnrollmentToken, Node, RevokedNode};

/// Nodes and enrollments, into the panel's and back.
pub(super) struct NodeMapper;

impl NodeMapper {
    pub fn node(message: &proto::Node) -> Result<Node, PanelError> {
        Wire::read(Self::shape(message), "node")
    }

    pub fn nodes(messages: &[proto::Node]) -> Result<Vec<Node>, PanelError> {
        messages.iter().map(Self::node).collect()
    }

    pub fn revoked(answer: &proto::RevokeNodeResponse) -> Result<RevokedNode, PanelError> {
        let node = answer
            .node
            .as_ref()
            .ok_or_else(|| PanelError::Unreadable("the answer has no node".into()))?;
        Wire::read(
            json!({"node": Self::shape(node), "failedTasks": answer.failed_tasks}),
            "revoked node",
        )
    }

    pub fn token(answer: &proto::CreateEnrollmentResponse) -> Result<EnrollmentToken, PanelError> {
        Wire::read(
            json!({
                "token": answer.token,
                "id": answer.id,
                "nodeId": answer.node_id,
                "labels": Wire::strings(&answer.labels),
                "expiresAt": Wire::time(answer.expires_at.as_ref()),
            }),
            "enrollment token",
        )
    }

    pub fn enrollment(request: &EnrollmentRequest) -> Result<proto::CreateEnrollmentRequest, PanelError> {
        Ok(proto::CreateEnrollmentRequest {
            node_id: request.node_id.as_ref().map(ToString::to_string).unwrap_or_default(),
            labels: request.labels.clone().into_iter().collect(),
            ttl: request.ttl.as_deref().map(Wire::parse_duration).transpose()?,
        })
    }

    fn shape(node: &proto::Node) -> Value {
        let capabilities = node.capabilities.clone().unwrap_or_default();
        json!({
            "id": node.id,
            "hostName": node.host_name,
            "labels": Wire::strings(&node.labels),
            "capabilities": {
                "axlrVersion": capabilities.axlr_version,
                "nodeVersion": capabilities.node_version,
                "nodeRevision": capabilities.node_revision,
                "policySupported": capabilities.policy_supported,
                "correlationSupported": capabilities.correlation_supported,
                "dedicated": capabilities.dedicated,
                "runtimes": capabilities.runtimes,
                "providers": capabilities.providers,
            },
            "status": node.status,
            "registeredAt": Wire::time(node.registered_at.as_ref()),
            "lastSeenAt": Wire::time(node.last_seen_at.as_ref()),
            "certificateSerial": Wire::text(&node.certificate_serial),
            "certificateNotAfter": Wire::time(node.certificate_not_after.as_ref()),
            "revokedAt": Wire::time(node.revoked_at.as_ref()),
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::domain::NodeStatus;

    fn message() -> proto::Node {
        proto::Node {
            id: "wrx80".into(),
            host_name: "wrx80.lan".into(),
            labels: [("zone".to_string(), "lab".to_string())].into_iter().collect(),
            capabilities: Some(proto::NodeCapabilities {
                axlr_version: "0.16.0".into(),
                policy_supported: true,
                runtimes: vec!["axlr".into()],
                ..Default::default()
            }),
            status: "Online".into(),
            registered_at: Some(Wire::timestamp("2026-10-24T10:00:00Z".parse().unwrap())),
            last_seen_at: Some(Wire::timestamp("2026-10-25T09:59:00Z".parse().unwrap())),
            certificate_serial: "ab12".into(),
            ..Default::default()
        }
    }

    #[test]
    fn reads_a_node() {
        let node = NodeMapper::node(&message()).unwrap();
        assert_eq!(node.status, NodeStatus::Online);
        assert_eq!(node.capabilities.axlr_version, "0.16.0");
        assert_eq!(node.labels["zone"], "lab");
        assert_eq!(node.certificate_serial.as_deref(), Some("ab12"));
        assert!(node.revoked_at.is_none() && node.certificate_not_after.is_none());
    }

    #[test]
    fn reads_a_revocation_and_a_token() {
        let revoked = NodeMapper::revoked(&proto::RevokeNodeResponse {
            node: Some(message()),
            failed_tasks: vec!["task-1".into()],
        })
        .unwrap();
        assert_eq!(revoked.failed_tasks[0].as_str(), "task-1");
        assert!(NodeMapper::revoked(&proto::RevokeNodeResponse::default()).is_err());
        let token = NodeMapper::token(&proto::CreateEnrollmentResponse {
            token: "s3cret".into(),
            id: "enr-1".into(),
            node_id: "wrx80".into(),
            expires_at: Some(Wire::timestamp("2026-10-25T11:00:00Z".parse().unwrap())),
            ..Default::default()
        })
        .unwrap();
        assert_eq!(token.token.expose(), "s3cret");
    }

    #[test]
    fn asks_for_an_enrollment_with_its_lifetime() {
        let request = EnrollmentRequest::parse("lab-1", "zone=lab", "1h30m").unwrap();
        let message = NodeMapper::enrollment(&request).unwrap();
        assert_eq!(
            (message.node_id.as_str(), message.labels["zone"].as_str()),
            ("lab-1", "lab")
        );
        assert_eq!(message.ttl.unwrap().seconds, 5400);
        assert!(
            NodeMapper::enrollment(&EnrollmentRequest::default())
                .unwrap()
                .ttl
                .is_none()
        );
    }
}
