use std::hash::{DefaultHasher, Hash, Hasher};

/// One thing on the screen the pointer can rest on: a card, a row of a
/// list, a tile, the inspector's header. A view names it by anything that
/// tells it apart from the others (`Surface::of(("task row", &task.id))`);
/// the name is kept as its hash, so a surface is small and cheap to copy
/// into a message.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub struct Surface(u64);

impl Surface {
    pub fn of(name: impl Hash) -> Self {
        let mut hasher = DefaultHasher::new();
        name.hash(&mut hasher);
        Self(hasher.finish())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_same_name_is_the_same_surface() {
        assert_eq!(Surface::of(("row", "task-1")), Surface::of(("row", "task-1")));
        assert_ne!(Surface::of(("row", "task-1")), Surface::of(("row", "task-2")));
        assert_ne!(Surface::of(("row", "task-1")), Surface::of(("card", "task-1")));
    }
}
