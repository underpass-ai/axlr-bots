//! Teams: one line each with how many bots it has and how many are on;
//! the team inspected beside it says who is in it and what happens.

use std::rc::Rc;

use iced::widget::{column, container, row};
use iced::{Alignment, Length};

use super::sheet::{self, Column, Head, Ink, Line};
use super::widgets::{El, empty, muted};
use super::{grid, listbar};
use crate::domain::{BotStatus, Team};
use crate::ui::find::Query;
use crate::ui::table::{ColumnRef, List, RowRef, SortColumn, TeamColumn};
use crate::ui::{App, Icon, Message, Screen, Tokens, Tone};

/// Every team, one line each: its name, its bots and how many are on.
/// One is always inspected beside the list.
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
        // How many of its bots are on: green while one is, quiet when
        // all are paused.
        Column::fixed(
            Head::Plain("On"),
            96.0,
            of(|_, team, _, _, tokens| {
                let on = team.members.iter().filter(|b| b.status == BotStatus::Enabled).count();
                if on > 0 {
                    super::widgets::status(Icon::Check, Tone::Good, format!("{on} on"), false, tokens)
                } else {
                    super::widgets::status(Icon::Pause, Tone::Idle, "paused", true, tokens)
                }
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
            opens: RowRef::Team(team.name.clone()),
        })
        .collect();
    let page = column![
        bar,
        sheet::sheet(
            columns,
            lines,
            app.density,
            &app.hover,
            app.tables.picked(List::Teams) > 0,
            tokens
        )
    ];
    grid::with_bar(app, List::Teams, page.into(), tokens)
}
