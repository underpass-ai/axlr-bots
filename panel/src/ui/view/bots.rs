//! Bots: the list by team, switching them on and off, and adding one.

use iced::widget::{Space, column, container, pick_list, row, text};
use iced::{Alignment, Length};

use super::sheet::{self, Column, Head, Ink, Line};
use super::widgets::{
    El, SMALL, action, bold, card, empty, field, icon, icon_button, mark, muted, section_title, status as status_word,
};
use super::{grid, levels, listbar, status};
use crate::application::BotNow;
use crate::domain::{AutonomyLevel, Bot, BotForm, ModelFit, NodeCapabilities, team_names};
use crate::ui::styles::{self, Kind};
use crate::ui::table::{BotColumn, BotShow, ColumnRef, List, RowRef, SortColumn};
use crate::ui::{App, Icon, Message, NodeChoice, Screen, Tokens, Tone};

pub fn view<'a>(app: &'a App, tokens: Tokens) -> El<'a> {
    let Some(fleet) = &app.fleet else {
        return super::overview::loading(app, tokens);
    };
    let listing = &app.tables.bot_listing;
    let chips = BotShow::ALL
        .into_iter()
        .map(|show| {
            listbar::chip(
                show.label(),
                fleet.bots.iter().filter(|b| show.takes(b)).count(),
                listing.show == show,
                Message::ShowBots(show),
                tokens,
            )
        })
        .collect();
    let acts = vec![
        action(
            Kind::Secondary,
            Icon::File,
            "From file",
            (!app.register.busy).then_some(Message::PickFile),
            tokens,
        ),
        action(
            Kind::Primary,
            Icon::Plus,
            "New bot",
            (!app.register.open).then_some(Message::RegisterOpen),
            tokens,
        ),
    ];
    let mut page = column![listbar::bar("Bots", fleet.bots.len().to_string(), chips, acts, tokens)];
    if app.register.open {
        page = page.push(listbar::panel(register_form(app, tokens), tokens));
    }
    page = page.push(if fleet.bots.is_empty() {
        listbar::nothing(empty(Icon::Bots, "No bot yet: add one", tokens))
    } else if !fleet.bots.iter().any(|b| listing.show.takes(b)) {
        listbar::nothing(empty(Icon::Bots, "No bot here", tokens))
    } else {
        list(app, fleet, tokens)
    });
    grid::with_bar(app, List::Bots, page.into(), tokens)
}

