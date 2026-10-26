use crate::domain::Bot;

/// A bot stopped: paused, and how many of its open tasks were cancelled
/// or could not be.
#[derive(Debug, Clone, PartialEq)]
pub struct StoppedBot {
    pub bot: Bot,
    pub cancelled: u32,
    pub not_cancelled: u32,
}
