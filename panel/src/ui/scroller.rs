use super::view::widgets::PAGE;

/// A part of the panel that scrolls by itself, where a row is brought
/// into sight: the rows of the list on show, or the palette's results.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Scroller {
    Rows,
    Palette,
}

impl Scroller {
    /// The id its scrollable carries.
    pub fn id(self) -> &'static str {
        match self {
            Self::Rows => PAGE,
            Self::Palette => "palette-results",
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_rows_scroll_as_the_page_and_the_palette_on_its_own() {
        assert_eq!(Scroller::Rows.id(), PAGE);
        assert_ne!(Scroller::Palette.id(), PAGE);
    }
}
