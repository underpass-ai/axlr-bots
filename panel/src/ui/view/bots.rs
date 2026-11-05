//! Bots: the list, switching them on and off, and adding one.

use iced::widget::{Space, column, container, pick_list, responsive, row, scrollable, table, text};
use iced::{Alignment, Length};

use super::widgets::{
    El, SMALL, action, bold, card, chip, empty, field, icon, icon_button, muted, pill, section_title, title,
};
use super::{grid, levels};
use crate::application::BotNow;
use crate::domain::{AutonomyLevel, Bot};
use crate::ui::styles::Kind;
use crate::ui::table::{BotColumn, ColumnRef, List, RowRef, SortColumn};
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
    let list: El<'a> = if fleet.bots.is_empty() {
        empty(Icon::Bots, "No bot yet: add one", tokens)
    } else {
        let mut body = column![].spacing(12);
        if let Some(bar) = grid::bar(app, List::Bots, tokens) {
            body = body.push(bar);
        }
        body.push(responsive(move |space| list(app, fleet, space.width < NARROW, tokens)).height(Length::Shrink))
            .into()
    };
    page = page.push(card(list, tokens));
    scrollable(page.padding(iced::Padding {
        right: 12.0,
        ..Default::default()
    }))
    .id(super::widgets::PAGE)
    .height(Length::Fill)
    .into()
}

/// Under this width the table leaves out Starts by and Open is an icon,
/// so a bot's name and what it does now keep room.
const NARROW: f32 = 900.0;

/// The bots in the table's order: a box, the status, the name and
/// model, what it does now, its level, team and place, how it starts
/// unless `narrow`, and its acts.
fn list<'a>(app: &'a App, fleet: &'a crate::application::Fleet, narrow: bool, tokens: Tokens) -> El<'a> {
    let switching = app.switching;
    let state = &app.tables.bots;
    let shown = state.sort.order(&fleet.bots);
    let head = |c: BotColumn| {
        grid::header(
            c.label(),
            state.sort.badge(c),
            Message::SortBy(ColumnRef::Bots(c)),
            tokens,
        )
    };
    let all = state.selection.covers(shown.iter().map(|b| &b.id));
    let mut columns = vec![
        table::column(
            grid::check(all, |on| Message::PickShown(List::Bots, on), tokens),
            move |b: &'a Bot| {
                let id = b.id.clone();
                grid::check(
                    state.selection.contains(&b.id),
                    move |on| Message::Pick(RowRef::Bot(id.clone()), on),
                    tokens,
                )
            },
        ),
        table::column(head(BotColumn::Status), move |b: &'a Bot| {
            pill(Tone::of_bot(b.status), b.status.name(), tokens)
        }),
        // A long name is cut to one line, whole on hover.
        table::column(head(BotColumn::Bot), move |b: &'a Bot| -> El<'a> {
            column![
                grid::fitted(b.id.to_string(), true, tokens),
                grid::fitted_muted(b.model.short_model().to_string(), tokens)
            ]
            .spacing(2)
            .into()
        })
        .width(Length::FillPortion(3)),
        table::column(bold("Now"), move |b: &'a Bot| -> El<'a> {
            let state = BotNow::of(b, &fleet.tasks, &[]);
            let mut cell = column![pill(Tone::of_now(&state), state.word(), tokens)].spacing(4);
            if let Some(task) = state.task() {
                cell = cell.push(muted(task.headline(36), tokens));
            }
            cell.into()
        })
        .width(Length::FillPortion(3)),
        table::column(head(BotColumn::Level), move |b: &'a Bot| chip(b.level.label(), tokens)),
        table::column(head(BotColumn::Team), move |b: &'a Bot| {
            grid::clipped(b.team_summary(), 14, false, tokens)
        }),
        table::column(head(BotColumn::RunsOn), move |b: &'a Bot| text(b.placement.describe()))
            .width(Length::FillPortion(2)),
    ];
    if !narrow {
        columns.push(
            table::column(head(BotColumn::StartsBy), move |b: &'a Bot| {
                text(b.trigger_summary()).size(SMALL)
            })
            .width(Length::FillPortion(2)),
        );
    }
    columns.push(table::column(text(""), move |b: &'a Bot| -> El<'a> {
        let switch = if b.status.can_pause() {
            grid::icon_act(
                Kind::Secondary,
                Icon::Pause,
                "Pause",
                (!switching).then(|| Message::Pause(b.id.clone())),
                tokens,
            )
        } else if b.status.can_enable() {
            grid::icon_act(
                Kind::Good,
                Icon::Play,
                "Turn on",
                (!switching).then(|| Message::Enable(b.id.clone())),
                tokens,
            )
        } else {
            Space::new().width(32).into()
        };
        let open = if narrow {
            grid::icon_act(
                Kind::Ghost,
                Icon::Next,
                "Open",
                Some(Message::Go(Screen::Bot(b.id.clone()))),
                tokens,
            )
        } else {
            action(
                Kind::Ghost,
                Icon::Play,
                "Open",
                Some(Message::Go(Screen::Bot(b.id.clone()))),
                tokens,
            )
        };
        row![
            switch,
            grid::icon_act(
                Kind::Ghost,
                Icon::Stop,
                "Stop bot and its tasks",
                (!switching).then(|| Message::AskStopBot(b.id.clone())),
                tokens
            ),
            open
        ]
        .spacing(4)
        .align_y(Alignment::Center)
        .into()
    }));
    table::table(columns, shown)
        .padding_x(8)
        .padding_y(10)
        .width(Length::Fill)
        .into()
}

