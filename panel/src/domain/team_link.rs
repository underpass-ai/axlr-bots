use serde::{Deserialize, Serialize};

/// One link of the team channel, as the control plane derives it from
/// the bots: team `from` may ask team `to`, because its `askers` name
/// `to` and the `responders` of `to` answer `from`.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct TeamLink {
    pub from: String,
    pub to: String,
    #[serde(default)]
    pub askers: Vec<String>,
    #[serde(default)]
    pub responders: Vec<String>,
}

impl TeamLink {
    /// The link seen from `team`: `asks dev`, `asked by qa`, or the two
    /// teams when it is neither's.
    pub fn heading(&self, team: &str) -> String {
        if self.from == team {
            format!("asks {}", self.to)
        } else if self.to == team {
            format!("asked by {}", self.from)
        } else {
            format!("{} asks {}", self.from, self.to)
        }
    }

    /// Who asks and who answers: `qa-jefe → dev-a, dev-b`.
    pub fn who(&self) -> String {
        let names = |bots: &[String]| {
            if bots.is_empty() {
                "nobody".to_string()
            } else {
                bots.join(", ")
            }
        };
        format!("{} → {}", names(&self.askers), names(&self.responders))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn says_who_asks_whom_from_either_side() {
        let link: TeamLink =
            serde_json::from_str(r#"{"from":"qa","to":"dev","askers":["qa-jefe"],"responders":["dev-a","dev-b"]}"#)
                .unwrap();
        assert_eq!(link.heading("qa"), "asks dev");
        assert_eq!(link.heading("dev"), "asked by qa");
        assert_eq!(link.heading("sre"), "qa asks dev");
        assert_eq!(link.who(), "qa-jefe → dev-a, dev-b");
        let bare: TeamLink = serde_json::from_str(r#"{"from":"qa","to":"dev"}"#).unwrap();
        assert_eq!(bare.who(), "nobody → nobody");
    }
}
