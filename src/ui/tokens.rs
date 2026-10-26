use iced::{Color, Theme, color, theme::Palette};

use super::ThemeMode;

/// The colours of one theme: ink on a warm white page (graphite on near
/// black in the dark theme), the brand's copper as the one accent, and
/// one strong colour per status.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct Tokens {
    pub page: Color,
    pub surface: Color,
    pub raised: Color,
    pub border: Color,
    pub text: Color,
    pub muted: Color,
    pub accent: Color,
    pub on_accent: Color,
    pub good: Color,
    pub busy: Color,
    pub wait: Color,
    pub bad: Color,
    pub idle: Color,
    pub dark: bool,
}

impl Tokens {
    pub const LIGHT: Tokens = Tokens {
        page: color!(0xFAFAF8),
        surface: color!(0xFFFFFF),
        raised: color!(0xF4F4F2),
        border: color!(0xE2E2DD),
        text: color!(0x111111),
        muted: color!(0x5B5B57),
        accent: color!(0xB85C38),
        on_accent: color!(0xFFFFFF),
        good: color!(0x2E7D4F),
        busy: color!(0xB85C38),
        wait: color!(0xA65F00),
        bad: color!(0xB3261E),
        idle: color!(0x75665A),
        dark: false,
    };

    pub const DARK: Tokens = Tokens {
        page: color!(0x0F0F10),
        surface: color!(0x161618),
        raised: color!(0x1B1B1E),
        border: color!(0x2E2E33),
        text: color!(0xF4F4F2),
        muted: color!(0xA5A5A0),
        accent: color!(0xE08A5A),
        on_accent: color!(0x111111),
        good: color!(0x6CC28C),
        busy: color!(0xE59A5C),
        wait: color!(0xE8B45A),
        bad: color!(0xF07B6B),
        idle: color!(0xA3A39E),
        dark: true,
    };

    pub fn of(mode: ThemeMode) -> Self {
        match mode {
            ThemeMode::Light => Self::LIGHT,
            ThemeMode::Dark => Self::DARK,
        }
    }

    /// The Iced theme the built-in widgets draw with.
    pub fn theme(&self) -> Theme {
        let name = if self.dark { "AXLR Bots dark" } else { "AXLR Bots light" };
        Theme::custom(
            name,
            Palette {
                background: self.page,
                text: self.text,
                primary: self.accent,
                success: self.good,
                warning: self.wait,
                danger: self.bad,
            },
        )
    }

    /// The colour with its opacity changed.
    pub fn fade(color: Color, alpha: f32) -> Color {
        Color { a: alpha, ..color }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::ui::Tone;
    use crate::ui::styles::{pill_fill, pill_ink};

    /// WCAG relative luminance contrast.
    fn contrast(a: Color, b: Color) -> f32 {
        let lum = |c: Color| {
            let ch = |v: f32| {
                if v <= 0.03928 {
                    v / 12.92
                } else {
                    ((v + 0.055) / 1.055).powf(2.4)
                }
            };
            0.2126 * ch(c.r) + 0.7152 * ch(c.g) + 0.0722 * ch(c.b)
        };
        let (x, y) = (lum(a), lum(b));
        (x.max(y) + 0.05) / (x.min(y) + 0.05)
    }

    #[test]
    fn text_and_status_colours_read_on_both_pages() {
        for tokens in [Tokens::LIGHT, Tokens::DARK] {
            assert!(contrast(tokens.text, tokens.page) >= 7.0);
            assert!(contrast(tokens.text, tokens.surface) >= 7.0);
            assert!(contrast(tokens.muted, tokens.surface) >= 4.5);
            assert!(contrast(tokens.muted, tokens.raised) >= 4.5);
            assert!(contrast(tokens.on_accent, tokens.accent) >= 4.5);
            for status in [tokens.good, tokens.busy, tokens.wait, tokens.bad, tokens.idle] {
                assert!(
                    contrast(status, tokens.surface) >= 4.5,
                    "{status:?} on {:?}",
                    tokens.surface
                );
            }
        }
    }

    /// A pill's word is read on the pill's own tint, not on the surface.
    #[test]
    fn pill_words_read_on_their_tint() {
        for tokens in [Tokens::LIGHT, Tokens::DARK] {
            for tone in [Tone::Good, Tone::Busy, Tone::Wait, Tone::Bad, Tone::Idle] {
                assert!(
                    contrast(pill_ink(tokens, tone), pill_fill(tokens, tone)) >= 4.5,
                    "{tone:?} dark={}",
                    tokens.dark
                );
            }
        }
    }

    #[test]
    fn picks_the_theme_of_the_mode() {
        assert_eq!(Tokens::of(ThemeMode::Dark), Tokens::DARK);
        assert_eq!(
            Tokens::of(ThemeMode::Light).theme().palette().primary,
            Tokens::LIGHT.accent
        );
        assert_eq!(Tokens::fade(Tokens::LIGHT.bad, 0.2).a, 0.2);
    }
}
