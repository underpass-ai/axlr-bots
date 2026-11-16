use super::organic::Surface;
use super::table::RowRef;
use crate::domain::BotId;

/// A place of the panel that glows under the pointer, named by what it
/// shows: the glow follows its row, card or tile, never a place on the
/// screen, so a list that changes under a still pointer moves no glow
/// to another row.
#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub enum HoverSpot {
    /// A row of a list.
    Row(RowRef),
    /// The inspector's header, over the row it shows.
    Head(RowRef),
    /// A card of Home, by its title: Fleet, Latest, Nodes.
    Card(&'static str),
    /// A number of Home's strip, by its words.
    Number(&'static str),
    /// A bot's tile in Home's fleet.
    Tile(BotId),
}

impl HoverSpot {
    /// The surface the organic glow knows it by.
    pub fn surface(&self) -> Surface {
        Surface::of(self)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::domain::TaskId;

    #[test]
    fn each_place_is_its_own_surface_and_the_same_place_the_same() {
        let task = || RowRef::Task(TaskId::new("task-1").unwrap());
        assert_eq!(HoverSpot::Row(task()).surface(), HoverSpot::Row(task()).surface());
        assert_ne!(HoverSpot::Row(task()).surface(), HoverSpot::Head(task()).surface());
        assert_ne!(
            HoverSpot::Row(task()).surface(),
            HoverSpot::Row(RowRef::Task(TaskId::new("task-2").unwrap())).surface()
        );
        assert_ne!(HoverSpot::Card("Fleet").surface(), HoverSpot::Number("Fleet").surface());
        let bot = BotId::new("ops-watch").unwrap();
        assert_ne!(
            HoverSpot::Tile(bot.clone()).surface(),
            HoverSpot::Row(RowRef::Bot(bot)).surface()
        );
    }
}
