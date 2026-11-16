use iced::Font;
use iced::widget::text;

use crate::ui::Tokens;
use crate::ui::view::widgets::{BODY, SEMIBOLD, SMALL};

/// How a cell's words are written: a name, plain words, a quiet aside
/// or a version in the fixed face.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Ink {
    /// A row's name: the bot, the node.
    Strong,
    Plain,
    /// Small and muted: a host, an age.
    Quiet,
    /// Small, muted, in the fixed face: a version.
    Code,
}

impl Ink {
    pub fn size(self) -> f32 {
        match self {
            Self::Strong | Self::Plain => BODY,
            Self::Quiet => SMALL,
            Self::Code => 12.5,
        }
    }

    /// The width of an average character, in ems: Lexend is a wide
    /// face, wider still in its semibold; the fixed face is 0.6.
    fn em(self) -> f32 {
        match self {
            Self::Strong => 0.56,
            Self::Plain | Self::Quiet => 0.52,
            Self::Code => 0.61,
        }
    }

    /// How many characters fit `width`, four at the least.
    pub fn fits(self, width: f32) -> usize {
        ((width / (self.em() * self.size())).floor() as usize).max(4)
    }

    /// The width `chars` characters take, on average.
    pub fn span(self, chars: usize) -> f32 {
        chars as f32 * self.em() * self.size()
    }

    /// The words in this ink.
    pub fn write<'a>(self, words: impl text::IntoFragment<'a>, tokens: Tokens) -> text::Text<'a> {
        let line = text(words).size(self.size()).wrapping(text::Wrapping::None);
        match self {
            Self::Strong => line.font(SEMIBOLD),
            Self::Plain => line,
            Self::Quiet => line.color(tokens.muted),
            Self::Code => line.font(Font::MONOSPACE).color(tokens.muted),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_wider_cell_fits_more_and_a_name_fewer() {
        assert!(Ink::Plain.fits(300.0) > Ink::Plain.fits(100.0));
        assert!(Ink::Strong.fits(300.0) < Ink::Plain.fits(300.0));
        assert!(Ink::Quiet.fits(300.0) > Ink::Plain.fits(300.0));
        assert_eq!(Ink::Code.fits(0.0), 4);
        assert_eq!(Ink::Quiet.size(), SMALL);
        assert_eq!(Ink::Plain.fits(Ink::Plain.span(20) + 0.5), 20);
    }
}
