/// A field the panel puts the keyboard in by itself: the filter over
/// the list on show (`/`), the palette's (when it opens), or the find in
/// a task's activity (`Ctrl F`).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Field {
    ListFilter,
    Palette,
    ActivityFind,
}

impl Field {
    /// The id its widget carries, which the runtime focuses.
    pub fn id(self) -> &'static str {
        match self {
            Self::ListFilter => "list-filter",
            Self::Palette => "palette-field",
            Self::ActivityFind => "activity-find",
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn each_field_has_its_own_id() {
        assert_ne!(Field::ListFilter.id(), Field::Palette.id());
        assert_ne!(Field::ActivityFind.id(), Field::ListFilter.id());
    }
}
