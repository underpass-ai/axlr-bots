use iced::Point;

use super::{Halo, Tint};

/// One frame of a field, as numbers: the shape of the surface it is drawn
/// on, the moment, where the pointer is and how strong the glow is now.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct Glow {
    /// The surface's width over its height.
    pub aspect: f32,
    /// Seconds: the waves drift as it grows.
    pub time: f32,
    /// Where the pointer is on the surface, each axis from 0 to 1.
    pub pointer: Point,
    pub tint: Tint,
    pub halo: Halo,
    /// How far the glow has faded in, from 0 (nothing) to 1 (whole).
    pub fade: f32,
}
