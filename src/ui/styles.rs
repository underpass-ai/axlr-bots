//! How the panel's surfaces, buttons and fields look, from the tokens.

use iced::widget::{button, container, svg, text_input};
use iced::{Background, Border, Color, Shadow, Theme, Vector};

use super::{Tokens, Tone};

/// Corner radius of cards, buttons and fields.
pub const RADIUS: f32 = 10.0;

/// A card on the page: the surface with a thin border and a soft shadow.
pub fn card(tokens: Tokens) -> impl Fn(&Theme) -> container::Style {
    move |_| container::Style {
        text_color: Some(tokens.text),
        background: Some(Background::Color(tokens.surface)),
        border: Border {
            color: tokens.border,
            width: 1.0,
            radius: RADIUS.into(),
        },
        shadow: Shadow {
            color: Tokens::fade(Color::BLACK, if tokens.dark { 0.35 } else { 0.06 }),
            offset: Vector::new(0.0, 2.0),
            blur_radius: 8.0,
        },
        snap: true,
    }
}

/// A card that stands out in a tone: the call waiting for a person.
pub fn tone_card(tokens: Tokens, tone: Tone) -> impl Fn(&Theme) -> container::Style {
    move |theme| {
        let color = tone.color(&tokens);
        container::Style {
            background: Some(Background::Color(mix(
                tokens.surface,
                color,
                if tokens.dark { 0.14 } else { 0.08 },
            ))),
            border: Border {
                color,
                width: 2.0,
                radius: RADIUS.into(),
            },
            ..card(tokens)(theme)
        }
    }
}

/// The page behind everything.
pub fn page(tokens: Tokens) -> impl Fn(&Theme) -> container::Style {
    move |_| container::Style {
        text_color: Some(tokens.text),
        background: Some(Background::Color(tokens.page)),
        ..container::Style::default()
    }
}

/// The side bar and the header strip.
pub fn raised(tokens: Tokens) -> impl Fn(&Theme) -> container::Style {
    move |_| container::Style {
        text_color: Some(tokens.text),
        background: Some(Background::Color(tokens.raised)),
        border: Border {
            color: tokens.border,
            width: 0.0,
            radius: 0.0.into(),
        },
        ..container::Style::default()
    }
}

/// A status pill: tinted fill, strong text, rounded ends.
pub fn pill(tokens: Tokens, tone: Tone) -> impl Fn(&Theme) -> container::Style {
    move |_| {
        let color = tone.color(&tokens);
        container::Style {
            text_color: Some(color),
            background: Some(Background::Color(mix(
                tokens.surface,
                color,
                if tokens.dark { 0.22 } else { 0.13 },
            ))),
            border: Border {
                color: Tokens::fade(color, 0.55),
                width: 1.0,
                radius: 999.0.into(),
            },
            ..container::Style::default()
        }
    }
}

/// A small neutral chip: a tool, a label.
pub fn chip(tokens: Tokens) -> impl Fn(&Theme) -> container::Style {
    move |_| container::Style {
        text_color: Some(tokens.text),
        background: Some(Background::Color(tokens.raised)),
        border: Border {
            color: tokens.border,
            width: 1.0,
            radius: 6.0.into(),
        },
        ..container::Style::default()
    }
}

/// The banner on top of the content.
pub fn banner(tokens: Tokens, tone: Tone) -> impl Fn(&Theme) -> container::Style {
    move |_| {
        let color = tone.color(&tokens);
        container::Style {
            text_color: Some(tokens.text),
            background: Some(Background::Color(mix(
                tokens.surface,
                color,
                if tokens.dark { 0.25 } else { 0.14 },
            ))),
            border: Border {
                color,
                width: 2.0,
                radius: RADIUS.into(),
            },
            ..container::Style::default()
        }
    }
}

/// The dimmed layer behind a dialog.
pub fn scrim(_: &Theme) -> container::Style {
    container::Style {
        background: Some(Background::Color(Tokens::fade(Color::BLACK, 0.45))),
        ..container::Style::default()
    }
}

/// The kinds of button.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Kind {
    /// The main act of a place: filled with the accent.
    Primary,
    /// Any other act: outlined.
    Secondary,
    /// An act that cannot be undone: filled red.
    Danger,
    /// Approve: filled green.
    Good,
    /// A quiet act in a row: no fill until hovered.
    Ghost,
    /// A side bar entry; `true` when it is the current place.
    Nav(bool),
    /// A filter chip; `true` when it is on.
    Chip(bool),
}

pub fn button_style(tokens: Tokens, kind: Kind) -> impl Fn(&Theme, button::Status) -> button::Style {
    move |_, status| {
        let hovered = matches!(status, button::Status::Hovered | button::Status::Pressed);
        let (background, text, border) = match kind {
            Kind::Primary => (Some(tokens.accent), tokens.on_accent, tokens.accent),
            Kind::Danger => (Some(tokens.bad), on(tokens.bad), tokens.bad),
            Kind::Good => (Some(tokens.good), on(tokens.good), tokens.good),
            Kind::Secondary => (Some(tokens.surface), tokens.text, tokens.border),
            Kind::Ghost => (hovered.then_some(tokens.raised), tokens.text, Color::TRANSPARENT),
            Kind::Nav(true) => (
                Some(mix(tokens.raised, tokens.accent, 0.18)),
                tokens.text,
                Color::TRANSPARENT,
            ),
            Kind::Nav(false) => (
                hovered.then(|| mix(tokens.raised, tokens.text, 0.06)),
                tokens.text,
                Color::TRANSPARENT,
            ),
            Kind::Chip(true) => (Some(tokens.accent), tokens.on_accent, tokens.accent),
            Kind::Chip(false) => (Some(tokens.surface), tokens.text, tokens.border),
        };
        let background = match (background, hovered, kind) {
            (Some(color), true, Kind::Primary | Kind::Danger | Kind::Good | Kind::Chip(_)) => Some(shade(color, 0.12)),
            (Some(color), true, Kind::Secondary) => Some(mix(color, tokens.text, 0.06)),
            (other, _, _) => other,
        };
        let disabled = status == button::Status::Disabled;
        button::Style {
            background: background.map(|c| Background::Color(if disabled { Tokens::fade(c, 0.45) } else { c })),
            text_color: if disabled { Tokens::fade(text, 0.55) } else { text },
            border: Border {
                color: border,
                width: 1.0,
                radius: 8.0.into(),
            },
            shadow: Shadow::default(),
            snap: true,
        }
    }
}

