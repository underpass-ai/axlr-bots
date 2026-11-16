use iced::Size;
use iced::advanced::image::Handle;

use super::{Field, Glow, Spread, Tint};

/// One frame of a surface's glow, ready to draw. A surface keeps it and
/// draws it again until the next frame, so drawing between two frames
/// costs nothing.
#[derive(Debug, Clone)]
pub(super) struct Picture {
    /// The frame it shows.
    pub frame: u64,
    /// The size of the surface it was made for, in points.
    pub surface: Size,
    pub tint: Tint,
    pub handle: Handle,
    /// The points it takes on the screen from the surface's corner.
    pub covers: Size,
}

impl Picture {
    /// Paints the frame for a surface of this size with corners of this
    /// radius. `grid` is the buffer the small field is painted in; it is
    /// kept from frame to frame. None for a surface that is not there.
    pub fn paint(frame: u64, surface: Size, radius: f32, glow: &Glow, grid: &mut Vec<u8>) -> Option<Self> {
        let spread = Spread::over(surface, radius)?;
        let field = Field::fitting(glow.aspect);
        field.paint(glow, grid);
        let pixels = spread.stretch(field, grid);
        Some(Self {
            frame,
            surface,
            tint: glow.tint,
            handle: Handle::from_rgba(spread.width(), spread.height(), pixels),
            covers: spread.covers(),
        })
    }

    /// Whether it still is the picture of this frame.
    pub fn shows(&self, frame: u64, surface: Size, tint: Tint) -> bool {
        self.frame == frame && self.surface == surface && self.tint == tint
    }
}

#[cfg(test)]
mod tests {
    use std::time::Instant;

    use iced::Point;

    use super::*;
    use crate::ui::Tokens;
    use crate::ui::organic::Halo;

    fn glow(surface: Size) -> Glow {
        Glow {
            aspect: surface.width / surface.height,
            time: 3.0,
            pointer: Point::new(0.5, 0.5),
            tint: Tint::of(Tokens::LIGHT),
            halo: Halo::of(surface),
            fade: 1.0,
        }
    }

    #[test]
    fn is_the_picture_of_its_frame_surface_and_theme_only() {
        let surface = Size::new(300.0, 36.0);
        let mut grid = Vec::new();
        let picture = Picture::paint(7, surface, 0.0, &glow(surface), &mut grid).unwrap();
        assert_eq!(grid.len(), 56 * 28 * 4, "the small field stays in the buffer");
        assert_eq!(picture.covers, Size::new(301.0, 37.0));
        let light = Tint::of(Tokens::LIGHT);
        assert!(picture.shows(7, surface, light));
        assert!(!picture.shows(8, surface, light));
        assert!(!picture.shows(7, Size::new(301.0, 36.0), light));
        assert!(!picture.shows(7, surface, Tint::of(Tokens::DARK)));
        assert!(Picture::paint(7, Size::new(0.0, 36.0), 0.0, &glow(surface), &mut grid).is_none());
    }

    /// How long a frame's picture takes to paint, for the surfaces the
    /// panel has. Not part of the suite: it prints, and only an optimised
    /// build tells the truth
    /// (`cargo test --release organic_cost -- --ignored --nocapture`).
    #[test]
    #[ignore = "a measurement; run it with --release --ignored --nocapture"]
    fn organic_cost() {
        for (name, surface) in [
            ("tile 140x24", Size::new(140.0, 24.0)),
            ("row 1290x36", Size::new(1290.0, 36.0)),
            ("header 640x130", Size::new(640.0, 130.0)),
            ("card 420x300", Size::new(420.0, 300.0)),
            ("column 420x700", Size::new(420.0, 700.0)),
            ("window 1360x860", Size::new(1360.0, 860.0)),
        ] {
            let mut grid = Vec::new();
            let rounds = 200;
            let start = Instant::now();
            let mut bytes = 0;
            for frame in 0..rounds {
                let picture = Picture::paint(frame, surface, 8.0, &glow(surface), &mut grid).unwrap();
                if let Handle::Rgba { pixels, .. } = &picture.handle {
                    bytes = pixels.len();
                }
            }
            let each = start.elapsed() / rounds as u32;
            println!("{name}: {each:?} a frame, {} KiB", bytes / 1024);
            assert!(each.as_millis() < 100, "{name} takes {each:?}");
        }
    }
}
