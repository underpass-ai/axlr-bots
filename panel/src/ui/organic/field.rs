use super::Glow;

/// A small grid of pixels painted with slow, warped waves, brightest
/// under the pointer and clear far from it. It is drawn stretched and
/// smooth behind a surface, so a few pixels are enough.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Field {
    pub width: u32,
    pub height: u32,
}

impl Field {
    /// The grid the design asks for.
    pub const SMALL: Field = Field { width: 56, height: 28 };

    /// The grid's pixels for this frame, row by row from the top, four
    /// bytes each: red, green, blue and opacity (not multiplied in).
    pub fn pixels(&self, glow: &Glow) -> Vec<u8> {
        let mut pixels = Vec::new();
        self.paint(glow, &mut pixels);
        pixels
    }

    /// The same, into a buffer that is kept from frame to frame.
    ///
    /// A pixel stands for its centre. Two waves, each bent by the other
    /// axis, give a height `n` from 0 to 1 that picks the colour between
    /// the tint's ends and thins the glow where it is low; the distance
    /// to the pointer, in heights of the surface, gives the fall-off.
    /// A clear pixel keeps its colour, so stretching never greys an edge.
    pub fn paint(&self, glow: &Glow, pixels: &mut Vec<u8>) {
        let Glow {
            aspect,
            time,
            pointer,
            tint,
            halo,
            fade,
        } = *glow;
        let reach = halo.reach * (aspect * 0.5).max(1.0);
        let strength = halo.peak * fade.clamp(0.0, 1.0);
        pixels.clear();
        pixels.reserve(self.width as usize * self.height as usize * 4);
        for y in 0..self.height {
            let v = (y as f32 + 0.5) / self.height as f32;
            for x in 0..self.width {
                let u = (x as f32 + 0.5) / self.width as f32;
                let wx = u * aspect * 1.6 + 0.35 * (v * 5.1 + time * 0.55).sin();
                let wy = v * 1.6 + 0.35 * (u * aspect * 4.3 - time * 0.4).cos();
                let n = 0.5
                    + 0.25 * (wx * 3.1 + time * 0.6).sin()
                    + 0.25 * (wy * 3.7 - time * 0.45 + (wx * 2.2 + time * 0.3).sin()).sin();
                let (dx, dy) = ((u - pointer.x) * aspect, v - pointer.y);
                let fall = (1.0 - (dx * dx + dy * dy).sqrt() / reach).max(0.0);
                let alpha = strength * fall * fall * (0.45 + 0.55 * n);
                pixels.extend_from_slice(&tint.at(n));
                pixels.push((alpha * 255.0).round().clamp(0.0, 255.0) as u8);
            }
        }
    }
}

impl Default for Field {
    fn default() -> Self {
        Self::SMALL
    }
}

#[cfg(test)]
mod tests {
    use iced::Point;

    use super::*;
    use crate::ui::Tokens;
    use crate::ui::organic::{Halo, Tint};

    fn glow() -> Glow {
        Glow {
            aspect: 2.0,
            time: 3.5,
            pointer: Point::new(0.5, 0.5),
            tint: Tint::of(Tokens::LIGHT),
            halo: Halo::WIDE,
            fade: 1.0,
        }
    }

    /// The opacity of the pixel at this column and row.
    fn alpha(field: Field, pixels: &[u8], x: u32, y: u32) -> u8 {
        pixels[((y * field.width + x) * 4 + 3) as usize]
    }

    fn most(halo: Halo, fade: f32) -> u8 {
        (halo.peak * fade * 255.0).ceil() as u8
    }

    #[test]
    fn has_four_bytes_for_every_pixel_of_the_grid() {
        assert_eq!(Field::default(), Field::SMALL);
        assert_eq!(Field::SMALL.pixels(&glow()).len(), 56 * 28 * 4);
        let odd = Field { width: 9, height: 5 };
        assert_eq!(odd.pixels(&glow()).len(), 9 * 5 * 4);
    }

