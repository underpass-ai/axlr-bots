use super::DomainError;

/// The longest identifier the control plane accepts.
const MAX_IDENTIFIER_LENGTH: usize = 64;

/// Checks an identifier as the control plane does: trimmed, 1 to 64
/// characters of lowercase letters, digits, '.', '_' and '-'.
pub(crate) fn validate_identifier(field: &'static str, raw: &str) -> Result<String, DomainError> {
    let value = raw.trim();
    if value.is_empty() {
        return Err(DomainError::new(field, "must not be empty"));
    }
    if value.len() > MAX_IDENTIFIER_LENGTH {
        return Err(DomainError::new(field, "must be at most 64 characters"));
    }
    let allowed =
        |c: char| c.is_ascii_lowercase() || c.is_ascii_digit() || matches!(c, '.' | '_' | '-');
    if !value.chars().all(allowed) {
        return Err(DomainError::new(
            field,
            "may only use lowercase letters, digits, '.', '_' and '-'",
        ));
    }
    Ok(value.to_string())
}

/// Checks a free text as the control plane does: trimmed, not empty, at
/// most `max` bytes, no control character but newlines and tabs.
pub(crate) fn validate_text(
    field: &'static str,
    raw: &str,
    max: usize,
) -> Result<String, DomainError> {
    let value = raw.trim();
    if value.is_empty() {
        return Err(DomainError::new(field, "must not be empty"));
    }
    if value.len() > max {
        return Err(DomainError::new(field, "is too long"));
    }
    if value
        .chars()
        .any(|c| c.is_control() && c != '\n' && c != '\t')
    {
        return Err(DomainError::new(
            field,
            "must not contain control characters",
        ));
    }
    Ok(value.to_string())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn identifiers_follow_the_control_plane_rules() {
        assert_eq!(
            validate_identifier("id", "  bot-1.a_b ").unwrap(),
            "bot-1.a_b"
        );
        assert!(validate_identifier("id", "").is_err());
        assert!(validate_identifier("id", "Bot").is_err());
        assert!(validate_identifier("id", "a b").is_err());
        assert!(validate_identifier("id", &"a".repeat(65)).is_err());
        assert!(validate_identifier("id", &"a".repeat(64)).is_ok());
    }

    #[test]
    fn texts_are_trimmed_bounded_and_printable() {
        assert_eq!(
            validate_text("t", " hello\nworld ", 20).unwrap(),
            "hello\nworld"
        );
        assert!(validate_text("t", "   ", 20).is_err());
        assert!(validate_text("t", "abc", 2).is_err());
        assert!(validate_text("t", "a\u{7}b", 20).is_err());
        assert!(validate_text("t", "a\tb", 20).is_ok());
    }
}
