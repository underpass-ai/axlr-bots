//! The panel's building blocks: icons, pills, cards, buttons, fields.

use iced::font::Weight;
use iced::widget::{button, column, container, row, svg, text, text_input, tooltip};
use iced::{Alignment, Color, Element, Font, Length, Padding};

use crate::ui::styles::{self, Kind};
use crate::ui::{Icon, Message, Tokens, Tone};

pub type El<'a> = Element<'a, Message>;

/// The weight that carries a heading or a number.
pub const BOLD: Font = Font {
    weight: Weight::Bold,
    ..Font::DEFAULT
};

/// The text sizes: body, small, section title, page title, big number.
pub const BODY: f32 = 16.0;
pub const SMALL: f32 = 14.0;
pub const SECTION: f32 = 19.0;
pub const TITLE: f32 = 27.0;
pub const NUMBER: f32 = 34.0;

pub fn icon<'a>(glyph: Icon, size: f32, color: Color) -> El<'a> {
    svg(glyph.handle())
        .width(size)
        .height(size)
        .style(styles::tint(color))
        .into()
}

pub fn bold<'a>(words: impl text::IntoFragment<'a>) -> text::Text<'a> {
    text(words).font(BOLD)
}

pub fn muted<'a>(words: impl text::IntoFragment<'a>, tokens: Tokens) -> text::Text<'a> {
    text(words).color(tokens.muted).size(SMALL)
}

/// A page title with its icon.
pub fn title<'a>(glyph: Icon, words: impl text::IntoFragment<'a>, tokens: Tokens) -> El<'a> {
    row![icon(glyph, 28.0, tokens.accent), bold(words).size(TITLE)]
        .spacing(12)
        .align_y(Alignment::Center)
        .into()
}

/// A section title with its icon.
pub fn section_title<'a>(glyph: Icon, words: impl text::IntoFragment<'a>, tokens: Tokens) -> El<'a> {
    row![icon(glyph, 20.0, tokens.muted), bold(words).size(SECTION)]
        .spacing(10)
        .align_y(Alignment::Center)
        .into()
}

/// A status in a pill: the tone's icon, colour and a word or two.
pub fn pill<'a>(tone: Tone, label: impl text::IntoFragment<'a>, tokens: Tokens) -> El<'a> {
    container(
        row![
            icon(tone.icon(), 14.0, tone.color(&tokens)),
            bold(label).size(SMALL).color(tone.color(&tokens))
        ]
        .spacing(6)
        .align_y(Alignment::Center),
    )
    .padding(Padding::from([3, 10]))
    .style(styles::pill(tokens, tone))
    .into()
}

/// A small neutral tag: a tool, a label, an id.
pub fn chip<'a>(words: impl text::IntoFragment<'a>, tokens: Tokens) -> El<'a> {
    container(text(words).size(SMALL))
        .padding(Padding::from([2, 8]))
        .style(styles::chip(tokens))
        .into()
}

/// A card with its content padded.
pub fn card<'a>(content: impl Into<El<'a>>, tokens: Tokens) -> container::Container<'a, Message> {
    container(content)
        .padding(18)
        .width(Length::Fill)
        .style(styles::card(tokens))
}

/// A card with a titled section.
pub fn section<'a>(glyph: Icon, words: impl text::IntoFragment<'a>, body: impl Into<El<'a>>, tokens: Tokens) -> El<'a> {
    card(
        column![section_title(glyph, words, tokens), body.into()].spacing(14),
        tokens,
    )
    .into()
}

/// A button with an icon and a word or two; disabled without a message.
pub fn action<'a>(
    kind: Kind,
    glyph: Icon,
    label: impl text::IntoFragment<'a>,
    on_press: Option<Message>,
    tokens: Tokens,
) -> El<'a> {
    let color = match kind {
        Kind::Primary | Kind::Chip(true) => tokens.on_accent,
        Kind::Danger => styles::on(tokens.bad),
        Kind::Good => styles::on(tokens.good),
        _ => tokens.text,
    };
    button(
        row![icon(glyph, 16.0, color), bold(label).size(BODY)]
            .spacing(8)
            .align_y(Alignment::Center),
    )
    .padding(Padding::from([9, 16]))
    .style(styles::button_style(tokens, kind))
    .on_press_maybe(on_press)
    .into()
}

/// An icon alone in a quiet button, with a tip on hover.
pub fn icon_button<'a>(glyph: Icon, tip: &'a str, on_press: Option<Message>, tokens: Tokens) -> El<'a> {
    let button = button(icon(glyph, 18.0, tokens.text))
        .padding(8)
        .style(styles::button_style(tokens, Kind::Ghost))
        .on_press_maybe(on_press);
    tooltip(
        button,
        container(text(tip).size(SMALL)).padding(6).style(styles::chip(tokens)),
        tooltip::Position::Bottom,
    )
    .into()
}

/// A labelled text field.
pub fn field<'a>(
    label: &'a str,
    placeholder: &'a str,
    value: &'a str,
    on_input: impl Fn(String) -> Message + 'a,
    on_submit: Option<Message>,
    tokens: Tokens,
) -> El<'a> {
    let input = text_input(placeholder, value)
        .on_input(on_input)
        .on_submit_maybe(on_submit)
        .padding(10)
        .size(BODY)
        .style(styles::field(tokens));
    column![bold(label).size(SMALL), input].spacing(6).into()
}

/// A line of a fact list: a label, then the value.
pub fn fact<'a>(label: &'a str, value: impl Into<El<'a>>, tokens: Tokens) -> El<'a> {
    row![container(muted(label, tokens)).width(150), value.into()]
        .spacing(12)
        .align_y(Alignment::Center)
        .into()
}

/// What a place shows when it has nothing yet.
pub fn empty<'a>(glyph: Icon, words: impl text::IntoFragment<'a>, tokens: Tokens) -> El<'a> {
    container(
        row![icon(glyph, 22.0, tokens.muted), text(words).color(tokens.muted)]
            .spacing(12)
            .align_y(Alignment::Center),
    )
    .padding(24)
    .center_x(Length::Fill)
    .into()
}
