//! The panel's settings file and the local install's defaults.

mod install_discovery;
mod toml_config_store;
mod toml_shortcuts;

pub use install_discovery::InstallDiscovery;
pub use toml_config_store::TomlConfigStore;
pub use toml_shortcuts::TomlShortcuts;
