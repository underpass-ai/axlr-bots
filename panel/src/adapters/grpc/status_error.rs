use prost::Message;
use tonic::{Code, Status};

use super::proto::ErrorDetail;
use crate::application::PanelError;

/// `google.rpc.Status`, the message `grpc-status-details-bin` carries: the
/// operator API puts its `ErrorDetail` among the details.
#[derive(Clone, PartialEq, Message)]
struct RpcStatus {
    #[prost(int32, tag = "1")]
    code: i32,
    #[prost(string, tag = "2")]
    message: String,
    #[prost(message, repeated, tag = "3")]
    details: Vec<prost_types::Any>,
}

/// The TLS alerts a server sends when it does not take the client's
/// certificate, in the words rustls gives them.
const CERTIFICATE_ALERTS: [&str; 6] = [
    "UnknownCA",
    "BadCertificate",
    "CertificateRequired",
    "CertificateRevoked",
    "CertificateExpired",
    "UnsupportedCertificate",
];

const DETAIL_TYPE: &str = "axlrbots.operator.v1.ErrorDetail";

/// A failed call, as the panel says it: the `kind` of the `ErrorDetail`
/// first (the names the HTTP API had), the gRPC code when there is none.
pub struct StatusError;

impl StatusError {
    pub fn classify(status: &Status) -> PanelError {
        let detail = Self::detail(status);
        let message = match &detail {
            Some(d) if !d.message.is_empty() => d.message.clone(),
            _ => Self::words(status),
        };
        if let Some(detail) = &detail {
            match detail.kind.as_str() {
                "validation" | "too_large" => return PanelError::Invalid(message),
                "not_found" => return PanelError::NotFound(message),
                "state" | "approval_expired" => return PanelError::State(message),
                "conflict" => return PanelError::Conflict(message),
                "forbidden" => return PanelError::Forbidden(message),
                "unauthorized" => return PanelError::Unauthorized(message),
                "internal" => return PanelError::Server(message),
                _ => {}
            }
        }
        // An HTTP-only server answers a gRPC call with a plain HTTP body
        // (the REST API's 404 or JSON error): tonic cannot frame it and says
        // so as INTERNAL. That is a control plane without the gRPC API, as
        // UNIMPLEMENTED says it of one without the service.
        if status.code() == Code::Internal
            && (status.message().contains("invalid compression flag")
                || status.message().contains("while receiving response with status"))
        {
            return PanelError::NotFound(format!("the address does not speak gRPC ({message})"));
        }
        // The control plane refused the handshake because of the panel's
        // certificate: a received alert, as rustls names it.
        if matches!(status.code(), Code::Unavailable | Code::Unknown | Code::Internal)
            && CERTIFICATE_ALERTS.iter().any(|alert| message.contains(alert))
        {
            return PanelError::Forbidden(format!(
                "the control plane refused the operator certificate ({message}): issue one with `axlr-bots ca operator issue <name>` on the control plane and set controlPlane.certFile and controlPlane.keyFile, or put it where the command leaves it"
            ));
        }
        match status.code() {
            Code::InvalidArgument | Code::OutOfRange | Code::ResourceExhausted => PanelError::Invalid(message),
            // A control plane without the service answers UNIMPLEMENTED, as
            // one older than a route answered 404.
            Code::NotFound | Code::Unimplemented => PanelError::NotFound(message),
            Code::FailedPrecondition => PanelError::State(message),
            Code::Aborted | Code::AlreadyExists => PanelError::Conflict(message),
            Code::PermissionDenied => PanelError::Forbidden(message),
            Code::Unauthenticated => PanelError::Unauthorized(message),
            Code::Unavailable | Code::DeadlineExceeded | Code::Cancelled => PanelError::Unreachable(message),
            _ => PanelError::Server(message),
        }
    }

    /// The status's message with what lies under it: a refused
    /// connection or a TLS failure is in the sources, not the message.
    fn words(status: &Status) -> String {
        let mut text = if status.message().is_empty() {
            format!("gRPC {:?}", status.code())
        } else {
            status.message().to_string()
        };
        let mut source = std::error::Error::source(status);
        while let Some(cause) = source {
            let said = cause.to_string();
            if !text.contains(&said) {
                text = format!("{text}: {said}");
            }
            source = cause.source();
        }
        if status.code() == Code::DeadlineExceeded {
            text = format!("no answer in time: {text}");
        }
        text
    }

    fn detail(status: &Status) -> Option<ErrorDetail> {
        let bytes = status.details();
        if bytes.is_empty() {
            return None;
        }
        if let Ok(rpc) = RpcStatus::decode(bytes)
            && let Some(any) = rpc.details.iter().find(|d| d.type_url.ends_with(DETAIL_TYPE))
        {
            return ErrorDetail::decode(any.value.as_slice()).ok();
        }
        // A server that puts the ErrorDetail itself in the details.
        ErrorDetail::decode(bytes).ok().filter(|d| !d.kind.is_empty())
    }

