//! Bots: the list, switching them on and off, and adding one.

use iced::widget::{Space, column, container, pick_list, row, scrollable, table, text};
use iced::{Alignment, Length};

use super::widgets::{
    El, SMALL, action, bold, card, chip, empty, field, icon_button, muted, pill, section_title, title,
};
use crate::domain::{AutonomyLevel, Bot};
use crate::ui::styles::Kind;
use crate::ui::{App, Icon, Message, NodeChoice, Screen, Tokens, Tone};

pub fn view<'a>(app: &'a App, tokens: Tokens) -> El<'a> {
    let Some(fleet) = &app.fleet else {
        return super::overview::loading(app, tokens);
    };
    let mut page = column![
        row![
            title(Icon::Bots, "Bots", tokens),
            Space::new().width(Length::Fill),
            action(
                Kind::Secondary,
                Icon::File,
                "From file",
                (!app.register.busy).then_some(Message::PickFile),
                tokens
            ),
            action(
                Kind::Primary,
                Icon::Plus,
                "New bot",
                (!app.register.open).then_some(Message::RegisterOpen),
                tokens
            ),
        ]
        .spacing(10)
        .align_y(Alignment::Center)
    ]
    .spacing(18);
    if app.register.open {
        page = page.push(register_form(app, tokens));
    }
    let switching = app.switching;
    let columns = vec![
        table::column(bold("Status"), move |b: &'a Bot| {
            pill(Tone::of_bot(b.status), b.status.name(), tokens)
        }),
        table::column(bold("Bot"), move |b: &'a Bot| -> El<'a> {
            column![bold(b.id.to_string()), muted(b.owner.clone(), tokens)]
                .spacing(2)
                .into()
        }),
        table::column(bold("Level"), move |b: &'a Bot| chip(b.level.label(), tokens)),
        table::column(bold("Model"), move |b: &'a Bot| text(b.model.short_model().to_string())),
        table::column(bold("Runs on"), move |b: &'a Bot| text(b.placement.describe())),
        table::column(bold("Starts by"), move |b: &'a Bot| {
            text(b.trigger_summary()).size(SMALL)
        }),
        table::column(text(""), move |b: &'a Bot| -> El<'a> {
            let switch = if b.status.can_pause() {
                action(
                    Kind::Secondary,
                    Icon::Pause,
                    "Pause",
                    (!switching).then(|| Message::Pause(b.id.clone())),
                    tokens,
                )
            } else if b.status.can_enable() {
                action(
                    Kind::Good,
                    Icon::Play,
                    "Turn on",
                    (!switching).then(|| Message::Enable(b.id.clone())),
                    tokens,
                )
            } else {
                Space::new().into()
            };
            row![
                switch,
                action(
                    Kind::Ghost,
                    Icon::Play,
                    "Open",
                    Some(Message::Go(Screen::Bot(b.id.clone()))),
                    tokens
                )
            ]
            .spacing(8)
            .into()
        }),
    ];
    let list: El<'a> = if fleet.bots.is_empty() {
        empty(Icon::Bots, "No bot yet: add one", tokens)
    } else {
        table::table(columns, &fleet.bots)
            .padding_x(12)
            .padding_y(10)
            .width(Length::Fill)
            .into()
    };
    page = page.push(card(list, tokens));
    scrollable(page.padding(iced::Padding {
        right: 12.0,
        ..Default::default()
    }))
    .height(Length::Fill)
    .into()
}

fn register_form<'a>(app: &'a App, tokens: Tokens) -> El<'a> {
    let form = &app.register.form;
    let nodes: Vec<NodeChoice> = std::iter::once(NodeChoice::Any)
        .chain(
            app.fleet
                .iter()
                .flat_map(|f| f.nodes.iter().map(|n| NodeChoice::One(n.id.clone()))),
        )
        .collect();
    let selected = if form.node.is_empty() {
        Some(NodeChoice::Any)
    } else {
        nodes
            .iter()
            .find(|c| matches!(c, NodeChoice::One(id) if id.as_str() == form.node))
            .cloned()
    };
    let busy = app.register.busy;
    card(
        column![
            row![
                section_title(Icon::Plus, "New bot", tokens),
                Space::new().width(Length::Fill),
                icon_button(Icon::Cross, "Close", Some(Message::RegisterClose), tokens)
            ]
            .align_y(Alignment::Center),
            row![
                container(field("Name", "helper", &form.name, Message::RegisterName, None, tokens))
                    .width(Length::FillPortion(1)),
                container(field(
                    "Mission",
                    "What the bot is for, in one line",
                    &form.mission,
                    Message::RegisterMission,
                    None,
                    tokens
                ))
                .width(Length::FillPortion(3)),
            ]
            .spacing(14),
            row![
                column![
                    bold("Level").size(SMALL),
                    pick_list(&AutonomyLevel::ALL[..], Some(form.level), Message::RegisterLevel).padding(10)
                ]
                .spacing(6),
                container(field(
                    "Tools",
                    "local_read, local_list",
                    &form.tools,
                    Message::RegisterTools,
                    None,
                    tokens
                ))
                .width(Length::FillPortion(2)),
                container(field(
                    "Model",
                    "provider/model",
                    &form.model,
                    Message::RegisterModel,
                    None,
                    tokens
                ))
                .width(Length::FillPortion(2)),
                column![
                    bold("Runs on").size(SMALL),
                    pick_list(nodes, selected, Message::RegisterNode).padding(10)
                ]
                .spacing(6),
            ]
            .spacing(14)
            .align_y(Alignment::End),
            row![action(
                Kind::Primary,
                Icon::Plus,
                if busy { "Adding…" } else { "Add bot" },
                (!busy).then_some(Message::RegisterSubmit),
                tokens
            )],
        ]
        .spacing(14),
        tokens,
    )
    .into()
}
