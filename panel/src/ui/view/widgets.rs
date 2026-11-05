//! The panel's building blocks: fonts, icons, pills, cards, buttons, fields.

use iced::font::{Family, Weight};
use iced::widget::text::{LineHeight, Wrapping};
use iced::widget::text_editor::Binding;
use iced::widget::{
    Space, button, column, container, mouse_area, responsive, row, svg, text, text_editor, text_input, tooltip,
};
use iced::{Alignment, Color, Element, Font, Length, Padding, mouse};

pub use super::voice::Voice;
use crate::ui::styles::{self, Kind};
use crate::ui::{Icon, Message, ReaderKey, Readers, Tokens, Tone};

pub type El<'a> = Element<'a, Message>;

/// The id of the scrollable page of every screen, which goes back to its
/// top when another screen opens.
pub const PAGE: &str = "page";

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

/// The sign of a floor, `≥ $0.42`: a cost some run did not tell. The
/// pixel face has no glyph for it, so it is drawn in the face for words.
pub const FLOOR: char = '≥';

/// How wide a point is drawn in the pixel face, in ems. Every glyph of
/// the face is half an em wide, and a point's ink sits at the left of
/// its cell: at a full cell `$0.12` reads `$0. 12`.
const POINT_EMS: f32 = 0.25;

/// How wide the sign of a floor is beside a number, in the number's ems.
const FLOOR_EMS: f32 = 0.6;

/// How many ems `value` takes in the pixel face.
fn number_ems(value: &str) -> f32 {
    value
        .chars()
        .map(|c| match c {
            '.' => POINT_EMS,
            FLOOR => FLOOR_EMS,
            ' ' => 0.0,
            _ => 0.5,
        })
        .sum()
}

/// The size at which `value` fits `width` in the pixel face: `NUMBER`
/// while it fits, smaller for a long value, a cost of `$1234.56` in a
/// narrow window, never under `SMALL`.
fn number_size(value: &str, width: f32) -> f32 {
    // Two pixels to spare, so rounding never pushes the last digit out.
    ((width - 2.0) / number_ems(value).max(0.5)).clamp(SMALL, NUMBER)
}

/// A number in the pixel face at `size`: each point in a cell as wide as
/// its ink, and the sign of a floor in the face for words.
fn pixel_number<'a>(value: &str, size: f32, color: Color) -> El<'a> {
    let mut line = row![].align_y(Alignment::End);
    let value = match value.strip_prefix(FLOOR) {
        Some(rest) => {
            line = line.push(
                container(text(FLOOR.to_string()).font(REGULAR).size(size * 0.55).color(color))
                    .width(size * FLOOR_EMS)
                    .padding(Padding {
                        bottom: size * 0.12,
                        ..Padding::ZERO
                    }),
            );
            rest.trim_start()
        }
        None => value,
    };
    for (index, part) in value.split('.').enumerate() {
        if index > 0 {
            line = line.push(
                container(number(".").size(size).color(color).wrapping(Wrapping::None))
                    .width(size * POINT_EMS)
                    .clip(true),
            );
        }
        if !part.is_empty() {
            line = line.push(
                number(part.to_string())
                    .size(size)
                    .color(color)
                    .wrapping(Wrapping::None),
            );
        }
    }
    line.into()
}

/// A number in the pixel face as big as its place lets it be, on a line
/// as high as a big number's, so the cards of a row end level.
fn fitted_number<'a>(value: String, color: Color) -> El<'a> {
    responsive(move |space| {
        container(pixel_number(&value, number_size(&value, space.width), color))
            .height(LineHeight::default().to_absolute(NUMBER.into()))
            .align_y(Alignment::End)
            .into()
    })
    .height(Length::Shrink)
    .into()
}

/// How Markdown reads in the panel: in its face for words, with the
/// theme's colours. Iced's own style draws it in the system's face.
/// Code in a line is ink on a chip, as the rest of the panel's chips,
/// not white on black.
pub fn markdown_style(tokens: Tokens) -> iced::widget::markdown::Style {
    use iced::widget::markdown::{Highlight, Style};
    Style {
        font: REGULAR,
        inline_code_color: tokens.text,
        inline_code_highlight: Highlight {
            background: tokens.raised.into(),
            border: iced::border::rounded(4).color(tokens.border).width(1),
        },
        inline_code_padding: Padding::from([0, 2]),
        ..Style::from_palette(tokens.theme().palette())
    }
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
    pill_with(tone.icon(), tone, label, tokens)
}

/// A pill in a tone's colour with an icon of its own, for a status
/// whose meaning is not its tone's: a severity, a diagnosis.
pub fn pill_with<'a>(glyph: Icon, tone: Tone, label: impl text::IntoFragment<'a>, tokens: Tokens) -> El<'a> {
    let ink = styles::pill_ink(tokens, tone);
    container(
        row![icon(glyph, 13.0, ink), semibold(label).size(SMALL).color(ink)]
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
        // A gap the label never closes, even when the card is narrow.
        row![
            text(label).size(SMALL).color(tokens.muted),
            Space::new().width(Length::Fill),
            icon(glyph, 16.0, color)
        ]
        .spacing(6)
        .align_y(Alignment::Center),
        fitted_number(value, color),
    ]
    .spacing(4);
    // Seven cards share a row: a side of 12 leaves the longest label,
    // `expired today`, its gap to the icon at 1360 px.
    let card = container(body).padding(12).width(Length::Fill);
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

/// Words to read that a person can select a part of and copy with the
/// mouse and Ctrl+C: a read-only editor as tall as its words, with no
/// box and no caret until clicked, whose wheel scrolls the page. Until
/// the state holds the passage's reader, the words show as plain text.
pub fn reader<'a>(readers: &'a Readers, key: ReaderKey, words: String, voice: Voice, tokens: Tokens) -> El<'a> {
    match readers.get(&key).filter(|reader| reader.source() == words) {
        Some(reader) => text_editor(reader.content())
            .on_action(move |action| Message::Read(key.clone(), action))
            .key_binding(selection_only)
            .font(voice.font())
            .size(voice.size())
            .padding(0)
            .wrapping(Wrapping::WordOrGlyph)
            .style(styles::reading(tokens, voice.color(tokens)))
            .into(),
        None => text(words)
            .font(voice.font())
            .size(voice.size())
            .color(voice.color(tokens))
            .wrapping(Wrapping::WordOrGlyph)
            .width(Length::Fill)
            .into(),
    }
}

/// A reader's keys: those that move, select and copy; none that edits.
fn selection_only(press: text_editor::KeyPress) -> Option<Binding<Message>> {
    match Binding::from_key_press(press)? {
        binding @ (Binding::Copy
        | Binding::Unfocus
        | Binding::Move(_)
        | Binding::Select(_)
        | Binding::SelectWord
        | Binding::SelectLine
        | Binding::SelectAll) => Some(binding),
        _ => None,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_point_and_a_floor_take_their_own_width() {
        assert_eq!(number_ems("12"), 1.0);
        assert_eq!(number_ems("$0.12"), 2.25);
        assert_eq!(number_ems("≥ $0.12"), 2.85);
        assert_eq!(number_size("1/43", 400.0), NUMBER);
        assert_eq!(number_size("$1234.56", 40.0), SMALL);
        assert!((number_size("$0.12", 92.0) - 40.0).abs() < 1e-3);
        assert_eq!(number_size("", 100.0), NUMBER);
    }
}
