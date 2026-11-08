//! Nodes: who runs the bots, how healthy, and enrolling a new host.

use iced::widget::{Space, column, container, responsive, row, scrollable, table, text};
use iced::{Alignment, Length};

use super::grid;
use super::widgets::{
    BODY, El, Voice, action, bold, card, chip, empty, field, icon, muted, pill, reader, section_title, title,
};
use crate::domain::{CertificateHealth, Node};
use crate::ui::format;
use crate::ui::styles::{self, Kind};
use crate::ui::table::{ColumnRef, List, NodeColumn, RowRef, SortColumn};
use crate::ui::{App, Icon, Message, ReaderKey, Tokens, Tone};

pub fn view<'a>(app: &'a App, tokens: Tokens) -> El<'a> {
    let Some(fleet) = &app.fleet else {
        return super::overview::loading(app, tokens);
    };
    let mut page = column![
        row![
            title(Icon::Nodes, "Nodes", tokens),
            Space::new().width(Length::Fill),
            action(
                Kind::Primary,
                Icon::Key,
                "New host token",
                (!app.mint.open).then_some(Message::MintOpen),
                tokens
            ),
        ]
        .align_y(Alignment::Center)
    ]
    .spacing(18);
    if app.mint.open {
        page = page.push(mint_form(app, tokens));
    }
    let list: El<'a> = if fleet.nodes.is_empty() {
        empty(Icon::Nodes, "No node yet: make a token and enroll a host", tokens)
    } else {
        responsive(move |space| list(app, &fleet.nodes, space.width < NARROW, tokens))
            .height(Length::Shrink)
            .into()
    };
    page = page.push(card(list, tokens));
    let page = scrollable(page.padding(iced::Padding {
        right: 12.0,
        bottom: grid::BAR_ROOM,
        ..Default::default()
    }))
    .id(super::widgets::PAGE)
    .height(Length::Fill);
    grid::with_bar(app, List::Nodes, page.into(), tokens)
}

/// Under this width the table leaves out the AXLR column (its version
/// goes under the node) and Revoke is an icon, so the node keeps room.
const NARROW: f32 = 900.0;

/// The nodes in the table's order: a box, the status, the node, its
/// labels, when it was last seen, its certificate, AXLR unless `narrow`,
/// and Revoke.
fn list<'a>(app: &'a App, nodes: &'a [Node], narrow: bool, tokens: Tokens) -> El<'a> {
    let now = app.now;
    let state = &app.tables.nodes;
    let shown = state.sort.order(nodes);
    let head = |c: NodeColumn| {
        grid::header(
            c.label(),
            state.sort.badge(c),
            Message::SortBy(ColumnRef::Nodes(c)),
            tokens,
        )
    };
    let all = state.selection.covers(shown.iter().map(|n| &n.id));
    let mut columns = vec![
        table::column(
            grid::check(all, |on| Message::PickShown(List::Nodes, on), tokens),
            move |n: &'a Node| {
                let id = n.id.clone();
                grid::check(
                    state.selection.contains(&n.id),
                    move |on| Message::Pick(RowRef::Node(id.clone()), on),
                    tokens,
                )
            },
        ),
        table::column(head(NodeColumn::Status), move |n: &'a Node| {
            pill(Tone::of_node(n.status), n.status.name(), tokens)
        }),
        table::column(head(NodeColumn::Node), move |n: &'a Node| -> El<'a> {
            let mut cell = column![
                grid::fitted(n.id.to_string(), true, tokens),
                muted(grid::shorten(&n.host_name, 28), tokens)
            ]
            .spacing(2);
            if narrow && !n.capabilities.axlr_version.is_empty() {
                cell = cell.push(muted(grid::shorten(&n.capabilities.axlr_version, 24), tokens));
            }
            if n.capabilities.dedicated {
                cell = cell.push(chip("dedicated", tokens));
            }
            if !n.capabilities.providers.is_empty() {
                cell = cell.push(providers(n, tokens));
            }
            cell.into()
        })
        .width(Length::FillPortion(if narrow { 3 } else { 2 })),
        table::column(bold("Labels"), move |n: &'a Node| -> El<'a> {
            let mut chips = row![].spacing(6);
            for pair in n.label_pairs() {
                chips = chips.push(chip(pair, tokens));
            }
            chips.wrap().into()
        })
        .width(Length::FillPortion(if narrow { 2 } else { 3 })),
        table::column(head(NodeColumn::LastSeen), move |n: &'a Node| {
            text(format::age(n.last_seen_at, now))
        }),
        table::column(head(NodeColumn::Certificate), move |n: &'a Node| {
            certificate(n, now, tokens)
        }),
    ];
    if !narrow {
        columns.push(table::column(head(NodeColumn::Axlr), move |n: &'a Node| {
            text(if n.capabilities.axlr_version.is_empty() {
                "—".to_string()
            } else {
                n.capabilities.axlr_version.clone()
            })
        }));
    }
    columns.push(table::column(text(""), move |n: &'a Node| -> El<'a> {
        if n.status.can_revoke() && narrow {
            grid::icon_act(
                Kind::Ghost,
                Icon::Ban,
                "Revoke",
                Some(Message::AskRevoke(n.id.clone())),
                tokens,
            )
        } else if n.status.can_revoke() {
            action(
                Kind::Ghost,
                Icon::Ban,
                "Revoke",
                Some(Message::AskRevoke(n.id.clone())),
                tokens,
            )
        } else {
            muted(
                format!(
                    "revoked {}",
                    n.revoked_at.map(|at| format::age(at, now)).unwrap_or_default()
                ),
                tokens,
            )
            .into()
        }
    }));
    grid::sheet(columns, shown)
}

