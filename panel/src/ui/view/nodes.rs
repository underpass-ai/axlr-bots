//! Nodes: who runs the bots, how healthy, and enrolling a new host.

use std::rc::Rc;

use iced::widget::{Space, column, container, row, text};
use iced::{Alignment, Length};

use super::sheet::{self, Column, Head, Ink, Line};
use super::widgets::{BODY, El, Voice, action, bold, card, empty, field, icon, mark, reader, section_title};
use super::{grid, listbar, status};
use crate::domain::{CertificateHealth, Node, NodeStatus, Version};
use crate::ui::find::{NodeRows, Query};
use crate::ui::format;
use crate::ui::styles::{self, Kind};
use crate::ui::table::{ColumnRef, List, NodeColumn, RowRef, SortColumn};
use crate::ui::{App, Icon, Message, ReaderKey, Tokens, Tone};

pub fn view<'a>(app: &'a App, tokens: Tokens) -> El<'a> {
    let Some(fleet) = &app.fleet else {
        return super::overview::loading(app, tokens);
    };
    let rows = app.tables.nodes_shown;
    let query = app.list_query();
    let narrow = app.screen.inspected().is_some();
    let shown = app.node_rows();
    let revoked = NodeRows::revoked(&fleet.nodes);
    let mut chips = vec![listbar::filter(&app.tables.query, tokens)];
    if revoked > 0 || rows.shows_revoked() {
        chips.push(listbar::toggle(
            "Revoked",
            revoked,
            rows.shows_revoked(),
            Some(Message::ShowRevoked(!rows.shows_revoked())),
            tokens,
        ));
    }
    let acts = vec![action(
        Kind::Primary,
        Icon::Key,
        "New host token",
        (!app.mint.open).then_some(Message::MintOpen),
        tokens,
    )];
    let mut page = column![listbar::bar("Nodes", shown.len().to_string(), chips, acts, tokens)];
    if app.mint.open {
        page = page.push(listbar::panel(mint_form(app, tokens), tokens));
    }
    if revoked > 0 {
        let nodes = format::count(revoked as u64, "revoked node is", "revoked nodes are");
        page = page.push(if rows.shows_revoked() {
            listbar::note(
                format!("{nodes} shown."),
                Some(("Hide them", Message::ShowRevoked(false))),
                Vec::new(),
                Some(listbar::keys(tokens)),
                tokens,
            )
        } else {
            listbar::note(
                format!("{nodes} hidden."),
                Some(("Show them", Message::ShowRevoked(true))),
                Vec::new(),
                Some(listbar::keys(tokens)),
                tokens,
            )
        });
    }
    page = page.push(if fleet.nodes.is_empty() {
        listbar::nothing(empty(
            Icon::Nodes,
            "No node yet: make a token and enroll a host",
            tokens,
        ))
    } else if shown.is_empty() && !query.is_empty() {
        let among = rows.rows(&fleet.nodes).len();
        listbar::nothing(empty(
            Icon::Lens,
            format!("Nothing matches {} among the {among} nodes.", query.text()),
            tokens,
        ))
    } else if shown.is_empty() {
        listbar::nothing(empty(Icon::Nodes, "No node in the fleet now", tokens))
    } else {
        list(app, shown, query, narrow, tokens)
    });
    grid::with_bar(app, List::Nodes, page.into(), tokens)
}

