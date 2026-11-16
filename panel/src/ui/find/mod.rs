//! Finding things in the panel: what a person typed and how a text
//! answers it, the lists as they are shown (repeats folded, rows in
//! groups, the row the keyboard is on), the command palette and the
//! find of a task's activity. Plain state the views read; no widget
//! here.

mod collapsed;
mod fold_repeats;
mod folded;
mod group_key;
mod marks;
mod matcher;
mod node_rows;
mod query;
mod row_cursor;
mod score;
mod searchable;
mod task_group;
mod task_grouping;
mod task_line;
mod task_what;
mod team_group;
#[cfg(test)]
mod test_rows;
mod text_match;

pub use collapsed::Collapsed;
pub use fold_repeats::FoldRepeats;
use folded::Folded;
pub use group_key::GroupKey;
pub use marks::Marks;
pub use matcher::Matcher;
pub use node_rows::NodeRows;
pub use query::Query;
pub use row_cursor::RowCursor;
pub use score::Score;
pub use searchable::Searchable;
pub use task_group::TaskGroup;
pub use task_grouping::TaskGrouping;
pub use task_line::TaskLine;
pub use task_what::TaskWhat;
pub use team_group::{NO_TEAM, TeamGroup};
pub use text_match::TextMatch;
