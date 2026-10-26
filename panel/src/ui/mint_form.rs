use crate::domain::EnrollmentToken;

/// The form that mints a host's one-time token, and the token it got,
/// shown once.
#[derive(Debug, Clone, Default, PartialEq)]
pub struct MintForm {
    pub open: bool,
    pub node_id: String,
    pub labels: String,
    pub ttl: String,
    pub busy: bool,
    pub token: Option<EnrollmentToken>,
}

impl MintForm {
    /// A fresh form, the lifetime at one hour.
    pub fn opened() -> Self {
        Self {
            open: true,
            ttl: "1h".into(),
            ..Self::default()
        }
    }
}
