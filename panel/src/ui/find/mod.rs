//! Finding things in the panel: what a person typed and how a text
//! answers it, the lists as they are shown (repeats folded, rows in
//! groups, the row the keyboard is on), the command palette and the
//! find of a task's activity. Plain state the views read; no widget
//! here.

mod activity_find;
mod collapsed;
mod drawn_rows;
mod fold_repeats;
mod folded;
mod group_key;
mod marks;
mod matcher;
mod node_rows;
mod palette;
mod palette_entry;
mod palette_glyph;
mod palette_section;
mod palette_target;
mod place;
mod query;
mod row_cursor;
mod row_spot;
mod score;
mod searchable;
mod step_filter;
mod step_kind;
mod step_text;
mod task_group;
mod task_grouping;
mod task_line;
mod task_what;
mod team_group;
#[cfg(test)]
mod test_rows;
mod text_match;

pub use activity_find::ActivityFind;
pub use collapsed::Collapsed;
pub use drawn_rows::DrawnRows;
pub use fold_repeats::FoldRepeats;
use folded::Folded;
pub use group_key::GroupKey;
pub use marks::Marks;
pub use matcher::Matcher;
pub use node_rows::NodeRows;
pub use palette::Palette;
pub use palette_entry::PaletteEntry;
pub use palette_glyph::PaletteGlyph;
pub use palette_section::PaletteSection;
pub use palette_target::PaletteTarget;
pub use place::Place;
pub use query::Query;
pub use row_cursor::RowCursor;
pub use row_spot::RowSpot;
pub use score::Score;
pub use searchable::Searchable;
pub use step_filter::StepFilter;
pub use step_kind::StepKind;
pub use step_text::StepText;
pub use task_group::TaskGroup;
pub use task_grouping::TaskGrouping;
pub use task_line::TaskLine;
pub use task_what::TaskWhat;
pub use team_group::{NO_TEAM, TeamGroup};
pub use text_match::TextMatch;
