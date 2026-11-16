use iced::keyboard::{self, Key, Modifiers, key::Named};

/// A key the panel answers to by itself: Esc closes, Enter confirms or
/// opens, the arrows move over rows and tabs, and a few shortcuts open
/// the palette, the find and the filter. A key a widget took first
/// never becomes one: a focused one-line field keeps the letters typed
/// into it (so `/`, `j` and `k`), Left and Right, Esc, and Enter when it
/// submits, and lets Up, Down and the Ctrl shortcuts through; a focused
/// text of several lines (a passage being selected, the settings as
/// JSON) keeps the arrows too.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Keypress {
    Escape,
    Enter,
    /// Shift+Enter: the match before, where Enter goes to the next.
    ShiftEnter,
    /// Ctrl+K, or Cmd+K: the command palette.
    Palette,
    /// Ctrl+F, or Cmd+F: find in what is open.
    Find,
    /// `/`: the list's filter.
    Filter,
    /// The up arrow, or `k`.
    Up,
    /// The down arrow, or `j`.
    Down,
    Left,
    Right,
}

impl Keypress {
    /// The key pressed, when the panel answers to it: alone, with Ctrl
    /// or Cmd for the shortcuts, or with Shift for Shift+Enter and for a
    /// `/` the keyboard only gives with Shift. Any other modifier held
    /// makes it another key, which is none of these.
    pub fn of(event: keyboard::Event) -> Option<Self> {
        let keyboard::Event::KeyPressed {
            key,
            modified_key,
            physical_key,
            modifiers,
            ..
        } = event
        else {
            return None;
        };
        if modifiers == Modifiers::CTRL || modifiers == Modifiers::LOGO {
            // By the key's place on the keyboard, so the shortcut is the
            // same key on a keyboard that does not write latin letters.
            return match key.to_latin(physical_key).map(|c| c.to_ascii_lowercase()) {
                Some('k') => Some(Self::Palette),
                Some('f') => Some(Self::Find),
                _ => None,
            };
        }
        if modifiers == Modifiers::SHIFT {
            return match modified_key.as_ref() {
                Key::Named(Named::Enter) => Some(Self::ShiftEnter),
                Key::Character("/") => Some(Self::Filter),
                _ => None,
            };
        }
        if !modifiers.is_empty() {
            return None;
        }
        match key.as_ref() {
            Key::Named(Named::Escape) => Some(Self::Escape),
            Key::Named(Named::Enter) => Some(Self::Enter),
            Key::Named(Named::ArrowUp) | Key::Character("k") => Some(Self::Up),
            Key::Named(Named::ArrowDown) | Key::Character("j") => Some(Self::Down),
            Key::Named(Named::ArrowLeft) => Some(Self::Left),
            Key::Named(Named::ArrowRight) => Some(Self::Right),
            Key::Character("/") => Some(Self::Filter),
            _ => None,
        }
    }
}

#[cfg(test)]
mod tests {
    use iced::keyboard::Location;
    use iced::keyboard::key::{Code, Physical};

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

    /// A key as a keyboard gives it: what it writes alone, what it
    /// writes with the modifiers held, and where it is on the board.
    fn typed(key: &str, modified: &str, code: Code, modifiers: Modifiers) -> keyboard::Event {
        keyboard::Event::KeyPressed {
            key: Key::Character(key.into()),
            modified_key: Key::Character(modified.into()),
            physical_key: Physical::Code(code),
            location: Location::Standard,
            modifiers,
            text: None,
            repeat: false,
        }
    }

    fn alone(key: Key) -> Option<Keypress> {
        Keypress::of(pressed(key, Modifiers::empty()))
    }

    #[test]
    fn the_arrows_and_j_k_move() {
        assert_eq!(alone(Key::Named(Named::ArrowUp)), Some(Keypress::Up));
        assert_eq!(alone(Key::Named(Named::ArrowDown)), Some(Keypress::Down));
        assert_eq!(alone(Key::Named(Named::ArrowLeft)), Some(Keypress::Left));
        assert_eq!(alone(Key::Named(Named::ArrowRight)), Some(Keypress::Right));
        assert_eq!(alone(Key::Character("j".into())), Some(Keypress::Down));
        assert_eq!(alone(Key::Character("k".into())), Some(Keypress::Up));
        assert_eq!(alone(Key::Character("h".into())), None);
        assert_eq!(alone(Key::Named(Named::Tab)), None);
    }