/// An icon in a colour.
pub fn tint(color: Color) -> impl Fn(&Theme, svg::Status) -> svg::Style {
    move |_, _| svg::Style { color: Some(color) }
}

/// A text field on the theme's surface.
pub fn field(tokens: Tokens) -> impl Fn(&Theme, text_input::Status) -> text_input::Style {
    move |_, status| {
        let focused = matches!(status, text_input::Status::Focused { .. });
        text_input::Style {
            background: Background::Color(tokens.surface),
            border: Border {
                color: if focused { tokens.accent } else { tokens.border },
                width: if focused { 2.0 } else { 1.0 },
                radius: 8.0.into(),
            },
            icon: tokens.muted,
            placeholder: Tokens::fade(tokens.muted, 0.75),
            value: tokens.text,
            selection: Tokens::fade(tokens.accent, 0.35),
        }
    }
}

/// The text colour that reads on a filled colour: ink on light fills,
/// white on dark ones.
pub fn on(fill: Color) -> Color {
    let luminance = 0.2126 * fill.r + 0.7152 * fill.g + 0.0722 * fill.b;
    if luminance > 0.5 {
        Tokens::LIGHT.text
    } else {
        Color::WHITE
    }
}

/// `base` with `amount` of `over` mixed in.
pub fn mix(base: Color, over: Color, amount: f32) -> Color {
    Color {
        r: base.r + (over.r - base.r) * amount,
        g: base.g + (over.g - base.g) * amount,
        b: base.b + (over.b - base.b) * amount,
        a: 1.0,
    }
}

/// A colour made darker by `amount`.
pub fn shade(color: Color, amount: f32) -> Color {
    mix(color, Color::BLACK, amount)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn button_styles_follow_the_kind_and_the_status() {
        let theme = Tokens::LIGHT.theme();
        let tokens = Tokens::LIGHT;
        let primary = button_style(tokens, Kind::Primary)(&theme, button::Status::Active);
        assert_eq!(primary.background, Some(Background::Color(tokens.accent)));
        assert_eq!(primary.text_color, tokens.on_accent);
        let hovered = button_style(tokens, Kind::Primary)(&theme, button::Status::Hovered);
        assert_ne!(hovered.background, primary.background);
        let disabled = button_style(tokens, Kind::Danger)(&theme, button::Status::Disabled);
        assert!(disabled.text_color.a < 1.0);
        assert_eq!(
            button_style(tokens, Kind::Ghost)(&theme, button::Status::Active).background,
            None
        );
        assert!(
            button_style(tokens, Kind::Ghost)(&theme, button::Status::Hovered)
                .background
                .is_some()
        );
        assert!(
            button_style(tokens, Kind::Nav(true))(&theme, button::Status::Active)
                .background
                .is_some()
        );
        assert!(
            button_style(tokens, Kind::Nav(false))(&theme, button::Status::Active)
                .background
                .is_none()
        );
        assert!(
            button_style(tokens, Kind::Nav(false))(&theme, button::Status::Hovered)
                .background
                .is_some()
        );
        for kind in [Kind::Secondary, Kind::Good, Kind::Chip(true), Kind::Chip(false)] {
            let active = button_style(tokens, kind)(&theme, button::Status::Active);
            let hover = button_style(tokens, kind)(&theme, button::Status::Pressed);
            assert_ne!(active.background, hover.background, "{kind:?}");
        }
    }

    #[test]
    fn containers_fields_and_colours() {
        let theme = Tokens::DARK.theme();
        let tokens = Tokens::DARK;
        assert_eq!(card(tokens)(&theme).background, Some(Background::Color(tokens.surface)));
        assert_eq!(tone_card(tokens, Tone::Wait)(&theme).border.color, tokens.wait);
        assert_eq!(page(tokens)(&theme).background, Some(Background::Color(tokens.page)));
        assert_eq!(
            raised(tokens)(&theme).background,
            Some(Background::Color(tokens.raised))
        );
        assert_eq!(pill(tokens, Tone::Bad)(&theme).text_color, Some(tokens.bad));
        assert!(chip(tokens)(&theme).background.is_some());
        assert_eq!(banner(tokens, Tone::Good)(&theme).border.color, tokens.good);
        assert!(scrim(&theme).background.is_some());
        assert_eq!(
            tint(tokens.accent)(&theme, svg::Status::Idle).color,
            Some(tokens.accent)
        );
        let focused = field(tokens)(&theme, text_input::Status::Focused { is_hovered: false });
        assert_eq!(focused.border.color, tokens.accent);
        assert_eq!(
            field(tokens)(&theme, text_input::Status::Active).border.color,
            tokens.border
        );
        assert_eq!(on(Color::WHITE), Tokens::LIGHT.text);
        assert_eq!(on(Color::BLACK), Color::WHITE);
        assert_eq!(mix(Color::BLACK, Color::WHITE, 0.5).r, 0.5);
        assert!(shade(Color::WHITE, 0.5).r < 1.0);
    }
}