/// The nodes on show in the table's order, one line each: a box, the
/// status with the marks of what needs a look, the node, its host, when
/// it was last seen, its AXLR, its agent, its providers, and Revoke. A
/// click on a row opens it in the inspector; beside it, `narrow`, a row
/// keeps the status, the name and when it was seen.
fn list<'a>(app: &'a App, shown: Vec<&'a Node>, query: Query, narrow: bool, tokens: Tokens) -> El<'a> {
    let now = app.now;
    let fleet = app.fleet.as_ref();
    let state = &app.tables.nodes;
    let query = Rc::new(query);
    let head = |c: NodeColumn| Head::Sort {
        label: c.label(),
        badge: state.sort.badge(c),
        column: ColumnRef::Nodes(c),
    };
    let all = state.selection.covers(shown.iter().map(|n| &n.id));
    let columns = if narrow {
        let query = query.clone();
        vec![
            Column::fixed(head(NodeColumn::Status), 100.0, move |n: &'a Node, _| {
                status::node(n.status, tokens)
            }),
            Column::share(head(NodeColumn::Node), 1, move |n: &'a Node, width| {
                sheet::found(n.id.to_string(), width, Ink::Strong, &query, tokens)
            }),
            Column::fixed(head(NodeColumn::LastSeen), 64.0, move |n: &'a Node, _| {
                Ink::Quiet.write(format::age(n.last_seen_at, now), tokens).into()
            })
            .end(),
        ]
    } else {
        vec![
            Column::fixed(Head::Check { all, list: List::Nodes }, 18.0, move |n: &'a Node, _| {
                let id = n.id.clone();
                grid::check(
                    state.selection.contains(&n.id),
                    move |on| Message::Pick(RowRef::Node(id.clone()), on),
                    tokens,
                )
            }),
            // The status, and beside it the marks of a node behind the fleet
            // and of a certificate that runs out: each tip says what to do.
            Column::fixed(head(NodeColumn::Status), 132.0, move |n: &'a Node, _| {
                let word = status::node(n.status, tokens);
                let word = match (n.status, n.revoked_at) {
                    (NodeStatus::Revoked, Some(at)) => {
                        sheet::tip(word, format!("Revoked {}", format::ago(at, now)), tokens)
                    }
                    _ => word,
                };
                let mut cell = row![word].spacing(4).align_y(Alignment::Center);
                if let Some(behind) = fleet.and_then(|f| f.behind(n)) {
                    cell = cell.push(mark(Icon::Update, tokens.wait, behind.tip(), tokens));
                }
                if let Some((tone, tip)) = certificate_mark(n, now) {
                    cell = cell.push(mark(Icon::Alert, tone.color(&tokens), tip, tokens));
                }
                cell.into()
            }),
            // The node's name; on hover its labels and its certificate,
            // which have no column of their own.
            Column::share(head(NodeColumn::Node), 3, {
                let query = query.clone();
                move |n: &'a Node, width| {
                    let name = n.id.to_string();
                    let short = grid::shorten(&name, Ink::Strong.fits(width));
                    let line = container(Ink::Strong.write_found(short, &query, tokens)).clip(true);
                    sheet::tip(line.into(), about(n, now), tokens)
                }
            }),
            Column::share(head(NodeColumn::Host), 3, {
                let query = query.clone();
                move |n: &'a Node, width| sheet::found(n.host_name.clone(), width, Ink::Quiet, &query, tokens)
            }),
            Column::fixed(head(NodeColumn::LastSeen), 64.0, move |n: &'a Node, _| {
                Ink::Quiet.write(format::age(n.last_seen_at, now), tokens).into()
            }),
            Column::fixed(head(NodeColumn::Axlr), 110.0, move |n: &'a Node, width| {
                sheet::words(dash(&n.capabilities.axlr_version), width, Ink::Code, tokens)
            }),
            Column::fixed(Head::Plain("Agent"), 124.0, move |n: &'a Node, width| {
                sheet::words(agent_version(n).unwrap_or_default(), width, Ink::Code, tokens)
            }),
            Column::share(Head::Plain("Providers"), 6, {
                let query = query.clone();
                move |n: &'a Node, width| sheet::found(providers(n), width, Ink::Quiet, &query, tokens)
            }),
            Column::fixed(Head::None, grid::ACT, move |n: &'a Node, _| {
                grid::act_slot(n.status.can_revoke().then(|| {
                    grid::icon_act(
                        Kind::Ghost,
                        Icon::Ban,
                        "Revoke",
                        Some(Message::AskRevoke(n.id.clone())),
                        tokens,
                    )
                }))
            }),
        ]
    };
    let cursor = state.cursor.selected();
    let lines = shown
        .into_iter()
        .map(|n| Line::Row {
            row: n,
            picked: state.selection.contains(&n.id),
            selected: cursor == Some(&n.id),
            opens: RowRef::Node(n.id.clone()),
        })
        .collect();
    sheet::sheet(
        columns,
        lines,
        app.density,
        &app.hover,
        app.tables.picked(List::Nodes) > 0,
        tokens,
    )
}

/// The value, or a dash for one the node did not say.
fn dash(value: &str) -> String {
    if value.trim().is_empty() {
        "—".to_string()
    } else {
        value.to_string()
    }
}

