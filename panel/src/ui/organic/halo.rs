use iced::Size;

/// How strong a glow is right under the pointer and how far it reaches.
/// A big surface (a card, the inspector's header) gets a faint, tight
/// one; a small one (a row, a tile) a stronger one that covers it.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct Halo {
    /// The glow's opacity under the pointer, where its waves are highest.
    pub peak: f32,
    /// How far from the pointer the glow ends, in heights of the surface
    /// (in half widths, on a surface more than twice as wide as tall).
    pub reach: f32,
}

impl Halo {
    /// On a card.
    pub const WIDE: Halo = Halo {
        peak: 0.16,
        reach: 0.42,
    };

    /// On a row or a tile. The design's sketch says 0.24; at 0.20 the
    /// muted words still read (4.5 to 1) on the page, on a surface and
    /// on a raised band, in both themes, where the glow is strongest.
    pub const NEAR: Halo = Halo { peak: 0.20, reach: 0.9 };

    /// A surface with more square pixels than this is a big one.
    pub const BIG: f32 = 120_000.0;

    /// The halo that fits a surface of this size.
    pub fn of(size: Size) -> Self {
        if size.width * size.height > Self::BIG {
            Self::WIDE
        } else {
            Self::NEAR
        }
    }
}

#[cfg(test)]
mod tests {
    use iced::Color;

    use super::*;
    use crate::ui::Tokens;
    use crate::ui::organic::Tint;

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

    /// The ground with the glow's colour over it at this opacity, as the
    /// renderers blend it.
    fn under(ground: Color, [r, g, b]: [u8; 3], alpha: f32) -> Color {
        let over = |ground: f32, glow: u8| ground * (1.0 - alpha) + f32::from(glow) / 255.0 * alpha;
        Color::from_rgb(over(ground.r, r), over(ground.g, g), over(ground.b, b))
    }

    #[test]
    fn a_big_surface_gets_the_faint_halo_and_a_small_one_the_near_one() {
        assert_eq!(Halo::of(Size::new(640.0, 300.0)), Halo::WIDE);
        assert_eq!(Halo::of(Size::new(1290.0, 36.0)), Halo::NEAR);
        assert_eq!(Halo::of(Size::new(140.0, 24.0)), Halo::NEAR);
        let (card, tile) = (Halo::of(Size::new(640.0, 300.0)), Halo::of(Size::new(140.0, 24.0)));
        assert!(card.peak < tile.peak && card.reach < tile.reach);
    }

    /// The words stay readable where the glow is strongest: whatever
    /// the waves' height there, on every ground a surface may have.
    #[test]
    fn words_read_over_the_strongest_glow_in_both_themes() {
        for tokens in [Tokens::LIGHT, Tokens::DARK] {
            let tint = Tint::of(tokens);
            for halo in [Halo::WIDE, Halo::NEAR] {
                for ground in [tokens.page, tokens.surface, tokens.raised] {
                    for step in 0..=100 {
                        let n = step as f32 / 100.0;
                        let lit = under(ground, tint.at(n), halo.peak * (0.45 + 0.55 * n));
                        assert!(
                            contrast(tokens.muted, lit) >= 4.5,
                            "muted on {ground:?} at {n}, peak {}, dark={}",
                            halo.peak,
                            tokens.dark
                        );
                        assert!(contrast(tokens.text, lit) >= 7.0);
                    }
                }
            }
        }
    }
}