    #[test]
    fn is_never_stronger_than_the_peak_times_the_fade() {
        for halo in [Halo::WIDE, Halo::NEAR] {
            for fade in [0.25, 0.5, 1.0] {
                for time in [0.0, 1.7, 40.0, 125.0] {
                    for aspect in [0.6, 2.0, 36.0] {
                        let pixels = Field::SMALL.pixels(&Glow {
                            halo,
                            fade,
                            time,
                            aspect,
                            ..glow()
                        });
                        let strongest = pixels.chunks_exact(4).map(|pixel| pixel[3]).max().unwrap();
                        assert!(strongest <= most(halo, fade), "{strongest} at fade {fade}");
                        assert!(strongest > 0, "something shows at fade {fade}");
                    }
                }
            }
        }
    }

    #[test]
    fn is_brightest_near_the_pointer_and_clear_far_from_it() {
        let field = Field::SMALL;
        for (pointer, far) in [
            (Point::new(0.2, 0.3), (55, 27)),
            (Point::new(0.8, 0.7), (0, 0)),
            (Point::new(0.5, 0.5), (0, 27)),
        ] {
            let pixels = field.pixels(&Glow { pointer, ..glow() });
            let brightest = (0..field.height)
                .flat_map(|y| (0..field.width).map(move |x| (x, y)))
                .max_by_key(|&(x, y)| alpha(field, &pixels, x, y))
                .unwrap();
            let at = (
                (pointer.x * field.width as f32) as i64,
                (pointer.y * field.height as f32) as i64,
            );
            // Within the waves' reach of the pointer: they thin the glow
            // by up to a half, so its crest may sit a few pixels away.
            assert!(
                (brightest.0 as i64 - at.0).abs() <= 6 && (brightest.1 as i64 - at.1).abs() <= 6,
                "brightest at {brightest:?}, pointer at {at:?}"
            );
            assert!(alpha(field, &pixels, at.0 as u32, at.1 as u32) >= 16);
            assert_eq!(alpha(field, &pixels, far.0, far.1), 0);
        }
    }

    #[test]
    fn a_row_glows_along_its_width_and_a_card_only_around_the_pointer() {
        let field = Field::SMALL;
        let card = field.pixels(&glow());
        assert_eq!(alpha(field, &card, 2, 14), 0);
        let row = field.pixels(&Glow {
            aspect: 36.0,
            halo: Halo::NEAR,
            ..glow()
        });
        assert!(alpha(field, &row, 20, 14) > 0);
        assert_eq!(alpha(field, &row, 0, 14), 0);
    }

    #[test]
    fn the_same_frame_is_the_same_pixels() {
        let field = Field::SMALL;
        assert_eq!(field.pixels(&glow()), field.pixels(&glow()));
        let mut kept = vec![7; 3];
        field.paint(&glow(), &mut kept);
        assert_eq!(kept, field.pixels(&glow()));
        assert_ne!(field.pixels(&glow()), field.pixels(&Glow { time: 4.5, ..glow() }));
    }

    #[test]
    fn nothing_shows_before_it_fades_in() {
        let pixels = Field::SMALL.pixels(&Glow { fade: 0.0, ..glow() });
        assert!(pixels.chunks_exact(4).all(|pixel| pixel[3] == 0));
    }

    /// Stretching blends a clear pixel with its neighbours: it must
    /// carry the tint, not black.
    #[test]
    fn a_clear_pixel_keeps_the_tint() {
        let tint = Tint::of(Tokens::DARK);
        let pixels = Field::SMALL.pixels(&Glow {
            tint,
            fade: 0.0,
            ..glow()
        });
        for pixel in pixels.chunks_exact(4) {
            assert!(pixel[0] >= 0x9F && pixel[0] <= 0xE0, "red {}", pixel[0]);
            assert!(pixel[2] >= 0x5A && pixel[2] <= 0xCE, "blue {}", pixel[2]);
        }
    }

    #[test]
    fn the_colour_runs_between_the_two_ends_of_the_tint() {
        let pixels = Field::SMALL.pixels(&glow());
        let reds: Vec<u8> = pixels.chunks_exact(4).map(|pixel| pixel[0]).collect();
        assert!(reds.iter().all(|red| (0x44..=0xB8).contains(red)));
        assert!(
            reds.iter().max().unwrap() - reds.iter().min().unwrap() > 40,
            "it is not one flat colour"
        );
    }
}
