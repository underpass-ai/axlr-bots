//! The composition root: reads the command line and the settings, wires
//! the adapters to the ports and starts the window.

use std::sync::Arc;

use axlr_bots_panel::adapters::config::{InstallDiscovery, TomlConfigStore, TomlShortcuts};
use axlr_bots_panel::adapters::http::HttpConnector;
use axlr_bots_panel::adapters::system::{DesktopNotifier, DialogFilePicker, FsDefinitionReader, SystemClock, UuidKeys};
use axlr_bots_panel::application::Connect;
use axlr_bots_panel::ui::{App, Command, Services, USAGE, launch};

fn main() -> std::process::ExitCode {
    let args: Vec<String> = std::env::args().skip(1).collect();
    let launch_options = match Command::parse(&args) {
        Ok(Command::Run(options)) => options,
        Ok(Command::Help) => {
            println!("{USAGE}");
            return std::process::ExitCode::SUCCESS;
        }
        Ok(Command::Version) => {
            println!("axlr-bots-panel {}", env!("CARGO_PKG_VERSION"));
            return std::process::ExitCode::SUCCESS;
        }
        Err(problem) => {
            eprintln!("axlr-bots-panel: {problem}\n\n{USAGE}");
            return std::process::ExitCode::from(2);
        }
    };
    let discovery = InstallDiscovery::from_env();
    let store = Arc::new(TomlConfigStore::new(
        discovery.config_dir().join("axlr-bots-panel/config.toml"),
        discovery.discover(),
    ));
    let shortcuts = Arc::new(TomlShortcuts::new(
        discovery.config_dir().join("axlr-bots-panel/shortcuts.toml"),
    ));
    let clock = Arc::new(SystemClock);
    let connect = Arc::new(Connect::new(Arc::new(HttpConnector), store, clock.clone()));
    let result = launch(move || {
        let (config, saved) = connect.initial();
        let services = Services {
            control_plane: None,
            clock: clock.clone(),
            keys: Arc::new(UuidKeys),
            picker: Arc::new(DialogFilePicker),
            reader: Arc::new(FsDefinitionReader),
            connect: connect.clone(),
            notifier: Arc::new(DesktopNotifier),
            shortcuts: shortcuts.clone(),
        };
        App::new(services, launch_options.clone(), &config, saved)
    });
    match result {
        Ok(()) => std::process::ExitCode::SUCCESS,
        Err(error) => {
            eprintln!("axlr-bots-panel: {error}");
            std::process::ExitCode::FAILURE
        }
    }
}
