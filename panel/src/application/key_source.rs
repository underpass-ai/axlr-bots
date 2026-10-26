use crate::domain::IdempotencyKey;

/// Where a new submission's idempotency key comes from.
pub trait KeySource: Send + Sync {
    fn next_key(&self) -> IdempotencyKey;
}