    /// The status a server answers with: the code and an `ErrorDetail`
    /// among the details. What a fake control plane uses in tests.
    pub fn refuse(code: Code, kind: &str, message: &str) -> Status {
        let detail = ErrorDetail {
            kind: kind.into(),
            message: message.into(),
            ..Default::default()
        };
        let rpc = RpcStatus {
            code: code as i32,
            message: message.into(),
            details: vec![prost_types::Any {
                type_url: format!("type.googleapis.com/{DETAIL_TYPE}"),
                value: detail.encode_to_vec(),
            }],
        };
        Status::with_details(code, message, rpc.encode_to_vec().into())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn reads_the_kind_first_then_the_code() {
        let cases = [
            (Code::InvalidArgument, "validation", PanelError::Invalid("nope".into())),
            (Code::ResourceExhausted, "too_large", PanelError::Invalid("nope".into())),
            (Code::NotFound, "not_found", PanelError::NotFound("nope".into())),
            (Code::FailedPrecondition, "state", PanelError::State("nope".into())),
            (
                Code::FailedPrecondition,
                "approval_expired",
                PanelError::State("nope".into()),
            ),
            (Code::Aborted, "conflict", PanelError::Conflict("nope".into())),
            (
                Code::PermissionDenied,
                "forbidden",
                PanelError::Forbidden("nope".into()),
            ),
            (
                Code::Unauthenticated,
                "unauthorized",
                PanelError::Unauthorized("nope".into()),
            ),
            (Code::Internal, "internal", PanelError::Server("nope".into())),
            // The kind wins over the code.
            (Code::Unknown, "not_found", PanelError::NotFound("nope".into())),
            // A kind of a newer control plane falls back to the code.
            (Code::Aborted, "from_the_future", PanelError::Conflict("nope".into())),
        ];
        for (code, kind, expected) in cases {
            assert_eq!(
                StatusError::classify(&StatusError::refuse(code, kind, "nope")),
                expected,
                "{kind}"
            );
        }
    }

    #[test]
    fn falls_back_to_the_code_without_details() {
        let status = |code| StatusError::classify(&Status::new(code, "words"));
        assert_eq!(status(Code::Unimplemented), PanelError::NotFound("words".into()));
        assert_eq!(status(Code::Unauthenticated), PanelError::Unauthorized("words".into()));
        assert_eq!(status(Code::Unavailable), PanelError::Unreachable("words".into()));
        assert_eq!(status(Code::Internal), PanelError::Server("words".into()));
        assert_eq!(status(Code::Unknown), PanelError::Server("words".into()));
        assert_eq!(
            StatusError::classify(&Status::new(Code::NotFound, "")),
            PanelError::NotFound("gRPC NotFound".into())
        );
    }

    #[test]
    fn an_http_answer_to_a_grpc_call_is_a_control_plane_without_grpc() {
        let status = Status::internal(
            "protocol error: received message with invalid compression flag: 52 (valid flags are 0 and 1) while receiving response with status: 404 Not Found",
        );
        assert!(matches!(StatusError::classify(&status), PanelError::NotFound(m) if m.contains("does not speak gRPC")));
        // Any other internal error stays the control plane's own.
        assert!(matches!(
            StatusError::classify(&Status::internal("boom")),
            PanelError::Server(_)
        ));
    }

    #[test]
    fn reads_an_error_detail_put_straight_in_the_details() {
        let detail = ErrorDetail {
            kind: "forbidden".into(),
            message: "revoked".into(),
            revoked: true,
            ..Default::default()
        };
        let status = Status::with_details(Code::PermissionDenied, "x", detail.encode_to_vec().into());
        assert_eq!(StatusError::classify(&status), PanelError::Forbidden("revoked".into()));
        let garbage = Status::with_details(Code::Internal, "plain", vec![0xff, 0xff].into());
        assert_eq!(StatusError::classify(&garbage), PanelError::Server("plain".into()));
    }

    #[test]
    fn a_refused_operator_certificate_says_how_to_get_one() {
        for alert in [
            "UnknownCA",
            "BadCertificate",
            "CertificateRequired",
            "CertificateRevoked",
        ] {
            let status = Status::unavailable(format!(
                "transport error: connection error: received fatal alert: {alert}"
            ));
            assert!(
                matches!(StatusError::classify(&status), PanelError::Forbidden(m) if m.contains("ca operator issue") && m.contains(alert)),
                "{alert}"
            );
        }
        // Other transport failures stay unreachable.
        let down = Status::unavailable("transport error: tcp connect error: Connection refused");
        assert!(matches!(StatusError::classify(&down), PanelError::Unreachable(_)));
    }
}