/// The model providers the node named, one chip each: a local model by
/// its id, without the `local:`.
fn providers<'a>(node: &Node, tokens: Tokens) -> El<'a> {
    let mut chips = row![].spacing(6);
    for provider in &node.capabilities.providers {
        chips = chips.push(chip(
            provider.strip_prefix("local:").unwrap_or(provider).to_string(),
            tokens,
        ));
    }
    chips.wrap().into()
}

fn certificate<'a>(node: &Node, now: chrono::DateTime<chrono::Utc>, tokens: Tokens) -> El<'a> {
    match node.certificate_health(now) {
        CertificateHealth::Good(days) => pill(Tone::Good, format!("{days} days"), tokens),
        CertificateHealth::Soon(days) => pill(Tone::Wait, format!("{days} days"), tokens),
        CertificateHealth::Expired => pill(Tone::Bad, "Expired", tokens),
        CertificateHealth::Unknown => {
            if node.certificate_serial.is_some() {
                muted("not seen yet", tokens).into()
            } else {
                muted("local socket", tokens).into()
            }
        }
    }
}

fn mint_form<'a>(app: &'a App, tokens: Tokens) -> El<'a> {
    let form = &app.mint;
    let mut body = column![
        row![
            section_title(Icon::Key, "Token for a new host", tokens),
            Space::new().width(Length::Fill),
            super::widgets::icon_button(Icon::Cross, "Close", Some(Message::MintClose), tokens)
        ]
        .align_y(Alignment::Center),
        row![
            container(field(
                "Node name",
                "empty: the control plane picks one",
                &form.node_id,
                Message::MintNodeId,
                Some(Message::MintSubmit),
                tokens
            ))
            .width(Length::FillPortion(2)),
            container(field(
                "Labels",
                "zone=eu, gpu=rtx3090",
                &form.labels,
                Message::MintLabels,
                Some(Message::MintSubmit),
                tokens
            ))
            .width(Length::FillPortion(3)),
            container(field(
                "Lifetime",
                "1h",
                &form.ttl,
                Message::MintTtl,
                Some(Message::MintSubmit),
                tokens
            ))
            .width(Length::FillPortion(1)),
        ]
        .spacing(14),
        row![action(
            Kind::Primary,
            Icon::Key,
            if form.busy { "Making…" } else { "Make token" },
            (!form.busy).then_some(Message::MintSubmit),
            tokens
        )],
    ]
    .spacing(14);
    if let Some(token) = &form.token {
        let command = token.enroll_command(&control_address(app));
        body = body.push(
            container(
                column![
                    row![
                        icon(Icon::Alert, 20.0, tokens.wait),
                        bold("Copy it now. It is shown once.").size(BODY)
                    ]
                    .spacing(10)
                    .align_y(Alignment::Center),
                    super::widgets::fact(
                        "Node",
                        reader(
                            &app.readers,
                            ReaderKey::MintedNode,
                            token.node_id.to_string(),
                            Voice::Body,
                            tokens
                        ),
                        tokens
                    ),
                    super::widgets::fact("Expires", text(format::when(token.expires_at)), tokens),
                    container(reader(
                        &app.readers,
                        ReaderKey::MintedToken,
                        token.token.expose().to_string(),
                        Voice::Mono,
                        tokens
                    ))
                    .padding(10)
                    .width(Length::Fill)
                    .style(styles::chip(tokens)),
                    row![
                        action(
                            Kind::Primary,
                            Icon::Copy,
                            "Copy token",
                            Some(Message::Copy(token.token.expose().to_string())),
                            tokens
                        ),
                        action(
                            Kind::Secondary,
                            Icon::Copy,
                            "Copy command",
                            Some(Message::Copy(command)),
                            tokens
                        ),
                        action(Kind::Ghost, Icon::Check, "Done", Some(Message::MintClose), tokens),
                    ]
                    .spacing(10),
                ]
                .spacing(10),
            )
            .padding(16)
            .width(Length::Fill)
            .style(styles::tone_card(tokens, Tone::Wait)),
        );
    }
    card(body, tokens).into()
}

/// The node address a host enrolls with: the operator address's host on
/// the node port.
fn control_address(app: &App) -> String {
    let api = app.settings.api_url.trim().trim_end_matches('/');
    let host = api
        .trim_start_matches("https://")
        .rsplit_once(':')
        .map(|(h, _)| h)
        .unwrap_or("127.0.0.1");
    format!("https://{host}:7878")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_node_address_takes_the_api_host() {
        let mut app = crate::ui::view::tests::app();
        app.settings.api_url = "https://cp.example:7879/".into();
        assert_eq!(control_address(&app), "https://cp.example:7878");
        app.settings.api_url = "https://cp.example".into();
        assert_eq!(control_address(&app), "https://127.0.0.1:7878");
    }
}
