//! Settings: where the operator API is and how to trust it. The first
//! run starts here with the local install's values.

use iced::widget::{column, row, scrollable, text};
use iced::{Alignment, Length};

use super::widgets::{El, action, bold, card, field, icon, muted, section, title};
use crate::ui::styles::Kind;
use crate::ui::{App, Icon, Message, ThemeMode, Tokens};

pub fn view<'a>(app: &'a App, tokens: Tokens) -> El<'a> {
    let form = &app.settings;
    let mut page = column![title(Icon::Settings, "Settings", tokens)].spacing(18);
    if !form.saved {
        page = page.push(card(
            row![
                icon(Icon::Bots, 30.0, tokens.accent),
                column![
                    bold("Welcome. Check these three values.").size(20.0),
                    text("They come from this host's install. Press Connect: the panel saves them once the control plane answers."),
                ]
                .spacing(6),
            ]
            .spacing(16)
            .align_y(Alignment::Center),
            tokens,
        ));
    }
    let save = if form.busy { "Connecting…" } else { "Connect" };
    page = page.push(section(
        Icon::Key,
        "Control plane",
        column![
            field(
                "API address",
                "https://127.0.0.1:7879",
                &form.api_url,
                Message::SettingsUrl,
                Some(Message::SettingsSave),
                tokens
            ),
            field(
                "Fleet CA file",
                "~/.local/state/axlr-bots/ca/ca.crt",
                &form.ca_path,
                Message::SettingsCa,
                Some(Message::SettingsSave),
                tokens
            ),
            field(
                "Token file",
                "~/.local/state/axlr-bots/api.token",
                &form.token_file,
                Message::SettingsToken,
                Some(Message::SettingsSave),
                tokens
            ),
            muted(
                "The token stays in its file. The panel reads it and never shows it.",
                tokens
            ),
            row![action(
                Kind::Primary,
                Icon::Play,
                save,
                (!form.busy).then_some(Message::SettingsSave),
                tokens
            )],
        ]
        .spacing(14),
        tokens,
    ));
    let (light, dark) = (app.mode == ThemeMode::Light, app.mode == ThemeMode::Dark);
    page = page.push(section(
        Icon::Sun,
        "Look",
        row![
            action(
                Kind::Chip(light),
                Icon::Sun,
                "Light",
                (!light).then_some(Message::ToggleTheme),
                tokens
            ),
            action(
                Kind::Chip(dark),
                Icon::Moon,
                "Dark",
                (!dark).then_some(Message::ToggleTheme),
                tokens
            ),
        ]
        .spacing(10),
        tokens,
    ));
    scrollable(page.max_width(900).padding(iced::Padding {
        right: 12.0,
        ..Default::default()
    }))
    .height(Length::Fill)
    .into()
}
