//! The window around every screen: the side bar with the emblem, the thin
//! header with the connection, the banner, and the dialog of an act to
//! confirm.

use iced::widget::{Space, button, center, column, container, opaque, row, stack, svg, text, tooltip};
use iced::{Alignment, Color, Element, Length, Padding};

use super::widgets::{BODY, El, SEMIBOLD, SMALL, action, bold, icon, icon_button, muted};
use crate::ui::format;
use crate::ui::styles::{self, Kind};
use crate::ui::{App, Icon, Message, Screen, Section, ThemeMode, Tokens, Tone, brand};

/// The emblem's width in the side bar; its height keeps the 50 × 34 ratio
/// of the two-line mark.
const EMBLEM_WIDTH: f32 = 100.0;
/// The side bar's width.
const SIDEBAR_WIDTH: f32 = 196.0;
/// The widest the banner gets over the page's foot.
const BANNER_WIDTH: f32 = 820.0;

pub fn frame<'a>(app: &'a App, content: El<'a>, tokens: Tokens) -> Element<'a, Message> {
    let body = row![
        sidebar(app, tokens),
        container(Space::new().width(1).height(Length::Fill)).style(styles::rule(tokens)),
        main(app, content, tokens)
    ]
    .height(Length::Fill);
    let page = container(column![
        header(app, tokens),
        container(Space::new().width(Length::Fill).height(1)).style(styles::rule(tokens)),
        body
    ])
    .style(styles::page(tokens))
    .width(Length::Fill)
    .height(Length::Fill);
    match &app.confirm {
        Some(confirm) => {
            // A Pause, Turn on, Approve or Resolve on many rows asks too,
            // but does not look like a Stop.
            let (kind, glyph, ink) = if confirm.destructive() {
                (Kind::Danger, Icon::Ban, tokens.bad)
            } else {
                (Kind::Primary, Icon::Check, tokens.accent)
            };
            let mut body = column![
                row![icon(Icon::Alert, 26.0, ink), bold(confirm.title()).size(22.0)]
                    .spacing(12)
                    .align_y(Alignment::Center)
            ]
            .spacing(14);
            for line in confirm.lines() {
                body = body.push(text(line));
            }
            body = body.push(
                row![
                    action(Kind::Secondary, Icon::Back, "Keep it", Some(Message::ConfirmNo), tokens),
                    action(kind, glyph, confirm.act(), Some(Message::ConfirmYes), tokens),
                ]
                .spacing(12),
            );
            let dialog = container(body).padding(26).max_width(520).style(styles::card(tokens));
            stack![page, opaque(center(opaque(dialog)).style(styles::scrim))].into()
        }
        None => page.into(),
    }
}

/// The thin strip over everything: the connection, when it was last read,
/// the theme.
fn header<'a>(app: &'a App, tokens: Tokens) -> El<'a> {
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
    let mut place = row![icon(Icon::Nodes, 14.0, tokens.muted), muted(where_to(app), tokens)]
        .spacing(6)
        .align_y(Alignment::Center);
    if let Some((version, tip)) = server_version(app) {
        place = place.push(tooltip(
            muted(version, tokens),
            container(text(tip).size(SMALL)).padding(6).style(styles::chip(tokens)),
            tooltip::Position::Bottom,
        ));
    }
    container(
        row![
            Space::new().width(Length::Fill),
            place,
            row![
                icon(Icon::Dot, 16.0, tone.color(&tokens)),
                text(status).font(SEMIBOLD).size(SMALL)
            ]
            .spacing(4)
            .align_y(Alignment::Center),
            muted(refreshed, tokens),
            icon_button(theme_icon, theme_tip, Some(Message::ToggleTheme), tokens),
        ]
        .spacing(16)
        .align_y(Alignment::Center),
    )
    .padding(Padding::from([4, 16]))
    .width(Length::Fill)
    .style(styles::raised(tokens))
    .into()
}

/// The control plane this window talks to, so two windows on two
/// control planes are told apart.
fn where_to(app: &App) -> String {
    app.endpoint
        .clone()
        .unwrap_or_else(|| crate::ui::format::endpoint(&app.settings.api_url))
}

