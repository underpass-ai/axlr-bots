//! A bot's Edit (its mission and tools) and Delete, on its page.

use super::{App, Banner, BotEditForm, DeleteForm, Effect, Message, Screen, Tone};
use crate::application::{PanelError, SavedEdit};
use crate::domain::{BotId, DeletedBot};

impl App {
    /// The messages of a bot's Edit and Delete.
    pub(super) fn bot_edit(&mut self, message: Message) -> Vec<Effect> {
        match message {
            Message::EditOpen(id) => {
                if let Some(view) = self.bot.as_ref().filter(|v| v.bot.id == id) {
                    self.edit = BotEditForm::opened(&view.bot);
                    self.delete = DeleteForm::default();
                }
            }
            Message::EditClose => self.edit = BotEditForm::default(),
            Message::EditMission(words) => {
                if let Some(draft) = self.edit.draft.as_mut() {
                    draft.mission = words;
                }
            }
            Message::EditAccess(index, access) => {
                if let Some(draft) = self.edit.draft.as_mut() {
                    draft.tools.set(index, access);
                }
            }
            Message::EditRemove(index) => {
                if let Some(draft) = self.edit.draft.as_mut() {
                    draft.tools.remove(index);
                }
            }
            Message::EditNewTool(words) => {
                self.edit.new_tool = words;
                self.edit.error = None;
            }
            Message::EditNewAccess(access) => self.edit.new_access = access,
            Message::EditAdd(name) => self.edit.add(name.as_deref()),
            Message::EditSave => return self.edit_save(),
            Message::EditSaved(result) => self.edit_saved(result),
            Message::DeleteOpen(id) => {
                self.edit = BotEditForm::default();
                self.delete = DeleteForm::opened(id.clone());
                return vec![Effect::PreviewDelete(id)];
            }
            Message::DeletePreviewed(id, result) => {
                if self.delete.is_open_for(&id) {
                    match result {
                        Ok(preview) => self.delete.preview = Some(preview),
                        Err(error) => self.delete.error = Some(words(&error)),
                    }
                }
            }
            Message::DeleteTyped(words) => self.delete.typed = words,
            Message::DeleteClose => self.delete = DeleteForm::default(),
            Message::DeleteSend => {
                if self.delete.ready()
                    && let Some(id) = self.delete.bot.clone()
                {
                    self.delete.busy = true;
                    self.delete.error = None;
                    return vec![Effect::DeleteBot(id)];
                }
            }
            Message::BotDeleted(id, result) => return self.bot_deleted(id, result),
            _ => {}
        }
        Vec::new()
    }

    fn edit_save(&mut self) -> Vec<Effect> {
        if self.edit.busy {
            return Vec::new();
        }
        let (Some(id), Some(draft)) = (self.edit.bot.clone(), self.edit.draft.clone()) else {
            return Vec::new();
        };
        self.edit.busy = true;
        self.edit.error = None;
        vec![Effect::SaveBotEdit(id, draft)]
    }

    /// A saved edit closes and shows at once; one refused stays open
    /// with the reason; one over a change made meanwhile stays open on
    /// the bot as it now is, to be checked and saved again.
    fn edit_saved(&mut self, result: Result<SavedEdit, PanelError>) {
        self.edit.busy = false;
        match result {
            Ok(SavedEdit::Saved(bot)) | Ok(SavedEdit::Unchanged(bot)) => {
                self.banner = Some(Banner::good(
                    format!("{} saved", bot.id),
                    "Its tools and mission",
                    self.now,
                ));
                self.replace_bot(bot);
                self.edit = BotEditForm::default();
            }
            Ok(SavedEdit::ChangedMeanwhile(bot)) => {
                if let Some(draft) = self.edit.draft.as_mut() {
                    draft.base = bot.updated_at;
                }
                self.edit.error = Some(
                    "It changed while you edited it. Nothing was saved. Check your edit against it, then save again."
                        .into(),
                );
                self.replace_bot(bot);
            }
            Err(error) => self.edit.error = Some(words(&error)),
        }
    }

