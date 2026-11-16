use std::time::Instant;

use iced::advanced::widget::{Operation, Tree, tree};
use iced::advanced::{Clipboard, Layout, Shell, Widget, image, layout, mouse, overlay, renderer};
use iced::{Element, Event, Length, Point, Radians, Rectangle, Size, Vector, window};

use super::backdrop_state::BackdropState;
use super::picture::Picture;
use super::{Glow, Halo, Hover, Shine, Signal, Surface, Tint};

/// What a surface shows, with the organic glow behind it. It draws the
/// glow of its surface, if `Hover` says it has one, under the content
/// and over whatever is behind (so it goes *inside* the box that paints
/// the surface's ground), and tells where the pointer is on it.
///
/// It takes the size of its content, as if it were not there.
pub struct Backdrop<'a, Message, Theme = iced::Theme, Renderer = iced::Renderer> {
    content: Element<'a, Message, Theme, Renderer>,
    surface: Surface,
    shine: Option<Shine>,
    listens: bool,
    tint: Tint,
    halo: Option<Halo>,
    radius: f32,
    inset: f32,
    on_signal: Box<dyn Fn(Signal) -> Message + 'a>,
}

/// Wraps what a surface shows: `hover` says whether it glows now, `tint`
/// gives the glow's colours (`Tint::of(tokens)`), and `on_signal` makes
/// the message that carries the pointer's news back to `Hover::signal`.
pub fn backdrop<'a, Message, Theme, Renderer>(
    hover: &Hover,
    surface: Surface,
    tint: Tint,
    content: impl Into<Element<'a, Message, Theme, Renderer>>,
    on_signal: impl Fn(Signal) -> Message + 'a,
) -> Backdrop<'a, Message, Theme, Renderer> {
    Backdrop {
        content: content.into(),
        surface,
        shine: hover.shine(surface),
        listens: hover.listens(),
        tint,
        halo: None,
        radius: 0.0,
        inset: 0.0,
        on_signal: Box::new(on_signal),
    }
}

impl<Message, Theme, Renderer> Backdrop<'_, Message, Theme, Renderer> {
    /// The radius of the surface's corners: the glow is cut to them.
    pub fn radius(mut self, radius: f32) -> Self {
        self.radius = radius;
        self
    }

    /// The width of the surface's border: the glow stays inside it.
    pub fn inset(mut self, inset: f32) -> Self {
        self.inset = inset;
        self
    }

    /// The glow's strength and reach, instead of the ones its size gives.
    pub fn halo(mut self, halo: Halo) -> Self {
        self.halo = Some(halo);
        self
    }

    /// The part of the widget the glow fills.
    fn ground(&self, bounds: Rectangle) -> Rectangle {
        bounds.shrink(self.inset)
    }

    /// Tells where the pointer is when it has moved, and that it left.
    /// A move is told at most once a frame; the one that came too soon
    /// is owed and told with the next frame.
    fn follow(
        &self,
        state: &mut BackdropState,
        bounds: Rectangle,
        cursor: mouse::Cursor,
        viewport: &Rectangle,
        frame: bool,
        shell: &mut Shell<'_, Message>,
    ) {
        if state.surface != Some(self.surface) || !self.listens {
            if let Some(surface) = state.surface
                && state.hovered
                && self.listens
            {
                let when = Instant::now();
                shell.publish((self.on_signal)(Signal::Left { surface, when }));
            }
            *state = BackdropState {
                surface: Some(self.surface),
                grid: std::mem::take(&mut state.grid),
                ..BackdropState::default()
            };
        }
        if !self.listens {
            return;
        }
        let position = cursor.position();
        let moved = state.cursor != position || state.bounds != bounds;
        if !moved && !(frame && state.owed) {
            return;
        }
        state.cursor = position;
        state.bounds = bounds;
        let was_hovered = state.hovered;
        let over = cursor.position_over(bounds).filter(|point| viewport.contains(*point));
        state.hovered = over.is_some();
        let when = Instant::now();
        match over {
            Some(point) => {
                let due = state
                    .told
                    .is_none_or(|told| when.saturating_duration_since(told) >= Hover::FRAME);
                if due || !was_hovered {
                    let ground = self.ground(bounds);
                    let along = |at: f32, from: f32, length: f32| ((at - from) / length.max(1.0)).clamp(0.0, 1.0);
                    shell.publish((self.on_signal)(Signal::Over {
                        surface: self.surface,
                        at: Point::new(
                            along(point.x, ground.x, ground.width),
                            along(point.y, ground.y, ground.height),
                        ),
                        size: ground.size(),
                        when,
                    }));
                    state.told = Some(when);
                    state.owed = false;
                } else {
                    state.owed = true;
                }
            }
            None => {
                state.owed = false;
                if was_hovered {
                    shell.publish((self.on_signal)(Signal::Left {
                        surface: self.surface,
                        when,
                    }));
                }
            }
        }
    }

    /// Keeps the picture of the frame that is about to be drawn.
    fn prepare(&self, state: &mut BackdropState, bounds: Rectangle) {
        let Some(shine) = self.shine else {
            state.picture = None;
            return;
        };
        let ground = self.ground(bounds).size();
        if state
            .picture
            .as_ref()
            .is_some_and(|picture| picture.shows(shine.frame, ground, self.tint))
        {
            return;
        }
        let glow = Glow {
            aspect: ground.width / ground.height.max(1.0),
            time: shine.time,
            pointer: shine.pointer,
            tint: self.tint,
            halo: self.halo.unwrap_or_else(|| Halo::of(ground)),
            fade: shine.fade,
        };
        let radius = (self.radius - self.inset).max(0.0);
        state.picture = Picture::paint(shine.frame, ground, radius, &glow, &mut state.grid);
    }
}

