/// What runs cost: the dollars they told, and how many stopped before
/// telling it (killed at their timeout or cancelled with no receipt).
/// With such runs the dollars are what is known: the cost is at least
/// that.
#[derive(Debug, Clone, Copy, Default, PartialEq)]
pub struct Cost {
    pub usd: f64,
    pub unknown_runs: u32,
}

impl Cost {
    pub fn new(usd: f64, unknown_runs: u32) -> Self {
        Self { usd, unknown_runs }
    }

    /// Whether some run did not tell its cost: the dollars are a floor.
    pub fn is_floor(&self) -> bool {
        self.unknown_runs > 0
    }

    /// Both costs, added.
    pub fn plus(self, other: Self) -> Self {
        Self::new(self.usd + other.usd, self.unknown_runs + other.unknown_runs)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_run_that_did_not_tell_makes_the_sum_a_floor() {
        let told = Cost::new(0.25, 0);
        assert!(!told.is_floor());
        let sum = told.plus(Cost::new(0.0, 1)).plus(Cost::new(0.5, 2));
        assert_eq!(sum, Cost::new(0.75, 3));
        assert!(sum.is_floor());
        assert_eq!(Cost::default(), Cost::new(0.0, 0));
    }
}
