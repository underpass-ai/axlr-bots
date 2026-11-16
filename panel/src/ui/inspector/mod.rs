//! The inspector beside a list: which of its tabs is open for each kind
//! of row, remembered while the person moves from row to row. Plain
//! state the views read; no widget here.

mod bot_tab;
mod tab;
mod tabs;
mod task_tab;
mod team_tab;

pub use bot_tab::BotTab;
pub use tab::Tab;
pub use tabs::Tabs;
pub use task_tab::TaskTab;
pub use team_tab::TeamTab;
