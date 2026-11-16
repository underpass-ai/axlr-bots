use std::time::{Duration, Instant};

use iced::Point;

use super::Surface;

/// A surface that glows: the one under the pointer, fading in, or one
/// the pointer left, fading out.
#[derive(Debug, Clone, Copy, PartialEq)]
pub(super) struct Ember {
    pub surface: Surface,
    pub pointer: Point,
    /// The surface's size in square points: of two surfaces under the
    /// pointer, one inside the other, the smaller one glows.
    pub area: f32,
    /// How far in the fade is, from 0 to 1, at an even pace.
    pub level: f32,
    /// When the surface last told about the pointer.
    pub heard: Instant,
}

impl Ember {
    /// Brighter by the share of the fade in that `passed` is.
    pub fn brighten(&mut self, passed: Duration, fade_in: Duration) {
        self.level = (self.level + passed.as_secs_f32() / fade_in.as_secs_f32()).min(1.0);
    }

    /// Dimmer by the share of the fade out that `passed` is.
    pub fn dim(&mut self, passed: Duration, fade_out: Duration) {
        self.level = (self.level - passed.as_secs_f32() / fade_out.as_secs_f32()).max(0.0);
    }

    pub fn is_out(&self) -> bool {
        self.level <= 0.0
    }

    /// The fade as it is drawn: slow at both ends.
    pub fn eased(&self) -> f32 {
        self.level * self.level * (3.0 - 2.0 * self.level)
    }
}
