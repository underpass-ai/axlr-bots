use tonic::metadata::{Ascii, MetadataValue};
use tonic::service::Interceptor;
use tonic::{Request, Status};

use crate::application::PanelError;
use crate::domain::Secret;

/// The metadata key of the bearer token.
const AUTHORIZATION: &str = "authorization";
/// The metadata key of who decides.
const ACTOR: &str = "x-axlr-actor";

/// Puts the bearer token, and the actor when the panel has one, in the
/// metadata of every call.
#[derive(Clone)]
pub(super) struct OperatorAuth {
    bearer: MetadataValue<Ascii>,
    actor: Option<MetadataValue<Ascii>>,
}

impl OperatorAuth {
    pub fn new(token: &Secret, actor: Option<&str>) -> Result<Self, PanelError> {
        let mut bearer: MetadataValue<Ascii> = format!("Bearer {}", token.expose())
            .parse()
            .map_err(|_| PanelError::Config("the token has characters a request cannot carry".into()))?;
        bearer.set_sensitive(true);
        let actor = actor
            .map(|name| {
                name.parse()
                    .map_err(|_| PanelError::Config("the actor has characters a request cannot carry".into()))
            })
            .transpose()?;
        Ok(Self { bearer, actor })
    }
}

impl Interceptor for OperatorAuth {
    fn call(&mut self, mut request: Request<()>) -> Result<Request<()>, Status> {
        let metadata = request.metadata_mut();
        metadata.insert(AUTHORIZATION, self.bearer.clone());
        if let Some(actor) = &self.actor {
            metadata.insert(ACTOR, actor.clone());
        }
        Ok(request)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn carries_the_token_and_the_actor() {
        let mut auth = OperatorAuth::new(&Secret::new("tok"), Some("alice")).unwrap();
        let request = auth.call(Request::new(())).unwrap();
        assert_eq!(request.metadata().get("authorization").unwrap(), "Bearer tok");
        assert_eq!(request.metadata().get("x-axlr-actor").unwrap(), "alice");
        let mut quiet = OperatorAuth::new(&Secret::new("tok"), None).unwrap();
        assert!(
            quiet
                .call(Request::new(()))
                .unwrap()
                .metadata()
                .get("x-axlr-actor")
                .is_none()
        );
    }

    #[test]
    fn a_token_a_request_cannot_carry_is_a_settings_error_not_a_panic() {
        assert!(matches!(
            OperatorAuth::new(&Secret::new("bad\ntoken"), None),
            Err(PanelError::Config(_))
        ));
        assert!(matches!(
            OperatorAuth::new(&Secret::new("ok"), Some("bad\nactor")),
            Err(PanelError::Config(_))
        ));
    }
}
