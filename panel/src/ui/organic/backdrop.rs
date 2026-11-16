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
