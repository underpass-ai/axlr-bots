//! The command palette over the page: its field, its results under
//! their sections with what was typed marked, the one the arrows are on
//! tinted, and the keys it answers to at its foot.

use iced::widget::scrollable::{Direction, Scrollbar};
use iced::widget::{Space, button, column, container, row, scrollable, text, text_input};
use iced::{Alignment, Length, Padding};

use super::sheet::Ink;
use super::widgets::{El, icon, key_cap};
use crate::domain::NodeStatus;
use crate::ui::find::{PaletteEntry, PaletteGlyph};
use crate::ui::styles;
use crate::ui::{App, Field, Icon, Message, Scroller, ThemeMode, Tokens, Tone};

/// The room above the first heading and under the last result.
pub const PAD: f32 = 6.0;
/// The height of a result.
pub const ENTRY: f32 = 34.0;
/// The height of a section's heading.
pub const SECTION: f32 = 25.0;
/// The palette's width.
const WIDTH: f32 = 660.0;
/// The tallest the palette gets.
const HEIGHT: f32 = 560.0;
/// The height of its field's line.
const FIELD: f32 = 48.0;
/// The height of its foot.
const FOOT: f32 = 31.0;
/// The room the results' scroll bar takes beside them.
const BAR: f32 = 12.0;
/// The room at each side of a result, inside its tint.
const INSET: f32 = 10.0;
/// The room of a result's mark.
const GLYPH: f32 = 22.0;
/// The size of a result's muted words.
const SUB: f32 = 12.0;
/// How far below the window's top the palette opens.
pub const TOP: f32 = 96.0;

/// The room the palette keeps under it in a short window.
const UNDER: f32 = 16.0;

/// The palette: the field, the results, the foot; no taller than
/// `room`, the height under its top, leaves.
pub fn view<'a>(app: &'a App, room: f32, tokens: Tokens) -> El<'a> {
    let entries = app.palette_entries();
    let selected = app.palette.selected(entries.len());
    let field = row![
        icon(Icon::Lens, 18.0, tokens.muted),
        text_input("Search bots, tasks, nodes, teams, actions…", app.palette.typed())
            .id(Field::Palette.id())
            .on_input(Message::PaletteTyped)
            .size(16)
            .padding(Padding::from([6, 0]))
            .style(styles::bare_field(tokens)),
        key_cap("Esc", tokens),
    ]
    .spacing(10)
    .height(FIELD)
    .padding(Padding::from([0, 14]))
    .align_y(Alignment::Center);
    let mut results = column![].padding(PAD);
    let mut headings = 0;
    let mut last = None;
    for (at, entry) in entries.iter().enumerate() {
        if last != Some(entry.section) {
            last = Some(entry.section);
            headings += 1;
            results = results.push(
                container(
                    text(entry.section.label().to_uppercase())
                        .size(11.0)
                        .color(tokens.muted),
                )
                .height(SECTION)
                .padding(Padding::from([0, 10]).bottom(3))
                .align_y(Alignment::End),
            );
        }
        results = results.push(line(app, entry, at, selected == Some(at), tokens));
    }
    let tall = 2.0 * PAD + entries.len() as f32 * ENTRY + headings as f32 * SECTION;
    let body: El<'a> = if entries.is_empty() {
        container(text("Nothing found. Tasks are searched among the newest 200.").color(tokens.muted))
            .padding(Padding::from([40, 14]))
            .center_x(Length::Fill)
            .into()
    } else {
        // A thin bar beside the results, not over their right edge.
        scrollable(results)
            .direction(Direction::Vertical(
                Scrollbar::new().width(6).scroller_width(6).margin(2).spacing(2),
            ))
            .id(Scroller::Palette.id())
            .height(tall.min(HEIGHT.min(room - UNDER) - FIELD - FOOT - 2.0).max(ENTRY))
            .into()
    };
    let words = |w: &'a str| text(w).size(SUB).color(tokens.muted);
    let foot = row![
        key_cap("↑", tokens),
        key_cap("↓", tokens),
        words("move"),
        Space::new().width(10),
        key_cap("Enter", tokens),
        words("open"),
        Space::new().width(Length::Fill),
        words("Paste a task id to open it"),
    ]
    .spacing(4)
    .height(FOOT)
    .padding(Padding::from([0, 14]))
    .align_y(Alignment::Center);
    container(column![field, rule(tokens), body, rule(tokens), foot])
        .width(WIDTH)
        .style(styles::palette(tokens))
        .into()
}

/// One result: its mark, its words with what was typed marked, cut to
/// what the muted words after them leave, and those muted words. A
/// click does what it says.
fn line<'a>(app: &App, entry: &PaletteEntry, at: usize, selected: bool, tokens: Tokens) -> El<'a> {
    let sub = entry.sublabel.chars().count() as f32 * 0.52 * SUB;
    let room = WIDTH - 2.0 * PAD - BAR - 2.0 * INSET - GLYPH - 2.0 * 10.0 - sub;
    let short = super::grid::shorten(&entry.label, Ink::Plain.fits(room));
    let (mark, ink) = glyph(app, entry.glyph, tokens);
    button(
        row![
            container(icon(mark, 14.0, ink)).width(GLYPH).center_x(GLYPH),
            container(Ink::Plain.write_marked(short, &entry.marks, tokens))
                .width(Length::Fill)
                .clip(true),
            text(entry.sublabel.clone()).size(SUB).color(tokens.muted),
        ]
        .spacing(10)
        .height(Length::Fill)
        .align_y(Alignment::Center),
    )
    .width(Length::Fill)
    .height(ENTRY)
    .padding(Padding::from([0.0, INSET]))
    .style(styles::palette_entry(tokens, selected))
    .on_press(Message::PalettePicked(at))
    .into()
}

/// The mark before a result and its ink: what it is, and for a bot, a
/// node or a task where it stands, quiet as in the lists.
fn glyph(app: &App, glyph: PaletteGlyph, tokens: Tokens) -> (Icon, iced::Color) {
    let muted = tokens.muted;
    match glyph {
        PaletteGlyph::Go => (Icon::Next, muted),
        PaletteGlyph::New => (Icon::Plus, muted),
        PaletteGlyph::Theme => match app.mode {
            ThemeMode::Light => (Icon::Moon, muted),
            ThemeMode::Dark => (Icon::Sun, muted),
        },
        PaletteGlyph::NeedsYou => (Tone::Wait.icon(), tokens.wait),
        PaletteGlyph::Failed => (Tone::Bad.icon(), tokens.bad),
        PaletteGlyph::Bot(status) => (Tone::of_bot(status).icon(), muted),
        PaletteGlyph::Team => (Icon::Team, muted),
        PaletteGlyph::Node(NodeStatus::Revoked) => (Icon::Ban, muted),
        PaletteGlyph::Node(status) => (Tone::of_node(status).icon(), muted),
        PaletteGlyph::Task(status) => (Tone::of_task(status).icon(), muted),
    }
}

/// The line between the palette's parts.
fn rule<'a>(tokens: Tokens) -> El<'a> {
    container(Space::new().width(Length::Fill).height(1))
        .style(styles::rule(tokens))
        .into()
}
