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
