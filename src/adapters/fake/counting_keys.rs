use std::sync::atomic::{AtomicU64, Ordering};

use crate::application::KeySource;
use crate::domain::IdempotencyKey;

/// Keys `test-1`, `test-2`, … for tests.
#[derive(Default)]
pub struct CountingKeys {
    next: AtomicU64,
}

impl KeySource for CountingKeys {
    fn next_key(&self) -> IdempotencyKey {
        let n = self.next.fetch_add(1, Ordering::SeqCst) + 1;
        IdempotencyKey::new(&format!("test-{n}")).unwrap()
    }
}
