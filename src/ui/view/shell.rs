//! The window around every screen: the header with the emblem, the side
//! bar, the banner, and the dialog of an act to confirm.

use iced::widget::{Space, button, center, column, container, opaque, row, stack, svg, text};
use iced::{Alignment, Element, Length, Padding};

use super::widgets::{BODY, BOLD, El, SMALL, action, bold, icon, icon_button, muted};
use crate::ui::format;
use crate::ui::styles::{self, Kind};
use crate::ui::{App, Icon, Message, Screen, Section, ThemeMode, Tokens, Tone, brand};

/// The emblem's height in the header; its width keeps the 848 × 128 ratio.
const EMBLEM_HEIGHT: f32 = 30.0;

pub fn frame<'a>(app: &'a App, content: El<'a>, tokens: Tokens) -> Element<'a, Message> {
    let body = row![sidebar(app, tokens), main(app, content, tokens)].height(Length::Fill);
    let page = container(column![header(app, tokens), body])
        .style(styles::page(tokens))
        .width(Length::Fill)
        .height(Length::Fill);
    match &app.confirm {
        Some(confirm) => {
            let lines = confirm.lines();
            let dialog = container(
                column![
                    row![icon(Icon::Alert, 26.0, tokens.bad), bold(confirm.title()).size(22.0)]
                        .spacing(12)
                        .align_y(Alignment::Center),
                    text(lines[0]),
                    text(lines[1]),
                    row![
                        action(Kind::Secondary, Icon::Back, "Keep it", Some(Message::ConfirmNo), tokens),
                        action(
                            Kind::Danger,
                            Icon::Ban,
                            confirm.act(),
                            Some(Message::ConfirmYes),
                            tokens
                        ),
                    ]
                    .spacing(12)
                ]
                .spacing(14),
            )
            .padding(26)
            .max_width(520)
            .style(styles::card(tokens));
            stack![page, opaque(center(opaque(dialog)).style(styles::scrim))].into()
        }
        None => page.into(),
    }
}

fn header<'a>(app: &'a App, tokens: Tokens) -> El<'a> {
    let emblem = svg(brand::emblem(tokens.dark))
        .height(EMBLEM_HEIGHT)
        .width(EMBLEM_HEIGHT * 848.0 / 128.0);
    let (tone, status) = connection(app);
    let refreshed = match app.last_refresh {
        Some(at) => format!("updated {}", format::clock(at)),
        None => "not updated yet".to_string(),
    };
    let theme_icon = match app.mode {
        ThemeMode::Light => Icon::Moon,
        ThemeMode::Dark => Icon::Sun,
    };
    let theme_tip = match app.mode {
        ThemeMode::Light => "Dark theme",
        ThemeMode::Dark => "Light theme",
    };
    container(
        row![
            emblem,
            text("Control panel").size(BODY).color(tokens.muted),
            Space::new().width(Length::Fill),
            row![icon(Icon::Dot, 18.0, tone.color(&tokens)), bold(status).size(SMALL)]
                .spacing(4)
                .align_y(Alignment::Center),
            muted(refreshed, tokens),
            icon_button(theme_icon, theme_tip, Some(Message::ToggleTheme), tokens),
        ]
        .spacing(16)
        .align_y(Alignment::Center),
    )
    .padding(Padding::from([12, 20]))
    .width(Length::Fill)
    .style(styles::raised(tokens))
    .into()
}

/// The dot and the word of the header: online, connecting, or why not.
fn connection(app: &App) -> (Tone, String) {
    if let Some(error) = &app.fleet_error {
        return (Tone::Bad, error.title().to_string());
    }
    if !app.connected() {
        return (
            Tone::Idle,
            if app.settings.busy {
                "Connecting".into()
            } else {
                "Not connected".into()
            },
        );
    }
    match &app.fleet {
        Some(fleet) => {
            let online = fleet.overview.nodes_in(crate::domain::NodeStatus::Online);
            let tone = if fleet.overview.nodes_down() > 0 {
                Tone::Wait
            } else {
                Tone::Good
            };
            (tone, format!("{online}/{} nodes online", fleet.overview.total_nodes()))
        }
        None => (Tone::Idle, "Loading".into()),
    }
}

fn sidebar<'a>(app: &'a App, tokens: Tokens) -> El<'a> {
    let current = app.screen.section();
    let waiting = app.waiting_count();
    let entry = |glyph: Icon, label: &'a str, section: Section, screen: Screen, badge: u32| -> El<'a> {
        let selected = current == section;
        let color = if selected { tokens.accent } else { tokens.text };
        let mut line = row![
            icon(glyph, 20.0, color),
            text(label).font(BOLD).size(BODY).color(tokens.text)
        ]
        .spacing(12)
        .align_y(Alignment::Center);
        if badge > 0 {
            line = line.push(Space::new().width(Length::Fill)).push(
                container(
                    text(badge.to_string())
                        .font(BOLD)
                        .size(SMALL)
                        .color(styles::on(tokens.wait)),
                )
                .padding(Padding::from([1, 8]))
                .style(move |_| container::Style {
                    background: Some(tokens.wait.into()),
                    border: iced::Border {
                        radius: 999.0.into(),
                        ..Default::default()
                    },
                    ..Default::default()
                }),
            );
        }
        button(line)
            .width(Length::Fill)
            .padding(Padding::from([11, 14]))
            .style(styles::button_style(tokens, Kind::Nav(selected)))
            .on_press(Message::Go(screen))
            .into()
    };
    container(
        column![
            entry(Icon::Overview, "Home", Section::Overview, Screen::Overview, 0),
            entry(Icon::Nodes, "Nodes", Section::Nodes, Screen::Nodes, 0),
            entry(Icon::Bots, "Bots", Section::Bots, Screen::Bots, 0),
            entry(Icon::Tasks, "Tasks", Section::Tasks, Screen::Tasks, waiting),
            Space::new().height(Length::Fill),
            entry(Icon::Settings, "Settings", Section::Settings, Screen::Settings, 0),
        ]
        .spacing(4),
    )
    .padding(12)
    .width(200)
    .height(Length::Fill)
    .style(styles::raised(tokens))
    .into()
}

fn main<'a>(app: &'a App, content: El<'a>, tokens: Tokens) -> El<'a> {
    let mut stackable = column![]
        .spacing(14)
        .padding(Padding::from([20, 28]))
        .width(Length::Fill)
        .height(Length::Fill);
    if let Some(banner) = &app.banner {
        let mut words = row![
            icon(banner.tone.icon(), 20.0, banner.tone.color(&tokens)),
            bold(banner.title.clone()).size(BODY)
        ]
        .spacing(10)
        .align_y(Alignment::Center);
        if !banner.detail.is_empty() {
            words = words.push(text(banner.detail.clone()).size(BODY).color(tokens.muted));
        }
        let mut line = row![words, Space::new().width(Length::Fill)].align_y(Alignment::Center);
        if banner.tone == Tone::Bad && app.screen != Screen::Settings {
            line = line.push(action(
                Kind::Secondary,
                Icon::Settings,
                "Settings",
                Some(Message::Go(Screen::Settings)),
                tokens,
            ));
        }
        line = line.push(icon_button(Icon::Cross, "Close", Some(Message::DismissBanner), tokens));
        stackable = stackable.push(
            container(line.spacing(8))
                .padding(Padding::from([8, 14]))
                .width(Length::Fill)
                .style(styles::banner(tokens, banner.tone)),
        );
    }
    stackable.push(content).into()
}