impl<Message, Theme, Renderer> Widget<Message, Theme, Renderer> for Backdrop<'_, Message, Theme, Renderer>
where
    Renderer: image::Renderer<Handle = image::Handle>,
{
    fn tag(&self) -> tree::Tag {
        tree::Tag::of::<BackdropState>()
    }

    fn state(&self) -> tree::State {
        tree::State::new(BackdropState::default())
    }

    fn children(&self) -> Vec<Tree> {
        vec![Tree::new(&self.content)]
    }

    fn diff(&self, tree: &mut Tree) {
        tree.diff_children(std::slice::from_ref(&self.content));
    }

    fn size(&self) -> Size<Length> {
        self.content.as_widget().size()
    }

    fn size_hint(&self) -> Size<Length> {
        self.content.as_widget().size_hint()
    }

    fn layout(&mut self, tree: &mut Tree, renderer: &Renderer, limits: &layout::Limits) -> layout::Node {
        self.content
            .as_widget_mut()
            .layout(&mut tree.children[0], renderer, limits)
    }

    fn operate(&mut self, tree: &mut Tree, layout: Layout<'_>, renderer: &Renderer, operation: &mut dyn Operation) {
        self.content
            .as_widget_mut()
            .operate(&mut tree.children[0], layout, renderer, operation);
    }

    fn update(
        &mut self,
        tree: &mut Tree,
        event: &Event,
        layout: Layout<'_>,
        cursor: mouse::Cursor,
        renderer: &Renderer,
        clipboard: &mut dyn Clipboard,
        shell: &mut Shell<'_, Message>,
        viewport: &Rectangle,
    ) {
        self.content.as_widget_mut().update(
            &mut tree.children[0],
            event,
            layout,
            cursor,
            renderer,
            clipboard,
            shell,
            viewport,
        );
        let state: &mut BackdropState = tree.state.downcast_mut();
        let frame = matches!(event, Event::Window(window::Event::RedrawRequested(_)));
        self.follow(state, layout.bounds(), cursor, viewport, frame, shell);
        if frame {
            self.prepare(state, layout.bounds());
        }
    }

    fn mouse_interaction(
        &self,
        tree: &Tree,
        layout: Layout<'_>,
        cursor: mouse::Cursor,
        viewport: &Rectangle,
        renderer: &Renderer,
    ) -> mouse::Interaction {
        self.content
            .as_widget()
            .mouse_interaction(&tree.children[0], layout, cursor, viewport, renderer)
    }

    fn draw(
        &self,
        tree: &Tree,
        renderer: &mut Renderer,
        theme: &Theme,
        style: &renderer::Style,
        layout: Layout<'_>,
        cursor: mouse::Cursor,
        viewport: &Rectangle,
    ) {
        let state: &BackdropState = tree.state.downcast_ref();
        let content = |renderer: &mut Renderer| {
            self.content
                .as_widget()
                .draw(&tree.children[0], renderer, theme, style, layout, cursor, viewport);
        };
        let Some(picture) = state.picture.as_ref().filter(|_| self.shine.is_some()) else {
            content(renderer);
            return;
        };
        // A layer of its own for the glow, cut to the surface, and one
        // for the content: in one layer a renderer draws every picture
        // over every fill, and the glow would cover the content's own.
        let ground = self.ground(layout.bounds());
        if let Some(clip) = ground.intersection(viewport) {
            renderer.with_layer(clip, |renderer| {
                renderer.draw_image(
                    image::Image {
                        handle: picture.handle.clone(),
                        filter_method: image::FilterMethod::Linear,
                        rotation: Radians(0.0),
                        border_radius: Default::default(),
                        opacity: 1.0,
                        snap: true,
                    },
                    Rectangle::new(ground.position(), picture.covers),
                    clip,
                );
            });
        }
        renderer.with_layer(*viewport, content);
    }

    fn overlay<'b>(
        &'b mut self,
        tree: &'b mut Tree,
        layout: Layout<'b>,
        renderer: &Renderer,
        viewport: &Rectangle,
        translation: Vector,
    ) -> Option<overlay::Element<'b, Message, Theme, Renderer>> {
        self.content
            .as_widget_mut()
            .overlay(&mut tree.children[0], layout, renderer, viewport, translation)
    }
}