/// The bots by team, each team under a header that folds it, in the
/// table's order within their team: a box, the status (what the bot is
/// doing now, when it is doing something), the name, its level, model
/// and place, how it starts, and its acts.
fn list<'a>(app: &'a App, fleet: &'a crate::application::Fleet, tokens: Tokens) -> El<'a> {
    let switching = app.switching;
    let state = &app.tables.bots;
    let listing = &app.tables.bot_listing;
    let groups = listing.groups(state.sort.order(&fleet.bots));
    let head = |c: BotColumn| Head::Sort {
        label: c.label(),
        badge: state.sort.badge(c),
        column: ColumnRef::Bots(c),
    };
    let shown = listing.shown(&fleet.bots);
    let all = state.selection.covers(shown.iter().map(|b| &b.id));
    let columns = vec![
        Column::fixed(Head::Check { all, list: List::Bots }, 18.0, move |b: &'a Bot, _| {
            let id = b.id.clone();
            grid::check(
                state.selection.contains(&b.id),
                move |on| Message::Pick(RowRef::Bot(id.clone()), on),
                tokens,
            )
        }),
        // What it is doing now when it is doing something, else its
        // status; and the mark of a newer version ready beside it.
        Column::fixed(head(BotColumn::Status), 112.0, move |b: &'a Bot, _| {
            let mut cell = row![now(b, &fleet.tasks, tokens)].spacing(4).align_y(Alignment::Center);
            if let Some(update) = b.update() {
                cell = cell.push(mark(Icon::Update, tokens.accent, update.tip(), tokens));
            }
            cell.into()
        }),
        // The name, and its definition's version after it when it has one.
        Column::share(head(BotColumn::Bot), 4, move |b: &'a Bot, width| {
            let name = b.id.to_string();
            match b.version_label() {
                Some(version) => {
                    let room = width - 8.0 - Ink::Quiet.span(version.chars().count());
                    let short = grid::shorten(&name, Ink::Strong.fits(room));
                    let open = Message::Go(Screen::Bot(b.id.clone()));
                    let line = row![
                        sheet::link(short, room, open, tokens),
                        Ink::Quiet.write(version, tokens)
                    ]
                    .spacing(8)
                    .align_y(Alignment::Center);
                    sheet::tip(container(line).clip(true).into(), name, tokens)
                }
                None => sheet::link(name, width, Message::Go(Screen::Bot(b.id.clone())), tokens),
            }
        }),
        Column::fixed(head(BotColumn::Level), 86.0, move |b: &'a Bot, _| {
            Ink::Quiet.write(b.level.label(), tokens).into()
        }),
        Column::share(head(BotColumn::Model), 4, move |b: &'a Bot, width| {
            sheet::words(b.model.short_model().to_string(), width, Ink::Quiet, tokens)
        }),
        Column::share(head(BotColumn::RunsOn), 3, move |b: &'a Bot, width| {
            sheet::words(b.placement.describe(), width, Ink::Quiet, tokens)
        }),
        Column::share(head(BotColumn::StartsBy), 3, move |b: &'a Bot, width| {
            sheet::words(b.trigger_summary(), width, Ink::Quiet, tokens)
        }),
        Column::fixed(Head::None, 3.0 * grid::ACT + 8.0, move |b: &'a Bot, _| {
            let switch = if b.status.can_pause() {
                Some(grid::icon_act(
                    Kind::Ghost,
                    Icon::Pause,
                    "Pause",
                    (!switching).then(|| Message::Pause(b.id.clone())),
                    tokens,
                ))
            } else if b.status.can_enable() {
                Some(grid::icon_act(
                    Kind::Good,
                    Icon::Play,
                    "Turn on",
                    (!switching).then(|| Message::Enable(b.id.clone())),
                    tokens,
                ))
            } else {
                None
            };
            row![
                grid::act_slot(switch),
                grid::act_slot(Some(grid::icon_act(
                    Kind::Ghost,
                    Icon::Stop,
                    "Stop bot and its tasks",
                    (!switching).then(|| Message::AskStopBot(b.id.clone())),
                    tokens
                ))),
                grid::act_slot(Some(grid::icon_act(
                    Kind::Ghost,
                    Icon::Next,
                    "Open",
                    Some(Message::Go(Screen::Bot(b.id.clone()))),
                    tokens,
                ))),
            ]
            .spacing(4)
            .align_y(Alignment::Center)
            .into()
        }),
    ];
    let mut lines = Vec::new();
    for group in groups {
        lines.push(Line::Group {
            name: group.name().to_string(),
            note: group.counts(),
            open: group.open,
            press: Box::new(Message::FoldTeam(group.team.clone())),
        });
        if group.open {
            lines.extend(group.bots.into_iter().map(|b| Line::Row {
                row: b,
                picked: state.selection.contains(&b.id),
            }));
        }
    }
    sheet::sheet(columns, lines, app.density, app.tables.picked(List::Bots) > 0, tokens)
}

/// A bot's status in the list. A bot at work, waiting for a person or
/// with a task queued says that instead, in its colour, with the task
/// on hover: the list is where a person looks for who is busy.
fn now<'a>(bot: &'a Bot, tasks: &[crate::domain::Task], tokens: Tokens) -> El<'a> {
    let state = BotNow::of(bot, tasks, &[]);
    let Some(task) = state.task() else {
        return status::bot(bot.status, tokens);
    };
    let tone = Tone::of_now(&state);
    let glyph = match state {
        BotNow::Queued { .. } => Icon::Hourglass,
        _ => tone.icon(),
    };
    sheet::tip(
        status_word(glyph, tone, state.word(), false, tokens),
        format!("{}: {}", state.word(), task.headline(160)),
        tokens,
    )
}

/// The Team question: a name its teammates share, with the teams there
/// are.
fn team_field<'a>(app: &'a App, form: &'a BotForm, tokens: Tokens) -> El<'a> {
    let names: Vec<String> = app.teams().into_iter().map(|t| t.name).collect();
    let hint = if names.is_empty() {
        "a name its teammates share; empty: on its own".to_string()
    } else {
        format!("teams now: {}", names.join(", "))
    };
    let mut column = column![
        field(
            "Which team is it in?",
            "ops",
            &form.team,
            Message::RegisterTeam,
            None,
            tokens
        ),
        muted(hint, tokens),
    ]
    .spacing(4);
    let teams = form.teams();
    if teams != team_names(&form.team) {
        column = column.push(warning(format!("It will be saved as {}", teams.join(", ")), tokens));
    }
    column.into()
}

