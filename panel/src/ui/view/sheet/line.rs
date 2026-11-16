use crate::ui::Message;

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
        /// What a click on the row sends; boxed, as a message is far
        /// larger than a row.
        press: Box<Message>,
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
