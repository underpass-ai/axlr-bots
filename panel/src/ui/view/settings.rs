//! Settings: the panel's own, in sections as its `settings.json` names
//! them: where the operator API is and how to trust it, the look, what
//! the New bot form proposes. The file is editable here as JSON,
//! exportable and importable. A bot's configuration is never here: it
//! lives in the bot's definition. The first run starts here with the
//! local install's values.

use iced::widget::{column, container, row, scrollable, text, text_editor};
use iced::{Alignment, Font, Length};

use super::widgets::{El, SMALL, action, bold, card, field, icon, muted, pill_with, section, title};
use crate::ui::styles::{self, Kind};
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
    if app.settings_json.open {
        // The file in place of the form, as an editor switches between
        // its settings page and its settings.json.
        page = page.push(file(app, tokens));
    } else {
        page = page.push(control_plane(app, tokens));
        page = page.push(
            row![
                container(look(app, tokens)).width(Length::FillPortion(2)),
                container(file(app, tokens)).width(Length::FillPortion(3)),
            ]
            .spacing(18),
        );
        page = page.push(new_bot(app, tokens));
    }
    scrollable(page.max_width(1100).padding(iced::Padding {
        right: 12.0,
        ..Default::default()
    }))
    .height(Length::Fill)
    .into()
}

/// The three values, the state of the connection, and Connect, which
/// has something to press only while these values are not the ones
/// connected.
fn control_plane<'a>(app: &'a App, tokens: Tokens) -> El<'a> {
    let form = &app.settings;
    let submit = app.can_connect().then_some(Message::SettingsSave);
    let broken = form.connected.is_some() && !form.changed() && app.fleet_error.is_some();
    let (status, hint): (El<'a>, &'a str) = if form.busy {
        (
            pill_with(Icon::Hourglass, crate::ui::Tone::Wait, "Connecting", tokens),
            "Asking the control plane for its overview.",
        )
    } else if let (true, Some(error)) = (broken, &app.fleet_error) {
        (
            pill_with(Icon::Alert, crate::ui::Tone::Bad, error.title(), tokens),
            "The connection broke. Reconnect tries these values again.",
        )
    } else if form.changed() {
        (
            pill_with(Icon::Alert, crate::ui::Tone::Wait, "Changed", tokens),
            "Connect tries the new values and saves them once the control plane answers.",
        )
    } else if let Some(connected) = &form.connected {
        let _ = connected;
        (
            pill_with(Icon::Check, crate::ui::Tone::Good, "Connected", tokens),
            "These are the values in use. Change one and Connect wakes up.",
        )
    } else {
        (
            pill_with(Icon::Dot, crate::ui::Tone::Idle, "Not connected", tokens),
            "Connect tries these values and saves them once the control plane answers.",
        )
    };
    let label = if form.busy {
        "Connecting…"
    } else if broken {
        "Reconnect"
    } else if form.connected.is_some() && !form.changed() {
        "Connected"
    } else {
        "Connect"
    };
    section(
        Icon::Key,
        "Control plane",
        column![
            field(
                "API address",
                "https://127.0.0.1:7879",
                &form.api_url,
                Message::SettingsUrl,
                submit.clone(),
                tokens
            ),
            field(
                "Fleet CA file",
                "~/.local/state/axlr-bots/ca/ca.crt",
                &form.ca_path,
                Message::SettingsCa,
                submit.clone(),
                tokens
            ),
            field(
                "Token file",
                "~/.local/state/axlr-bots/api.token",
                &form.token_file,
                Message::SettingsToken,
                submit.clone(),
                tokens
            ),
            muted(
                "The token stays in its file. The panel reads it and never shows it.",
                tokens
            ),
            field(
                "Decide as",
                "alice",
                &form.actor,
                Message::SettingsActor,
                submit.clone(),
                tokens
            ),
            muted(
                "Who you approve and deny as. A bot that lists approvers takes only the names it lists; it is your word, not a login.",
                tokens
            ),
            row![
                action(Kind::Primary, Icon::Play, label, submit, tokens),
                status,
                muted(hint, tokens),
            ]
            .spacing(12)
            .align_y(Alignment::Center),
        ]
        .spacing(14),
        tokens,
    )
}

