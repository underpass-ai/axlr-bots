use iced::keyboard::{self, Key, key::Named};

/// A key the panel answers to by itself: Esc closes, Enter confirms. A
/// key a widget took first (Enter in a field) never becomes one.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Keypress {
    Escape,
    Enter,
}

impl Keypress {
    /// The key pressed alone, when the panel answers to it.
    pub fn of(event: keyboard::Event) -> Option<Self> {
        let keyboard::Event::KeyPressed { key, modifiers, .. } = event else {
            return None;
        };
        if !modifiers.is_empty() {
            return None;
        }
        match key {
            Key::Named(Named::Escape) => Some(Self::Escape),
            Key::Named(Named::Enter) => Some(Self::Enter),
            _ => None,
        }
    }
}

#[cfg(test)]
mod tests {
    use iced::keyboard::{Location, Modifiers};

    use super::*;

    fn pressed(key: Key, modifiers: Modifiers) -> keyboard::Event {
        keyboard::Event::KeyPressed {
            key: key.clone(),
            modified_key: key.clone(),
            physical_key: keyboard::key::Physical::Unidentified(keyboard::key::NativeCode::Unidentified),
            location: Location::Standard,
            modifiers,
            text: None,
            repeat: false,
        }
    }

    #[test]
    fn answers_to_esc_and_enter_alone() {
        assert_eq!(
            Keypress::of(pressed(Key::Named(Named::Escape), Modifiers::empty())),
            Some(Keypress::Escape)
        );
        assert_eq!(
            Keypress::of(pressed(Key::Named(Named::Enter), Modifiers::empty())),
            Some(Keypress::Enter)
        );
        assert_eq!(Keypress::of(pressed(Key::Named(Named::Enter), Modifiers::CTRL)), None);
        assert_eq!(
            Keypress::of(pressed(Key::Character("a".into()), Modifiers::empty())),
            None
        );
        assert_eq!(Keypress::of(keyboard::Event::ModifiersChanged(Modifiers::SHIFT)), None);
    }
}
