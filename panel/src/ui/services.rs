use std::sync::Arc;

use super::{BotView, Connection, Effect, Message};
use crate::application::{
    ApplyInBulk, ApprovePostmortem, BulkOutcome, CancelTask, Clock, Connect, ControlPlane, DecideTask,
    DefinitionReader, DeleteBot, DownloadArtifact, ExportSettings, FilePicker, FollowBotEvents, FollowTaskEvents,
    FollowTeamEvents, ImportSettings, KeySource, ListIncidents, ListTopics, LoadChat, LoadTaskDetail, MarkDiagnosis,
    MintEnrollmentToken, Notifier, PanelError, PatchBot, PublishEvent, RefreshFleet, RegisterBot, ResolveIncident,
    RevokeNode, SaveBotEdit, SaveSubscriptions, SendBackPostmortem, SendChat, ShortcutSource, ShowIncident, StopBot,
    SubmitTask, SwitchBot,
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
    /// Desktop notices.
    pub notifier: Arc<dyn Notifier>,
    /// The operator's one-press buttons.
    pub shortcuts: Arc<dyn ShortcutSource>,
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
            Effect::Copy(_) | Effect::FocusWindow | Effect::ShowEnd(_) | Effect::ScrollTop => return None,
            Effect::Notify { notice, open } => {
                return match self.notifier.show(notice).await {
                    Ok(true) => Some(Message::NoticeClicked(open)),
                    _ => None,
                };
            }
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
            Effect::SavePreferences { settings, quiet } => Message::PreferencesSaved {
                result: self.connect.save_preferences(&settings),
                quiet,
            },
            Effect::ExportSettings(settings) => {
                Message::SettingsExported(ExportSettings::new(self.picker.clone()).execute(&settings).await)
            }
            Effect::ImportSettings => Message::SettingsImported(
                ImportSettings::new(self.picker.clone(), self.reader.clone())
                    .execute()
                    .await,
            ),
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
            Effect::FollowTask { id, feed, wait } => {
                let result = match self.control_plane() {
                    Ok(cp) => FollowTaskEvents::new(cp).execute(id.clone(), feed, wait).await,
                    Err(e) => Err(e),
                };
                Message::TaskFollowed(id, result)
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
            Effect::SendChat {
                bot,
                message,
                earlier,
                running,
            } => Message::ChatSent(match self.control_plane() {
                Ok(cp) => {
                    SendChat::new(cp, self.keys.clone())
                        .execute(bot, &message, &earlier, running)
                        .await
                }
                Err(e) => Err(e),
            }),
            Effect::StopBot(id) => Message::BotStopped(match self.control_plane() {
                Ok(cp) => StopBot::new(cp).execute(id).await,
                Err(e) => Err(e),
            }),
            Effect::SaveBotEdit(id, edit) => Message::EditSaved(match self.control_plane() {
                Ok(cp) => SaveBotEdit::new(cp).execute(id, edit).await,
                Err(e) => Err(e),
            }),
            Effect::PreviewDelete(id) => {
                let result = match self.control_plane() {
                    Ok(cp) => DeleteBot::new(cp).preview(id.clone()).await,
                    Err(e) => Err(e),
                };
                Message::DeletePreviewed(id, result)
            }
            Effect::DeleteBot(id) => {
                let result = match self.control_plane() {
                    Ok(cp) => DeleteBot::new(cp).delete(id.clone()).await,
                    Err(e) => Err(e),
                };
                Message::BotDeleted(id, result)
            }
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
            Effect::Order { bot, objective, key } => Message::Ordered(match self.control_plane() {
                Ok(cp) => {
                    SubmitTask::new(cp, self.keys.clone())
                        .urgent(bot.as_str(), &objective, key)
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
            Effect::SaveSubscriptions(id, change) => {
                let result = match self.control_plane() {
                    Ok(cp) => SaveSubscriptions::new(cp).execute(id, change.clone()).await,
                    Err(e) => Err(e),
                };
                Message::SubscriptionsSaved(change, result)
            }
            Effect::LoadTopics(team) => {
                let result = match self.control_plane() {
                    Ok(cp) => ListTopics::new(cp).execute(team.clone()).await,
                    Err(e) => Err(e),
                };
                Message::TopicsLoaded(team, result)
            }
            Effect::Publish {
                team,
                name,
                message,
                to,
                data,
                key,
                shortcut,
            } => {
                let result = match self.control_plane() {
                    Ok(cp) => {
                        PublishEvent::new(cp)
                            .execute(team, &name, &message, &to, data, key)
                            .await
                    }
                    Err(e) => Err(e),
                };
                if shortcut {
                    Message::ShortcutPublished(result)
                } else {
                    Message::Published(result)
                }
            }
            Effect::Cancel(id) => Message::Cancelled(match self.control_plane() {
                Ok(cp) => CancelTask::new(cp).execute(id).await,
                Err(e) => Err(e),
            }),
            Effect::LoadIncidents => Message::IncidentsLoaded(match self.control_plane() {
                Ok(cp) => ListIncidents::new(cp).execute().await,
                Err(e) => Err(e),
            }),
            Effect::LoadIncident(id) => {
                let result = match self.control_plane() {
                    Ok(cp) => ShowIncident::new(cp).execute(id.clone()).await,
                    Err(e) => Err(e),
                };
                Message::IncidentLoaded(id, result)
            }
            Effect::MarkDiagnosis { id, verdict } => {
                let result = match self.control_plane() {
                    Ok(cp) => MarkDiagnosis::new(cp).execute(id.clone(), verdict).await,
                    Err(error) => Err(error),
                };
                Message::DiagnosisMarked(id, result)
            }
            Effect::ResolveIncident { id, reason } => Message::IncidentResolved(match self.control_plane() {
                Ok(cp) => ResolveIncident::new(cp).execute(id, &reason).await,
                Err(e) => Err(e),
            }),
            Effect::ApprovePostmortem { id, author } => Message::PostmortemReviewed(match self.control_plane() {
                Ok(cp) => ApprovePostmortem::new(cp).execute(id, author).await,
                Err(e) => Err(e),
            }),
            Effect::SendBackPostmortem { id, note, author } => {
                Message::PostmortemReviewed(match self.control_plane() {
                    Ok(cp) => SendBackPostmortem::new(cp).execute(id, &note, author).await,
                    Err(e) => Err(e),
                })
            }
            Effect::Download(execution, name) => Message::Downloaded(match self.control_plane() {
                Ok(cp) => {
                    DownloadArtifact::new(cp, self.picker.clone())
                        .execute(execution, name)
                        .await
                }
                Err(e) => Err(e),
            }),
            Effect::Decide(id, decision, call_id) => Message::Decided(match self.control_plane() {
                Ok(cp) => DecideTask::new(cp).execute(id, decision, call_id).await,
                Err(e) => Err(e),
            }),
            Effect::Bulk { action, targets } => Message::BulkDone(match self.control_plane() {
                Ok(cp) => ApplyInBulk::new(cp).execute(action, targets).await,
                Err(e) => {
                    let mut outcome = BulkOutcome::new(action);
                    for target in targets {
                        outcome.record(target.name(), Err(e.clone()));
                    }
                    outcome
                }
            }),
        };
        Some(message)
    }
}
