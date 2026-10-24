use serde::Deserialize;

use crate::application::PanelError;

/// The control plane's error body: `{"kind": "...", "error": "..."}`.
#[derive(Debug, Deserialize)]
pub(crate) struct ApiError {
    #[serde(default)]
    kind: String,
    #[serde(default)]
    error: String,
}

impl ApiError {
    /// The panel's error for an answer with the status and the body.
    pub(crate) fn classify(status: u16, body: &[u8]) -> PanelError {
        let parsed: Option<ApiError> = serde_json::from_slice(body).ok();
        let (kind, message) = match parsed {
            Some(e) => (e.kind, e.error),
            None => (String::new(), String::from_utf8_lossy(body).trim().to_string()),
        };
        let message = if message.is_empty() {
            format!("HTTP {status}")
        } else {
            message
        };
        match kind.as_str() {
            "validation" | "too_large" => PanelError::Invalid(message),
            "not_found" => PanelError::NotFound(message),
            "state" => PanelError::State(message),
            "conflict" => PanelError::Conflict(message),
            "forbidden" => PanelError::Forbidden(message),
            "unauthorized" => PanelError::Unauthorized(message),
            "internal" => PanelError::Server(message),
            _ => match status {
                400 | 413 => PanelError::Invalid(message),
                401 => PanelError::Unauthorized(message),
                403 => PanelError::Forbidden(message),
                404 => PanelError::NotFound(message),
                409 => PanelError::Conflict(message),
                _ => PanelError::Server(message),
            },
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn reads_the_kind_first_then_the_status() {
        let body = |kind: &str| format!(r#"{{"kind":"{kind}","error":"nope"}}"#).into_bytes();
        assert_eq!(
            ApiError::classify(400, &body("validation")),
            PanelError::Invalid("nope".into())
        );
        assert_eq!(
            ApiError::classify(413, &body("too_large")),
            PanelError::Invalid("nope".into())
        );
        assert_eq!(
            ApiError::classify(404, &body("not_found")),
            PanelError::NotFound("nope".into())
        );
        assert_eq!(
            ApiError::classify(409, &body("state")),
            PanelError::State("nope".into())
        );
        assert_eq!(
            ApiError::classify(409, &body("conflict")),
            PanelError::Conflict("nope".into())
        );
        assert_eq!(
            ApiError::classify(403, &body("forbidden")),
            PanelError::Forbidden("nope".into())
        );
        assert_eq!(
            ApiError::classify(401, &body("unauthorized")),
            PanelError::Unauthorized("nope".into())
        );
        assert_eq!(
            ApiError::classify(500, &body("internal")),
            PanelError::Server("nope".into())
        );
        assert_eq!(ApiError::classify(401, b"no"), PanelError::Unauthorized("no".into()));
        assert_eq!(ApiError::classify(400, b""), PanelError::Invalid("HTTP 400".into()));
        assert_eq!(ApiError::classify(403, b""), PanelError::Forbidden("HTTP 403".into()));
        assert_eq!(ApiError::classify(404, b""), PanelError::NotFound("HTTP 404".into()));
        assert_eq!(ApiError::classify(409, b""), PanelError::Conflict("HTTP 409".into()));
        assert_eq!(
            ApiError::classify(502, b"bad gateway"),
            PanelError::Server("bad gateway".into())
        );
    }
}
