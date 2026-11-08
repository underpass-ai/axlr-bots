//! The words of the screen on show that a person can select a part of:
//! a detail page's prose and ids. The views draw them with readers; the
//! state keeps one reader per passage (`Readers::sync`).

use super::{App, ReaderKey, Screen, SubmitForm};
use crate::domain::{Bot, Incident};

/// Every passage of the screen on show, with its words.
pub fn of(app: &App) -> Vec<(ReaderKey, String)> {
    match &app.screen {
        Screen::Bot(id) => app
            .bot
            .as_ref()
            .filter(|view| &view.bot.id == id)
            .map(|view| bot(&view.bot))
            .unwrap_or_default(),
        Screen::Team(name) => app
            .teams()
            .into_iter()
            .find(|team| &team.name == name)
            .map(|team| {
                team.members
                    .iter()
                    .map(|bot| {
                        (
                            ReaderKey::MemberPurpose(bot.id.clone()),
                            crate::domain::Team::purpose(bot),
                        )
                    })
                    .collect()
            })
            .unwrap_or_default(),
        Screen::Incident(id) => app
            .incidents
            .detail
            .as_ref()
            .filter(|incident| &incident.id == id)
            .map(incident)
            .unwrap_or_default(),
        Screen::Nodes => match &app.mint.token {
            Some(token) => vec![
                (ReaderKey::MintedNode, token.node_id.to_string()),
                (ReaderKey::MintedToken, token.token.expose().to_string()),
            ],
            None => Vec::new(),
        },
        Screen::Tasks => submit_key(&app.submit)
            .map(|words| vec![(ReaderKey::SubmitKey, words)])
            .unwrap_or_default(),
        _ => Vec::new(),
    }
}

fn bot(bot: &Bot) -> Vec<(ReaderKey, String)> {
    vec![
        (ReaderKey::BotMission, bot.mission.clone()),
        (ReaderKey::BotRunsOn, bot.placement.describe()),
        (ReaderKey::BotModel, model(bot)),
        (ReaderKey::BotBudgets, budgets(bot)),
    ]
}

fn incident(incident: &Incident) -> Vec<(ReaderKey, String)> {
    let mut passages = vec![
        (ReaderKey::IncidentTitle, incident.title.clone()),
        (ReaderKey::IncidentSource, incident.source.clone()),
        (ReaderKey::IncidentGroup, incident.group_key.clone()),
    ];
    if !incident.reason.is_empty() {
        passages.push((ReaderKey::IncidentReason, incident.reason.clone()));
    }
    if !incident.diagnosis.is_empty() {
        passages.push((ReaderKey::IncidentDiagnosis, incident.diagnosis.clone()));
    }
    if !incident.proposal.is_empty() {
        passages.push((ReaderKey::IncidentProposal, incident.proposal.clone()));
    }
    if let Some(postmortem) = incident.postmortem.as_ref().filter(|p| !p.markdown.is_empty()) {
        passages.push((ReaderKey::IncidentPostmortem, postmortem.markdown.clone()));
    }
    for fingerprint in incident.fingerprints.iter().chain(&incident.resolved_fingerprints) {
        passages.push((ReaderKey::Fingerprint(fingerprint.clone()), fingerprint.clone()));
    }
    passages
}

/// The bot's model in a line: its name, how it is routed, where it may run.
pub fn model(bot: &Bot) -> String {
    format!(
        "{} · {} · {}",
        bot.model.model,
        bot.model.routing_policy,
        if bot.model.allow_remote_providers {
            "remote allowed"
        } else {
            "local only"
        }
    )
}

/// The bot's budgets per task in a line.
pub fn budgets(bot: &Bot) -> String {
    format!(
        "{} model calls · {} tool calls · {} per task",
        limit(bot.budgets.max_model_calls_per_task),
        limit(bot.budgets.max_tool_calls_per_task),
        bot.budgets.timeout_label()
    )
}

/// A budget, or `default` when the bot leaves it to the control plane.
fn limit(value: u32) -> String {
    if value == 0 {
        "default".to_string()
    } else {
        value.to_string()
    }
}

/// The open form's key in a sentence, while the task is not urgent.
pub fn submit_key(form: &SubmitForm) -> Option<String> {
    let key = form.key.as_ref().filter(|_| form.open && !form.urgent)?;
    Some(format!("key {}: sending twice makes one task", key.as_str()))
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::domain::IdempotencyKey;

    #[test]
    fn a_budget_left_to_the_control_plane_says_default() {
        assert_eq!(limit(0), "default");
        assert_eq!(limit(12), "12");
    }

    #[test]
    fn the_new_task_key_reads_while_the_form_is_open_and_not_urgent() {
        let key = IdempotencyKey::new("panel-1").unwrap();
        let mut form = SubmitForm::opened(None, key);
        assert_eq!(
            submit_key(&form).as_deref(),
            Some("key panel-1: sending twice makes one task")
        );
        form.urgent = true;
        assert_eq!(submit_key(&form), None);
        assert_eq!(submit_key(&SubmitForm::default()), None);
    }
}
