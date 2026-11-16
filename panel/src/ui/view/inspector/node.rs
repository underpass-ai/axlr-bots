//! A node in the inspector: its status and host with Revoke, when it
//! was last seen, its AXLR and agent, its certificate, its labels, the
//! model providers it named and the bots placed on it.

use iced::widget::text::Wrapping;
use iced::widget::{column, container, row, text};
use iced::{Alignment, Length};

use super::super::widgets::{El, SMALL, action, empty, mark, muted, pill};
use super::{Fact, part};
use crate::domain::{CertificateHealth, NodeId};
use crate::ui::styles::Kind;
use crate::ui::{App, Icon, Message, Screen, Tokens, Tone, format};

/// The node inspected, `width` wide.
pub fn view<'a>(app: &'a App, id: &'a NodeId, width: f32, tokens: Tokens) -> El<'a> {
    let fleet = app.fleet.as_ref();
    let Some(node) = fleet.and_then(|f| f.nodes.iter().find(|n| &n.id == id)) else {
        return column![
            super::head(app, Vec::new(), Vec::new(), id.as_str(), None, width, tokens),
            empty(Icon::Nodes, "This node is not in the fleet now", tokens)
        ]
        .into();
    };
    let mut first = vec![
        pill(Tone::of_node(node.status), node.status.name(), tokens),
        muted(node.host_name.clone(), tokens).wrapping(Wrapping::None).into(),
    ];
    if let Some(behind) = fleet.and_then(|f| f.behind(node)) {
        first.push(mark(Icon::Update, tokens.wait, behind.tip(), tokens));
    }
    if let Some((tone, tip)) = super::super::nodes::certificate_mark(node, app.now) {
        first.push(mark(Icon::Alert, tone.color(&tokens), tip, tokens));
    }
    let acts = node
        .status
        .can_revoke()
        .then(|| {
            action(
                Kind::Danger,
                Icon::Ban,
                "Revoke",
                Some(Message::AskRevoke(node.id.clone())),
                tokens,
            )
        })
        .into_iter()
        .collect();
    let certificate = match node.certificate_health(app.now) {
        CertificateHealth::Good(_) | CertificateHealth::Soon(_) => node
            .certificate_not_after
            .map(|at| format!("until {}", at.format("%Y-%m-%d")))
            .unwrap_or_default(),
        CertificateHealth::Expired => "expired".to_string(),
        CertificateHealth::Unknown if node.certificate_serial.is_some() => "not seen yet".to_string(),
        CertificateHealth::Unknown => "local socket".to_string(),
    };
    let seen = match node.revoked_at {
        Some(at) => format!("revoked {}", format::ago(at, app.now)),
        None => format::ago(node.last_seen_at, app.now),
    };
    let facts = super::facts(
        vec![
            ("Seen", Fact::Words(seen)),
            ("AXLR", Fact::Words(dash(&node.capabilities.axlr_version))),
            (
                "Agent",
                Fact::Words(super::super::nodes::agent_version(node).unwrap_or_else(|| "—".into())),
            ),
            ("Certificate", Fact::Words(certificate)),
        ],
        width,
        tokens,
    );
    let labels = node.label_pairs();
    let mut page = column![part(
        "Labels",
        if labels.is_empty() {
            El::from(muted("None", tokens))
        } else {
            super::tags(labels, tokens)
        },
        tokens
    )]
    .spacing(16);
    if node.capabilities.dedicated {
        page = page.push(muted("Dedicated: it runs only the bots placed on it", tokens));
    }
    page = page.push(part(
        "Model providers",
        if node.capabilities.providers.is_empty() {
            El::from(muted("None named", tokens))
        } else {
            super::tags(node.capabilities.providers.clone(), tokens)
        },
        tokens,
    ));
    let placed: Vec<_> = fleet
        .map(|f| f.bots.iter().filter(|b| b.placement.nodes.contains(&node.id)).collect())
        .unwrap_or_default();
    let mut bots = column![];
    for bot in &placed {
        bots = bots.push(super::mini(
            row![
                container(super::super::status::bot(bot.status, tokens)).width(96),
                container(
                    text(bot.id.to_string())
                        .size(SMALL)
                        .font(super::super::widgets::SEMIBOLD)
                )
                .width(Length::Fill)
                .clip(true),
                muted(bot.groups.join(", "), tokens).wrapping(Wrapping::None),
            ]
            .spacing(10)
            .align_y(Alignment::Center)
            .into(),
            Some(Message::Go(Screen::Bot(bot.id.clone()))),
            tokens,
        ));
    }
    let heading = format!("Bots that run here · {}", placed.len());
    let bots: El<'a> = if placed.is_empty() {
        muted("None placed on it by name", tokens).into()
    } else {
        bots.into()
    };
    page = page.push(part_owned(heading, bots, tokens));
    column![
        super::head(app, first, acts, node.id.as_str(), None, width, tokens),
        facts,
        super::body(page),
    ]
    .into()
}

/// A part whose heading is made at the time.
fn part_owned<'a>(heading: String, content: El<'a>, tokens: Tokens) -> El<'a> {
    column![super::super::widgets::label(&heading, tokens).size(11.0), content]
        .spacing(6)
        .width(Length::Fill)
        .into()
}

/// The value, or a dash for one the node did not say.
fn dash(value: &str) -> String {
    if value.trim().is_empty() {
        "—".to_string()
    } else {
        value.to_string()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_value_the_node_did_not_say_is_a_dash() {
        assert_eq!(dash(" "), "—");
        assert_eq!(dash("0.18.3"), "0.18.3");
    }
}
