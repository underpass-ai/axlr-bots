use chrono::{DateTime, Utc};

/// The time, for "today" and for ages.
pub trait Clock: Send + Sync {
    fn now(&self) -> DateTime<Utc>;
    /// Midnight of the operator's day, in UTC.
    fn start_of_today(&self) -> DateTime<Utc>;
}
