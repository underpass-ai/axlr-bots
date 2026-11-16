use iced::advanced::widget::operation::{Outcome, scrollable::Scrollable};
use iced::advanced::widget::{Id, Operation};
use iced::{Rectangle, Vector};

use super::Reveal;

/// Brings a part of a scrollable into sight by its id, however tall the
/// parts before it are: a first pass reads where the part is drawn in
/// the scrollable's content, then a `Reveal` scrolls just enough. What a
/// find needs to show its match among lines of many heights.
#[derive(Debug, Clone)]
pub struct RevealChild {
    scroller: Id,
    target: Id,
    /// Where the scrollable's content starts, once met.
    content: Option<f32>,
    /// The part's top and height in the content, once met.
    found: Option<(f32, f32)>,
}

impl RevealChild {
    pub fn new(scroller: impl Into<Id>, target: impl Into<Id>) -> Self {
        Self {
            scroller: scroller.into(),
            target: target.into(),
            content: None,
            found: None,
        }
    }
}

impl Operation for RevealChild {
    fn traverse(&mut self, operate: &mut dyn FnMut(&mut dyn Operation<()>)) {
        operate(self);
    }

    fn scrollable(
        &mut self,
        id: Option<&Id>,
        _bounds: Rectangle,
        content_bounds: Rectangle,
        _translation: Vector,
        _state: &mut dyn Scrollable,
    ) {
        if id == Some(&self.scroller) {
            self.content = Some(content_bounds.y);
        }
    }

    fn container(&mut self, id: Option<&Id>, bounds: Rectangle) {
        if id != Some(&self.target) {
            return;
        }
        if let Some(top) = self.content {
            self.found = Some((bounds.y - top, bounds.height));
        }
    }

    fn finish(&self) -> Outcome<()> {
        match self.found {
            Some((top, height)) => Outcome::Chain(Box::new(Reveal::new(self.scroller.clone(), top, height))),
            None => Outcome::None,
        }
    }
}

#[cfg(test)]
mod tests {
    use iced::advanced::widget::operation::scrollable::{AbsoluteOffset, RelativeOffset};

    use super::*;

    #[derive(Default)]
    struct Stub;

    impl Scrollable for Stub {
        fn snap_to(&mut self, _: RelativeOffset<Option<f32>>) {}
        fn scroll_to(&mut self, _: AbsoluteOffset<Option<f32>>) {}
        fn scroll_by(&mut self, _: AbsoluteOffset, _: Rectangle, _: Rectangle) {}
    }

    fn rect(y: f32, height: f32) -> Rectangle {
        Rectangle::new(iced::Point::new(0.0, y), iced::Size::new(400.0, height))
    }

    #[test]
    fn finds_the_part_in_its_scrollables_content_then_reveals_it() {
        let mut op = RevealChild::new("steps", "step-7");
        assert!(matches!(op.finish(), Outcome::None), "nothing met yet");
        op.container(Some(&Id::new("step-7")), rect(500.0, 30.0));
        assert!(
            matches!(op.finish(), Outcome::None),
            "a part before its scrollable is not it"
        );
        op.scrollable(
            Some(&Id::new("other")),
            rect(0.0, 300.0),
            rect(40.0, 900.0),
            Vector::ZERO,
            &mut Stub,
        );
        op.scrollable(
            Some(&Id::new("steps")),
            rect(100.0, 300.0),
            rect(100.0, 900.0),
            Vector::ZERO,
            &mut Stub,
        );
        op.container(Some(&Id::new("step-6")), rect(440.0, 30.0));
        op.container(Some(&Id::new("step-7")), rect(470.0, 60.0));
        assert_eq!(op.found, Some((370.0, 60.0)));
        assert!(matches!(op.finish(), Outcome::Chain(_)));
    }
}
