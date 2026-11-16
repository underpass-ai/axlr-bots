//! Organic hover: a faint field of slow waves, tinted between the
//! brand's copper and the busy slate, that glows behind the card, the
//! row or the tile the pointer is on and follows it.
//!
//! The pieces: [`Field`] paints the waves on a small grid and [`Spread`]
//! stretches them smooth to a surface; [`Hover`] knows which [`Surface`]
//! glows and how much, from the [`Signal`]s the surfaces send and the
//! frames' times; [`backdrop`] wraps what a surface shows, draws the glow
//! behind it and sends the signals. [`Motion`] turns it all off.
//!
//! In the panel:
//!
//! - the state keeps a `Hover`, made with the settings' `look.motion`
//!   (`Hover::set_motion` when the setting changes);
//! - a view wraps a surface's content with `backdrop(&hover, surface,
//!   Tint::of(tokens), content, Message::…)`, *inside* the box that
//!   paints the surface's ground (a card's container, a row's button)
//!   and with that box's padding moved inside the backdrop, so the glow
//!   fills the surface and lies over its ground and under its words;
//!   `.radius(…)` and `.inset(…)` give the surface's corners and the
//!   width of its border;
//! - the update hands that message's `Signal` to `Hover::signal` and a
//!   frame's time to `Hover::tick`, and calls `Hover::release` when the
//!   screen changes under the pointer;
//! - the subscription adds `frames(&hover)`, which is empty unless a
//!   glow is changing.
//!
//! `examples/organic.rs` is all of that in a window of its own.

mod backdrop;
mod backdrop_state;
#[cfg(test)]
mod demo_tests;
mod ember;
mod field;
mod glow;
mod halo;
mod hover;
mod motion;
mod picture;
mod shine;
mod signal;
mod spread;
mod surface;
mod tint;

use std::time::Instant;

use iced::Subscription;

pub use backdrop::{Backdrop, backdrop};
pub use field::Field;
pub use glow::Glow;
pub use halo::Halo;
pub use hover::Hover;
pub use motion::Motion;
pub use shine::Shine;
pub use signal::Signal;
pub use spread::Spread;
pub use surface::Surface;
pub use tint::Tint;

/// The frames of the glows: a moment about thirty times a second while
/// a glow is changing, and nothing at all otherwise.
pub fn frames(hover: &Hover) -> Subscription<Instant> {
    if hover.is_animating() {
        iced::time::every(Hover::FRAME)
    } else {
        Subscription::none()
    }
}
