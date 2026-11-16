use crate::ui::Message;

/// One line of a list: a row, or the header of a group of rows that
/// opens and closes them.
#[derive(Debug, Clone)]
pub enum Line<T> {
    /// A row; `picked` tints it.
    Row { row: T, picked: bool },
    Group {
        name: String,
        /// What the group holds, beside its name: `3 on · 1 paused`.
        note: String,
        open: bool,
        /// What a press on it sends; boxed, as a message is far larger
        /// than a row.
        press: Box<Message>,
    },
}
