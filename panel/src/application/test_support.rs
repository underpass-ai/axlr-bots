//! What the use cases' tests share: the sample bots at a fixed moment.

use std::sync::Arc;

use chrono::{DateTime, Utc};

use crate::adapters::fake::{FakeControlPlane, FixedClock};

pub(crate) fn now() -> DateTime<Utc> {
    "2026-10-25T10:00:00Z".parse().unwrap()
}

pub(crate) fn fleet() -> (Arc<FakeControlPlane>, Arc<FixedClock>) {
    (
        Arc::new(FakeControlPlane::sample(now())),
        Arc::new(FixedClock::new(now())),
    )
}

pub(crate) fn run<F: std::future::Future>(future: F) -> F::Output {
    futures::executor::block_on(future)
}
