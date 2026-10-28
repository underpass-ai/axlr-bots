use std::sync::Arc;

use super::{ControlPlane, PanelError};
use crate::domain::{Bot, BotPatch, Subscription};

/// What the operator does to a bot's topics: listen to one, stop
/// listening, approve or deny a bot's ask. Each is one `PATCH` of the
/// whole list; the asks it does not touch go back as they came.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum SubscriptionChange {
    /// Listen to a topic; one of the same pattern is replaced.
    Listen(Subscription),
    /// Stop listening to a topic, or drop an ask.
    Remove(String),
    /// Approve a bot's ask: it hears the topic from now on.
    Approve(String),
    /// Deny a bot's ask: it is dropped.
    Deny(String),
}

impl SubscriptionChange {
    /// The bot's list after the change.
    pub fn apply(&self, current: &[Subscription]) -> Vec<Subscription> {
        match self {
            Self::Listen(sub) => {
                let mut list: Vec<Subscription> = current.to_vec();
                match list.iter_mut().find(|s| s.topic == sub.topic) {
                    Some(old) => *old = sub.clone(),
                    None => list.push(sub.clone()),
                }
                list
            }
            Self::Remove(topic) | Self::Deny(topic) => current.iter().filter(|s| &s.topic != topic).cloned().collect(),
            Self::Approve(topic) => current
                .iter()
                .map(|s| if &s.topic == topic { s.approved() } else { s.clone() })
                .collect(),
        }
    }

    /// The panel's words for what happened: `almacen listens to error.*`.
    pub fn done(&self, bot: &Bot) -> String {
        match self {
            Self::Listen(sub) => format!("{} listens to {}", bot.id, sub.topic),
            Self::Remove(topic) => format!("{} no longer listens to {topic}", bot.id),
            Self::Approve(topic) => format!("{} listens to {topic}", bot.id),
            Self::Deny(topic) => format!("{} will not hear {topic}", bot.id),
        }
    }
}

/// Saves a change of a bot's topics.
pub struct SaveSubscriptions {
    control_plane: Arc<dyn ControlPlane>,
}

impl SaveSubscriptions {
    pub fn new(control_plane: Arc<dyn ControlPlane>) -> Self {
        Self { control_plane }
    }

    /// Reads the bot as the control plane has it now, applies the change
    /// and writes the whole list back.
    pub async fn execute(&self, bot: crate::domain::BotId, change: SubscriptionChange) -> Result<Bot, PanelError> {
        let current = self.control_plane.get_bot(bot.clone()).await?;
        let patch = BotPatch {
            subscriptions: Some(change.apply(&current.subscriptions)),
            ..BotPatch::default()
        };
        patch.validate()?;
        self.control_plane.patch_bot(bot, patch).await
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::application::test_support::{fleet, run};
    use crate::domain::{BotId, DeliveryMode};

    fn ask(topic: &str) -> Subscription {
        serde_json::from_str(&format!(
            r#"{{"topic":"{topic}","status":"pending","askedBy":"bot","why":"x","askedAt":"2026-10-28T10:00:00Z"}}"#
        ))
        .unwrap()
    }

    #[test]
    fn approving_one_ask_keeps_the_other_as_it_came() {
        let (control_plane, _) = fleet();
        control_plane.set_subscriptions("bot", vec![ask("error.*"), ask("done.*")]);
        let id = BotId::new("bot").unwrap();
        let bot = run(SaveSubscriptions::new(control_plane.clone())
            .execute(id.clone(), SubscriptionChange::Approve("error.*".into())))
        .unwrap();
        assert!(!bot.subscriptions[0].is_pending());
        assert_eq!(bot.subscriptions[1], ask("done.*"));
        assert_eq!(
            control_plane
                .calls()
                .iter()
                .filter(|c| c.starts_with("patch_bot"))
                .count(),
            1
        );
        let bot = run(SaveSubscriptions::new(control_plane.clone())
            .execute(id.clone(), SubscriptionChange::Deny("done.*".into())))
        .unwrap();
        assert_eq!(bot.subscriptions.len(), 1);
        let listen = Subscription::new("error.*", Some(DeliveryMode::Note), "").unwrap();
        let bot =
            run(SaveSubscriptions::new(control_plane.clone()).execute(id.clone(), SubscriptionChange::Listen(listen)))
                .unwrap();
        assert_eq!(bot.subscriptions[0].mode(), DeliveryMode::Note);
        let warn = Subscription::new("warn.*", None, "").unwrap();
        let bot =
            run(SaveSubscriptions::new(control_plane.clone()).execute(id.clone(), SubscriptionChange::Listen(warn)))
                .unwrap();
        assert_eq!(bot.subscriptions.len(), 2);
        let bot = run(
            SaveSubscriptions::new(control_plane.clone()).execute(id, SubscriptionChange::Remove("error.*".into()))
        )
        .unwrap();
        assert_eq!(bot.subscriptions.len(), 1);
        assert_eq!(
            SubscriptionChange::Remove("x".into()).done(&bot),
            "bot no longer listens to x"
        );
        assert_eq!(SubscriptionChange::Deny("x".into()).done(&bot), "bot will not hear x");
        assert_eq!(SubscriptionChange::Approve("x".into()).done(&bot), "bot listens to x");
    }

    #[test]
    fn seventeen_topics_are_refused_before_the_call() {
        let (control_plane, _) = fleet();
        control_plane.set_subscriptions(
            "bot",
            (0..16)
                .map(|i| Subscription::new(&format!("t{i}"), None, "").unwrap())
                .collect(),
        );
        let one_more = Subscription::new("t16", None, "").unwrap();
        let result = run(SaveSubscriptions::new(control_plane.clone())
            .execute(BotId::new("bot").unwrap(), SubscriptionChange::Listen(one_more)));
        assert!(result.is_err());
        assert!(!control_plane.calls().iter().any(|c| c.starts_with("patch_bot")));
    }
}