    #[test]
    fn ctrl_or_cmd_with_k_and_f_open_the_palette_and_the_find() {
        for held in [Modifiers::CTRL, Modifiers::LOGO] {
            assert_eq!(Keypress::of(typed("k", "k", Code::KeyK, held)), Some(Keypress::Palette));
            assert_eq!(Keypress::of(typed("f", "f", Code::KeyF, held)), Some(Keypress::Find));
            assert_eq!(
                Keypress::of(typed("K", "K", Code::KeyK, held)),
                Some(Keypress::Palette),
                "with the capitals locked"
            );
            assert_eq!(Keypress::of(typed("j", "j", Code::KeyJ, held)), None);
            assert_eq!(Keypress::of(pressed(Key::Named(Named::ArrowDown), held)), None);
        }
        assert_eq!(
            Keypress::of(typed("к", "к", Code::KeyK, Modifiers::CTRL)),
            Some(Keypress::Palette),
            "the same key on a keyboard that writes other letters"
        );
        assert_eq!(
            Keypress::of(pressed(Key::Character("к".into()), Modifiers::CTRL)),
            None,
            "a letter that is not latin from a key the system does not place"
        );
    }

    #[test]
    fn a_slash_opens_the_filter_also_where_it_needs_shift() {
        assert_eq!(alone(Key::Character("/".into())), Some(Keypress::Filter));
        assert_eq!(
            Keypress::of(typed("7", "/", Code::Digit7, Modifiers::SHIFT)),
            Some(Keypress::Filter),
            "Shift+7 writes the slash on many keyboards"
        );
        assert_eq!(Keypress::of(typed("7", "7", Code::Digit7, Modifiers::empty())), None);
        assert_eq!(
            Keypress::of(typed("/", "?", Code::Slash, Modifiers::SHIFT)),
            None,
            "Shift with the slash key writes a question mark"
        );
        assert_eq!(Keypress::of(typed("/", "/", Code::Slash, Modifiers::CTRL)), None);
    }

    #[test]
    fn shift_enter_is_its_own_key() {
        assert_eq!(
            Keypress::of(pressed(Key::Named(Named::Enter), Modifiers::SHIFT)),
            Some(Keypress::ShiftEnter)
        );
        assert_eq!(Keypress::of(pressed(Key::Named(Named::Escape), Modifiers::SHIFT)), None);
        assert_eq!(
            Keypress::of(pressed(Key::Named(Named::ArrowDown), Modifiers::SHIFT)),
            None
        );
        assert_eq!(Keypress::of(typed("j", "J", Code::KeyJ, Modifiers::SHIFT)), None);
    }

    #[test]
    fn a_key_with_any_other_modifier_is_none_of_these() {
        let held = [
            Modifiers::ALT,
            Modifiers::CTRL | Modifiers::SHIFT,
            Modifiers::CTRL | Modifiers::ALT,
            Modifiers::CTRL | Modifiers::LOGO,
            Modifiers::SHIFT | Modifiers::ALT,
        ];
        for modifiers in held {
            assert_eq!(
                Keypress::of(typed("k", "k", Code::KeyK, modifiers)),
                None,
                "{modifiers:?}"
            );
            assert_eq!(
                Keypress::of(typed("f", "f", Code::KeyF, modifiers)),
                None,
                "{modifiers:?}"
            );
            assert_eq!(
                Keypress::of(typed("/", "/", Code::Slash, modifiers)),
                None,
                "{modifiers:?}"
            );
            for key in [Named::Enter, Named::Escape, Named::ArrowUp, Named::ArrowRight] {
                assert_eq!(Keypress::of(pressed(Key::Named(key), modifiers)), None, "{modifiers:?}");
            }
        }
        assert_eq!(
            Keypress::of(keyboard::Event::KeyReleased {
                key: Key::Named(Named::Enter),
                modified_key: Key::Named(Named::Enter),
                physical_key: Physical::Code(Code::Enter),
                location: Location::Standard,
                modifiers: Modifiers::empty(),
            }),
            None,
            "a key let go is not a key pressed"
        );
    }
}
