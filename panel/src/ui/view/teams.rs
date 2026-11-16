//! Teams: who is in each, what each member is for, and the team's
//! timeline as it happens.

use std::rc::Rc;

use iced::widget::{column, container, row};
use iced::{Alignment, Length};

use super::sheet::{self, Column, Head, Ink, Line};
use super::widgets::{El, empty, muted};
use super::{grid, listbar};
use crate::domain::Team;
use crate::ui::find::Query;
use crate::ui::format;
use crate::ui::styles::Kind;
use crate::ui::table::{ColumnRef, List, RowRef, SortColumn, TeamColumn};
use crate::ui::{App, Icon, Message, Screen, Tokens};

/// Every team with its members and its last word, one line each.
pub fn list<'a>(app: &'a App, tokens: Tokens) -> El<'a> {
    if app.fleet.is_none() {
        return super::overview::loading(app, tokens);
    }
    let teams = app.teams();
    let query = app.list_query();
    let rows = app.team_rows();
    let count = if query.is_empty() {
        teams.len().to_string()
    } else {
        format!("{} of {}", rows.len(), teams.len())
    };
    let filter = vec![listbar::filter(&app.tables.query, tokens)];
    let bar = listbar::bar("Teams", count, filter, Vec::new(), tokens);
    if teams.is_empty() {
        return column![
            bar,
            listbar::nothing(
                column![
                    empty(Icon::Team, "No team yet", tokens),
                    container(muted(
                        "Give two bots the same Team, in New bot or on a bot's page: they know each other, ask each other and warn each other.",
                        tokens
                    ))
                    .center_x(Length::Fill),
                ]
                .spacing(10)
                .into()
            )
        ]
        .into();
    }
    if rows.is_empty() {
        let words = format!("Nothing matches {} among the {} teams.", query.text(), teams.len());
        return column![bar, listbar::nothing(empty(Icon::Lens, words, tokens))].into();
    }
    let state = &app.tables.teams;
    let query = Rc::new(query);
    let shown: Rc<Vec<Team>> = Rc::new(rows);
    let head = |c: TeamColumn| Head::Sort {
        label: c.label(),
        badge: state.sort.badge(c),
        column: ColumnRef::Teams(c),
    };
    let all = state.selection.covers(shown.iter().map(|t| &t.name));
    // A row is its team's place in `shown`, which every cell keeps.
    let of = |cell: fn(&'a App, &Team, f32, &Query, Tokens) -> El<'a>| {
        let shown = shown.clone();
        let query = query.clone();
        move |at: usize, width: f32| cell(app, &shown[at], width, &query, tokens)
    };
    let columns = vec![
        Column::fixed(
            Head::Check { all, list: List::Teams },
            18.0,
            of(|app, team, _, _, tokens| {
                let name = team.name.clone();
                grid::check(
                    app.tables.teams.selection.contains(&team.name),
                    move |on| Message::Pick(RowRef::Team(name.clone()), on),
                    tokens,
                )
            }),
        ),
        Column::share(
            head(TeamColumn::Team),
            3,
            of(|_, team, width, query, tokens| {
                let lonely = team.lonely();
                let room = if lonely { width - 24.0 } else { width };
                let open = Message::Go(Screen::Team(team.name.clone()));
                let mut cell = row![sheet::link(team.name.clone(), room, open, query, tokens)]
                    .spacing(4)
                    .align_y(Alignment::Center);
                if lonely {
                    cell = cell.push(super::widgets::mark(
                        Icon::Alert,
                        tokens.wait,
                        "Only one bot: a typo?".to_string(),
                        tokens,
                    ));
                }
                cell.into()
            }),
        ),
        Column::fixed(
            head(TeamColumn::Bots),
            44.0,
            of(|_, team, _, _, tokens| Ink::Plain.write(team.members.len().to_string(), tokens).into()),
        ),
        Column::share(
            Head::Plain("Members"),
            8,
            of(|_, team, width, query, tokens| {
                let names: Vec<String> = team.members.iter().map(|b| b.id.to_string()).collect();
                sheet::found(names.join(" · "), width, Ink::Quiet, query, tokens)
            }),
        ),
        Column::share(
            Head::Plain("Last word"),
            8,
            of(|app, team, width, _, tokens| {
                let last = app
                    .watch
                    .feed(&team.name)
                    .and_then(|f| f.events.iter().rev().find(|e| e.event_type == "bot.event.emitted"));
                match last {
                    Some(event) => sheet::pair(
                        &format!("{} {}", format::clock(event.created_at), event.label()),
                        &format::event_detail(event),
                        width,
                        &Query::default(),
                        tokens,
                    ),
                    None => muted("—", tokens).into(),
                }
            }),
        ),
        Column::fixed(
            Head::None,
            grid::ACT,
            of(|_, team, _, _, tokens| {
                grid::act_slot(Some(grid::icon_act(
                    Kind::Ghost,
                    Icon::Next,
                    "Open",
                    Some(Message::Go(Screen::Team(team.name.clone()))),
                    tokens,
                )))
            }),
        ),
    ];
    let cursor = state.cursor.selected();
    let lines = shown
        .iter()
        .enumerate()
        .map(|(at, team)| Line::Row {
            row: at,
            picked: state.selection.contains(&team.name),
            selected: cursor == Some(&team.name),
            press: Box::new(Message::RowPicked(RowRef::Team(team.name.clone()))),
        })
        .collect();
    let page = column![
        bar,
        sheet::sheet(columns, lines, app.density, app.tables.picked(List::Teams) > 0, tokens)
    ];
    grid::with_bar(app, List::Teams, page.into(), tokens)
}
