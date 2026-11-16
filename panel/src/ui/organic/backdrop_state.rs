use std::time::Instant;

use iced::{Point, Rectangle};

use super::Surface;
use super::picture::Picture;

/// What a backdrop remembers between two events: where the pointer was,
/// what it last told about it, and the picture it draws.
#[derive(Debug, Default)]
pub(super) struct BackdropState {
    /// The surface it stood for: a list that changes may put another
    /// surface in this place.
    pub surface: Option<Surface>,
    pub cursor: Option<Point>,
    pub bounds: Rectangle,
    pub hovered: bool,
    /// When it last told where the pointer is.
    pub told: Option<Instant>,
    /// The pointer moved since, too soon to tell: the next frame does.
    pub owed: bool,
    pub picture: Option<Picture>,
    /// The small field's pixels, kept so each frame needs no new buffer.
    pub grid: Vec<u8>,
}
