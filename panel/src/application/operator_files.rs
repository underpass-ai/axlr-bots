use std::path::PathBuf;

use super::PanelConfig;

/// The operator's client certificate and key as the panel will read
/// them: the ones the settings name, or where `axlr-bots ca operator
/// issue` leaves them for the local operator, the same place the CLI
/// looks: `$AXLR_BOTS_OPERATOR_DIR`, else
/// `<config dir>/axlr-bots/operator/operator.{crt,key}`.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct OperatorFiles {
    pub cert: PathBuf,
    pub key: PathBuf,
}

impl OperatorFiles {
    const CERT: &'static str = "operator.crt";
    const KEY: &'static str = "operator.key";

    /// The files for these settings; `env` answers an environment
    /// variable as a path.
    pub fn resolve(config: &PanelConfig, env: &dyn Fn(&str) -> Option<PathBuf>) -> Self {
        let dir = Self::default_dir(env);
        Self {
            cert: config.cert_path.clone().unwrap_or_else(|| dir.join(Self::CERT)),
            key: config.key_path.clone().unwrap_or_else(|| dir.join(Self::KEY)),
        }
    }

    /// Where `ca operator issue` writes without `--out`.
    pub fn default_dir(env: &dyn Fn(&str) -> Option<PathBuf>) -> PathBuf {
        if let Some(dir) = env("AXLR_BOTS_OPERATOR_DIR") {
            return dir;
        }
        let config = env("XDG_CONFIG_HOME")
            .or_else(|| env("HOME").map(|home| home.join(".config")))
            .unwrap_or_else(|| PathBuf::from("."));
        config.join("axlr-bots/operator")
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn env(pairs: &'static [(&'static str, &'static str)]) -> impl Fn(&str) -> Option<PathBuf> {
        move |name| pairs.iter().find(|(k, _)| *k == name).map(|(_, v)| PathBuf::from(v))
    }

    fn config() -> PanelConfig {
        PanelConfig {
            api_url: "https://h:7880".into(),
            ca_path: "ca.crt".into(),
            token_file: "api.token".into(),
            actor: None,
            cert_path: None,
            key_path: None,
        }
    }

    #[test]
    fn looks_where_ca_operator_issue_leaves_the_files() {
        let home = env(&[("HOME", "/home/op")]);
        let files = OperatorFiles::resolve(&config(), &home);
        assert_eq!(
            files.cert,
            PathBuf::from("/home/op/.config/axlr-bots/operator/operator.crt")
        );
        assert_eq!(
            files.key,
            PathBuf::from("/home/op/.config/axlr-bots/operator/operator.key")
        );
        let xdg = env(&[("HOME", "/home/op"), ("XDG_CONFIG_HOME", "/cfg")]);
        assert_eq!(
            OperatorFiles::default_dir(&xdg),
            PathBuf::from("/cfg/axlr-bots/operator")
        );
        let dir = env(&[("AXLR_BOTS_OPERATOR_DIR", "/ops"), ("HOME", "/home/op")]);
        assert_eq!(
            OperatorFiles::resolve(&config(), &dir).cert,
            PathBuf::from("/ops/operator.crt")
        );
        assert_eq!(
            OperatorFiles::default_dir(&env(&[])),
            PathBuf::from("./axlr-bots/operator")
        );
    }

    #[test]
    fn the_settings_win_over_the_default_place() {
        let mut named = config();
        named.cert_path = Some("/x/c.pem".into());
        let files = OperatorFiles::resolve(&named, &env(&[("HOME", "/home/op")]));
        assert_eq!(files.cert, PathBuf::from("/x/c.pem"));
        assert_eq!(
            files.key,
            PathBuf::from("/home/op/.config/axlr-bots/operator/operator.key")
        );
    }
}
