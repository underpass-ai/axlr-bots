use iced::Color;

use crate::ui::Tokens;

/// The two colours a field runs between: the brand's copper where its
/// waves are low, the calm slate of work in progress where they are high.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct Tint {
    pub from: Color,
    pub to: Color,
}

impl Tint {
    /// The tint of a theme: its accent and its busy slate.
    pub fn of(tokens: Tokens) -> Self {
        Self {
            from: tokens.accent,
            to: tokens.busy,
        }
    }

    /// The colour `n` of the way from one end to the other (0 to 1), as
    /// the three bytes of a pixel.
    pub fn at(&self, n: f32) -> [u8; 3] {
        let channel = |from: f32, to: f32| ((from + (to - from) * n) * 255.0).round().clamp(0.0, 255.0) as u8;
        [
            channel(self.from.r, self.to.r),
            channel(self.from.g, self.to.g),
            channel(self.from.b, self.to.b),
        ]
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn runs_from_the_accent_to_the_busy_slate() {
        let tint = Tint::of(Tokens::LIGHT);
        assert_eq!(tint.at(0.0), [0xB8, 0x5C, 0x38]);
        assert_eq!(tint.at(1.0), [0x44, 0x62, 0x7D]);
        let dark = Tint::of(Tokens::DARK);
        assert_eq!(dark.at(0.0), [0xE0, 0x8A, 0x5A]);
        assert_eq!(dark.at(1.0), [0x9F, 0xB7, 0xCE]);
        let [r, g, b] = tint.at(0.5);
        assert!(r > 0x44 && r < 0xB8 && g > 0x5C && g < 0x62 && b > 0x38 && b < 0x7D);
    }
}
