use std::path::PathBuf;

use crate::application::PanelConfig;

/// The operator address the install runbook uses.
const DEFAULT_API: &str = "127.0.0.1:7879";

/// Finds the local install's settings: the state directory, the operator
/// address and the token file the `axlr-bots` unit runs with, with the
/// install's defaults where the unit says nothing.
pub struct InstallDiscovery {
    home: PathBuf,
    xdg_config: Option<PathBuf>,
    xdg_state: Option<PathBuf>,
}

impl InstallDiscovery {
    pub fn new(home: PathBuf, xdg_config: Option<PathBuf>, xdg_state: Option<PathBuf>) -> Self {
        Self {
            home,
            xdg_config,
            xdg_state,
        }
    }

    /// From the process's environment.
    pub fn from_env() -> Self {
        let var = |name: &str| std::env::var_os(name).filter(|v| !v.is_empty()).map(PathBuf::from);
        Self::new(
            var("HOME").unwrap_or_else(|| PathBuf::from("/")),
            var("XDG_CONFIG_HOME"),
            var("XDG_STATE_HOME"),
        )
    }

    /// The user's config directory.
    pub fn config_dir(&self) -> PathBuf {
        self.xdg_config.clone().unwrap_or_else(|| self.home.join(".config"))
    }

    /// The settings for this host: the user unit's when it exists, the
    /// system unit's next, the install's defaults otherwise.
    pub fn discover(&self) -> PanelConfig {
        let units = [
            self.config_dir().join("systemd/user/axlr-bots.service"),
            PathBuf::from("/etc/systemd/system/axlr-bots.service"),
        ];
        let unit = units.iter().find_map(|path| std::fs::read_to_string(path).ok());
        self.from_unit(unit.as_deref())
    }

    /// The settings a unit's `ExecStart` gives, or the defaults.
    pub fn from_unit(&self, unit: Option<&str>) -> PanelConfig {
        let args: Vec<String> = unit
            .and_then(|text| text.lines().find_map(|l| l.trim().strip_prefix("ExecStart=")))
            .map(|line| line.split_whitespace().map(str::to_string).collect())
            .unwrap_or_default();
        let flag = |name: &str| {
            args.iter().enumerate().find_map(|(i, a)| {
                if let Some(value) = a.strip_prefix(&format!("{name}=")) {
                    Some(value.to_string())
                } else if a == name {
                    args.get(i + 1).cloned()
                } else {
                    None
                }
            })
        };
        let state = flag("--state")
            .map(|s| self.expand(&s))
            .unwrap_or_else(|| self.default_state());
        let api = flag("--api-listen").unwrap_or_else(|| DEFAULT_API.to_string());
        let token_file = flag("--api-token-file")
            .map(|s| self.expand(&s))
            .unwrap_or_else(|| state.join("api.token"));
        PanelConfig {
            api_url: format!("https://{}", reachable(&api)),
            ca_path: state.join("ca/ca.crt"),
            token_file,
        }
    }

    fn default_state(&self) -> PathBuf {
        self.xdg_state
            .clone()
            .unwrap_or_else(|| self.home.join(".local/state"))
            .join("axlr-bots")
    }

    fn expand(&self, raw: &str) -> PathBuf {
        let raw = raw.replace("%h", &self.home.to_string_lossy());
        match raw.strip_prefix("~/") {
            Some(rest) => self.home.join(rest),
            None => PathBuf::from(raw),
        }
    }
}

/// A listen address as a client reaches it: a wildcard host is this host.
fn reachable(listen: &str) -> String {
    let (host, port) = match listen.rsplit_once(':') {
        Some((host, port)) => (host, port),
        None => return listen.to_string(),
    };
    match host {
        "" | "0.0.0.0" | "[::]" | "::" => format!("127.0.0.1:{port}"),
        _ => format!("{host}:{port}"),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn discovery() -> InstallDiscovery {
        InstallDiscovery::new(PathBuf::from("/home/op"), None, None)
    }

    #[test]
    fn reads_the_units_exec_start() {
        let unit = "[Service]\nExecStart=/home/op/.local/bin/axlr-bots --state /srv/axlr --socket /srv/axlr/s.sock serve --listen 0.0.0.0:7878 --api-listen 0.0.0.0:7900 --api-token-file=%h/secret.token\n";
        let config = discovery().from_unit(Some(unit));
        assert_eq!(config.api_url, "https://127.0.0.1:7900");
        assert_eq!(config.ca_path, PathBuf::from("/srv/axlr/ca/ca.crt"));
        assert_eq!(config.token_file, PathBuf::from("/home/op/secret.token"));
    }

    #[test]
    fn falls_back_to_the_install_defaults() {
        let config = discovery().from_unit(None);
        assert_eq!(config.api_url, "https://127.0.0.1:7879");
        assert_eq!(
            config.ca_path,
            PathBuf::from("/home/op/.local/state/axlr-bots/ca/ca.crt")
        );
        assert_eq!(
            config.token_file,
            PathBuf::from("/home/op/.local/state/axlr-bots/api.token")
        );
        let xdg = InstallDiscovery::new("/home/op".into(), Some("/cfg".into()), Some("/st".into()));
        assert_eq!(
            xdg.from_unit(Some("ExecStart=axlr-bots serve")).token_file,
            PathBuf::from("/st/axlr-bots/api.token")
        );
        assert_eq!(xdg.config_dir(), PathBuf::from("/cfg"));
        let tilde = discovery().from_unit(Some(
            "ExecStart=axlr-bots --state ~/state serve --api-listen cp.example:7879",
        ));
        assert_eq!(tilde.api_url, "https://cp.example:7879");
        assert_eq!(tilde.ca_path, PathBuf::from("/home/op/state/ca/ca.crt"));
    }

    #[test]
    fn a_wildcard_address_is_this_host() {
        assert_eq!(reachable("[::]:7879"), "127.0.0.1:7879");
        assert_eq!(reachable(":7879"), "127.0.0.1:7879");
        assert_eq!(reachable("10.0.0.2:7879"), "10.0.0.2:7879");
        assert_eq!(reachable("cp.example"), "cp.example");
    }

    #[test]
    fn discovers_from_the_files_on_disk() {
        let home = tempfile::tempdir().unwrap();
        let units = home.path().join(".config/systemd/user");
        std::fs::create_dir_all(&units).unwrap();
        std::fs::write(
            units.join("axlr-bots.service"),
            "ExecStart=axlr-bots --state /var/x serve --api-listen 127.0.0.1:7999\n",
        )
        .unwrap();
        let discovery = InstallDiscovery::new(home.path().to_path_buf(), None, None);
        assert_eq!(discovery.discover().api_url, "https://127.0.0.1:7999");
        let _ = InstallDiscovery::from_env().config_dir();
    }
}