/// The node agent's own build, `v0.4.0`, with its commit when it names
/// one; none for a node older than the field.
pub(super) fn agent_version(node: &Node) -> Option<String> {
    let raw = node.capabilities.node_version.trim();
    if raw.is_empty() {
        return None;
    }
    let version = Version::parse(raw).map_or_else(|| raw.to_string(), |v| v.label());
    let revision: String = node.capabilities.node_revision.chars().take(7).collect();
    Some(if revision.is_empty() {
        version
    } else {
        format!("{version} · {revision}")
    })
}

/// The model providers the node named, on one line; a local model
/// keeps its `local:` so it is told from a provider.
fn providers(node: &Node) -> String {
    node.capabilities.providers.join(" · ")
}

/// What the list has no column for, said on the node's name: its
/// labels, whether it is dedicated, and its certificate.
fn about(node: &Node, now: chrono::DateTime<chrono::Utc>) -> String {
    let mut lines = vec![node.id.to_string()];
    let labels = node.label_pairs();
    if !labels.is_empty() {
        lines.push(format!("Labels: {}", labels.join(", ")));
    }
    if node.capabilities.dedicated {
        lines.push("Dedicated: it runs only the bots placed on it".to_string());
    }
    lines.push(match node.certificate_health(now) {
        CertificateHealth::Good(days) | CertificateHealth::Soon(days) => {
            format!("Certificate: {days} days left")
        }
        CertificateHealth::Expired => "Certificate: expired".to_string(),
        CertificateHealth::Unknown if node.certificate_serial.is_some() => "Certificate: not seen yet".to_string(),
        CertificateHealth::Unknown => "Local socket: no certificate".to_string(),
    });
    lines.join("\n")
}

/// The mark of a certificate that needs a look: amber while it runs
/// out, red once it has. None for a healthy one, or a revoked node's.
pub(super) fn certificate_mark(node: &Node, now: chrono::DateTime<chrono::Utc>) -> Option<(Tone, String)> {
    if node.status == NodeStatus::Revoked {
        return None;
    }
    match node.certificate_health(now) {
        CertificateHealth::Soon(days) => Some((Tone::Wait, format!("Its certificate runs out in {days} days"))),
        CertificateHealth::Expired => Some((Tone::Bad, "Its certificate expired".to_string())),
        CertificateHealth::Good(_) | CertificateHealth::Unknown => None,
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
    fn says_what_has_no_column_and_marks_a_certificate_that_runs_out() {
        use crate::application::ControlPlane;
        use crate::application::test_support::{fleet, now, run};
        let (cp, _) = fleet();
        let mut node = run(cp.list_nodes()).unwrap().remove(1);
        let said = about(&node, now());
        assert!(said.starts_with("wrx80\nLabels: "), "{said}");
        assert!(said.contains("gpu=rtx3090") && said.ends_with("days left"), "{said}");
        assert_eq!(certificate_mark(&node, now()), None);
        node.certificate_not_after = Some(now() + chrono::Duration::days(3));
        let (tone, tip) = certificate_mark(&node, now()).unwrap();
        assert_eq!((tone, tip.as_str()), (Tone::Wait, "Its certificate runs out in 3 days"));
        node.certificate_not_after = Some(now() - chrono::Duration::days(1));
        assert_eq!(certificate_mark(&node, now()).unwrap().0, Tone::Bad);
        assert!(about(&node, now()).ends_with("Certificate: expired"));
        node.status = NodeStatus::Revoked;
        assert_eq!(certificate_mark(&node, now()), None, "a revoked node needs no look");
        node.capabilities.dedicated = true;
        node.certificate_not_after = None;
        node.certificate_serial = None;
        let said = about(&node, now());
        assert!(said.contains("Dedicated") && said.ends_with("no certificate"), "{said}");
        assert_eq!(dash(" "), "—");
        assert_eq!(agent_version(&node), None);
        node.capabilities.node_version = "0.4.0".into();
        node.capabilities.node_revision = "4924ad73145295ce".into();
        assert_eq!(agent_version(&node).as_deref(), Some("v0.4.0 · 4924ad7"));
        assert_eq!(providers(&node), "openrouter");
    }

    #[test]
    fn the_node_address_takes_the_api_host() {
        let mut app = crate::ui::view::tests::app();
        app.settings.api_url = "https://cp.example:7879/".into();
        assert_eq!(control_address(&app), "https://cp.example:7878");
        app.settings.api_url = "https://cp.example".into();
        assert_eq!(control_address(&app), "https://127.0.0.1:7878");
    }
}
