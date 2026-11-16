/// A place of the panel a person goes to by name: the entries of its
/// rail.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Place {
    Home,
    Tasks,
    Bots,
    Teams,
    Nodes,
    Settings,
}

impl Place {
    /// Every place, in the order the rail shows them.
    pub const ALL: [Place; 6] = [
        Self::Home,
        Self::Tasks,
        Self::Bots,
        Self::Teams,
        Self::Nodes,
        Self::Settings,
    ];

    /// Its name on the rail.
    pub fn label(self) -> &'static str {
        match self {
            Self::Home => "Home",
            Self::Tasks => "Tasks",
            Self::Bots => "Bots",
            Self::Teams => "Teams",
            Self::Nodes => "Nodes",
            Self::Settings => "Settings",
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn every_place_has_its_name_once() {
        let names: Vec<&str> = Place::ALL.into_iter().map(Place::label).collect();
        assert_eq!(names, vec!["Home", "Tasks", "Bots", "Teams", "Nodes", "Settings"]);
    }
}
