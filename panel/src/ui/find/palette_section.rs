/// A part of the command palette's results, under its own heading. The
/// results come in this order: what the panel can do, then the bots,
/// the teams, the nodes and the tasks.
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
pub enum PaletteSection {
    Actions,
    Bots,
    Teams,
    Nodes,
    Tasks,
}

impl PaletteSection {
    /// Every section, in the order the palette shows them.
    pub const ALL: [PaletteSection; 5] = [Self::Actions, Self::Bots, Self::Teams, Self::Nodes, Self::Tasks];

    /// Its heading.
    pub fn label(self) -> &'static str {
        match self {
            Self::Actions => "Actions",
            Self::Bots => "Bots",
            Self::Teams => "Teams",
            Self::Nodes => "Nodes",
            Self::Tasks => "Tasks",
        }
    }

    /// The most results it shows, so every section fits without a
    /// scroll: with nothing typed the acts lead; once a person types,
    /// the bots take the room.
    pub fn cap(self, searching: bool) -> usize {
        match (self, searching) {
            (Self::Actions, false) => 6,
            (Self::Actions, true) => 4,
            (Self::Bots, false) => 4,
            (Self::Bots, true) => 7,
            (Self::Teams, _) => 3,
            (Self::Nodes, _) => 4,
            (Self::Tasks, _) => 8,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn come_in_order_under_their_headings() {
        let labels: Vec<&str> = PaletteSection::ALL.into_iter().map(PaletteSection::label).collect();
        assert_eq!(labels, vec!["Actions", "Bots", "Teams", "Nodes", "Tasks"]);
        assert!(PaletteSection::ALL.windows(2).all(|w| w[0] < w[1]));
    }

    #[test]
    fn typing_gives_the_bots_the_room_of_the_acts() {
        let caps = |searching| -> Vec<usize> { PaletteSection::ALL.into_iter().map(|s| s.cap(searching)).collect() };
        assert_eq!(caps(false), vec![6, 4, 3, 4, 8]);
        assert_eq!(caps(true), vec![4, 7, 3, 4, 8]);
    }
}
