//! A list as a sheet: a header that stays, and under it the rows, one
//! line each at the height the settings' density gives, which alone
//! scroll. Groups of rows fold under their own header.

mod column;
mod head;
mod ink;
mod line;

use iced::widget::scrollable::{Direction, Scrollbar};
use iced::widget::{Space, button, column, container, responsive, row, scrollable, text, tooltip};
use iced::{Alignment, Length, Padding};

pub use column::Column;
pub use head::Head;
pub use ink::Ink;
pub use line::Line;

use super::grid;
use super::widgets::{El, PAGE, SMALL, icon, label, semibold};
use crate::application::Density;
use crate::ui::find::Query;
use crate::ui::styles::{self, Kind};
use crate::ui::table::SortDirection;
use crate::ui::{Icon, Message, Tokens};

/// The height of a sheet's header, its line included.
pub const HEAD: f32 = 30.0;
/// The height of a group's header, its line included.
pub const GROUP: f32 = 28.0;
/// The room at each side of a sheet's lines.
pub const SIDE: f32 = 14.0;
/// The room between two columns.
pub const GAP: f32 = 12.0;
/// The width of the accent's bar beside the row the keyboard is on.
const BAR: f32 = 3.0;

/// The sheet of a list: `columns` over `lines`. Only the lines scroll.
/// With `room`, the last line can be scrolled clear of the bar of acts
/// that floats over the sheet's foot.
pub fn sheet<'a, T: Copy + 'a>(
    columns: Vec<Column<'a, T>>,
    lines: Vec<Line<T>>,
    density: Density,
    room: bool,
    tokens: Tokens,
) -> El<'a> {
    responsive(move |space| {
        let widths = widths(&columns, space.width);
        let mut head = row![].spacing(GAP).align_y(Alignment::Center);
        for (column, width) in columns.iter().zip(&widths) {
            head = head.push(cell(header(&column.head, tokens), *width, column.end));
        }
        let mut body = column![];
        for line in &lines {
            body = body.push(match line {
                Line::Row {
                    row: item,
                    picked,
                    selected,
                    press,
                } => {
                    let mut cells = row![].spacing(GAP).align_y(Alignment::Center);
                    for (column, width) in columns.iter().zip(&widths) {
                        cells = cells.push(cell((column.cell)(*item, *width), *width, column.end));
                    }
                    let selected = *selected;
                    let bar =
                        container(Space::new().width(BAR).height(Length::Fill)).style(move |_| container::Style {
                            background: selected.then(|| tokens.accent.into()),
                            ..container::Style::default()
                        });
                    // The row is a button: a click opens it, the pointer
                    // washes it; its box, its name and its acts are
                    // buttons of their own and take their clicks first.
                    let line = button(row![
                        bar,
                        container(cells)
                            .padding(Padding {
                                left: SIDE - BAR,
                                right: SIDE,
                                ..Padding::ZERO
                            })
                            .height(Length::Fill)
                            .align_y(Alignment::Center)
                    ])
                    .width(Length::Fill)
                    .height(density.row_height() - 1.0)
                    .padding(0)
                    .style(styles::row(tokens, *picked || selected))
                    .on_press((**press).clone());
                    strip(line.into(), styles::hairline(tokens))
                }
                Line::Group {
                    name,
                    note,
                    open,
                    press,
                } => group(name, note, *open, (**press).clone(), tokens),
            });
        }
        if room {
            body = body.push(Space::new().height(grid::BAR_ROOM));
        }
        column![
            strip(
                container(head)
                    .padding(Padding::from([0.0, SIDE]))
                    .height(HEAD - 1.0)
                    .align_y(Alignment::Center)
                    .into(),
                styles::rule(tokens)
            ),
            scrollable(body)
                .direction(Direction::Vertical(
                    Scrollbar::new().width(6).scroller_width(6).margin(2)
                ))
                .id(PAGE)
                .width(Length::Fill)
                .height(Length::Fill)
        ]
        .into()
    })
    .into()
}

