use std::time::Instant;

use iced::{Point, Size};

use super::Surface;

/// What a surface tells about the pointer: it is over it, here, or it
/// has left. The surface says when, so the state that follows the hover
/// needs no clock of its own.
#[derive(Debug, Clone, Copy, PartialEq)]
pub enum Signal {
    Over {
        surface: Surface,
        /// Where on the surface, each axis from 0 to 1.
        at: Point,
        /// The surface's size, in points.
        size: Size,
        when: Instant,
    },
    Left {
        surface: Surface,
        when: Instant,
    },
}
