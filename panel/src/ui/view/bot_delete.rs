//! A bot's Delete, on its page: what deleting it will do, read first,
//! and its id typed again before the button works.

use iced::widget::{column, container, row, text, text_input};
use iced::{Alignment, Length};

use super::widgets::{El, SMALL, action, bold, icon, muted, section_title, semibold};
use crate::domain::Bot;
use crate::ui::styles::{self, Kind};
use crate::ui::{App, Icon, Message, Tokens, Tone};

pub fn card<'a>(app: &'a App, bot: &'a Bot, tokens: Tokens) -> Option<El<'a>> {
    let form = &app.delete;
    if !form.is_open_for(&bot.id) {
        return None;
    }
    let mut body = column![
        section_title(Icon::Ban, format!("Delete {}?", bot.id), tokens),
        text("It goes for good. Its past tasks, events and files stay as history."),
    ]
    .spacing(10);
    match &form.preview {
        None if form.error.is_none() => body = body.push(muted("Reading what it has going…", tokens)),
        None => {}
        Some(preview) => {
            let tasks = match preview.open_tasks {
                0 => "No task of it is pending, running or waiting: none to cancel.".to_string(),
                1 => "1 of its tasks will be cancelled: pending, running or waiting.".to_string(),
                n => format!("{n} of its tasks will be cancelled: pending, running or waiting."),
            };
            body = body.push(line(Icon::Stop, tasks, tokens)).push(line(
                Icon::Clock,
                "The tasks it asked other bots for will stop too. Its timers stop.".into(),
                tokens,
            ));
            if !preview.named_by.is_empty() {
                let mut named = column![semibold("Other bots still name it").size(SMALL)].spacing(4);
                for reference in &preview.named_by {
                    named = named.push(line(Icon::Alert, reference.describe(), tokens));
                }
                named = named.push(muted("They stay as they are. They hear nothing more from it.", tokens).size(SMALL));
                body = body.push(named);
            }
        }
    }
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
    let label = format!("Type {} to delete it", bot.id);
    body = body.push(
        column![
            semibold(label).size(SMALL),
            row![
                container(
                    text_input(bot.id.as_str(), &form.typed)
                        .on_input(Message::DeleteTyped)
                        .on_submit_maybe(form.ready().then_some(Message::DeleteSend))
                        .padding(10)
                        .style(styles::field(tokens))
                )
                .width(Length::Fixed(320.0)),
                action(
                    Kind::Danger,
                    Icon::Ban,
                    if form.busy {
                        "Deleting…".to_string()
                    } else {
                        format!("Delete {}", bot.id)
                    },
                    form.ready().then_some(Message::DeleteSend),
                    tokens
                ),
                action(Kind::Ghost, Icon::Cross, "Keep it", Some(Message::DeleteClose), tokens),
            ]
            .spacing(10)
            .align_y(Alignment::Center),
        ]
        .spacing(6),
    );
    Some(
        container(body)
            .padding(18)
            .width(Length::Fill)
            .style(styles::tone_card(tokens, Tone::Bad))
            .into(),
    )
}

fn line<'a>(glyph: Icon, words: String, tokens: Tokens) -> El<'a> {
    row![icon(glyph, 14.0, tokens.muted), bold(words).size(SMALL)]
        .spacing(8)
        .align_y(Alignment::Center)
        .into()
}
