use iced::{Color, Font};

use super::widgets::{BODY, BOLD, REGULAR, SMALL};
use crate::ui::Tokens;

/// How a reader's words look, so they sit level with the texts around
/// them: body prose, small prose, a muted line, an id in mono, a heading.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Voice {
    Body,
    Small,
    Muted,
    Mono,
    Heading,
}

impl Voice {
    pub fn font(self) -> Font {
        match self {
            Self::Mono => Font::MONOSPACE,
            Self::Heading => BOLD,
            Self::Body | Self::Small | Self::Muted => REGULAR,
        }
    }

    pub fn size(self) -> f32 {
        match self {
            Self::Body => BODY,
            Self::Heading => 22.0,
            Self::Small | Self::Muted | Self::Mono => SMALL,
        }
    }

    pub fn color(self, tokens: Tokens) -> Color {
        match self {
            Self::Muted => tokens.muted,
            _ => tokens.text,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn each_voice_matches_its_neighbours() {
        let tokens = Tokens::LIGHT;
        assert_eq!(Voice::Body.size(), BODY);
        assert_eq!(Voice::Muted.color(tokens), tokens.muted);
        assert_eq!(Voice::Mono.font(), Font::MONOSPACE);
        assert_eq!(Voice::Heading.font(), BOLD);
        assert_eq!(Voice::Small.color(tokens), tokens.text);
    }
}
