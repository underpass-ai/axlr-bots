use super::Bot;

/// A team of bots: a name its members share. The control plane keeps no
/// team of its own; the panel reads it from the bots, as it lists them.
#[derive(Debug, Clone, PartialEq)]
pub struct Team {
    pub name: String,
    pub members: Vec<Bot>,
}

impl Team {
    /// The teams the bots name, sorted, each with its members sorted by id.
    pub fn from_bots(bots: &[Bot]) -> Vec<Team> {
        let mut teams: Vec<Team> = Vec::new();
        for bot in bots {
            for name in &bot.groups {
                match teams.iter_mut().find(|t| &t.name == name) {
                    Some(team) => team.members.push(bot.clone()),
                    None => teams.push(Team {
                        name: name.clone(),
                        members: vec![bot.clone()],
                    }),
                }
            }
        }
        for team in &mut teams {
            team.members.sort_by(|a, b| a.id.as_str().cmp(b.id.as_str()));
        }
        teams.sort_by(|a, b| a.name.cmp(&b.name));
        teams
    }

    /// A team of one bot: most likely a name typed differently.
    pub fn lonely(&self) -> bool {
        self.members.len() == 1
    }

    /// What a member is for, in one line: its mission's first sentence,
    /// cut at 120 characters.
    pub fn purpose(bot: &Bot) -> String {
        let text = bot.mission.split_whitespace().collect::<Vec<_>>().join(" ");
        let mut end = text.len();
        for (i, c) in text.char_indices() {
            let after = i + c.len_utf8();
            if matches!(c, '.' | '!' | '?') && (after == text.len() || text[after..].starts_with(' ')) {
                end = after;
                break;
            }
        }
        let first = &text[..end];
        if first.chars().count() > 120 {
            let cut: String = first.chars().take(119).collect();
            format!("{}…", cut.trim_end())
        } else {
            first.to_string()
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn bot(id: &str, groups: &[&str], mission: &str) -> Bot {
        serde_json::from_value(serde_json::json!({
            "id": id, "name": id, "mission": mission, "groups": groups, "status": "Enabled",
            "createdAt": "2026-10-26T10:00:00Z", "updatedAt": "2026-10-26T10:00:00Z"
        }))
        .unwrap()
    }

    #[test]
    fn reads_the_teams_from_the_bots() {
        let bots = vec![
            bot("quad-1", &["port"], "Fly quad-1. Then land."),
            bot("plane", &["port", "air"], "Fly the coast."),
            bot("alone", &[], "Nothing."),
        ];
        let teams = Team::from_bots(&bots);
        assert_eq!(
            teams.iter().map(|t| t.name.as_str()).collect::<Vec<_>>(),
            vec!["air", "port"]
        );
        assert_eq!(
            teams[1].members.iter().map(|b| b.id.as_str()).collect::<Vec<_>>(),
            vec!["plane", "quad-1"]
        );
        assert!(teams[0].lonely() && !teams[1].lonely());
        assert_eq!(Team::purpose(&bots[0]), "Fly quad-1.");
        assert_eq!(Team::purpose(&bots[1]), "Fly the coast.");
        let long = bot("x", &[], &"word ".repeat(40));
        assert_eq!(Team::purpose(&long).chars().count(), 120);
        assert_eq!(
            Team::purpose(&bot("v", &[], "Version 0.15 is out. Next.")),
            "Version 0.15 is out."
        );
    }
}
