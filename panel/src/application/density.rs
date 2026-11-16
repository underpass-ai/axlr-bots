use serde::{Deserialize, Serialize};

/// How tall a list's rows are: comfortable, or compact to see more of
/// them at once. The panel's look, kept in its settings as
/// `look.density`.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum Density {
    #[default]
    Comfortable,
    Compact,
}

impl Density {
    pub const ALL: [Density; 2] = [Density::Comfortable, Density::Compact];

    pub fn parse(raw: &str) -> Option<Self> {
        match raw {
            "comfortable" => Some(Self::Comfortable),
            "compact" => Some(Self::Compact),
            _ => None,
        }
    }

    pub fn as_str(self) -> &'static str {
        match self {
            Self::Comfortable => "comfortable",
            Self::Compact => "compact",
        }
    }

    /// The word on its button.
    pub fn label(self) -> &'static str {
        match self {
            Self::Comfortable => "Comfortable",
            Self::Compact => "Compact",
        }
    }

    /// The height of one row of a list, its hairline included, in
    /// pixels.
    pub fn row_height(self) -> f32 {
        match self {
            Self::Comfortable => 36.0,
            Self::Compact => 30.0,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_names_and_measures_its_rows() {
        assert_eq!(Density::default(), Density::Comfortable);
        for density in Density::ALL {
            assert_eq!(Density::parse(density.as_str()), Some(density));
            assert!(!density.label().is_empty());
        }
        assert_eq!(Density::parse("cosy"), None);
        assert_eq!(Density::Comfortable.row_height(), 36.0);
        assert_eq!(Density::Compact.row_height(), 30.0);
        assert_eq!(serde_json::to_string(&Density::Compact).unwrap(), "\"compact\"");
        assert_eq!(
            serde_json::from_str::<Density>("\"comfortable\"").unwrap(),
            Density::Comfortable
        );
    }
}
