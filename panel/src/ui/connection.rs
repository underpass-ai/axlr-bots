use std::fmt;
use std::sync::Arc;

use crate::application::ControlPlane;

/// A client of the control plane that answered, carried in a message.
#[derive(Clone)]
pub struct Connection(pub Arc<dyn ControlPlane>);

impl fmt::Debug for Connection {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("Connection")
    }
}
