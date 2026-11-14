//! The operator API over gRPC: TLS trusting the fleet CA alone, the bearer
//! token in the `authorization` metadata and the actor in `x-axlr-actor`.
//!
//! The wire types and the client and server stubs are generated at build
//! time from `api/proto` (see `build.rs`). The mappers turn them into the
//! panel's domain types at this boundary and nowhere else.

mod activity_mapper;
mod artifact_receiver;
mod bot_mapper;
mod definition_mapper;
mod event_mapper;
mod execution_mapper;
mod group_mapper;
mod grpc_connector;
mod grpc_control_plane;
mod json_fields;
mod node_mapper;
mod operator_auth;
mod operator_channel;
mod overview_mapper;
mod patch_mapper;
mod status_error;
mod task_mapper;
mod wire;

/// The operator API's generated messages and stubs.
#[allow(clippy::all, clippy::pedantic, clippy::nursery, missing_docs, unreachable_pub)]
pub mod proto {
    include!(concat!(env!("OUT_DIR"), "/axlrbots.operator.v1.rs"));
}

pub use grpc_connector::GrpcConnector;
pub use grpc_control_plane::GrpcControlPlane;
pub use status_error::StatusError;
