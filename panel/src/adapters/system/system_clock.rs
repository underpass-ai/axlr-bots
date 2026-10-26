use chrono::{DateTime, Local, TimeZone, Utc};

use crate::application::Clock;

/// The system's clock; the operator's day is the local one.
#[derive(Debug, Default, Clone, Copy)]
pub struct SystemClock;

impl Clock for SystemClock {
    fn now(&self) -> DateTime<Utc> {
        Utc::now()
    }

    fn start_of_today(&self) -> DateTime<Utc> {
        let today = Local::now().date_naive();
        let midnight = today.and_hms_opt(0, 0, 0).unwrap_or_default();
        match Local.from_local_datetime(&midnight).earliest() {
            Some(local) => local.with_timezone(&Utc),
            None => midnight.and_utc(),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn today_starts_before_now_and_less_than_a_day_ago() {
        let clock = SystemClock;
        let (start, now) = (clock.start_of_today(), clock.now());
        assert!(start <= now);
        assert!(now - start < chrono::Duration::hours(25));
    }
}
