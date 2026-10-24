//! AXLR Bots control panel: a desktop application that operates a running
//! control plane through its operator API.
//!
//! Hexagonal: `domain` holds the value types, `application` the use cases
//! and the ports they need, `adapters` the ports' implementations, and
//! `ui` the Iced interface over the use cases.

pub mod domain;
