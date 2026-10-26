use super::DomainError;
use super::identifier::validate_text;

/// What a task asks of its bot: 1 to 8000 bytes of printable text.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Objective(String);

impl Objective {
    pub fn new(raw: &str) -> Result<Self, DomainError> {
        validate_text("objective", raw, 8000).map(Self)
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn validates_the_text() {
        assert_eq!(Objective::new("  List the files ").unwrap().as_str(), "List the files");
        assert!(Objective::new("   ").is_err());
        assert!(Objective::new(&"x".repeat(8001)).is_err());
    }
}
