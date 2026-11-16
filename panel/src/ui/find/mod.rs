//! Finding things in the panel: what a person typed and how a text
//! answers it, the lists as they are shown (repeats folded, rows in
//! groups, the row the keyboard is on), the command palette and the
//! find of a task's activity. Plain state the views read; no widget
//! here.

mod folded;
mod marks;
mod matcher;
mod query;
mod score;
mod text_match;

use folded::Folded;
pub use marks::Marks;
pub use matcher::Matcher;
pub use query::Query;
pub use score::Score;
pub use text_match::TextMatch;
