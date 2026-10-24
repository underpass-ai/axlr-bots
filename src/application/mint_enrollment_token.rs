use std::sync::Arc;

use super::{ControlPlane, PanelError};
use crate::domain::{EnrollmentRequest, EnrollmentToken};

/// Mints the one-time token a new host enrolls with, from the form's
/// node id, labels and lifetime.
pub struct MintEnrollmentToken {
    control_plane: Arc<dyn ControlPlane>,
}

impl MintEnrollmentToken {
    pub fn new(control_plane: Arc<dyn ControlPlane>) -> Self {
        Self { control_plane }
    }

    pub async fn execute(&self, node_id: &str, labels: &str, ttl: &str) -> Result<EnrollmentToken, PanelError> {
        let request = EnrollmentRequest::parse(node_id, labels, ttl)?;
        self.control_plane.mint_enrollment_token(request).await
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::application::test_support::{fleet, run};

    #[test]
    fn mints_a_token_from_the_form() {
        let (control_plane, _) = fleet();
        let uc = MintEnrollmentToken::new(control_plane.clone());
        let token = run(uc.execute("web-2", "zone=eu", "1h")).unwrap();
        assert_eq!(token.node_id.as_str(), "web-2");
        assert_eq!(token.labels.get("zone").map(String::as_str), Some("eu"));
        assert!(token.token.expose().starts_with("axlr1."));
        let generated = run(uc.execute("", "", "")).unwrap();
        assert!(generated.node_id.as_str().starts_with("node-"));
        let refused = run(uc.execute("", "zone", "")).unwrap_err();
        assert!(matches!(refused, PanelError::Invalid(_)));
        assert_eq!(
            control_plane
                .calls()
                .iter()
                .filter(|c| *c == "mint_enrollment_token")
                .count(),
            2
        );
    }
}