/// The version the control plane runs, `v0.4.0` (its short commit for a
/// build without one), and on hover what the nodes are measured
/// against; none before the first read of its build.
pub(crate) fn server_version(app: &App) -> Option<(String, String)> {
    let build = app.fleet.as_ref()?.build.as_ref()?;
    let shown = match build.server_version() {
        Some(version) => version.label(),
        None if !build.running.revision.is_empty() => build.running.revision.chars().take(7).collect(),
        None => return None,
    };
    let mut tip = format!(
        "The control plane runs {shown} ({}): a node whose agent is older is marked.",
        build.running.describe()
    );
    if let Some(least) = build.minimum_axlr() {
        tip.push_str(&format!("\nNodes need AXLR {} or newer.", least.label()));
    }
    Some((format!("· {shown}"), tip))
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

/// The side bar: the two-line emblem, then the places. The current one
/// reads in the accent with a bar beside it; a count that needs a person
/// sits at the end of its line.
fn sidebar<'a>(app: &'a App, tokens: Tokens) -> El<'a> {
    let current = app.screen.section();
    let waiting = app.waiting_count();
    let entry = |glyph: Icon, label: &'a str, section: Section, screen: Screen, badge: u32| -> El<'a> {
        let selected = current == section;
        let color = if selected { tokens.accent } else { tokens.text };
        let mut line = row![
            icon(glyph, 18.0, color),
            text(label).font(SEMIBOLD).size(BODY).color(color)
        ]
        .spacing(12)
        .align_y(Alignment::Center);
        if badge > 0 {
            line = line.push(Space::new().width(Length::Fill)).push(
                container(
                    text(badge.to_string())
                        .font(SEMIBOLD)
                        .size(SMALL)
                        .color(styles::on(tokens.wait)),
                )
                .padding(Padding::from([1, 7]))
                .style(move |_| container::Style {
                    background: Some(tokens.wait.into()),
                    border: iced::Border {
                        radius: styles::RADIUS_PILL.into(),
                        ..Default::default()
                    },
                    ..Default::default()
                }),
            );
        }
        let bar = container(Space::new().width(3).height(22)).style(move |_| container::Style {
            background: Some(if selected { tokens.accent } else { Color::TRANSPARENT }.into()),
            border: iced::Border {
                radius: 2.0.into(),
                ..Default::default()
            },
            ..Default::default()
        });
        row![
            bar,
            button(line)
                .width(Length::Fill)
                .padding(Padding::from([10, 12]))
                .style(styles::button_style(tokens, Kind::Nav(selected)))
                .on_press(Message::Go(screen))
        ]
        .spacing(6)
        .align_y(Alignment::Center)
        .into()
    };
    let emblem = container(
        svg(brand::emblem(tokens.dark))
            .width(EMBLEM_WIDTH)
            .height(EMBLEM_WIDTH * 34.0 / 50.0),
    )
    .padding(Padding::from([10, 12]).bottom(18));
    container(
        column![
            emblem,
            entry(Icon::Overview, "Home", Section::Overview, Screen::Overview, 0),
            entry(Icon::Nodes, "Nodes", Section::Nodes, Screen::Nodes, 0),
            entry(Icon::Bots, "Bots", Section::Bots, Screen::Bots, 0),
            entry(Icon::Team, "Teams", Section::Teams, Screen::Teams, 0),
            entry(Icon::Tasks, "Tasks", Section::Tasks, Screen::Tasks, waiting),
            entry(Icon::Alert, "Incidents", Section::Incidents, Screen::Incidents, 0),
            Space::new().height(Length::Fill),
            entry(Icon::Settings, "Settings", Section::Settings, Screen::Settings, 0),
        ]
        .spacing(2),
    )
    .padding(Padding::from([10, 8]))
    .width(SIDEBAR_WIDTH)
    .height(Length::Fill)
    .style(styles::raised(tokens))
    .into()
}

/// The page, and over its foot the banner after an act: it floats, so
/// the page never moves when it comes or goes.
fn main<'a>(app: &'a App, content: El<'a>, tokens: Tokens) -> El<'a> {
    let page = container(content)
        .padding(Padding::from([20, 28]))
        .width(Length::Fill)
        .height(Length::Fill);
    // Always two layers, so the page keeps its widgets (its scroll) when
    // the banner comes and goes.
    let Some(banner) = &app.banner else {
        return stack![page, Space::new()].into();
    };
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
    if banner.tone == Tone::Bad {
        line = line.push(icon_button(
            Icon::Copy,
            "Copy error",
            Some(Message::Copy(banner.copy_text())),
            tokens,
        ));
    }
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
    let toast = container(line.spacing(8))
        .padding(Padding::from([8, 14]))
        .width(Length::Fill)
        .max_width(BANNER_WIDTH)
        .style(styles::banner(tokens, banner.tone));
    stack![
        page,
        container(toast)
            .padding(Padding::from([20, 28]))
            .width(Length::Fill)
            .height(Length::Fill)
            .align_x(Alignment::Center)
            .align_y(Alignment::End),
    ]
    .into()
}

#[cfg(test)]
mod tests {
    use std::sync::Arc;

    use super::*;
    use crate::adapters::fake::FakeControlPlane;
    use crate::domain::{BuildIdentity, BuildStatus};
    use crate::ui::test_runtime::send;

    #[test]
    fn the_header_names_the_version_the_control_plane_runs() {
        let fake = Arc::new(FakeControlPlane::sample("2026-10-25T10:00:00Z".parse().unwrap()));
        let mut app = super::super::tests::app_over(fake.clone());
        assert_eq!(server_version(&app), None, "nothing before the first read");
        send(&mut app, Message::SettingsSave);
        assert_eq!(
            server_version(&app),
            None,
            "a build without a version or a commit says nothing"
        );
        let build = |version: &str, minimum: &str| BuildStatus {
            running: BuildIdentity {
                version: version.into(),
                revision: "4924ad73145295ce".into(),
                ..Default::default()
            },
            minimum_axlr: minimum.into(),
            ..Default::default()
        };
        if let Some(fleet) = app.fleet.as_mut() {
            fleet.build = Some(build("0.4.0", "0.14.0"));
        }
        let (shown, tip) = server_version(&app).unwrap();
        assert_eq!(shown, "· v0.4.0");
        assert!(tip.contains("runs v0.4.0 (4924ad731452)"), "{tip}");
        assert!(tip.ends_with("Nodes need AXLR v0.14.0 or newer."), "{tip}");
        if let Some(fleet) = app.fleet.as_mut() {
            fleet.build = Some(build("", ""));
        }
        let (shown, tip) = server_version(&app).unwrap();
        assert_eq!(shown, "· 4924ad7");
        assert!(!tip.contains("AXLR"), "{tip}");
    }
}
