use iced::widget::svg::Handle;

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
}

impl Icon {
    pub const ALL: [Icon; 28] = [
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
        }
    }

    pub fn handle(self) -> Handle {
        Handle::from_memory(self.bytes())
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
}