impl<'a, Message, Theme, Renderer> From<Backdrop<'a, Message, Theme, Renderer>>
    for Element<'a, Message, Theme, Renderer>
where
    Message: 'a,
    Theme: 'a,
    Renderer: image::Renderer<Handle = image::Handle> + 'a,
{
    fn from(backdrop: Backdrop<'a, Message, Theme, Renderer>) -> Self {
        Element::new(backdrop)
    }
}

#[cfg(test)]
mod tests {
    use iced::widget::{Space, container};
    use iced_test::Simulator;

    use super::*;
    use crate::ui::Tokens;
    use crate::ui::organic::Motion;
    use crate::ui::view_tests::{panel_settings, turn};

    const BOUNDS: Rectangle = Rectangle {
        x: 100.0,
        y: 50.0,
        width: 402.0,
        height: 102.0,
    };
    const WINDOW: Rectangle = Rectangle {
        x: 0.0,
        y: 0.0,
        width: 800.0,
        height: 600.0,
    };

    fn surface(name: &str) -> Surface {
        Surface::of(name)
    }

    /// A surface with a hairline border, as a card has.
    fn over<'a>(hover: &Hover, name: &str) -> Backdrop<'a, Signal> {
        backdrop(
            hover,
            surface(name),
            Tint::of(Tokens::LIGHT),
            Space::new().width(BOUNDS.width).height(BOUNDS.height),
            |signal| signal,
        )
        .radius(8.0)
        .inset(1.0)
    }

    /// What the surface tells when the pointer is here.
    fn told(
        widget: &Backdrop<'_, Signal>,
        state: &mut BackdropState,
        cursor: mouse::Cursor,
        frame: bool,
    ) -> Vec<Signal> {
        let mut messages = Vec::new();
        let mut shell = Shell::new(&mut messages);
        widget.follow(state, BOUNDS, cursor, &WINDOW, frame, &mut shell);
        messages
    }

    fn pointer(x: f32, y: f32) -> mouse::Cursor {
        mouse::Cursor::Available(Point::new(x, y))
    }

    /// A hover whose glow on this surface is whole.
    fn glowing(name: &str) -> Hover {
        let start = Instant::now();
        let mut hover = Hover::default();
        hover.signal(Signal::Over {
            surface: surface(name),
            at: Point::new(0.5, 0.5),
            size: BOUNDS.size(),
            when: start,
        });
        hover.tick(start + Hover::FADE_IN * 2);
        hover
    }

