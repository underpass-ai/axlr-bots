use super::{Notice, PortFuture};

/// Where desktop notices go. The answer says whether the person clicked
/// the notice before it went away.
pub trait Notifier: Send + Sync {
    fn show(&self, notice: Notice) -> PortFuture<'_, bool>;
}
