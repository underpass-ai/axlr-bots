use iced::Length;

use super::Head;
use crate::ui::view::widgets::El;

/// One column of a list: what stands over it, how wide it is, and what
/// it shows of a row. A cell is given the width its column got, so it
/// can cut its words to fit.
pub struct Column<'a, T> {
    pub(super) head: Head<'a>,
    pub(super) width: Length,
    /// Its words end at its right edge: a time, an age.
    pub(super) end: bool,
    pub(super) cell: Box<dyn Fn(T, f32) -> El<'a> + 'a>,
}

impl<'a, T> Column<'a, T> {
    /// A column of a width in pixels.
    pub fn fixed(head: Head<'a>, width: f32, cell: impl Fn(T, f32) -> El<'a> + 'a) -> Self {
        Self {
            head,
            width: Length::Fixed(width),
            end: false,
            cell: Box::new(cell),
        }
    }

    /// A column that shares what the fixed ones leave, by `portion`.
    pub fn share(head: Head<'a>, portion: u16, cell: impl Fn(T, f32) -> El<'a> + 'a) -> Self {
        Self {
            head,
            width: Length::FillPortion(portion),
            end: false,
            cell: Box::new(cell),
        }
    }

    /// Its head and its cells end at its right edge.
    pub fn end(mut self) -> Self {
        self.end = true;
        self
    }
}