/// The width in pixels of each column in a sheet `width` wide: the
/// fixed ones as asked, the others sharing what is left by their
/// portion, forty pixels at the least.
fn widths<T>(columns: &[Column<'_, T>], width: f32) -> Vec<f32> {
    let gaps = GAP * columns.len().saturating_sub(1) as f32;
    let fixed: f32 = columns
        .iter()
        .map(|c| match c.width {
            Length::Fixed(px) => px,
            _ => 0.0,
        })
        .sum();
    let portions: f32 = columns
        .iter()
        .map(|c| match c.width {
            Length::FillPortion(portion) => f32::from(portion),
            _ => 0.0,
        })
        .sum();
    let left = (width - 2.0 * SIDE - gaps - fixed).max(0.0);
    columns
        .iter()
        .map(|c| match c.width {
            Length::Fixed(px) => px,
            Length::FillPortion(portion) => (left * f32::from(portion) / portions.max(1.0)).floor().max(40.0),
            _ => 40.0,
        })
        .collect()
}

/// Something in its column: as wide as it, cut at its edge.
fn cell<'a>(content: El<'a>, width: f32, end: bool) -> El<'a> {
    let slot = container(content).width(width).clip(true);
    if end {
        slot.align_x(Alignment::End).into()
    } else {
        slot.into()
    }
}

/// A line of the sheet with the hairline under it.
fn strip<'a>(line: El<'a>, rule: impl Fn(&iced::Theme) -> container::Style + 'a) -> El<'a> {
    column![line, container(Space::new().width(Length::Fill).height(1)).style(rule)].into()
}

/// What stands over a column.
fn header<'a>(head: &Head<'a>, tokens: Tokens) -> El<'a> {
    match head {
        Head::None => Space::new().into(),
        Head::Plain(words) => label(words, tokens).wrapping(text::Wrapping::None).into(),
        Head::Check { all, list } => {
            let list = *list;
            grid::check(*all, move |on| Message::PickShown(list, on), tokens)
        }
        Head::Sort {
            label: words,
            badge,
            column,
        } => {
            let mut line = row![label(words, tokens).wrapping(text::Wrapping::None)]
                .spacing(3)
                .align_y(Alignment::Center);
            if let Some(badge) = badge {
                let glyph = match badge.direction {
                    SortDirection::Ascending => Icon::Up,
                    SortDirection::Descending => Icon::Down,
                };
                line = line.push(icon(glyph, 12.0, tokens.accent));
                if badge.several {
                    line = line.push(
                        semibold(badge.order.to_string())
                            .size(11.0)
                            .color(styles::accent_ink(tokens)),
                    );
                }
            }
            let tip = match badge {
                Some(b) => format!(
                    "Sorted {}. Click to turn it; Shift+click to add a column.",
                    b.direction.describe()
                ),
                None => "Click to sort by it; Shift+click to add it to the order.".to_string(),
            };
            tooltip(
                button(line)
                    .padding(0)
                    .style(styles::button_style(tokens, Kind::Link))
                    .on_press(Message::SortBy(*column)),
                container(text(tip).size(SMALL)).padding(6).style(styles::chip(tokens)),
                tooltip::Position::Bottom,
            )
            .into()
        }
    }
}

/// The header of a group of rows: a caret, its name and what it
/// holds; a press opens or closes it.
fn group<'a>(name: &str, note: &str, open: bool, press: Message, tokens: Tokens) -> El<'a> {
    let caret = if open { Icon::Down } else { Icon::Next };
    let line = button(
        row![
            icon(caret, 10.0, tokens.muted),
            semibold(name.to_string()).size(12.5).color(tokens.muted),
            text(note.to_string()).size(12.5).color(tokens.muted)
        ]
        .spacing(8)
        .height(Length::Fill)
        .align_y(Alignment::Center),
    )
    .width(Length::Fill)
    .height(GROUP - 1.0)
    .padding(Padding::from([0.0, SIDE]))
    .style(styles::button_style(tokens, Kind::Link))
    .on_press(press);
    strip(
        container(line).width(Length::Fill).style(styles::group(tokens)).into(),
        styles::rule(tokens),
    )
}

