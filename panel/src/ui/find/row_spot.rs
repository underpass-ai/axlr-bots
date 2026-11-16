/// Where a row is drawn in a list that scrolls, counted in lines: how
/// many rows and how many headers come before it. The view turns it
/// into pixels with its own heights, to scroll the row into sight.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub struct RowSpot {
    /// The rows above it.
    pub rows: usize,
    /// The group or section headers above it, its own included.
    pub headers: usize,
}

impl RowSpot {
    /// Its top in pixels, in a list whose rows are `row` high and whose
    /// headers are `header` high, under `top` pixels of room.
    pub fn top(self, top: f32, row: f32, header: f32) -> f32 {
        top + self.rows as f32 * row + self.headers as f32 * header
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn counts_the_rows_and_the_headers_above_in_pixels() {
        assert_eq!(RowSpot::default().top(0.0, 36.0, 28.0), 0.0);
        let spot = RowSpot { rows: 3, headers: 2 };
        assert_eq!(spot.top(6.0, 36.0, 28.0), 6.0 + 108.0 + 56.0);
    }
}