    /// The bot is gone: back to the Bots list, with what it did.
    fn bot_deleted(&mut self, id: BotId, result: Result<DeletedBot, PanelError>) -> Vec<Effect> {
        self.delete.busy = false;
        let deleted = match result {
            Ok(deleted) => deleted,
            Err(error) => {
                self.delete.error = Some(words(&error));
                return Vec::new();
            }
        };
        if let Some(fleet) = self.fleet.as_mut() {
            fleet.bots.retain(|b| b.id != id);
        }
        self.bot = None;
        self.delete = DeleteForm::default();
        let mut effects = self.go(Screen::Bots);
        self.banner = Some(if deleted.named_by.is_empty() {
            Banner::good(format!("{id} deleted"), deleted.summary(), self.now)
        } else {
            let names: Vec<&str> = deleted.named_by.iter().map(|r| r.bot_id.as_str()).collect();
            Banner {
                tone: Tone::Wait,
                title: format!("{id} deleted"),
                detail: format!("{} · still named by {}", deleted.summary(), dedup(names).join(", ")),
                at: self.now,
            }
        });
        effects.extend(self.refresh_now());
        effects
    }
}

/// The error in words: what went wrong, then the control plane's reason.
fn words(error: &PanelError) -> String {
    match error.detail() {
        "" => error.title().to_string(),
        detail => format!("{}: {detail}", error.title()),
    }
}

fn dedup(mut names: Vec<&str>) -> Vec<&str> {
    names.dedup();
    names
}

#[cfg(test)]
mod tests {
    use std::sync::Arc;

    use super::super::test_runtime::send;
    use super::*;
    use crate::adapters::fake::FakeControlPlane;
    use crate::domain::ToolAccess;

    /// The panel on a bot's page, and the fake behind it.
    fn on_bot(id: &str) -> (App, BotId, Arc<FakeControlPlane>) {
        let fake = Arc::new(FakeControlPlane::sample("2026-10-25T10:00:00Z".parse().unwrap()));
        let mut app = crate::ui::view::tests::app_over(fake.clone());
        send(&mut app, Message::SettingsSave);
        let id = BotId::new(id).unwrap();
        send(&mut app, Message::Go(Screen::Bot(id.clone())));
        assert!(app.bot.is_some());
        (app, id, fake)
    }

    #[test]
    fn edits_the_tools_and_shows_the_result_at_once() {
        let (mut app, id, fake) = on_bot("bot-wrx80");
        send(&mut app, Message::EditOpen(id.clone()));
        assert!(app.edit.is_open_for(&id));
        send(&mut app, Message::EditNewTool("local_search".into()));
        send(&mut app, Message::EditNewAccess(ToolAccess::OnItsOwn));
        send(&mut app, Message::EditAdd(None));
        send(&mut app, Message::EditAdd(Some("Not A Tool".into())));
        assert!(app.edit.error.as_deref().unwrap().contains("lowercase"));
        send(&mut app, Message::EditNewTool("x".into()));
        assert!(app.edit.error.is_none(), "typing again clears the error");
        send(&mut app, Message::EditAccess(2, ToolAccess::Never));
        send(&mut app, Message::EditRemove(0));
        send(&mut app, Message::EditMission("Watch the disks.".into()));
        send(&mut app, Message::EditSave);
        let bot = &app.bot.as_ref().unwrap().bot;
        assert_eq!(bot.allowed_tools, ["local_list", "local_write", "local_search"]);
        assert_eq!(bot.mission, "Watch the disks.");
        assert_eq!(bot.tool_rules.deny[0].tool, "local_write");
        assert!(!app.edit.is_open_for(&id));
        assert_eq!(app.banner.as_ref().unwrap().title, "bot-wrx80 saved");
        assert_eq!(fake.calls().iter().filter(|c| c.starts_with("patch_bot")).count(), 1);
        // Cancel drops the edit; leaving the page does too.
        send(&mut app, Message::EditOpen(id.clone()));
        send(&mut app, Message::EditClose);
        assert!(app.edit.draft.is_none());
        send(&mut app, Message::EditOpen(id));
        send(&mut app, Message::Go(Screen::Bots));
        assert!(app.edit.draft.is_none());
    }

