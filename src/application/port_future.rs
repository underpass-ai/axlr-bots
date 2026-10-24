use std::future::Future;
use std::pin::Pin;

use super::PanelError;

/// What a port's call returns: a future the UI can run on its executor.
pub type PortFuture<'a, T> = Pin<Box<dyn Future<Output = Result<T, PanelError>> + Send + 'a>>;