    #[test]
    fn tells_where_on_its_ground_the_pointer_is() {
        let hover = Hover::default();
        let widget = over(&hover, "a");
        let mut state = BackdropState::default();
        let signals = told(&widget, &mut state, pointer(201.0, 76.0), false);
        let [
            Signal::Over {
                surface: told_surface,
                at,
                size,
                ..
            },
        ] = signals[..]
        else {
            panic!("{signals:?}");
        };
        assert_eq!(told_surface, surface("a"));
        // Inside the border: 400 by 100, from 101, 51.
        assert_eq!(size, Size::new(400.0, 100.0));
        assert_eq!(at, Point::new(0.25, 0.25));
        assert!(state.hovered);

        // On the border itself it is at the ground's very edge.
        let mut state = BackdropState::default();
        let signals = told(&widget, &mut state, pointer(100.2, 151.8), false);
        let [Signal::Over { at, .. }] = signals[..] else {
            panic!("{signals:?}");
        };
        assert_eq!(at, Point::new(0.0, 1.0));
    }

    #[test]
    fn says_nothing_while_the_pointer_is_elsewhere_or_still() {
        let hover = Hover::default();
        let widget = over(&hover, "a");
        let mut state = BackdropState::default();
        assert_eq!(told(&widget, &mut state, pointer(20.0, 20.0), false), []);
        assert_eq!(told(&widget, &mut state, mouse::Cursor::Unavailable, false), []);
        assert_eq!(told(&widget, &mut state, pointer(300.0, 100.0), false).len(), 1);
        assert_eq!(told(&widget, &mut state, pointer(300.0, 100.0), false), []);
        assert_eq!(told(&widget, &mut state, pointer(300.0, 100.0), true), []);
    }

    #[test]
    fn tells_a_move_once_a_frame_and_the_last_one_with_the_next_frame() {
        let hover = Hover::default();
        let widget = over(&hover, "a");
        let mut state = BackdropState::default();
        assert_eq!(told(&widget, &mut state, pointer(300.0, 100.0), false).len(), 1);
        // Two moves right after: too soon, they are owed.
        assert_eq!(told(&widget, &mut state, pointer(310.0, 100.0), false), []);
        assert_eq!(told(&widget, &mut state, pointer(321.0, 100.0), false), []);
        assert!(state.owed);
        assert_eq!(
            told(&widget, &mut state, pointer(321.0, 100.0), true),
            [],
            "a frame that comes too soon"
        );
        // A frame's time later the next frame tells where it stopped.
        state.told = state.told.map(|told| told - Hover::FRAME);
        let signals = told(&widget, &mut state, pointer(321.0, 100.0), true);
        let [Signal::Over { at, .. }] = signals[..] else {
            panic!("{signals:?}");
        };
        assert_eq!(at, Point::new(0.55, 0.49));
        assert!(!state.owed);
        // And a move then is told at once.
        state.told = state.told.map(|told| told - Hover::FRAME);
        assert_eq!(told(&widget, &mut state, pointer(330.0, 100.0), false).len(), 1);
    }

    #[test]
    fn tells_when_the_pointer_leaves() {
        let hover = Hover::default();
        let widget = over(&hover, "a");
        for gone in [
            pointer(20.0, 20.0),
            mouse::Cursor::Unavailable,
            mouse::Cursor::Levitating(Point::new(300.0, 100.0)),
        ] {
            let mut state = BackdropState::default();
            assert_eq!(told(&widget, &mut state, pointer(300.0, 100.0), false).len(), 1);
            let signals = told(&widget, &mut state, gone, false);
            assert!(
                matches!(signals[..], [Signal::Left { surface: left, .. }] if left == surface("a")),
                "{signals:?}"
            );
            assert!(!state.hovered);
            assert_eq!(told(&widget, &mut state, gone, false), []);
        }
    }

    /// A row scrolled out of sight is not under the pointer, wherever
    /// the pointer is.
    #[test]
    fn a_surface_out_of_sight_has_no_pointer() {
        let hover = Hover::default();
        let widget = over(&hover, "a");
        let mut state = BackdropState::default();
        let mut messages = Vec::new();
        let mut shell = Shell::new(&mut messages);
        let sight = Rectangle { y: 200.0, ..WINDOW };
        widget.follow(&mut state, BOUNDS, pointer(300.0, 100.0), &sight, false, &mut shell);
        assert_eq!(messages, []);
    }

