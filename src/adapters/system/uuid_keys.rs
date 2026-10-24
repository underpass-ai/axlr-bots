use crate::application::KeySource;
use crate::domain::IdempotencyKey;

/// Keys `panel-<uuid>`: unique per form, so a retry repeats its own key.
#[derive(Debug, Default, Clone, Copy)]
pub struct UuidKeys;

impl KeySource for UuidKeys {
    fn next_key(&self) -> IdempotencyKey {
        IdempotencyKey::new(&format!("panel-{}", uuid::Uuid::new_v4())).expect("a uuid key is valid")
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn keys_are_unique_and_prefixed() {
        let (a, b) = (UuidKeys.next_key(), UuidKeys.next_key());
        assert!(a.as_str().starts_with("panel-"));
        assert_ne!(a, b);
    }
}
