use super::{Screen, ThemeMode};

/// How the panel starts: `--theme light|dark` and `--screen <place>`.
#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub struct Launch {
    pub theme: ThemeMode,
    pub screen: Screen,
}

/// What the command line asks for.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Command {
    Run(Launch),
    Help,
    Version,
}

pub const USAGE: &str =
    "usage: axlr-bots-panel [--theme light|dark] [--screen overview|nodes|bots|bot:<id>|tasks|task:<id>|settings]

The AXLR Bots control panel. Settings live in ~/.config/axlr-bots-panel/config.toml;
the first run fills them from the local install. ICED_BACKEND=tiny-skia draws without a GPU.";

impl Command {
    pub fn parse(args: &[String]) -> Result<Self, String> {
        let mut launch = Launch::default();
        let mut iter = args.iter();
        while let Some(arg) = iter.next() {
            let (flag, inline) = match arg.split_once('=') {
                Some((flag, value)) => (flag, Some(value.to_string())),
                None => (arg.as_str(), None),
            };
            let mut value = || {
                inline
                    .clone()
                    .or_else(|| iter.next().cloned())
                    .ok_or(format!("{flag} needs a value"))
            };
            match flag {
                "-h" | "--help" => return Ok(Self::Help),
                "-V" | "--version" => return Ok(Self::Version),
                "--theme" => {
                    let raw = value()?;
                    launch.theme = ThemeMode::parse(&raw).ok_or(format!("--theme is light or dark, not {raw}"))?;
                }
                "--screen" => {
                    let raw = value()?;
                    launch.screen = Screen::parse(&raw).ok_or(format!("--screen does not know {raw}"))?;
                }
                other => return Err(format!("unknown argument {other}")),
            }
        }
        Ok(Self::Run(launch))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn parse(args: &[&str]) -> Result<Command, String> {
        Command::parse(&args.iter().map(|a| a.to_string()).collect::<Vec<_>>())
    }

    #[test]
    fn reads_the_flags() {
        assert_eq!(parse(&[]), Ok(Command::Run(Launch::default())));
        assert_eq!(
            parse(&["--theme", "dark", "--screen=nodes"]),
            Ok(Command::Run(Launch {
                theme: ThemeMode::Dark,
                screen: Screen::Nodes
            }))
        );
        assert_eq!(parse(&["--help"]), Ok(Command::Help));
        assert_eq!(parse(&["-V"]), Ok(Command::Version));
        assert!(parse(&["--theme"]).unwrap_err().contains("needs a value"));
        assert!(parse(&["--theme", "blue"]).unwrap_err().contains("light or dark"));
        assert!(parse(&["--screen", "home"]).unwrap_err().contains("does not know"));
        assert!(parse(&["--bogus"]).unwrap_err().contains("unknown"));
        assert!(USAGE.contains("--screen"));
    }
}
