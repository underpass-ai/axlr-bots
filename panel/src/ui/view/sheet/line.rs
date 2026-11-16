use crate::ui::Message;
use crate::ui::table::RowRef;

/// One line of a list: a row, or the header of a group of rows that
/// opens and closes them.
#[derive(Debug, Clone)]
pub enum Line<T> {
    Row {
        row: T,
        /// Picked for an act: the picked wash.
        picked: bool,
        /// The keyboard is on it: the picked wash and the accent's bar.
        selected: bool,
        /// The row a click opens in the inspector, and the name its
        /// glow under the pointer goes by.
        opens: RowRef,
    },
    Group {
        name: String,
        /// What the group holds, beside its name: `3 on · 1 paused`.
        note: String,
        open: bool,
        /// What a press on it sends.
        press: Box<Message>,
    },
}