/// Words on one line in a cell `width` wide: cut with an ellipsis to
/// the characters that fit, whole on hover when cut.
pub fn words<'a>(words: impl Into<String>, width: f32, ink: Ink, tokens: Tokens) -> El<'a> {
    found(words, width, ink, &Query::default(), tokens)
}

/// Words on one line in a cell `width` wide, as `words` draws them,
/// with the parts the list's filter found on the mark's wash: in what
/// shows of them, once cut.
pub fn found<'a>(words: impl Into<String>, width: f32, ink: Ink, query: &Query, tokens: Tokens) -> El<'a> {
    let words = words.into();
    let short = grid::shorten(&words, ink.fits(width));
    let line = container(ink.write_found(short.clone(), query, tokens)).clip(true);
    if short == words {
        return line.into();
    }
    tip(line.into(), words, tokens)
}

/// A row's name on one line in a cell `width` wide, which opens the
/// row's own page: ink, the accent under the pointer; cut with an
/// ellipsis, whole on hover when cut; what the filter found in it on
/// the mark's wash.
pub fn link<'a>(words: impl Into<String>, width: f32, press: Message, query: &Query, tokens: Tokens) -> El<'a> {
    let words = words.into();
    let short = grid::shorten(&words, Ink::Strong.fits(width));
    let cut = short != words;
    let line: El<'a> = button(Ink::Strong.write_found(short, query, tokens))
        .padding(0)
        .style(styles::button_style(tokens, Kind::Link))
        .on_press(press)
        .into();
    if cut { tip(line, words, tokens) } else { line }
}

/// Two parts on one line in a cell `width` wide: the first in ink, the
/// second muted after it in what the first leaves, both cut to fit and
/// whole on hover when cut; what the filter found in them on the mark's
/// wash.
pub fn pair<'a>(first: &str, second: &str, width: f32, query: &Query, tokens: Tokens) -> El<'a> {
    let fit = Ink::Plain.fits(width);
    let head = grid::shorten(first, fit);
    let used = head.chars().count();
    // The second part needs room to say something: a few words.
    let tail = if second.is_empty() || fit < used + 12 {
        String::new()
    } else {
        grid::shorten(second, fit - used - 3)
    };
    let cut = head != first || tail != second;
    let mut line = row![Ink::Plain.write_found(head, query, tokens)]
        .spacing(8)
        .align_y(Alignment::Center);
    if !tail.is_empty() {
        line = line.push(Ink::Muted.write_found(tail, query, tokens));
    }
    let line = container(line).clip(true);
    if !cut {
        return line.into();
    }
    let whole = if second.is_empty() {
        first.to_string()
    } else {
        format!("{first}\n{second}")
    };
    tip(line.into(), whole, tokens)
}

/// `content` with `words` on hover, in a box that wraps them.
pub fn tip<'a>(content: El<'a>, words: String, tokens: Tokens) -> El<'a> {
    tooltip(
        content,
        container(text(words).size(SMALL))
            .padding(6)
            .max_width(560)
            .style(styles::chip(tokens)),
        tooltip::Position::Bottom,
    )
    .into()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn columns() -> Vec<Column<'static, u8>> {
        vec![
            Column::fixed(Head::None, 100.0, |_, _| Space::new().into()),
            Column::share(Head::Plain("What"), 3, |_, _| Space::new().into()),
            Column::share(Head::Plain("Who"), 1, |_, _| Space::new().into()).end(),
        ]
    }

    #[test]
    fn fixed_columns_keep_their_width_and_the_others_share_the_rest() {
        let columns = columns();
        // 600 wide: 28 of sides and 24 of gaps leave 548; 100 are fixed.
        assert_eq!(widths(&columns, 600.0), vec![100.0, 336.0, 112.0]);
        assert_eq!(widths(&columns, 0.0), vec![100.0, 40.0, 40.0]);
        assert!(columns[2].end && !columns[0].end);
    }
}
