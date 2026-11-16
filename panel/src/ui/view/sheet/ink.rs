use iced::widget::{rich_text, span, text};
use iced::{Color, Font};

use crate::ui::Tokens;
use crate::ui::find::{Marks, Query};
use crate::ui::styles;
use crate::ui::view::widgets::{BODY, El, SEMIBOLD, SMALL};

/// How a cell's words are written: a name, plain words, a quiet aside
/// or a version in the fixed face.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Ink {
    /// A row's name: the bot, the node.
    Strong,
    Plain,
    /// Plain words in the muted ink: what follows a task's title.
    Muted,
    /// Small and muted: a host, an age.
    Quiet,
    /// Small, muted, in the fixed face: a version.
    Code,
}

impl Ink {
    pub fn size(self) -> f32 {
        match self {
            Self::Strong | Self::Plain | Self::Muted => BODY,
            Self::Quiet => SMALL,
            Self::Code => 12.5,
        }
    }

    /// The width of an average character, in ems: Lexend is a wide
    /// face, wider still in its semibold; the fixed face is 0.6.
    fn em(self) -> f32 {
        match self {
            Self::Strong => 0.56,
            Self::Plain | Self::Muted | Self::Quiet => 0.52,
            Self::Code => 0.61,
        }
    }

    /// How many characters fit `width`, four at the least.
    pub fn fits(self, width: f32) -> usize {
        ((width / (self.em() * self.size())).floor() as usize).max(4)
    }

    /// The words cut with an ellipsis to what `width` holds, measured
    /// letter by letter: a capital, a digit or an `m` takes more room
    /// than an `i` or a space, so a line of ids is cut sooner than a
    /// line of words and ends in its ellipsis, never at the edge.
    pub fn cut(self, words: &str, width: f32) -> String {
        let size = self.size();
        let wide = |c: char| self.letter(c) * size;
        if words.chars().map(wide).sum::<f32>() <= width {
            return words.to_string();
        }
        let room = width - wide('…');
        let mut used = 0.0;
        let mut kept = String::new();
        for c in words.chars() {
            used += wide(c);
            if used > room {
                break;
            }
            kept.push(c);
        }
        let kept = kept.trim_end();
        format!("{kept}…")
    }

    /// The width of one letter in this ink, in ems.
    fn letter(self, c: char) -> f32 {
        if self == Self::Code {
            return 0.61;
        }
        let em = match c {
            ' ' => 0.26,
            'i' | 'l' | 'j' | 'I' | '.' | ',' | ':' | ';' | '\'' | '|' | '!' | '(' | ')' | '[' | ']' | '·' => 0.3,
            'f' | 't' | 'r' => 0.38,
            '-' | '_' => 0.42,
            'm' | 'w' | 'M' | 'W' => 0.82,
            'A'..='Z' | '0'..='9' => 0.64,
            'a'..='z' => 0.55,
            _ => 0.6,
        };
        if self == Self::Strong { em * 1.06 } else { em }
    }

    /// The width `chars` characters take, on average.
    pub fn span(self, chars: usize) -> f32 {
        chars as f32 * self.em() * self.size()
    }

    /// The words in this ink.
    pub fn write<'a>(self, words: impl text::IntoFragment<'a>, tokens: Tokens) -> text::Text<'a> {
        let line = text(words).size(self.size()).wrapping(text::Wrapping::None);
        match self.color(tokens) {
            Some(color) => line.font(self.font()).color(color),
            None => line.font(self.font()),
        }
    }

    /// The words in this ink with the parts a search found on the
    /// mark's wash; plain words when it found none in them.
    pub fn write_found<'a>(self, words: String, query: &Query, tokens: Tokens) -> El<'a> {
        let marks = query.marks(&words);
        self.write_marked(words, &marks, tokens)
    }

    /// The words in this ink with the parts marked on the mark's wash;
    /// a mark past their end (they were cut) is left out.
    pub fn write_marked<'a>(self, words: String, marks: &Marks, tokens: Tokens) -> El<'a> {
        if marks.is_empty() {
            return self.write(words, tokens).into();
        }
        let wash = styles::mark_fill(tokens);
        let spans: Vec<text::Span<'a, (), Font>> = marks
            .pieces(&words)
            .into_iter()
            .map(|(piece, found)| {
                let piece = span(piece.to_string());
                if found { piece.background(wash) } else { piece }
            })
            .collect();
        let line = rich_text(spans)
            .size(self.size())
            .font(self.font())
            .wrapping(text::Wrapping::None);
        match self.color(tokens) {
            Some(color) => line.color(color).into(),
            None => line.into(),
        }
    }

    fn font(self) -> Font {
        match self {
            Self::Strong => SEMIBOLD,
            Self::Code => Font::MONOSPACE,
            Self::Plain | Self::Muted | Self::Quiet => crate::ui::view::widgets::REGULAR,
        }
    }

    /// Its colour, when it is not the page's ink.
    fn color(self, tokens: Tokens) -> Option<Color> {
        match self {
            Self::Strong | Self::Plain => None,
            Self::Muted | Self::Quiet | Self::Code => Some(tokens.muted),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_line_of_ids_is_cut_sooner_than_a_line_of_words() {
        let width = 200.0;
        let words = Ink::Plain.cut("the bot reads a file and says what is in it today", width);
        let ids = Ink::Plain.cut("20261116T000025Z-BOTS-WORK-TEAMS-GATES-7722-ABCDEF", width);
        assert!(words.ends_with('…') && ids.ends_with('…'));
        assert!(ids.chars().count() < words.chars().count());
    }

    #[test]
    fn words_that_fit_stay_whole_and_a_cut_ends_in_its_ellipsis() {
        assert_eq!(Ink::Plain.cut("Say the time", 300.0), "Say the time");
        let cut = Ink::Strong.cut("bot-act-with-approval and more", 120.0);
        assert!(cut.ends_with('…') && !cut.ends_with(" …"), "{cut}");
        assert_eq!(Ink::Code.cut("0123456789", 0.61 * 12.5 * 10.0), "0123456789");
        assert_eq!(Ink::Plain.cut("anything", 0.0), "…");
    }

    #[test]
    fn a_wider_cell_fits_more_and_a_name_fewer() {
        assert!(Ink::Plain.fits(300.0) > Ink::Plain.fits(100.0));
        assert!(Ink::Strong.fits(300.0) < Ink::Plain.fits(300.0));
        assert!(Ink::Quiet.fits(300.0) > Ink::Plain.fits(300.0));
        assert_eq!(Ink::Code.fits(0.0), 4);
        assert_eq!(Ink::Quiet.size(), SMALL);
        assert_eq!(Ink::Plain.fits(Ink::Plain.span(20) + 0.5), 20);
        assert_eq!(Ink::Muted.fits(300.0), Ink::Plain.fits(300.0));
    }

    #[test]
    fn only_a_name_is_semibold_and_only_plain_words_take_the_pages_ink() {
        assert_eq!(Ink::Strong.font(), SEMIBOLD);
        assert_eq!(Ink::Code.font(), Font::MONOSPACE);
        for ink in [Ink::Strong, Ink::Plain] {
            assert_eq!(ink.color(Tokens::LIGHT), None);
        }
        for ink in [Ink::Muted, Ink::Quiet, Ink::Code] {
            assert_eq!(ink.color(Tokens::DARK), Some(Tokens::DARK.muted));
        }
    }
}
