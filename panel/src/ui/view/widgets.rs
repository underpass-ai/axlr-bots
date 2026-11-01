//! The panel's building blocks: fonts, icons, pills, cards, buttons, fields.

use iced::font::{Family, Weight};
use iced::widget::text::{LineHeight, Wrapping};
use iced::widget::{Space, button, column, container, mouse_area, responsive, row, svg, text, text_input, tooltip};
use iced::{Alignment, Color, Element, Font, Length, Padding, mouse};

use crate::ui::styles::{self, Kind};
use crate::ui::{Icon, Message, Tokens, Tone};

pub type El<'a> = Element<'a, Message>;

/// The panel's typeface for words: Lexend, made for reading fluency,
/// embedded with the binary (`assets/fonts`).
pub const REGULAR: Font = Font {
    family: Family::Name("Lexend"),
    weight: Weight::Normal,
    ..Font::DEFAULT
};

/// The weight of a name or a label.
pub const SEMIBOLD: Font = Font {
    family: Family::Name("Lexend"),
    weight: Weight::Semibold,
    ..Font::DEFAULT
};

/// The weight that carries a heading.
pub const BOLD: Font = Font {
    family: Family::Name("Lexend"),
    weight: Weight::Bold,
    ..Font::DEFAULT
};

/// The typeface of a big number: DotGothic16, a thin pixel face in the
/// spirit of the emblem.
pub const NUMERAL: Font = Font {
    family: Family::Name("DotGothic16"),
    weight: Weight::Normal,
    ..Font::DEFAULT
};

/// The text sizes: body, small, section title, page title, big number.
pub const BODY: f32 = 16.0;
pub const SMALL: f32 = 14.0;
pub const SECTION: f32 = 15.0;
pub const TITLE: f32 = 26.0;
pub const NUMBER: f32 = 40.0;

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

pub fn semibold<'a>(words: impl text::IntoFragment<'a>) -> text::Text<'a> {
    text(words).font(SEMIBOLD)
}

pub fn muted<'a>(words: impl text::IntoFragment<'a>, tokens: Tokens) -> text::Text<'a> {
    text(words).color(tokens.muted).size(SMALL)
}

/// A number in the pixel face.
pub fn number<'a>(words: impl text::IntoFragment<'a>) -> text::Text<'a> {
    text(words).font(NUMERAL).size(NUMBER)
}

/// The size at which `value` fits `width` in the pixel face, whose every
/// glyph is half an em wide: `NUMBER` while it fits, smaller for a long
/// value, a cost of `$1234.56` in a narrow window, never under `SMALL`.
fn number_size(value: &str, width: f32) -> f32 {
    let ems = value.chars().count() as f32 * 0.5;
    // Two pixels to spare, so rounding never pushes the last digit out.
    ((width - 2.0) / ems).clamp(SMALL, NUMBER)
}

/// A number in the pixel face as big as its place lets it be, on a line
/// as high as a big number's, so the cards of a row end level.
fn fitted_number<'a>(value: String, color: Color) -> El<'a> {
    responsive(move |space| {
        container(
            number(value.clone())
                .size(number_size(&value, space.width))
                .color(color)
                .wrapping(Wrapping::None),
        )
        .height(LineHeight::default().to_absolute(NUMBER.into()))
        .align_y(Alignment::End)
        .into()
    })
    .height(Length::Shrink)
    .into()
}

/// A page title with its icon.
pub fn title<'a>(glyph: Icon, words: impl text::IntoFragment<'a>, tokens: Tokens) -> El<'a> {
    row![icon(glyph, 26.0, tokens.accent), bold(words).size(TITLE)]
        .spacing(12)
        .align_y(Alignment::Center)
        .into()
}

/// A section title with its icon: small and bold, in the muted ink.
pub fn section_title<'a>(glyph: Icon, words: impl text::IntoFragment<'a>, tokens: Tokens) -> El<'a> {
    row![
        icon(glyph, 16.0, tokens.muted),
        bold(words).size(SECTION).color(tokens.muted)
    ]
    .spacing(8)
    .align_y(Alignment::Center)
    .into()
}

/// A status in a pill: the tone's icon, colour and a word or two.
pub fn pill<'a>(tone: Tone, label: impl text::IntoFragment<'a>, tokens: Tokens) -> El<'a> {
    let ink = styles::pill_ink(tokens, tone);
    container(
        row![icon(tone.icon(), 13.0, ink), semibold(label).size(SMALL).color(ink)]
            .spacing(5)
            .align_y(Alignment::Center),
    )
    .padding(Padding::from([2, 8]))
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

/// A big number in a card: two words and the icon on top, the number in
/// the pixel face under them, smaller only when it would not fit. A
/// number that needs a person (amber) or says something failed (red)
/// tints the whole card. A click goes where the number comes from.
pub fn stat<'a>(
    glyph: Icon,
    value: String,
    label: &'a str,
    tone: Tone,
    on_press: Option<Message>,
    tokens: Tokens,
) -> El<'a> {
    let color = tone.color(&tokens);
    let body = column![
        row![
            text(label).size(SMALL).color(tokens.muted),
            Space::new().width(Length::Fill),
            icon(glyph, 16.0, color)
        ]
        .align_y(Alignment::Center),
        fitted_number(value, color),
    ]
    .spacing(4);
    let card = container(body).padding(Padding::from([12, 16])).width(Length::Fill);
    let card = if matches!(tone, Tone::Wait | Tone::Bad) {
        card.style(styles::tone_card(tokens, tone))
    } else {
        card.style(styles::card(tokens))
    };
    let area = mouse_area(card);
    match on_press {
        Some(message) => container(area.on_press(message).interaction(mouse::Interaction::Pointer))
            .width(Length::Fill)
            .into(),
        None => container(area).width(Length::Fill).into(),
    }
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
        row![icon(glyph, 16.0, color), semibold(label).size(BODY)]
            .spacing(8)
            .align_y(Alignment::Center),
    )
    .padding(Padding::from([8, 14]))
    .style(styles::button_style(tokens, kind))
    .on_press_maybe(on_press)
    .into()
}

/// A quiet small button that copies `value`, named for what it copies.
pub fn copy_button<'a>(label: &'a str, value: impl Into<String>, tokens: Tokens) -> El<'a> {
    button(
        row![
            icon(Icon::Copy, 14.0, tokens.muted),
            semibold(label).size(SMALL).color(tokens.muted)
        ]
        .spacing(6)
        .align_y(Alignment::Center),
    )
    .padding(Padding::from([4, 8]))
    .style(styles::button_style(tokens, Kind::Ghost))
    .on_press(Message::Copy(value.into()))
    .into()
}

/// A card with a titled section and buttons at the right of its title.
pub fn section_with<'a>(
    glyph: Icon,
    words: impl text::IntoFragment<'a>,
    tools: Vec<El<'a>>,
    body: impl Into<El<'a>>,
    tokens: Tokens,
) -> El<'a> {
    let mut head = row![section_title(glyph, words, tokens), Space::new().width(Length::Fill)]
        .spacing(6)
        .align_y(Alignment::Center);
    for tool in tools {
        head = head.push(tool);
    }
    card(column![head, body.into()].spacing(14), tokens).into()
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
    column![semibold(label).size(SMALL), input].spacing(6).into()
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
