use std::fmt;

use crate::domain::{BotId, NodeId};

/// An entry of the bot picker: every bot, or one.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum BotChoice {
    All,
    One(BotId),
}

impl fmt::Display for BotChoice {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::All => f.write_str("All bots"),
            Self::One(id) => write!(f, "{id}"),
        }
    }
}

/// An entry of the node picker: any node, or one.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum NodeChoice {
    Any,
    One(NodeId),
}

impl fmt::Display for NodeChoice {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Any => f.write_str("Any node"),
            Self::One(id) => write!(f, "{id}"),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn shows_the_choice() {
        assert_eq!(BotChoice::All.to_string(), "All bots");
        assert_eq!(BotChoice::One(BotId::new("bot").unwrap()).to_string(), "bot");
        assert_eq!(NodeChoice::Any.to_string(), "Any node");
        assert_eq!(NodeChoice::One(NodeId::new("wrx80").unwrap()).to_string(), "wrx80");
    }
}
