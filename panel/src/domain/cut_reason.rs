/// Why the control plane stopped recording an execution's progress.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum CutReason {
    /// The execution reached its limit of progress events.
    EventLimit,
    /// Its items reached the size limit.
    ByteLimit,
    /// A batch lost items to a bound.
    BatchTruncated,
    /// A reason a newer control plane gave.
    Other,
}

impl CutReason {
    pub fn label(self) -> &'static str {
        match self {
            Self::EventLimit => "event limit reached",
            Self::ByteLimit => "size limit reached",
            Self::BatchTruncated => "a batch lost steps",
            Self::Other => "cut",
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn says_why_in_words() {
        assert_eq!(CutReason::EventLimit.label(), "event limit reached");
        assert_eq!(CutReason::Other.label(), "cut");
    }
}
