use serde::{Deserialize, Serialize};

/// Whether the organic backgrounds move: the faint glow behind the row,
/// the card or the tile under the pointer. The panel's look, kept in its
/// settings as `look.motion`. Off, nothing listens to the pointer,
/// nothing is drawn and no frame is asked for.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum Motion {
    #[default]
    On,
    Off,
}

impl Motion {
    pub const ALL: [Motion; 2] = [Motion::On, Motion::Off];

    pub fn parse(raw: &str) -> Option<Self> {
        match raw {
            "on" => Some(Self::On),
            "off" => Some(Self::Off),
            _ => None,
        }
    }

    pub fn as_str(self) -> &'static str {
        match self {
            Self::On => "on",
            Self::Off => "off",
        }
    }

    /// The word on its button.
    pub fn label(self) -> &'static str {
        match self {
            Self::On => "On",
            Self::Off => "Off",
        }
    }

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
    fn parses_names_and_the_settings_write_it_as_on_or_off() {
        for motion in Motion::ALL {
            assert_eq!(Motion::parse(motion.as_str()), Some(motion));
            assert!(!motion.label().is_empty());
        }
        assert_eq!(Motion::parse("still"), None);
        assert_eq!(serde_json::to_string(&Motion::Off).unwrap(), "\"off\"");
        assert_eq!(serde_json::from_str::<Motion>("\"on\"").unwrap(), Motion::On);
    }
}