/// The Team question: a name its teammates share, with the teams there
/// are.
fn team_field<'a>(app: &'a App, value: &'a str, tokens: Tokens) -> El<'a> {
    let names: Vec<String> = app.teams().into_iter().map(|t| t.name).collect();
    let hint = if names.is_empty() {
        "a name its teammates share; empty: on its own".to_string()
    } else {
        format!("teams now: {}", names.join(", "))
    };
    column![
        field(
            "Which team is it in?",
            "ops",
            value,
            Message::RegisterTeam,
            None,
            tokens
        ),
        muted(hint, tokens),
    ]
    .spacing(4)
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
    let mut body = column![
        row![
            section_title(Icon::Plus, "New bot", tokens),
            Space::new().width(Length::Fill),
            icon_button(Icon::Cross, "Close", Some(Message::RegisterClose), tokens)
        ]
        .align_y(Alignment::Center),
        container(field(
            "Name",
            "deploy-watch",
            &form.name,
            Message::RegisterName,
            None,
            tokens
        ))
        .width(360),
        field(
            "What should it do?",
            "In your words: what it watches, what it does, what it tells you",
            &form.mission,
            Message::RegisterMission,
            None,
            tokens
        ),
        row![
            column![
                bold("Where does it run?").size(SMALL),
                pick_list(nodes, selected, Message::RegisterNode).padding(10).width(360)
            ]
            .spacing(6),
            container(team_field(app, &form.team, tokens)).width(360),
        ]
        .spacing(14)
        .align_y(Alignment::Start),
        row![
            icon(Icon::Check, 14.0, tokens.good),
            muted(
                format!(
                    "{}: it works on its own and {}.",
                    levels::plain(form.level).0,
                    level_promise(form.level)
                ),
                tokens
            )
        ]
        .spacing(8)
        .align_y(Alignment::Center),
        row![
            action(
                Kind::Primary,
                Icon::Plus,
                if busy { "Adding…" } else { "Add bot" },
                (!busy).then_some(Message::RegisterSubmit),
                tokens
            ),
            action(
                Kind::Ghost,
                if app.register.advanced {
                    Icon::Cross
                } else {
                    Icon::Settings
                },
                if app.register.advanced {
                    "Hide advanced"
                } else {
                    "Advanced"
                },
                Some(Message::RegisterAdvanced(!app.register.advanced)),
                tokens
            ),
        ]
        .spacing(12)
        .align_y(Alignment::Center),
    ]
    .spacing(14);
    if app.register.advanced {
        let mut choices = column![bold("How much may it do alone?")].spacing(10);
        for level in levels::CHOICES {
            let picked = level == form.level;
            choices = choices.push(levels::choice(
                level,
                picked,
                (!picked).then_some(Message::RegisterLevel(level)),
                tokens,
            ));
        }
        body = body.push(
            row![
                container(choices).width(Length::FillPortion(1)),
                column![
                    field(
                        "Tools",
                        "local_read, local_list",
                        &form.tools,
                        Message::RegisterTools,
                        None,
                        tokens
                    ),
                    field(
                        "Model",
                        "provider/model",
                        &form.model,
                        Message::RegisterModel,
                        None,
                        tokens
                    ),
                    muted("It starts when you give it a task.", tokens),
                ]
                .spacing(14)
                .width(Length::FillPortion(1)),
            ]
            .spacing(24),
        );
    }
    card(body, tokens).into()
}

/// What the picked level promises, after "it works on its own and".
fn level_promise(level: AutonomyLevel) -> &'static str {
    match level {
        AutonomyLevel::ActWithApproval => "asks you first before anything that changes the world",
        AutonomyLevel::ActWithinPolicy => "acts within its rules without asking",
        AutonomyLevel::Observe => "only watches and reports",
        AutonomyLevel::Propose => "only proposes",
        AutonomyLevel::Unknown => "follows a level this panel does not know",
    }
}
