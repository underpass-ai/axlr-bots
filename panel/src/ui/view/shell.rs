//! The window around every screen: the top bar with the emblem, the
//! search button and the connection, the rail of places, the banner,
//! and the dialog of an act to confirm.

use iced::widget::{Space, button, center, column, container, mouse_area, opaque, row, stack, svg, text, tooltip};
use iced::{Alignment, Color, Element, Length, Padding};

use super::widgets::{BODY, El, SMALL, TINY, action, bold, icon, icon_button, key_cap, muted, semibold};
use crate::ui::format;
use crate::ui::styles::{self, Kind};
use crate::ui::{App, Icon, Message, Screen, Section, ThemeMode, Tokens, Tone, brand};

/// The top bar's height.
pub const TOP_BAR: f32 = 44.0;
/// The rail's width; the emblem sits over it in the top bar.
pub const RAIL: f32 = 68.0;
/// The emblem's height in the top bar; its width keeps the 50 × 34
/// ratio of the two-line mark.
const EMBLEM_HEIGHT: f32 = 26.0;
/// The widest the search button gets.
const SEARCH_WIDTH: f32 = 520.0;
/// The widest the banner gets over the page's foot.
const BANNER_WIDTH: f32 = 820.0;

pub fn frame<'a>(app: &'a App, content: El<'a>, tokens: Tokens) -> Element<'a, Message> {
    let body = row![
        rail(app, tokens),
        container(Space::new().width(1).height(Length::Fill)).style(styles::rule(tokens)),
        main(app, content, tokens)
    ]
    .height(Length::Fill);
    let page = container(column![
        top_bar(app, tokens),
        container(Space::new().width(Length::Fill).height(1)).style(styles::rule(tokens)),
        body
    ])
    .style(styles::page(tokens))
    .width(Length::Fill)
    .height(Length::Fill);
    match &app.confirm {
        Some(confirm) => {
            // A Pause, Turn on or Approve on many rows asks too,
            // but does not look like a Stop.
            let (kind, glyph, ink) = if confirm.destructive() {
                (Kind::Danger, Icon::Ban, tokens.bad)
            } else {
                (Kind::Primary, Icon::Check, tokens.accent)
            };
            let mut body = column![
                row![icon(Icon::Alert, 22.0, ink), bold(confirm.title()).size(20.0)]
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
            let dialog = container(body).padding(24).max_width(520).style(styles::card(tokens));
            stack![page, opaque(center(opaque(dialog)).style(styles::scrim))].into()
        }
        None if app.palette.is_open() => {
            // A click on the dim layer closes the palette; one on the
            // palette stays in it.
            let layer = container(opaque(super::palette::view(app, tokens)))
                .width(Length::Fill)
                .height(Length::Fill)
                .align_x(Alignment::Center)
                .padding(Padding::ZERO.top(super::palette::TOP))
                .style(styles::scrim);
            stack![page, opaque(mouse_area(layer).on_press(Message::PaletteClose))].into()
        }
        None => page.into(),
    }
}

/// The strip over everything: the emblem over the rail, the search
/// button in the middle, then the control plane and its version, the
/// nodes online, when it was last read and the theme.
fn top_bar<'a>(app: &'a App, tokens: Tokens) -> El<'a> {
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
    let emblem = container(
        svg(brand::emblem(tokens.dark))
            .width(EMBLEM_HEIGHT * 50.0 / 34.0)
            .height(EMBLEM_HEIGHT),
    )
    .center_x(RAIL)
    .center_y(Length::Fill);
    let mut place = row![muted(where_to(app), tokens)].spacing(6).align_y(Alignment::Center);
    if let Some((version, tip)) = server_version(app) {
        place = place.push(tooltip(
            muted(version, tokens),
            container(text(tip).size(SMALL)).padding(6).style(styles::chip(tokens)),
            tooltip::Position::Bottom,
        ));
    }
    container(
        row![
            emblem,
            container(search(tokens)).width(Length::Fill).center_x(Length::Fill),
            place,
            row![icon(Icon::Dot, 14.0, tone.color(&tokens)), semibold(status).size(SMALL)]
                .spacing(3)
                .align_y(Alignment::Center),
            muted(refreshed, tokens),
            icon_button(theme_icon, theme_tip, Some(Message::ToggleTheme), tokens),
        ]
        .spacing(14)
        .align_y(Alignment::Center),
    )
    .padding(Padding {
        right: 10.0,
        ..Padding::ZERO
    })
    .width(Length::Fill)
    .height(TOP_BAR)
    .style(styles::raised(tokens))
    .into()
}

