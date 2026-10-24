use std::path::PathBuf;

use serde::{Deserialize, Serialize};

use super::PanelError;

/// Where the control plane's operator API is and how to trust and reach
/// it: its https address, the fleet CA and the file holding the bearer
/// token (never the token itself).
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct PanelConfig {
    pub api_url: String,
    pub ca_path: PathBuf,
    pub token_file: PathBuf,
}

impl PanelConfig {
    /// The address without a trailing slash, checked to be https.
    pub fn validate(&self) -> Result<(), PanelError> {
        let url = self.api_url.trim();
        let Some(rest) = url.strip_prefix("https://") else {
            return Err(PanelError::Config("the API address must start with https://".into()));
        };
        if rest.trim_end_matches('/').is_empty() || rest.contains(char::is_whitespace) {
            return Err(PanelError::Config(
                "the API address needs a host, like https://127.0.0.1:7879".into(),
            ));
        }
        if self.ca_path.as_os_str().is_empty() {
            return Err(PanelError::Config("the CA file is missing".into()));
        }
        if self.token_file.as_os_str().is_empty() {
            return Err(PanelError::Config("the token file is missing".into()));
        }
        Ok(())
    }

    /// The address, trimmed and without a trailing slash.
    pub fn base_url(&self) -> String {
        self.api_url.trim().trim_end_matches('/').to_string()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn config(url: &str) -> PanelConfig {
        PanelConfig {
            api_url: url.into(),
            ca_path: "ca.crt".into(),
            token_file: "api.token".into(),
        }
    }

    #[test]
    fn checks_the_address_and_the_files() {
        assert!(config("https://127.0.0.1:7879/").validate().is_ok());
        assert_eq!(config(" https://127.0.0.1:7879/ ").base_url(), "https://127.0.0.1:7879");
        assert!(config("http://127.0.0.1:7879").validate().is_err());
        assert!(config("https://").validate().is_err());
        assert!(config("https://a b").validate().is_err());
        let mut no_ca = config("https://h");
        no_ca.ca_path = PathBuf::new();
        assert!(no_ca.validate().is_err());
        let mut no_token = config("https://h");
        no_token.token_file = PathBuf::new();
        assert!(no_token.validate().is_err());
    }
}
