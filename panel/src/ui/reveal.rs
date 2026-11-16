use iced::advanced::widget::operation::scrollable::{AbsoluteOffset, Scrollable};
use iced::advanced::widget::{Id, Operation};
use iced::{Rectangle, Vector};

/// Scrolls one scrollable just enough that a span of its content is in
/// sight: up when the span starts above what shows, down when it ends
/// below, not at all when it already shows. What the arrows need, so a
/// row the cursor reaches never hides under the list's edge and the
/// list does not jump when it is in sight.
#[derive(Debug, Clone)]
pub struct Reveal {
    target: Id,
    /// Where the span starts in the content, in pixels.
    top: f32,
    height: f32,
}

impl Reveal {
    pub fn new(target: impl Into<Id>, top: f32, height: f32) -> Self {
        Self {
            target: target.into(),
            top,
            height,
        }
    }

    /// Where the content should be scrolled to for the span to show in
    /// a viewport `height` high now scrolled to `at`; none when it shows.
    pub fn offset(&self, at: f32, height: f32) -> Option<f32> {
        if self.top < at {
            Some(self.top)
        } else if self.top + self.height > at + height {
            Some((self.top + self.height - height).max(0.0))
        } else {
            None
        }
    }
}

impl Operation for Reveal {
    fn traverse(&mut self, operate: &mut dyn FnMut(&mut dyn Operation<()>)) {
        operate(self);
    }

    fn scrollable(
        &mut self,
        id: Option<&Id>,
        bounds: Rectangle,
        _content_bounds: Rectangle,
        translation: Vector,
        state: &mut dyn Scrollable,
    ) {
        if id != Some(&self.target) {
            return;
        }
        if let Some(y) = self.offset(translation.y, bounds.height) {
            state.scroll_to(AbsoluteOffset { x: None, y: Some(y) });
        }
    }
}

#[cfg(test)]
mod tests {
    use iced::advanced::widget::operation::scrollable::RelativeOffset;

    use super::*;

    /// A scrollable that keeps where it was asked to go.
    #[derive(Default)]
    struct Stub {
        to: Option<AbsoluteOffset<Option<f32>>>,
    }

    impl Scrollable for Stub {
        fn snap_to(&mut self, _: RelativeOffset<Option<f32>>) {}

        fn scroll_to(&mut self, offset: AbsoluteOffset<Option<f32>>) {
            self.to = Some(offset);
        }

        fn scroll_by(&mut self, _: AbsoluteOffset, _: Rectangle, _: Rectangle) {}
    }

    fn ask(reveal: &mut Reveal, id: &'static str, at: f32) -> Option<Option<f32>> {
        let mut stub = Stub::default();
        let viewport = Rectangle::new(iced::Point::ORIGIN, iced::Size::new(400.0, 360.0));
        let content = Rectangle::new(iced::Point::ORIGIN, iced::Size::new(400.0, 7200.0));
        reveal.scrollable(Some(&Id::new(id)), viewport, content, Vector::new(0.0, at), &mut stub);
        stub.to.map(|to| to.y)
    }

    #[test]
    fn a_span_in_sight_scrolls_nothing() {
        let mut reveal = Reveal::new("rows", 72.0, 36.0);
        assert_eq!(ask(&mut reveal, "rows", 0.0), None);
        assert_eq!(reveal.offset(72.0, 360.0), None, "at the very top of what shows");
        let foot = Reveal::new("rows", 324.0, 36.0);
        assert_eq!(foot.offset(0.0, 360.0), None, "at the very foot of what shows");
    }

    #[test]
    fn a_span_below_scrolls_down_until_its_foot_shows() {
        let mut reveal = Reveal::new("rows", 720.0, 36.0);
        assert_eq!(ask(&mut reveal, "rows", 0.0), Some(Some(720.0 + 36.0 - 360.0)));
    }

    #[test]
    fn a_span_above_scrolls_up_to_its_top() {
        let mut reveal = Reveal::new("rows", 36.0, 36.0);
        assert_eq!(ask(&mut reveal, "rows", 400.0), Some(Some(36.0)));
    }

    #[test]
    fn another_scrollable_is_left_alone() {
        let mut reveal = Reveal::new("rows", 720.0, 36.0);
        assert_eq!(ask(&mut reveal, "palette", 0.0), None);
        let mut visited = 0;
        reveal.traverse(&mut |_| visited += 1);
        assert_eq!(visited, 1, "it goes into every widget that holds others");
    }
}
