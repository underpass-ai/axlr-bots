//! The ports' implementations: the operator API over HTTPS, the system's
//! clock, files and dialogs, the settings file, and the in-memory fakes.

pub mod config;
pub mod fake;
pub mod http;
pub mod system;
