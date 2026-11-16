use serde::{Deserialize, Serialize};

/// Whether the organic backgrounds move: the panel's look, kept in its
/// settings as `look.motion`. Off, nothing listens to the pointer, nothing
/// is drawn and no frame is asked for.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum Motion {
    #[default]
    On,
    Off,
}

impl Motion {
    pub fn is_on(self) -> bool {
        self == Self::On
    }

    pub fn toggled(self) -> Self {
        match self {
            Self::On => Self::Off,
            Self::Off => Self::On,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn motion_is_on_unless_the_settings_say_off() {
        assert!(Motion::default().is_on());
        assert!(!Motion::Off.is_on());
        assert_eq!(Motion::On.toggled(), Motion::Off);
        assert_eq!(Motion::Off.toggled(), Motion::On);
    }

    #[test]
    fn the_settings_write_it_as_on_or_off() {
        assert_eq!(serde_json::to_string(&Motion::Off).unwrap(), "\"off\"");
        assert_eq!(serde_json::from_str::<Motion>("\"on\"").unwrap(), Motion::On);
    }
}
