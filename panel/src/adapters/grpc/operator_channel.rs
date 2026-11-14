use std::time::Duration;

use tonic::service::interceptor::InterceptedService;
use tonic::transport::{Certificate, Channel, ClientTlsConfig, Endpoint};

use super::operator_auth::OperatorAuth;
use crate::application::{PanelConfig, PanelError};
use crate::domain::Secret;

/// The service the generated clients talk through: the channel with the
/// credentials in every call.
pub(super) type Authed = InterceptedService<Channel, OperatorAuth>;

/// How long a connection may take to open.
const CONNECT_TIMEOUT: Duration = Duration::from_secs(5);

/// The connection to the control plane the settings name: TLS 1.2+ that
/// trusts the fleet CA alone, opened on the first call.
pub(super) struct OperatorChannel;

impl OperatorChannel {
    /// Reads the CA and the token file now, so a missing or unreadable
    /// file is a settings error before any call. Needs a Tokio runtime.
    pub fn open(config: &PanelConfig) -> Result<Authed, PanelError> {
        config.validate()?;
        let ca = std::fs::read(&config.ca_path)
            .map_err(|e| PanelError::Config(format!("cannot read the CA {}: {e}", config.ca_path.display())))?;
        if !String::from_utf8_lossy(&ca).contains("-----BEGIN CERTIFICATE-----") {
            return Err(PanelError::Config(format!(
                "the CA {} is not a PEM certificate",
                config.ca_path.display()
            )));
        }
        let token = std::fs::read_to_string(&config.token_file).map_err(|e| {
            PanelError::Config(format!(
                "cannot read the token file {}: {e}",
                config.token_file.display()
            ))
        })?;
        let token = token.trim();
        if token.is_empty() {
            return Err(PanelError::Config(format!(
                "the token file {} is empty",
                config.token_file.display()
            )));
        }
        let auth = OperatorAuth::new(&Secret::new(token), config.actor.as_deref())?;
        let tls = ClientTlsConfig::new().ca_certificate(Certificate::from_pem(ca));
        let endpoint = Endpoint::from_shared(config.base_url())
            .map_err(|e| PanelError::Config(format!("the gRPC address: {e}")))?
            .tls_config(tls)
            .map_err(|e| PanelError::Config(format!("cannot set up TLS: {e}")))?
            .connect_timeout(CONNECT_TIMEOUT)
            .tcp_nodelay(true)
            .http2_keep_alive_interval(Duration::from_secs(30))
            .keep_alive_while_idle(true);
        Ok(InterceptedService::new(endpoint.connect_lazy(), auth))
    }
}