    /// A list that changes puts another row where this one was.
    #[test]
    fn another_surface_in_its_place_says_the_first_one_was_left() {
        let hover = Hover::default();
        let mut state = BackdropState::default();
        assert_eq!(
            told(&over(&hover, "a"), &mut state, pointer(300.0, 100.0), false).len(),
            1
        );
        let signals = told(&over(&hover, "b"), &mut state, pointer(300.0, 100.0), false);
        let [
            Signal::Left { surface: left, .. },
            Signal::Over { surface: entered, .. },
        ] = signals[..]
        else {
            panic!("{signals:?}");
        };
        assert_eq!((left, entered), (surface("a"), surface("b")));
        assert_eq!(state.surface, Some(surface("b")));
    }

    #[test]
    fn with_motion_off_it_says_and_keeps_nothing() {
        let off = Hover::new(Motion::Off);
        let widget = over(&off, "a");
        let mut state = BackdropState::default();
        assert_eq!(told(&widget, &mut state, pointer(300.0, 100.0), false), []);
        assert_eq!(told(&widget, &mut state, pointer(20.0, 20.0), true), []);
        widget.prepare(&mut state, BOUNDS);
        assert!(state.picture.is_none() && !state.hovered);

        // Turned off under the pointer: no word about leaving either.
        let mut state = BackdropState::default();
        assert_eq!(
            told(&over(&Hover::default(), "a"), &mut state, pointer(300.0, 100.0), false).len(),
            1
        );
        assert_eq!(told(&widget, &mut state, pointer(20.0, 20.0), false), []);
        assert!(!state.hovered);
    }

    #[test]
    fn keeps_one_picture_for_each_frame() {
        let mut hover = glowing("a");
        let mut state = BackdropState::default();
        over(&hover, "a").prepare(&mut state, BOUNDS);
        let first = state.picture.clone().expect("a glowing surface has a picture");
        assert_eq!(first.surface, Size::new(400.0, 100.0));
        assert_eq!(first.covers, Size::new(401.0, 101.0));
        let image::Handle::Rgba {
            width, height, pixels, ..
        } = &first.handle
        else {
            panic!("{:?}", first.handle);
        };
        assert_eq!((*width, *height, pixels.len()), (401, 101, 401 * 101 * 4));
        assert!(
            pixels.as_chunks::<4>().0.iter().any(|pixel| pixel[3] > 20),
            "the glow is in it"
        );

        // Drawn again before the next frame: the same picture.
        over(&hover, "a").prepare(&mut state, BOUNDS);
        assert_eq!(
            state.picture.as_ref().map(|picture| picture.handle.id()),
            Some(first.handle.id())
        );

        // The next frame, another size or the other theme: a new one.
        hover.tick(Instant::now() + Hover::FADE_IN * 3);
        over(&hover, "a").prepare(&mut state, BOUNDS);
        let second = state.picture.clone().unwrap();
        assert_ne!(second.handle.id(), first.handle.id());
        let wider = Rectangle { width: 502.0, ..BOUNDS };
        over(&hover, "a").prepare(&mut state, wider);
        assert_eq!(
            state.picture.as_ref().map(|picture| picture.surface),
            Some(Size::new(500.0, 100.0))
        );
        let mut dark = over(&hover, "a");
        dark.tint = Tint::of(Tokens::DARK);
        let before = state.picture.as_ref().map(|picture| picture.handle.id());
        dark.prepare(&mut state, wider);
        assert_ne!(state.picture.as_ref().map(|picture| picture.handle.id()), before);

        // A surface that does not glow keeps none.
        over(&hover, "b").prepare(&mut state, BOUNDS);
        assert!(state.picture.is_none());
    }

    /// In a real tree: it is as big as its content, and a move of the
    /// pointer over it comes out as its signal.
    #[test]
    fn takes_the_size_of_its_content_and_signals_through_the_interface() {
        let _turn = turn();
        let hover = Hover::default();
        let scene = container(over(&hover, "a")).padding(40);
        let mut ui = Simulator::with_size(panel_settings(), Size::new(600.0, 300.0), scene);
        ui.point_at(Point::new(141.0, 91.0));
        let _ = ui.simulate([Event::Mouse(mouse::Event::CursorMoved {
            position: Point::new(141.0, 91.0),
        })]);
        let signals: Vec<Signal> = ui.into_messages().collect();
        let [Signal::Over { at, size, .. }] = signals[..] else {
            panic!("{signals:?}");
        };
        assert_eq!(size, Size::new(400.0, 100.0));
        assert_eq!(at, Point::new(0.25, 0.5));
    }
}
