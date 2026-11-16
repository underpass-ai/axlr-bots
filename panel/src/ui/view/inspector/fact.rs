use iced::widget::text::Wrapping;
use iced::widget::{button, row, text};
use iced::{Alignment, Font};

use super::super::grid;
use super::super::widgets::{El, SMALL, icon};
use super::EM;
use crate::ui::styles::{self, Kind};
use crate::ui::{Icon, Message, Tokens};

/// A value among the facts: words, or an id in the fixed face with a
/// button that copies it whole.
pub enum Fact {
    Words(String),
    Copy(String),
}

impl Fact {
    pub fn draw<'a>(&self, width: f32, tokens: Tokens) -> El<'a> {
        match self {
            Self::Words(words) => {
                let fits = ((width / (EM * SMALL)).floor() as usize).max(4);
                text(grid::shorten(words, fits))
                    .size(SMALL)
                    .wrapping(Wrapping::None)
                    .into()
            }
            Self::Copy(value) if value.is_empty() => text("—").size(SMALL).into(),
            Self::Copy(value) => {
                let fits = (((width - 20.0) / (0.61 * 12.5)).floor() as usize).max(4);
                row![
                    text(grid::shorten(value, fits))
                        .size(12.5)
                        .font(Font::MONOSPACE)
                        .wrapping(Wrapping::None),
                    button(icon(Icon::Copy, 12.0, tokens.muted))
                        .padding(2)
                        .style(styles::button_style(tokens, Kind::Ghost))
                        .on_press(Message::Copy(value.clone())),
                ]
                .spacing(4)
                .align_y(Alignment::Center)
                .into()
            }
        }
    }
}
