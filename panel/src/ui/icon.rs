use iced::widget::svg::Handle;

use crate::domain::{IncidentSeverity, IncidentStatus};

/// The panel's pixel glyphs (`assets/icons`) and the brand's marks.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum Icon {
    Overview,
    Nodes,
    Bots,
    Tasks,
    Settings,
    Check,
    Cross,
    Stop,
    Pause,
    Play,
    Plus,
    Copy,
    Key,
    Ban,
    Clock,
    Coin,
    Back,
    File,
    Alert,
    Hourglass,
    Sun,
    Moon,
    Dot,
    Bolt,
    Pin,
    Wrench,
    Chip,
    Trace,
    Team,
    /// A sorted column, its smallest first.
    Up,
    /// A sorted column, its largest first.
    Down,
    Trash,
    /// Open: go to the row's own page.
    Next,
    /// A cause found: an incident diagnosed.
    Lens,
}

impl Icon {
    pub const ALL: [Icon; 34] = [
        Icon::Overview,
        Icon::Nodes,
        Icon::Bots,
        Icon::Tasks,
        Icon::Settings,
        Icon::Check,
        Icon::Cross,
        Icon::Stop,
        Icon::Pause,
        Icon::Play,
        Icon::Plus,
        Icon::Copy,
        Icon::Key,
        Icon::Ban,
        Icon::Clock,
        Icon::Coin,
        Icon::Back,
        Icon::File,
        Icon::Alert,
        Icon::Hourglass,
        Icon::Sun,
        Icon::Moon,
        Icon::Dot,
        Icon::Bolt,
        Icon::Pin,
        Icon::Wrench,
        Icon::Chip,
        Icon::Trace,
        Icon::Team,
        Icon::Up,
        Icon::Down,
        Icon::Trash,
        Icon::Next,
        Icon::Lens,
    ];

    fn bytes(self) -> &'static [u8] {
        match self {
            Icon::Overview => include_bytes!("../../assets/icons/overview.svg"),
            Icon::Nodes => include_bytes!("../../assets/icons/nodes.svg"),
            Icon::Bots => include_bytes!("../../assets/icons/bots.svg"),
            Icon::Tasks => include_bytes!("../../assets/icons/tasks.svg"),
            Icon::Settings => include_bytes!("../../assets/icons/settings.svg"),
            Icon::Check => include_bytes!("../../assets/icons/check.svg"),
            Icon::Cross => include_bytes!("../../assets/icons/cross.svg"),
            Icon::Stop => include_bytes!("../../assets/icons/stop.svg"),
            Icon::Pause => include_bytes!("../../assets/icons/pause.svg"),
            Icon::Play => include_bytes!("../../assets/icons/play.svg"),
            Icon::Plus => include_bytes!("../../assets/icons/plus.svg"),
            Icon::Copy => include_bytes!("../../assets/icons/copy.svg"),
            Icon::Key => include_bytes!("../../assets/icons/key.svg"),
            Icon::Ban => include_bytes!("../../assets/icons/ban.svg"),
            Icon::Clock => include_bytes!("../../assets/icons/clock.svg"),
            Icon::Coin => include_bytes!("../../assets/icons/coin.svg"),
            Icon::Back => include_bytes!("../../assets/icons/back.svg"),
            Icon::File => include_bytes!("../../assets/icons/file.svg"),
            Icon::Alert => include_bytes!("../../assets/icons/alert.svg"),
            Icon::Hourglass => include_bytes!("../../assets/icons/hourglass.svg"),
            Icon::Sun => include_bytes!("../../assets/icons/sun.svg"),
            Icon::Moon => include_bytes!("../../assets/icons/moon.svg"),
            Icon::Dot => include_bytes!("../../assets/icons/dot.svg"),
            Icon::Bolt => include_bytes!("../../assets/icons/bolt.svg"),
            Icon::Pin => include_bytes!("../../assets/icons/pin.svg"),
            Icon::Wrench => include_bytes!("../../assets/icons/wrench.svg"),
            Icon::Chip => include_bytes!("../../assets/icons/chip.svg"),
            Icon::Trace => include_bytes!("../../assets/icons/trace.svg"),
            Icon::Team => include_bytes!("../../assets/icons/team.svg"),
            Icon::Up => include_bytes!("../../assets/icons/up.svg"),
            Icon::Down => include_bytes!("../../assets/icons/down.svg"),
            Icon::Trash => include_bytes!("../../assets/icons/trash.svg"),
            Icon::Next => include_bytes!("../../assets/icons/next.svg"),
            Icon::Lens => include_bytes!("../../assets/icons/lens.svg"),
        }
    }

    pub fn handle(self) -> Handle {
        Handle::from_memory(self.bytes())
    }

    /// A severity is a level of alarm, never a state of work: the alert
    /// for the two that page someone, a dot for the milder ones.
    pub fn of_severity(severity: IncidentSeverity) -> Self {
        match severity {
            IncidentSeverity::Sev1 | IncidentSeverity::Sev2 => Self::Alert,
            IncidentSeverity::Sev3 | IncidentSeverity::Sev4 | IncidentSeverity::Unknown => Self::Dot,
        }
    }

    /// An incident's status: a diagnosed one shows the lens of a cause
    /// found, not a run, a proposed one the wrench of a fix; the others
    /// keep their tone's icon.
    pub fn of_incident(status: IncidentStatus) -> Self {
        match status {
            IncidentStatus::Diagnosed => Self::Lens,
            IncidentStatus::Proposed => Self::Wrench,
            other => super::Tone::of_incident(other).icon(),
        }
    }
}

/// The brand's marks.
pub mod brand {
    use iced::widget::svg::Handle;

    /// The one-line emblem for the header, in the version for the theme.
    pub fn emblem(dark: bool) -> Handle {
        if dark {
            Handle::from_memory(include_bytes!("../../assets/brand/axlr-bots-emblem-stacked-ink-dark.svg").as_slice())
        } else {
            Handle::from_memory(include_bytes!("../../assets/brand/axlr-bots-emblem-stacked-ink-light.svg").as_slice())
        }
    }

    /// The invader icon, square, for the window.
    pub const WINDOW_ICON_PNG: &[u8] = include_bytes!("../../assets/brand/axlr-bots-icon-128.png");
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn every_icon_is_an_svg() {
        for icon in Icon::ALL {
            let text = std::str::from_utf8(icon.bytes()).unwrap();
            assert!(
                text.starts_with("<svg") && text.contains("viewBox=\"0 0 16 16\""),
                "{icon:?}"
            );
            let _ = icon.handle();
        }
        let _ = brand::emblem(true);
        let _ = brand::emblem(false);
        assert_eq!(&brand::WINDOW_ICON_PNG[1..4], b"PNG");
    }

    #[test]
    fn incidents_never_borrow_the_icons_of_work() {
        assert_eq!(Icon::of_severity(IncidentSeverity::Sev1), Icon::Alert);
        assert_eq!(Icon::of_severity(IncidentSeverity::Sev3), Icon::Dot);
        assert_ne!(Icon::of_severity(IncidentSeverity::Sev4), Icon::Pause);
        assert_eq!(Icon::of_incident(IncidentStatus::Diagnosed), Icon::Lens);
        assert_eq!(Icon::of_incident(IncidentStatus::Proposed), Icon::Wrench);
        assert_eq!(Icon::of_incident(IncidentStatus::Open), Icon::Cross);
        assert_eq!(Icon::of_incident(IncidentStatus::Resolved), Icon::Check);
    }
}
