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
    let allowed = |c: char| c.is_ascii_lowercase() || c.is_ascii_digit() || matches!(c, '.' | '_' | '-');
    if !value.chars().all(allowed) {
        return Err(DomainError::new(
            field,
            "may only use lowercase letters, digits, '.', '_' and '-'",
        ));
    }
    Ok(value.to_string())
}

/// Makes an identifier out of free words: lowercase, without accents, what
/// is not allowed turned into single hyphens, cut at the longest length.
/// `None` when no letter or digit is left.
pub(crate) fn derive_identifier(raw: &str) -> Option<String> {
    let mut id = String::new();
    for c in raw.chars().flat_map(char::to_lowercase) {
        if ('\u{300}'..='\u{36f}').contains(&c) {
            continue;
        }
        if let Some(plain) = without_accent(c) {
            id.push_str(plain);
        } else if c != '-' && (c.is_ascii_lowercase() || c.is_ascii_digit() || matches!(c, '.' | '_')) {
            id.push(c);
        } else if !id.ends_with('-') {
            id.push('-');
        }
    }
    let edge = ['-', '.', '_'];
    let id = id.trim_start_matches(edge);
    let id = id[..id.len().min(MAX_IDENTIFIER_LENGTH)].trim_end_matches(edge);
    (!id.is_empty()).then(|| id.to_string())
}

/// The plain letters of a lowercase latin letter with an accent.
fn without_accent(c: char) -> Option<&'static str> {
    Some(match c {
        'á' | 'à' | 'â' | 'ä' | 'ã' | 'å' | 'ā' | 'ă' | 'ą' => "a",
        'æ' => "ae",
        'ç' | 'ć' | 'ĉ' | 'ċ' | 'č' => "c",
        'ď' | 'đ' | 'ð' => "d",
        'é' | 'è' | 'ê' | 'ë' | 'ē' | 'ĕ' | 'ė' | 'ę' | 'ě' => "e",
        'ĝ' | 'ğ' | 'ġ' | 'ģ' => "g",
        'ĥ' | 'ħ' => "h",
        'í' | 'ì' | 'î' | 'ï' | 'ĩ' | 'ī' | 'ĭ' | 'į' | 'ı' => "i",
        'ĵ' => "j",
        'ķ' => "k",
        'ĺ' | 'ļ' | 'ľ' | 'ŀ' | 'ł' => "l",
        'ñ' | 'ń' | 'ņ' | 'ň' => "n",
        'ó' | 'ò' | 'ô' | 'ö' | 'õ' | 'ø' | 'ō' | 'ŏ' | 'ő' => "o",
        'œ' => "oe",
        'ŕ' | 'ŗ' | 'ř' => "r",
        'ś' | 'ŝ' | 'ş' | 'š' => "s",
        'ß' => "ss",
        'ţ' | 'ť' | 'ŧ' => "t",
        'þ' => "th",
        'ú' | 'ù' | 'û' | 'ü' | 'ũ' | 'ū' | 'ŭ' | 'ů' | 'ű' | 'ų' => "u",
        'ŵ' => "w",
        'ý' | 'ÿ' | 'ŷ' => "y",
        'ź' | 'ż' | 'ž' => "z",
        _ => return None,
    })
}

/// Checks a free text as the control plane does: trimmed, not empty, at
/// most `max` bytes, no control character but newlines and tabs.
pub(crate) fn validate_text(field: &'static str, raw: &str, max: usize) -> Result<String, DomainError> {
    let value = raw.trim();
    if value.is_empty() {
        return Err(DomainError::new(field, "must not be empty"));
    }
    if value.len() > max {
        return Err(DomainError::new(field, "is too long"));
    }
    if value.chars().any(|c| c.is_control() && c != '\n' && c != '\t') {
        return Err(DomainError::new(field, "must not contain control characters"));
    }
    Ok(value.to_string())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn identifiers_follow_the_control_plane_rules() {
        assert_eq!(validate_identifier("id", "  bot-1.a_b ").unwrap(), "bot-1.a_b");
        assert!(validate_identifier("id", "").is_err());
        assert!(validate_identifier("id", "Bot").is_err());
        assert!(validate_identifier("id", "a b").is_err());
        assert!(validate_identifier("id", &"a".repeat(65)).is_err());
        assert!(validate_identifier("id", &"a".repeat(64)).is_ok());
    }

    #[test]
    fn identifiers_are_made_from_free_words() {
        let derive = |raw| derive_identifier(raw);
        assert_eq!(derive("NanoBot secured on ESP32").unwrap(), "nanobot-secured-on-esp32");
        assert_eq!(derive("  Vigía de Peñíscola  ").unwrap(), "vigia-de-peniscola");
        assert_eq!(derive("Vigi\u{301}a").unwrap(), "vigia");
        assert_eq!(derive("Straße Œuvre").unwrap(), "strasse-oeuvre");
        assert_eq!(derive("worker-1.a_b").unwrap(), "worker-1.a_b");
        assert_eq!(derive("a -- b / (c)").unwrap(), "a-b-c");
    }

    #[test]
    fn a_made_identifier_keeps_the_control_plane_rules() {
        let cut = derive_identifier(&format!("{}-bcd", "a".repeat(63))).unwrap();
        assert_eq!(cut, "a".repeat(63), "the hyphen left at the cut goes too");
        let long = derive_identifier(&"ñ".repeat(200)).unwrap();
        assert_eq!(long.len(), 64);
        assert!(validate_identifier("id", &long).is_ok());
    }

    #[test]
    fn words_with_no_letter_or_digit_make_no_identifier() {
        for raw in ["", "   ", "---", "...", "_", "¡!?", "日本語", "\u{301}"] {
            assert_eq!(derive_identifier(raw), None, "{raw:?}");
        }
    }

    #[test]
    fn texts_are_trimmed_bounded_and_printable() {
        assert_eq!(validate_text("t", " hello\nworld ", 20).unwrap(), "hello\nworld");
        assert!(validate_text("t", "   ", 20).is_err());
        assert!(validate_text("t", "abc", 2).is_err());
        assert!(validate_text("t", "a\u{7}b", 20).is_err());
        assert!(validate_text("t", "a\tb", 20).is_ok());
    }
}
