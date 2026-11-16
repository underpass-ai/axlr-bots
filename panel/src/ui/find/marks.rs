use std::ops::Range;

/// The parts of a text a search found, to highlight: byte ranges of the
/// text as it was written, in order, none touching the next, each on
/// whole characters.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct Marks {
    ranges: Vec<Range<usize>>,
}

impl Marks {
    /// Nothing to highlight.
    pub fn none() -> Self {
        Self::default()
    }

    /// The ranges put in order, the ones that touch or overlap made one.
    pub fn of(ranges: impl IntoIterator<Item = Range<usize>>) -> Self {
        let mut ranges: Vec<Range<usize>> = ranges.into_iter().filter(|r| r.start < r.end).collect();
        ranges.sort_by_key(|r| (r.start, r.end));
        let mut merged: Vec<Range<usize>> = Vec::with_capacity(ranges.len());
        for range in ranges {
            match merged.last_mut() {
                Some(last) if range.start <= last.end => last.end = last.end.max(range.end),
                _ => merged.push(range),
            }
        }
        Self { ranges: merged }
    }

    pub fn is_empty(&self) -> bool {
        self.ranges.is_empty()
    }

    pub fn ranges(&self) -> &[Range<usize>] {
        &self.ranges
    }

    /// The text cut where the highlight starts and ends, each piece with
    /// whether it is highlighted: what a row draws, piece by piece. A
    /// range that does not fit the text (the marks of another text) is
    /// left out.
    pub fn pieces<'a>(&self, text: &'a str) -> Vec<(&'a str, bool)> {
        let mut pieces = Vec::new();
        let mut done = 0;
        for range in &self.ranges {
            let (Some(before), Some(marked)) = (text.get(done..range.start), text.get(range.clone())) else {
                continue;
            };
            if !before.is_empty() {
                pieces.push((before, false));
            }
            pieces.push((marked, true));
            done = range.end;
        }
        match text.get(done..) {
            Some(rest) if !rest.is_empty() => pieces.push((rest, false)),
            _ => {}
        }
        pieces
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn orders_and_joins_its_ranges() {
        let marks = Marks::of([8..10, 0..2, 2..4, 9..12, 5..5]);
        assert_eq!(marks.ranges(), &[0..4, 8..12]);
        assert!(!marks.is_empty());
        assert!(Marks::none().is_empty());
        assert!(Marks::of(Some(3..3)).is_empty());
    }

    #[test]
    fn cuts_a_text_into_plain_and_highlighted_pieces() {
        let marks = Marks::of([4..7, 11..13]);
        assert_eq!(
            marks.pieces("the fox is on it"),
            vec![
                ("the ", false),
                ("fox", true),
                (" is ", false),
                ("on", true),
                (" it", false)
            ]
        );
        assert_eq!(Marks::of(Some(0..3)).pieces("fox"), vec![("fox", true)]);
        assert_eq!(Marks::none().pieces("fox"), vec![("fox", false)]);
        assert!(Marks::none().pieces("").is_empty());
    }

    #[test]
    fn leaves_out_a_range_that_does_not_fit_the_text() {
        let marks = Marks::of([1..2, 40..44]);
        assert_eq!(marks.pieces("abc"), vec![("a", false), ("b", true), ("c", false)]);
        let inside_a_letter = Marks::of(Some(1..2));
        assert_eq!(inside_a_letter.pieces("éa"), vec![("éa", false)]);
    }
}
