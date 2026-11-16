/// A field the panel puts the keyboard in by itself: the filter over
/// the list on show (`/`), or the palette's (when it opens).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Field {
    ListFilter,
    Palette,
}

impl Field {
    /// The id its widget carries, which the runtime focuses.
    pub fn id(self) -> &'static str {
        match self {
            Self::ListFilter => "list-filter",
            Self::Palette => "palette-field",
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn each_field_has_its_own_id() {
        assert_ne!(Field::ListFilter.id(), Field::Palette.id());
    }
}
