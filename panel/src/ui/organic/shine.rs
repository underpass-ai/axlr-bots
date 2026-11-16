use iced::Point;

/// The glow of one surface at this frame: what `Hover` hands a surface
/// to draw.
#[derive(Debug, Clone, Copy, PartialEq)]
pub struct Shine {
    /// Where the pointer is (or was last) on the surface, each axis from
    /// 0 to 1.
    pub pointer: Point,
    /// How far the glow has faded in, eased, from just above 0 to 1.
    pub fade: f32,
    /// The waves' moment, in seconds.
    pub time: f32,
    /// The number of the frame: it changes when there is a new picture
    /// to draw, and only then.
    pub frame: u64,
}