fn look<'a>(app: &'a App, tokens: Tokens) -> El<'a> {
    let (light, dark) = (app.mode == ThemeMode::Light, app.mode == ThemeMode::Dark);
    section(
        Icon::Sun,
        "Look",
        column![
            row![
                action(
                    Kind::Chip(light),
                    Icon::Sun,
                    "Light",
                    (!light).then_some(Message::SetTheme(ThemeMode::Light)),
                    tokens
                ),
                action(
                    Kind::Chip(dark),
                    Icon::Moon,
                    "Dark",
                    (!dark).then_some(Message::SetTheme(ThemeMode::Dark)),
                    tokens
                ),
            ]
            .spacing(10),
            muted("Kept as you switch.", tokens),
        ]
        .spacing(14),
        tokens,
    )
}

/// What the New bot form proposes: a panel preference about the form.
/// Each bot keeps its own model in its definition.
fn new_bot<'a>(app: &'a App, tokens: Tokens) -> El<'a> {
    let form = &app.settings;
    let changed = form.new_bot() != app.new_bot;
    section(
        Icon::Plus,
        "New bot",
        column![
            field(
                "Model proposed",
                "provider/model",
                &form.default_model,
                Message::SettingsModel,
                changed.then_some(Message::SettingsSavePreferences),
                tokens
            ),
            field(
                "Models offered, one click each",
                "anthropic/claude-haiku-5.5, codex/gpt-6-luna",
                &form.models,
                Message::SettingsModels,
                changed.then_some(Message::SettingsSavePreferences),
                tokens
            ),
            muted(
                "Only what the New bot form starts with. A bot's model is its own, on its page.",
                tokens
            ),
            row![action(
                Kind::Primary,
                Icon::Check,
                if changed { "Save" } else { "Saved" },
                changed.then_some(Message::SettingsSavePreferences),
                tokens
            )],
        ]
        .spacing(14),
        tokens,
    )
}

/// The settings file: where it is, edit it as JSON, export, import, reset.
/// Open as JSON, the editor stands where the form was.
fn file<'a>(app: &'a App, tokens: Tokens) -> El<'a> {
    let json = &app.settings_json;
    let bad = styles::pill_ink(tokens, crate::ui::Tone::Bad);
    let mut body = column![
        text(app.services.connect.location()).font(Font::MONOSPACE).size(SMALL),
        muted("Flat keys, dotted by section, as an editor's settings.json.", tokens),
    ]
    .spacing(6);
    if !json.open {
        body = body.push(
            row![
                action(
                    Kind::Secondary,
                    Icon::File,
                    "Edit as JSON",
                    Some(Message::SettingsJsonOpen),
                    tokens
                ),
                action(
                    Kind::Secondary,
                    Icon::Copy,
                    "Export…",
                    Some(Message::SettingsExport),
                    tokens
                ),
                action(
                    Kind::Secondary,
                    Icon::Back,
                    "Import…",
                    Some(Message::SettingsImport),
                    tokens
                ),
            ]
            .spacing(10)
            .wrap(),
        );
        body = body.push(row![action(
            Kind::Ghost,
            Icon::Wrench,
            "Reset to defaults",
            Some(Message::SettingsReset),
            tokens
        )]);
        return section(Icon::Trace, "Settings file", body.spacing(14), tokens);
    }
    body = body.push(
        text_editor(&json.content)
            .on_action(Message::SettingsJsonEdit)
            .font(Font::MONOSPACE)
            .size(SMALL)
            .padding(10)
            .height(Length::Fixed(420.0))
            .style(styles::reader(tokens)),
    );
    if let Some(error) = &json.error {
        body = body.push(
            row![icon(Icon::Alert, 16.0, bad), text(error).size(SMALL).color(bad)]
                .spacing(8)
                .align_y(Alignment::Center),
        );
    }
    body = body.push(
        row![
            action(
                Kind::Primary,
                Icon::Check,
                "Apply",
                Some(Message::SettingsJsonApply),
                tokens
            ),
            action(
                Kind::Secondary,
                Icon::Cross,
                "Close",
                Some(Message::SettingsJsonClose),
                tokens
            ),
            muted(
                "Apply keeps the look and the New bot defaults; new control plane values wait for Connect.",
                tokens
            ),
        ]
        .spacing(10)
        .align_y(Alignment::Center),
    );
    section(Icon::Trace, "Settings file", body.spacing(14), tokens)
}
