use super::DomainError;

/// The key that makes a submission happen once: the same bot and key
/// always name the same task.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct IdempotencyKey(String);

impl IdempotencyKey {
    pub fn new(raw: &str) -> Result<Self, DomainError> {
        let value = raw.trim();
        if value.is_empty() {
            return Err(DomainError::new("idempotency key", "must not be empty"));
        }
        if value.len() > 128 {
            return Err(DomainError::new("idempotency key", "must be at most 128 characters"));
        }
        if value.chars().any(|c| (c as u32) < 0x20 || c == '\u{7f}') {
            return Err(DomainError::new(
                "idempotency key",
                "must not contain control characters",
            ));
        }
        Ok(Self(value.to_string()))
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn validates_the_key() {
        assert_eq!(IdempotencyKey::new(" panel-1 ").unwrap().as_str(), "panel-1");
        assert!(IdempotencyKey::new("").is_err());
        assert!(IdempotencyKey::new(&"k".repeat(129)).is_err());
        assert!(IdempotencyKey::new("a\u{1}b").is_err());
    }
}
