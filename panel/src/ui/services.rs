use std::sync::Arc;

use super::{BotView, Connection, Effect, Message};
use crate::application::{
    CancelTask, Clock, Connect, ControlPlane, DecideTask, DefinitionReader, FilePicker, FollowBotEvents,
    FollowTeamEvents, KeySource, LoadChat, LoadTaskDetail, MintEnrollmentToken, PanelError, PatchBot, RefreshFleet,
    RegisterBot, RevokeNode, SendChat, StopBot, SubmitTask, SwitchBot,
};
use crate::domain::BotPatch;

/// The ports the panel's effects run through.
#[derive(Clone)]
pub struct Services {
    /// The control plane once connected.
    pub control_plane: Option<Arc<dyn ControlPlane>>,
    pub clock: Arc<dyn Clock>,
    pub keys: Arc<dyn KeySource>,
    pub picker: Arc<dyn FilePicker>,
    pub reader: Arc<dyn DefinitionReader>,
    pub connect: Arc<Connect>,
}

impl Services {
    fn control_plane(&self) -> Result<Arc<dyn ControlPlane>, PanelError> {
        self.control_plane
            .clone()
            .ok_or_else(|| PanelError::Config("not connected yet: check the settings".into()))
    }

    /// Carries out an effect and answers with the message of its result.
    /// The clipboard is the runtime's; it answers `None` here.
    pub async fn execute(self, effect: Effect) -> Option<Message> {
        let message = match effect {
            Effect::Copy(_) => return None,
            Effect::Connect { config, save } => {
                let result = if save {
                    self.connect.save(config).await
                } else {
                    self.connect.try_config(&config).await
                };
                Message::Connected {
                    result: result.map(Connection),
                    saved: save,
                }
            }
            Effect::PickFile => Message::FilePicked(self.picker.pick_definition().await),
            Effect::Refresh { generation } => {
                let result = match self.control_plane() {
                    Ok(cp) => RefreshFleet::new(cp, self.clock.clone()).execute().await,
                    Err(e) => Err(e),
                };
                Message::FleetLoaded { generation, result }
            }
            Effect::LoadTask(id) => {
                let result = match self.control_plane() {
                    Ok(cp) => LoadTaskDetail::new(cp).execute(id.clone()).await,
                    Err(e) => Err(e),
                };
                Message::TaskLoaded(id, result)
            }
            Effect::FollowBot(id, feed) => {
                let result = match self.control_plane() {
                    Ok(cp) => match cp.get_bot(id.clone()).await {
                        Ok(bot) => {
                            let (follow, load) = (FollowBotEvents::new(cp.clone()), LoadChat::new(cp));
                            let (feed, chat) =
                                futures::join!(follow.execute(id.clone(), feed), load.execute(id.clone()));
                            match (feed, chat) {
                                (Ok(feed), Ok(chat)) => Ok(BotView { bot, feed, chat }),
                                (Err(e), _) | (_, Err(e)) => Err(e),
                            }
                        }
                        Err(e) => Err(e),
                    },
                    Err(e) => Err(e),
                };
                Message::BotLoaded(id, result)
            }
            Effect::Mint { node_id, labels, ttl } => Message::Minted(match self.control_plane() {
                Ok(cp) => MintEnrollmentToken::new(cp).execute(&node_id, &labels, &ttl).await,
                Err(e) => Err(e),
            }),
            Effect::Revoke(id) => Message::Revoked(match self.control_plane() {
                Ok(cp) => RevokeNode::new(cp).execute(id).await,
                Err(e) => Err(e),
            }),
            Effect::RegisterForm(form) => Message::Registered(match self.control_plane() {
                Ok(cp) => RegisterBot::new(cp, self.reader.clone()).from_form(&form).await,
                Err(e) => Err(e),
            }),
            Effect::RegisterFile(path) => Message::Registered(match self.control_plane() {
                Ok(cp) => RegisterBot::new(cp, self.reader.clone()).from_file(path).await,
                Err(e) => Err(e),
            }),
            Effect::Enable(id) => Message::Switched(match self.control_plane() {
                Ok(cp) => SwitchBot::new(cp).enable(id).await,
                Err(e) => Err(e),
            }),
            Effect::Pause(id) => Message::Switched(match self.control_plane() {
                Ok(cp) => SwitchBot::new(cp).pause(id).await,
                Err(e) => Err(e),
            }),
            Effect::SetLevel(id, level) => Message::Leveled(match self.control_plane() {
                Ok(cp) => {
                    let patch = BotPatch {
                        level: Some(level),
                        ..BotPatch::default()
                    };
                    PatchBot::new(cp).execute(id, patch).await
                }
                Err(e) => Err(e),
            }),
            Effect::SendChat { bot, message, earlier } => Message::ChatSent(match self.control_plane() {
                Ok(cp) => {
                    SendChat::new(cp, self.keys.clone())
                        .execute(bot, &message, &earlier)
                        .await
                }
                Err(e) => Err(e),
            }),
            Effect::StopBot(id) => Message::BotStopped(match self.control_plane() {
                Ok(cp) => StopBot::new(cp).execute(id).await,
                Err(e) => Err(e),
            }),
            Effect::Submit {
                bot,
                objective,
                key,
                urgent,
            } => Message::Submitted(match self.control_plane() {
                Ok(cp) => {
                    let uc = SubmitTask::new(cp, self.keys.clone());
                    if urgent {
                        uc.urgent(bot.as_str(), &objective, key).await
                    } else {
                        uc.execute(bot.as_str(), &objective, key).await
                    }
                }
                Err(e) => Err(e),
            }),
            Effect::Recall { bot, key } => Message::Recalled(match self.control_plane() {
                Ok(cp) => {
                    SubmitTask::new(cp, self.keys.clone())
                        .urgent(bot.as_str(), super::app_teams::RECALL_OBJECTIVE, key)
                        .await
                }
                Err(e) => Err(e),
            }),
            Effect::FollowTeam { name, feed, wait } => {
                let result = match self.control_plane() {
                    Ok(cp) => FollowTeamEvents::new(cp).execute(name.clone(), feed, wait).await,
                    Err(e) => Err(e),
                };
                Message::TeamLoaded(name, result)
            }
            Effect::SetTeams(id, groups) => Message::TeamSaved(match self.control_plane() {
                Ok(cp) => {
                    let patch = BotPatch {
                        groups: Some(groups),
                        ..BotPatch::default()
                    };
                    PatchBot::new(cp).execute(id, patch).await
                }
                Err(e) => Err(e),
            }),
            Effect::Cancel(id) => Message::Cancelled(match self.control_plane() {
                Ok(cp) => CancelTask::new(cp).execute(id).await,
                Err(e) => Err(e),
            }),
            Effect::Decide(id, decision) => Message::Decided(match self.control_plane() {
                Ok(cp) => DecideTask::new(cp).execute(id, decision).await,
                Err(e) => Err(e),
            }),
        };
        Some(message)
    }
}
