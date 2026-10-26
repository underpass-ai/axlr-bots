/// Light or dark, in the brand's palette.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub enum ThemeMode {
    #[default]
    Light,
    Dark,
}

impl ThemeMode {
    pub fn toggled(self) -> Self {
        match self {
            Self::Light => Self::Dark,
            Self::Dark => Self::Light,
        }
    }

    pub fn parse(raw: &str) -> Option<Self> {
        match raw {
            "light" => Some(Self::Light),
            "dark" => Some(Self::Dark),
            _ => None,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn toggles_and_parses() {
        assert_eq!(ThemeMode::Light.toggled(), ThemeMode::Dark);
        assert_eq!(ThemeMode::Dark.toggled(), ThemeMode::Light);
        assert_eq!(ThemeMode::parse("dark"), Some(ThemeMode::Dark));
        assert_eq!(ThemeMode::parse("light"), Some(ThemeMode::Light));
        assert_eq!(ThemeMode::parse("blue"), None);
    }
}