/// The search button: it looks like the field it opens, says what can
/// be found and the keys that open it too.
fn search<'a>(tokens: Tokens) -> El<'a> {
    let field = button(
        row![
            icon(Icon::Lens, 14.0, tokens.muted),
            text("Search bots, tasks, nodes, actions…")
                .size(BODY)
                .color(tokens.muted)
                .width(Length::Fill),
            key_cap("Ctrl K", tokens),
        ]
        .spacing(8)
        .align_y(Alignment::Center),
    )
    .width(Length::Fill)
    .height(30)
    .padding(Padding::from([0, 10]))
    .style(styles::search(tokens))
    .on_press(Message::PaletteOpen);
    container(field).max_width(SEARCH_WIDTH).into()
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

/// The dot and the word of the top bar: online, connecting, or why not.
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

/// The rail: the places, each an icon over its name. The current one
/// reads in the accent with a bar at the window's edge; the count that
/// needs a person is a badge on Tasks. Settings sits at the foot.
fn rail<'a>(app: &'a App, tokens: Tokens) -> El<'a> {
    let current = app.screen.section();
    let waiting = app.waiting_count();
    let entry = |glyph: Icon, label: &'a str, section: Section, screen: Screen, badge: u32| -> El<'a> {
        let selected = current == section;
        let kind = Kind::Rail(selected);
        let place = button(
            column![
                icon(glyph, 20.0, super::widgets::ink(kind, tokens)),
                semibold(label).size(TINY)
            ]
            .spacing(3)
            .width(Length::Fill)
            .align_x(Alignment::Center),
        )
        .width(Length::Fill)
        .padding(Padding::from([8, 0]).bottom(7))
        .style(styles::button_style(tokens, kind))
        .on_press(Message::Go(screen));
        let bar = container(Space::new().width(3).height(Length::Fill)).style(move |_| container::Style {
            background: Some(if selected { tokens.accent } else { Color::TRANSPARENT }.into()),
            border: iced::Border {
                radius: 2.0.into(),
                ..Default::default()
            },
            ..Default::default()
        });
        let mut layers = stack![place, container(bar).padding(Padding::from([9, 0]))];
        if badge > 0 {
            layers = layers.push(
                container(
                    container(semibold(badge.to_string()).size(10.5))
                        .padding(Padding::from([0, 5]))
                        .style(styles::badge(tokens)),
                )
                .width(Length::Fill)
                .align_x(Alignment::End)
                .padding(Padding {
                    top: 3.0,
                    right: 10.0,
                    ..Padding::ZERO
                }),
            );
        }
        layers.into()
    };
    container(
        column![
            entry(Icon::Overview, "Home", Section::Overview, Screen::Overview, 0),
            entry(Icon::Tasks, "Tasks", Section::Tasks, Screen::Tasks, waiting),
            entry(Icon::Bots, "Bots", Section::Bots, Screen::Bots, 0),
            entry(Icon::Team, "Teams", Section::Teams, Screen::Teams, 0),
            entry(Icon::Nodes, "Nodes", Section::Nodes, Screen::Nodes, 0),
            Space::new().height(Length::Fill),
            entry(Icon::Settings, "Settings", Section::Settings, Screen::Settings, 0),
        ]
        .spacing(2),
    )
    .padding(Padding::from([8, 0]))
    .width(RAIL)
    .height(Length::Fill)
    .style(styles::raised(tokens))
    .into()
}

/// The page, and over its foot the banner after an act: it floats, so
/// the page never moves when it comes or goes.
fn main<'a>(app: &'a App, content: El<'a>, tokens: Tokens) -> El<'a> {
    let page = container(content).width(Length::Fill).height(Length::Fill);
    // Always two layers, so the page keeps its widgets (its scroll) when
    // the banner comes and goes.
    let Some(banner) = &app.banner else {
        return stack![page, Space::new()].into();
    };
    let mut words = row![
        icon(banner.tone.icon(), 18.0, banner.tone.color(&tokens)),
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
            .padding(Padding::from([16, 18]))
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