/// A line that asks the operator to look before adding the bot.
fn warning<'a>(words: String, tokens: Tokens) -> El<'a> {
    row![
        icon(Icon::Alert, 14.0, tokens.wait),
        text(words).size(SMALL).color(tokens.wait)
    ]
    .spacing(6)
    .align_y(Alignment::Center)
    .into()
}

/// Whether a bot already has the id this form's name gives: the control
/// plane would refuse the new one.
fn id_taken(app: &App, form: &BotForm) -> bool {
    let (Some(fleet), Ok(id)) = (&app.fleet, form.bot_id()) else {
        return false;
    };
    fleet.bots.iter().any(|b| b.id.as_str() == id)
}

/// What the name becomes: the id the bot will have, or why it has none.
/// Nothing until a name is typed.
fn id_note<'a>(app: &App, form: &BotForm, tokens: Tokens) -> Option<El<'a>> {
    if form.name.trim().is_empty() {
        return None;
    }
    Some(match form.bot_id() {
        Ok(id) if id_taken(app, form) => warning(format!("The id {id} is taken: choose another name"), tokens),
        Ok(id) => muted(format!("Its id will be {id}"), tokens).into(),
        Err(error) => warning(error.to_string(), tokens),
    })
}

/// A warning when the chosen node named its providers and none serves
/// the model typed: the bot can still be added, so the operator decides.
fn model_warning(app: &App, node: &str, model: &str) -> Option<String> {
    let node = app
        .fleet
        .as_ref()?
        .nodes
        .iter()
        .find(|n| n.id.as_str() == node.trim())?;
    if node.capabilities.fit(model) != ModelFit::Missing {
        return None;
    }
    let needs = NodeCapabilities::provider_for(model.trim());
    let has: Vec<&str> = node
        .capabilities
        .providers
        .iter()
        .map(|p| p.strip_prefix("local:").unwrap_or(p))
        .collect();
    Some(format!(
        "{} has no {needs} for {}: it has {}.",
        node.id,
        model.trim(),
        has.join(", ")
    ))
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
    let mut where_it_runs = column![
        bold("Where does it run?").size(SMALL),
        pick_list(nodes, selected, Message::RegisterNode)
            .padding(10)
            .width(360)
            .style(styles::picker(tokens))
            .menu_style(styles::menu(tokens))
    ]
    .spacing(6);
    if let Some(words) = model_warning(app, &form.node, &form.model) {
        where_it_runs = where_it_runs.push(warning(words, tokens));
    }
    let mut name = column![field(
        "Name",
        "Deploy watch",
        &form.name,
        Message::RegisterName,
        None,
        tokens
    )]
    .spacing(4);
    if let Some(note) = id_note(app, form, tokens) {
        name = name.push(note);
    }
    let mut body = column![
        row![
            section_title(Icon::Plus, "New bot", tokens),
            Space::new().width(Length::Fill),
            icon_button(Icon::Cross, "Close", Some(Message::RegisterClose), tokens)
        ]
        .align_y(Alignment::Center),
        container(name).width(360),
        field(
            "What should it do?",
            "In your words: what it watches, what it does, what it tells you",
            &form.mission,
            Message::RegisterMission,
            None,
            tokens
        ),
        row![
            where_it_runs.width(360),
            container(team_field(app, form, tokens)).width(360),
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
                (!busy && !id_taken(app, form)).then_some(Message::RegisterSubmit),
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
                    model_choices(app, &form.model, tokens),
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

/// The models the settings offer, one click each; the one in the field
/// is lit. The list comes from the panel's settings, not from any bot.
fn model_choices<'a>(app: &'a App, current: &str, tokens: Tokens) -> El<'a> {
    let mut chips = row![].spacing(6);
    for model in app.new_bot.choices() {
        let picked = model == current;
        chips = chips.push(action(
            Kind::Chip(picked),
            Icon::Chip,
            model.clone(),
            (!picked).then_some(Message::RegisterModel(model)),
            tokens,
        ));
    }
    chips.wrap().into()
}
