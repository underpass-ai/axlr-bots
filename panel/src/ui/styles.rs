//! How the panel's surfaces, buttons and fields look, from the tokens:
//! thin lines, slightly rounded corners, no shadows.

use iced::widget::{button, container, svg, text_editor, text_input};
use iced::{Background, Border, Color, Shadow, Theme};

use super::{Tokens, Tone};

/// Corner radius of cards.
pub const RADIUS: f32 = 8.0;
/// Corner radius of buttons, fields and chips.
pub const RADIUS_S: f32 = 6.0;
/// Corner radius of a status pill: nearly square, like the brand's pixels.
pub const RADIUS_PILL: f32 = 4.0;

/// A card on the page: the surface with a hairline border.
pub fn card(tokens: Tokens) -> impl Fn(&Theme) -> container::Style {
    move |_| container::Style {
        text_color: Some(tokens.text),
        background: Some(Background::Color(tokens.surface)),
        border: Border {
            color: tokens.border,
            width: 1.0,
            radius: RADIUS.into(),
        },
        // No shadow: the software renderer draws blurred shadows as grey
        // blocks, and the border already separates a card from the page.
        shadow: Shadow::default(),
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
                if tokens.dark { 0.14 } else { 0.07 },
            ))),
            border: Border {
                color,
                width: 1.0,
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

/// A thin line between the side bar or the header and the content.
pub fn rule(tokens: Tokens) -> impl Fn(&Theme) -> container::Style {
    move |_| container::Style {
        background: Some(Background::Color(tokens.border)),
        ..container::Style::default()
    }
}

/// The fill of a status pill: the surface with a little of the tone.
pub fn pill_fill(tokens: Tokens, tone: Tone) -> Color {
    mix(
        tokens.surface,
        tone.color(&tokens),
        if tokens.dark { 0.22 } else { 0.12 },
    )
}

/// The ink of a status pill: the tone, a shade darker on the light page so
/// it reads on its own tint.
pub fn pill_ink(tokens: Tokens, tone: Tone) -> Color {
    let color = tone.color(&tokens);
    if tokens.dark { color } else { shade(color, 0.18) }
}

/// A status pill: tinted fill, strong word, nearly square corners.
pub fn pill(tokens: Tokens, tone: Tone) -> impl Fn(&Theme) -> container::Style {
    move |_| container::Style {
        text_color: Some(pill_ink(tokens, tone)),
        background: Some(Background::Color(pill_fill(tokens, tone))),
        border: Border {
            color: Tokens::fade(tone.color(&tokens), 0.5),
            width: 1.0,
            radius: RADIUS_PILL.into(),
        },
        ..container::Style::default()
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
            radius: RADIUS_PILL.into(),
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
                if tokens.dark { 0.22 } else { 0.10 },
            ))),
            border: Border {
                color,
                width: 1.0,
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
    /// A side bar entry; `true` when it is the current place, which the
    /// accent marks (the bar beside it is drawn by the side bar).
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
            Kind::Nav(true) => (None, tokens.accent, Color::TRANSPARENT),
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
        // A chip or an entry that is on has no act left, but it is not off:
        // it keeps its full colour.
        let disabled = status == button::Status::Disabled && !matches!(kind, Kind::Chip(true) | Kind::Nav(true));
        button::Style {
            background: background.map(|c| Background::Color(if disabled { Tokens::fade(c, 0.45) } else { c })),
            text_color: if disabled { Tokens::fade(text, 0.55) } else { text },
            border: Border {
                color: border,
                width: 1.0,
                radius: RADIUS_S.into(),
            },
            shadow: Shadow::default(),
            snap: true,
        }
    }
}

/// A chat bubble: the person's in a light wash of the accent, the bot's
/// in the raised grey.
pub fn bubble(tokens: Tokens, mine: bool) -> impl Fn(&Theme) -> container::Style {
    move |_| container::Style {
        background: Some(Background::Color(if mine {
            mix(tokens.surface, tokens.accent, 0.14)
        } else {
            tokens.raised
        })),
        text_color: Some(tokens.text),
        border: Border {
            radius: RADIUS_S.into(),
            ..Border::default()
        },
        ..container::Style::default()
    }
}

