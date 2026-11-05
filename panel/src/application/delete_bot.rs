use std::sync::Arc;

use super::{ControlPlane, DeletePreview, PanelError};
use crate::domain::{BotId, BotReference, DeletedBot, TaskFilter};

/// Deletes a bot, and says beforehand what that will do. The control
/// plane does it in one call (`DELETE /v1/bots/{id}`); the list's bulk
/// delete calls `delete` once per bot.
pub struct DeleteBot {
    control_plane: Arc<dyn ControlPlane>,
}

impl DeleteBot {
    pub fn new(control_plane: Arc<dyn ControlPlane>) -> Self {
        Self { control_plane }
    }

    /// Its tasks still pending, running or waiting (every one, not only
    /// the panel's latest), and the other bots that name it: as a
    /// delegation target or as a sender of a topic they listen to.
    pub async fn preview(&self, id: BotId) -> Result<DeletePreview, PanelError> {
        let filter = TaskFilter {
            bot: Some(id.clone()),
            ..TaskFilter::with_limit(TaskFilter::MAX_LIMIT)
        };
        let tasks = self.control_plane.list_tasks(filter).await?;
        let bots = self.control_plane.list_bots().await?;
        let mut named_by = Vec::new();
        for other in bots.iter().filter(|b| b.id != id) {
            if other
                .delegation
                .as_ref()
                .is_some_and(|d| d.to.iter().any(|t| t == id.as_str()))
            {
                named_by.push(BotReference {
                    bot_id: other.id.to_string(),
                    how: "delegation".into(),
                    topic: String::new(),
                });
            }
            for sub in other
                .subscriptions
                .iter()
                .filter(|s| s.from.iter().any(|f| f == id.as_str()))
            {
                named_by.push(BotReference {
                    bot_id: other.id.to_string(),
                    how: "subscription".into(),
                    topic: sub.topic.clone(),
                });
            }
        }
        Ok(DeletePreview {
            open_tasks: tasks.iter().filter(|t| t.status.can_cancel()).count() as u32,
            named_by,
        })
    }

    pub async fn delete(&self, id: BotId) -> Result<DeletedBot, PanelError> {
        self.control_plane.delete_bot(id).await
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::application::test_support::{fleet, run};
    use crate::domain::{BotDelegation, Subscription};

    #[test]
    fn says_what_will_happen_then_deletes() {
        let (control_plane, _) = fleet();
        let id = BotId::new("bot-wrx80").unwrap();
        let others: Vec<_> = run(control_plane.list_bots())
            .unwrap()
            .into_iter()
            .filter(|b| b.id != id)
            .map(|b| b.id.to_string())
            .collect();
        control_plane.change_bot(&others[0], |bot| {
            bot.delegation = Some(BotDelegation {
                to: vec!["bot-wrx80".into()],
            });
            bot.subscriptions = vec![Subscription {
                topic: "warn.*".into(),
                from: vec!["bot-wrx80".into()],
                ..Subscription::default()
            }];
        });
        let uc = DeleteBot::new(control_plane.clone());
        let preview = run(uc.preview(id.clone())).unwrap();
        let open = run(control_plane.list_tasks(TaskFilter {
            bot: Some(id.clone()),
            ..TaskFilter::with_limit(TaskFilter::MAX_LIMIT)
        }))
        .unwrap()
        .iter()
        .filter(|t| t.status.can_cancel())
        .count() as u32;
        assert_eq!(preview.open_tasks, open);
        let words: Vec<String> = preview.named_by.iter().map(BotReference::describe).collect();
        assert_eq!(
            words,
            [
                format!("{} may ask it for a task", others[0]),
                format!("{} listens to warn.* from it", others[0])
            ]
        );
        let deleted = run(uc.delete(id.clone())).unwrap();
        assert_eq!(deleted.cancelled.len() as u32, open);
        assert_eq!(deleted.named_by, preview.named_by);
        assert!(matches!(
            run(control_plane.get_bot(id.clone())),
            Err(PanelError::NotFound(_))
        ));
        assert!(matches!(run(uc.delete(id)), Err(PanelError::NotFound(_))));
    }
}
