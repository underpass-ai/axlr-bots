//! A bot's Edit, on its page: its mission and its tools in plain words,
//! each tool's choice (runs on its own, asks you first, never runs), Add
//! with the names the fleet already uses, Save and Cancel.

use iced::widget::{Space, button, column, container, row, text, text_input};
use iced::{Alignment, Length, Padding};

use super::widgets::{El, SMALL, action, bold, icon, muted, section_with, segmented, semibold};
use crate::domain::{AutonomyLevel, Bot, CORE_TOOLS, ToolAccess};
use crate::ui::styles::{self, Kind};
use crate::ui::{App, Icon, Message, Tokens};

/// The most names offered under the Add field.
const OFFERED: usize = 12;

pub fn card<'a>(app: &'a App, bot: &'a Bot, tokens: Tokens) -> Option<El<'a>> {
    let form = &app.edit;
    let draft = form.draft.as_ref().filter(|_| form.is_open_for(&bot.id))?;
    let mission = column![
        semibold("What it does").size(SMALL),
        text_input("Its standing job, in a sentence or two", &draft.mission)
            .on_input(Message::EditMission)
            .padding(10)
            .style(styles::field(tokens)),
    ]
    .spacing(6);

    let mut list = column![semibold("Its tools").size(SMALL)].spacing(10);
    if draft.tools.tools.is_empty() {
        list = list.push(muted("No tool: it can only think and answer.", tokens));
    }
    for (index, setting) in draft.tools.tools.iter().enumerate() {
        let choices = segmented(
            ToolAccess::ALL,
            setting.access,
            ToolAccess::label,
            move |access| Message::EditAccess(index, access),
            tokens,
        );
        let mut line = column![
            row![
                container(bold(setting.tool.clone())).width(Length::FillPortion(2)),
                choices,
                Space::new().width(Length::Fill),
                action(
                    Kind::Ghost,
                    Icon::Cross,
                    "Remove",
                    Some(Message::EditRemove(index)),
                    tokens
                ),
            ]
            .spacing(10)
            .align_y(Alignment::Center)
        ]
        .spacing(2);
        for words in bot.tool_rules.about(&setting.tool) {
            line = line.push(muted(format!("Also {words}: kept as it is"), tokens).size(SMALL));
        }
        list = list.push(line);
    }
    list = list.push(muted(level_note(bot.level), tokens).size(SMALL));

    let ready = !form.new_tool.trim().is_empty();
    let picks = segmented(
        ToolAccess::ALL,
        form.new_access,
        ToolAccess::label,
        Message::EditNewAccess,
        tokens,
    );
    let add = row![
        text_input("local_read, docdb.find…", &form.new_tool)
            .on_input(Message::EditNewTool)
            .on_submit_maybe(ready.then_some(Message::EditAdd(None)))
            .padding(10)
            .width(Length::FillPortion(2))
            .style(styles::field(tokens)),
        picks,
        action(
            Kind::Secondary,
            Icon::Plus,
            "Add",
            ready.then_some(Message::EditAdd(None)),
            tokens
        ),
    ]
    .spacing(10)
    .align_y(Alignment::Center);
    let mut offers = row![muted("Known:", tokens).size(SMALL)]
        .spacing(6)
        .align_y(Alignment::Center);
    let offered = draft.tools.offers(known(app, bot), &form.new_tool);
    for name in offered.iter().take(OFFERED) {
        offers = offers.push(
            button(
                row![icon(Icon::Plus, 12.0, tokens.muted), text(name.clone()).size(SMALL)]
                    .spacing(4)
                    .align_y(Alignment::Center),
            )
            .padding(Padding::from([4, 8]))
            .style(styles::button_style(tokens, Kind::Chip(false)))
            .on_press(Message::EditAdd(Some(name.clone()))),
        );
    }
    let mut adding = column![semibold("Add a tool").size(SMALL), add].spacing(8);
    if !offered.is_empty() {
        adding = adding.push(offers.wrap());
    }

    let mut body = column![mission, list, adding].spacing(18);
    if let Some(error) = &form.error {
        body = body.push(
            row![
                icon(Icon::Alert, 16.0, tokens.bad),
                text(error.clone()).color(tokens.bad)
            ]
            .spacing(8)
            .align_y(Alignment::Center),
        );
    }
    body = body.push(
        row![
            action(
                Kind::Primary,
                Icon::Check,
                if form.busy { "Saving…" } else { "Save" },
                (!form.busy).then_some(Message::EditSave),
                tokens
            ),
            action(Kind::Ghost, Icon::Cross, "Cancel", Some(Message::EditClose), tokens),
            muted("Saved changes count from its next task.", tokens),
        ]
        .spacing(10)
        .align_y(Alignment::Center),
    );
    Some(section_with(
        Icon::Wrench,
        format!("Edit {}", bot.id),
        Vec::new(),
        body,
        tokens,
    ))
}

/// The tool names the panel can offer: AXLR's own, and every tool a bot
/// of the fleet lists.
fn known<'a>(app: &'a App, bot: &'a Bot) -> impl Iterator<Item = &'a str> {
    let fleet = app.fleet.iter().flat_map(|f| f.bots.iter());
    CORE_TOOLS
        .into_iter()
        .chain(bot.allowed_tools.iter().map(String::as_str))
        .chain(fleet.flat_map(|b| b.allowed_tools.iter().map(String::as_str)))
}

/// What «asks you first» means at the bot's level, in a line.
fn level_note(level: AutonomyLevel) -> &'static str {
    match level {
        AutonomyLevel::ActWithApproval => "At its level, Ask me before acting, a tool that asks waits for your OK.",
        AutonomyLevel::ActWithinPolicy => {
            "At its level, Act on its own, nothing asks you: Asks you first counts at Ask me before acting."
        }
        _ => "At its level it only watches: a tool that changes things does not run.",
    }
}