/// A card to pick among a few: the picked one in a light wash of the
/// accent with an accent border, so its words stay ink on light.
pub fn choice(tokens: Tokens, picked: bool) -> impl Fn(&Theme, button::Status) -> button::Style {
    move |_, status| {
        let hovered = matches!(status, button::Status::Hovered | button::Status::Pressed);
        let background = if picked {
            mix(tokens.surface, tokens.accent, 0.10)
        } else if hovered {
            tokens.raised
        } else {
            tokens.surface
        };
        button::Style {
            background: Some(Background::Color(background)),
            text_color: tokens.text,
            border: Border {
                color: if picked { tokens.accent } else { tokens.border },
                width: if picked { 1.5 } else { 1.0 },
                radius: RADIUS_S.into(),
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
                width: if focused { 1.5 } else { 1.0 },
                radius: RADIUS_S.into(),
            },
            icon: tokens.muted,
            placeholder: Tokens::fade(tokens.muted, 0.75),
            value: tokens.text,
            selection: Tokens::fade(tokens.accent, 0.35),
        }
    }
}

/// Text to read and select, not to edit: the chip's quiet fill, the
/// accent's border once focused.
pub fn reader(tokens: Tokens) -> impl Fn(&Theme, text_editor::Status) -> text_editor::Style {
    move |_, status| {
        let focused = matches!(status, text_editor::Status::Focused { .. });
        text_editor::Style {
            background: Background::Color(tokens.surface),
            border: Border {
                color: if focused { tokens.accent } else { tokens.border },
                width: 1.0,
                radius: RADIUS_S.into(),
            },
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
    fn bubbles_tell_the_person_from_the_bot() {
        let theme = Tokens::LIGHT.theme();
        let tokens = Tokens::LIGHT;
        let mine = bubble(tokens, true)(&theme);
        let theirs = bubble(tokens, false)(&theme);
        assert_ne!(mine.background, theirs.background);
        assert_eq!(theirs.background, Some(Background::Color(tokens.raised)));
    }

    #[test]
    fn a_picked_choice_stays_readable() {
        let theme = Tokens::LIGHT.theme();
        let tokens = Tokens::LIGHT;
        let picked = choice(tokens, true)(&theme, button::Status::Disabled);
        assert_eq!((picked.text_color, picked.border.color), (tokens.text, tokens.accent));
        let other = choice(tokens, false)(&theme, button::Status::Hovered);
        assert_eq!(other.background, Some(Background::Color(tokens.raised)));
        let idle = choice(tokens, false)(&theme, button::Status::Active);
        assert_eq!(idle.background, Some(Background::Color(tokens.surface)));
    }

    #[test]
    fn the_reader_marks_its_focus() {
        let theme = Tokens::LIGHT.theme();
        let tokens = Tokens::LIGHT;
        let idle = reader(tokens)(&theme, text_editor::Status::Active);
        let focused = reader(tokens)(&theme, text_editor::Status::Focused { is_hovered: false });
        assert_eq!(idle.border.color, tokens.border);
        assert_eq!(focused.border.color, tokens.accent);
        assert_eq!(idle.value, tokens.text);
    }

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
        let current = button_style(tokens, Kind::Nav(true))(&theme, button::Status::Active);
        assert_eq!(current.background, None);
        assert_eq!(current.text_color, tokens.accent);
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

    /// The theme button that is on is disabled, as there is nothing left
    /// to press, but it must still read.
    #[test]
    fn a_chip_that_is_on_keeps_its_colour_when_disabled() {
        let theme = Tokens::DARK.theme();
        let on = button_style(Tokens::DARK, Kind::Chip(true))(&theme, button::Status::Disabled);
        assert_eq!(on.background, Some(Background::Color(Tokens::DARK.accent)));
        assert_eq!(on.text_color, Tokens::DARK.on_accent);
        let off = button_style(Tokens::DARK, Kind::Chip(false))(&theme, button::Status::Disabled);
        assert!(off.text_color.a < 1.0);
    }

    #[test]
    fn containers_fields_and_colours() {
        let theme = Tokens::DARK.theme();
        let tokens = Tokens::DARK;
        assert_eq!(card(tokens)(&theme).background, Some(Background::Color(tokens.surface)));
        assert_eq!(card(tokens)(&theme).border.width, 1.0);
        assert_eq!(tone_card(tokens, Tone::Wait)(&theme).border.color, tokens.wait);
        assert_eq!(tone_card(tokens, Tone::Wait)(&theme).border.width, 1.0);
        assert_eq!(page(tokens)(&theme).background, Some(Background::Color(tokens.page)));
        assert_eq!(
            raised(tokens)(&theme).background,
            Some(Background::Color(tokens.raised))
        );
        assert_eq!(rule(tokens)(&theme).background, Some(Background::Color(tokens.border)));
        assert_eq!(pill(tokens, Tone::Bad)(&theme).text_color, Some(tokens.bad));
        assert_ne!(
            pill(Tokens::LIGHT, Tone::Bad)(&theme).text_color,
            Some(Tokens::LIGHT.bad)
        );
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