    #[test]
    fn a_change_made_meanwhile_is_never_overwritten_and_errors_show_in_words() {
        let (mut app, id, fake) = on_bot("bot-wrx80");
        send(&mut app, Message::EditOpen(id.clone()));
        send(&mut app, Message::EditRemove(0));
        fake.change_bot("bot-wrx80", |bot| bot.mission = "Changed elsewhere.".into());
        send(&mut app, Message::EditSave);
        assert!(
            app.edit
                .error
                .as_deref()
                .unwrap()
                .starts_with("It changed while you edited it")
        );
        assert_eq!(app.bot.as_ref().unwrap().bot.mission, "Changed elsewhere.");
        // Saved again, on the bot as it now is: the edit goes through and
        // keeps the other change.
        send(&mut app, Message::EditMission("Changed elsewhere.".into()));
        send(&mut app, Message::EditSave);
        assert!(app.edit.draft.is_none());
        assert!(
            !app.bot
                .as_ref()
                .unwrap()
                .bot
                .allowed_tools
                .contains(&"local_read".to_string())
        );
        send(&mut app, Message::EditOpen(id));
        fake.fail_with(Some(PanelError::Invalid("tool rules: refused".into())));
        send(&mut app, Message::EditRemove(0));
        send(&mut app, Message::EditSave);
        assert_eq!(app.edit.error.as_deref(), Some("Check the input: tool rules: refused"));
        assert!(!app.edit.busy);
    }

    #[test]
    fn deletes_only_once_read_and_typed_then_shows_the_list() {
        let (mut app, id, fake) = on_bot("bot-wrx80");
        send(&mut app, Message::DeleteOpen(id.clone()));
        assert!(app.delete.preview.is_some());
        send(&mut app, Message::DeleteSend);
        assert!(app.bot.is_some(), "not typed: nothing happens");
        send(&mut app, Message::DeleteTyped("bot-wrx8".into()));
        send(&mut app, Message::DeleteSend);
        assert!(app.bot.is_some());
        send(&mut app, Message::DeleteTyped("bot-wrx80".into()));
        send(&mut app, Message::DeleteSend);
        assert_eq!(app.screen, Screen::Bots);
        assert!(app.fleet.as_ref().unwrap().bots.iter().all(|b| b.id != id));
        let banner = app.banner.as_ref().unwrap();
        assert_eq!(banner.title, "bot-wrx80 deleted");
        assert!(fake.calls().iter().any(|c| c == "delete_bot bot-wrx80"));
    }

    #[test]
    fn a_refused_delete_stays_on_the_page_with_the_reason() {
        let (mut app, id, fake) = on_bot("bot-wrx80");
        send(&mut app, Message::DeleteOpen(id.clone()));
        send(&mut app, Message::DeleteTyped("bot-wrx80".into()));
        fake.fail_with(Some(PanelError::State("a task could not be cancelled".into())));
        send(&mut app, Message::DeleteSend);
        assert_eq!(app.screen, Screen::Bot(id.clone()));
        assert_eq!(
            app.delete.error.as_deref(),
            Some("Not now: a task could not be cancelled")
        );
        send(&mut app, Message::DeleteClose);
        assert!(!app.delete.is_open_for(&id));
        // A preview that fails says why; a late preview of another bot is dropped.
        send(&mut app, Message::DeleteOpen(id.clone()));
        assert!(app.delete.error.is_some() && app.delete.preview.is_none());
        app.apply(Message::DeletePreviewed(
            BotId::new("other").unwrap(),
            Ok(Default::default()),
        ));
        assert!(app.delete.preview.is_none());
    }
}
