use std::path::PathBuf;

use serde::{Deserialize, Serialize};

use super::{NewBotDefaults, PanelConfig, PanelError, PanelSettings, Theme};

/// The settings file as written: one flat object whose keys are dotted
/// by section, like an editor's `settings.json`. The three control
/// plane keys come together or not at all.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct SettingsDocument {
    #[serde(rename = "controlPlane.apiUrl", default)]
    pub api_url: Option<String>,
    #[serde(rename = "controlPlane.caFile", default)]
    pub ca_file: Option<PathBuf>,
    #[serde(rename = "controlPlane.tokenFile", default)]
    pub token_file: Option<PathBuf>,
    /// Who the panel decides as; optional, with the other three.
    #[serde(rename = "controlPlane.actor", default)]
    pub actor: Option<String>,
    /// The operator's client certificate and key; optional, with the
    /// other three: absent, the panel looks where `ca operator issue`
    /// leaves them.
    #[serde(rename = "controlPlane.certFile", default)]
    pub cert_file: Option<PathBuf>,
    #[serde(rename = "controlPlane.keyFile", default)]
    pub key_file: Option<PathBuf>,
    #[serde(rename = "look.theme", default)]
    pub theme: Theme,
    #[serde(rename = "newBot.defaultModel", default = "default_model")]
    pub default_model: String,
    #[serde(rename = "newBot.models", default = "default_models")]
    pub models: Vec<String>,
}

fn default_model() -> String {
    NewBotDefaults::default().model
}

fn default_models() -> Vec<String> {
    NewBotDefaults::default().models
}

impl From<&PanelSettings> for SettingsDocument {
    fn from(settings: &PanelSettings) -> Self {
        let control_plane = settings.control_plane.as_ref();
        Self {
            api_url: control_plane.map(|c| c.api_url.clone()),
            ca_file: control_plane.map(|c| c.ca_path.clone()),
            token_file: control_plane.map(|c| c.token_file.clone()),
            actor: control_plane.and_then(|c| c.actor.clone()),
            cert_file: control_plane.and_then(|c| c.cert_path.clone()),
            key_file: control_plane.and_then(|c| c.key_path.clone()),
            theme: settings.theme,
            default_model: settings.new_bot.model.clone(),
            models: settings.new_bot.models.clone(),
        }
    }
}

impl TryFrom<SettingsDocument> for PanelSettings {
    type Error = PanelError;

    fn try_from(document: SettingsDocument) -> Result<Self, PanelError> {
        let actor = document.actor.map(|a| a.trim().to_string()).filter(|a| !a.is_empty());
        let (cert_path, key_path) = (document.cert_file, document.key_file);
        let control_plane = match (document.api_url, document.ca_file, document.token_file) {
            (None, None, None) if actor.is_some() || cert_path.is_some() || key_path.is_some() => {
                return Err(PanelError::Config(
                    "controlPlane.actor, controlPlane.certFile and controlPlane.keyFile go with controlPlane.apiUrl, controlPlane.caFile and controlPlane.tokenFile"
                        .into(),
                ));
            }
            (None, None, None) => None,
            (Some(api_url), Some(ca_path), Some(token_file)) => Some(PanelConfig {
                api_url,
                ca_path,
                token_file,
                actor,
                cert_path,
                key_path,
            }),
            _ => {
                return Err(PanelError::Config(
                    "controlPlane.apiUrl, controlPlane.caFile and controlPlane.tokenFile go together: give the three or none".into(),
                ));
            }
        };
        Ok(Self {
            control_plane,
            theme: document.theme,
            new_bot: NewBotDefaults {
                model: document.default_model,
                models: document.models,
            }
            .tidy(),
        })
    }
}
