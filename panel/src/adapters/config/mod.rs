//! The panel's settings file and the local install's defaults.

mod install_discovery;
mod json_settings_store;
mod toml_shortcuts;

pub use install_discovery::InstallDiscovery;
pub use json_settings_store::JsonSettingsStore;
pub use toml_shortcuts::TomlShortcuts;
