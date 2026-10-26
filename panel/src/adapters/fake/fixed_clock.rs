use std::sync::Mutex;

use chrono::{DateTime, Duration, Utc};

use crate::application::Clock;

/// A clock that says what it is told: tests read ages against it.
pub struct FixedClock {
    now: Mutex<DateTime<Utc>>,
}

impl FixedClock {
    pub fn new(now: DateTime<Utc>) -> Self {
        Self { now: Mutex::new(now) }
    }

    pub fn advance(&self, by: Duration) {
        let mut now = self.now.lock().unwrap();
        *now += by;
    }
}

impl Clock for FixedClock {
    fn now(&self) -> DateTime<Utc> {
        *self.now.lock().unwrap()
    }

    /// Midnight UTC of the fixed day.
    fn start_of_today(&self) -> DateTime<Utc> {
        let now = self.now();
        now.date_naive().and_hms_opt(0, 0, 0).unwrap().and_utc()
    }
}
