//! Incidents: the grouped faults the bots triage, the ones that need you
//! first, one incident's alerts, diagnosis, proposal and tasks, the
//! Approve and Deny of its apply, and closing one by hand.

use iced::widget::text::Wrapping;
use iced::widget::{Space, column, container, responsive, row, scrollable, table, text, text_input};
use iced::{Alignment, Length};

use super::grid;
use super::widgets::{
    BODY, El, PILL_SLOT, Voice, action, bold, card, empty, fact, link, muted, pill, pill_with, reader, section, slot,
    title,
};
use crate::domain::{Incident, IncidentPhase, IncidentStatus, TaskId};
use crate::ui::format;
use crate::ui::styles::{self, Kind};
use crate::ui::table::{ColumnRef, IncidentColumn, List, RowRef, SortColumn, needs_you_first};
use crate::ui::{App, Icon, Message, ReaderKey, Screen, Tokens, Tone};

/// Every incident, the ones that need you first, then the last seen.
pub fn list<'a>(app: &'a App, tokens: Tokens) -> El<'a> {
    let mut page = column![row![title(Icon::Alert, "Incidents", tokens)].align_y(Alignment::Center)].spacing(18);
    let Some(incidents) = &app.incidents.list else {
        return page
            .push(empty(Icon::Hourglass, "Loading the incidents…", tokens))
            .into();
    };
    let body: El<'a> = if incidents.is_empty() {
        empty(Icon::Check, "No incident: nothing is firing", tokens)
    } else {
        responsive(move |space| table_of(app, incidents, space.width < NARROW, tokens))
            .height(Length::Shrink)
            .into()
    };
    page = page.push(card(body, tokens));
    let page = scrollable(page.padding(iced::Padding {
        right: 12.0,
        bottom: grid::BAR_ROOM,
        ..Default::default()
    }))
    .id(super::widgets::PAGE)
    .height(Length::Fill);
    grid::with_bar(app, List::Incidents, page.into(), tokens)
}

/// Under this width the severity and the status share one column and
/// the repeats go, so the title keeps room to read beside Open.
const NARROW: f32 = 900.0;

/// The incidents in the table's order: a box, the severity and status,
/// the title, the bot, the repeats unless `narrow`, when last seen, Open.
fn table_of<'a>(app: &'a App, incidents: &'a [Incident], narrow: bool, tokens: Tokens) -> El<'a> {
    let now = app.now;
    let state = &app.tables.incidents;
    let shown = needs_you_first(state.sort.order(incidents));
    let head = |c: IncidentColumn| {
        grid::header(
            c.label(),
            state.sort.badge(c),
            Message::SortBy(ColumnRef::Incidents(c)),
            tokens,
        )
    };
    let all = state.selection.covers(shown.iter().map(|i| &i.id));
    let mut columns = vec![table::column(
        grid::check(all, |on| Message::PickShown(List::Incidents, on), tokens),
        move |i: &'a Incident| {
            let id = i.id.clone();
            grid::check(
                state.selection.contains(&i.id),
                move |on| Message::Pick(RowRef::Incident(id.clone()), on),
                tokens,
            )
        },
    )];
    if narrow {
        columns.push(table::column(
            column![head(IncidentColumn::Severity), head(IncidentColumn::Status)].spacing(2),
            move |i: &'a Incident| -> El<'a> {
                column![severity(i, tokens), status_and_wait(i, tokens)]
                    .spacing(4)
                    .into()
            },
        ));
    } else {
        columns.push(table::column(head(IncidentColumn::Severity), move |i: &'a Incident| {
            severity(i, tokens)
        }));
        columns.push(table::column(head(IncidentColumn::Status), move |i: &'a Incident| {
            status_and_wait(i, tokens)
        }));
    }
    // The title takes what the other columns leave and wraps there, so
    // the smallest window keeps the age and Open in sight.
    columns.push(
        table::column(head(IncidentColumn::Incident), move |i: &'a Incident| {
            text(i.title.clone()).wrapping(Wrapping::WordOrGlyph)
        })
        .width(Length::Fill),
    );
    columns.push(table::column(head(IncidentColumn::Bot), move |i: &'a Incident| {
        grid::clipped(i.bot_id.to_string(), if narrow { 12 } else { 20 }, true, tokens)
    }));
    if !narrow {
        columns.push(table::column(head(IncidentColumn::Repeats), move |i: &'a Incident| {
            text(i.repeats.to_string())
        }));
    }
    columns.push(table::column(head(IncidentColumn::LastSeen), move |i: &'a Incident| {
        text(format::age(i.last_seen_at, now)).wrapping(Wrapping::None)
    }));
    columns.push(table::column(text(""), move |i: &'a Incident| {
        super::widgets::open("Open", Message::Go(Screen::Incident(i.id.clone())), tokens)
    }));
    grid::sheet(columns, shown)
}

/// One incident: where it stands, its alerts, what the triage found, the
/// tasks that worked on it, and Resolve.
pub fn detail<'a>(app: &'a App, id: &'a str, tokens: Tokens) -> El<'a> {
    let back = action(
        Kind::Ghost,
        Icon::Back,
        "Incidents",
        Some(Message::Go(Screen::Incidents)),
        tokens,
    );
    let Some(incident) = app.incidents.detail.as_ref().filter(|i| i.id == id) else {
        return column![back, empty(Icon::Hourglass, "Loading the incident…", tokens)]
            .spacing(14)
            .into();
    };
    let form = &app.incidents.resolve;
    let mut header = row![
        back,
        severity(incident, tokens),
        status(incident, tokens),
        reader(
            &app.readers,
            ReaderKey::IncidentTitle,
            incident.title.clone(),
            Voice::Heading,
            tokens
        ),
    ]
    .spacing(12)
    .align_y(Alignment::Center);
    if incident.needs_you {
        header = header.push(needs_you(tokens));
    }
    if incident.status.can_resolve() && !form.open {
        header = header.push(action(
            Kind::Secondary,
            Icon::Check,
            "Resolve",
            Some(Message::ResolveOpen),
            tokens,
        ));
    }
    let mut page = column![header].spacing(16);
    if let Some(card) = decision_card(app, incident, tokens) {
        page = page.push(card);
    }
    if form.open && incident.status.can_resolve() {
        page = page.push(resolve_form(app, tokens));
    }
    page = page.push(facts(app, incident, tokens));
    if !incident.reason.is_empty() {
        let why = match incident.status {
            IncidentStatus::Resolved => "Why it was resolved",
            IncidentStatus::Escalated => "Why it was escalated",
            IncidentStatus::Open if incident.cycle > 1 => "Why it is open again",
            _ => "Reason",
        };
        page = page.push(section(
            Icon::Alert,
            why,
            reader(
                &app.readers,
                ReaderKey::IncidentReason,
                incident.reason.clone(),
                Voice::Body,
                tokens,
            ),
            tokens,
        ));
    }
    if !incident.diagnosis.is_empty() {
        page = page.push(section(
            Icon::Check,
            "Diagnosis",
            reader(
                &app.readers,
                ReaderKey::IncidentDiagnosis,
                incident.diagnosis.clone(),
                Voice::Body,
                tokens,
            ),
            tokens,
        ));
    }
    if !incident.proposal.is_empty() {
        page = page.push(section(
            Icon::Wrench,
            proposal_heading(incident),
            reader(
                &app.readers,
                ReaderKey::IncidentProposal,
                incident.proposal.clone(),
                Voice::Body,
                tokens,
            ),
            tokens,
        ));
    }
    page = page.push(
        row![
            container(section(Icon::Alert, "Alerts", alerts(app, incident, tokens), tokens))
                .width(Length::FillPortion(1)),
            container(section(Icon::Tasks, "Tasks", tasks(app, incident, tokens), tokens))
                .width(Length::FillPortion(1)),
        ]
        .spacing(14),
    );
    scrollable(page.padding(iced::Padding {
        right: 12.0,
        ..Default::default()
    }))
    .id(super::widgets::PAGE)
    .height(Length::Fill)
    .into()
}

fn facts<'a>(app: &'a App, incident: &'a Incident, tokens: Tokens) -> El<'a> {
    let mut left = column![
        fact(
            "Bot",
            link(
                incident.bot_id.to_string(),
                Message::Go(Screen::Bot(incident.bot_id.clone())),
                tokens
            ),
            tokens
        ),
        fact("Tenant", text(incident.tenant_id.clone()), tokens),
        fact(
            "Source",
            reader(
                &app.readers,
                ReaderKey::IncidentSource,
                incident.source.clone(),
                Voice::Body,
                tokens
            ),
            tokens
        ),
        fact(
            "Opened",
            text(format!(
                "{} · {}",
                format::when(incident.opened_at),
                format::ago(incident.opened_at, app.now)
            )),
            tokens
        ),
        fact("Last seen", text(format::ago(incident.last_seen_at, app.now)), tokens),
    ]
    .spacing(8)
    .width(Length::FillPortion(1));
    if let Some(at) = incident.resolved_at {
        left = left.push(fact("Resolved", text(format::ago(at, app.now)), tokens));
    }
    let right = column![
        fact("Cost", text(format::spend(incident.cost())), tokens),
        fact("Repeats", text(incident.repeats.to_string()), tokens),
        fact("Retries", text(incident.retries.to_string()), tokens),
        fact("Sent back", text(incident.send_backs.to_string()), tokens),
        fact("Cycle", text(incident.cycle.to_string()), tokens),
        fact(
            "Group",
            reader(
                &app.readers,
                ReaderKey::IncidentGroup,
                incident.group_key.clone(),
                Voice::Mono,
                tokens
            ),
            tokens
        ),
    ]
    .spacing(8)
    .width(Length::FillPortion(1));
    let right = match incident.approvals {
        Some(approvals) => right.push(fact("Approvals", text(approvals.summary()), tokens)),
        None => right,
    };
    card(row![left, right].spacing(20), tokens).into()
}

/// What the proposal is now: waiting for you, being applied, applied and
/// being verified, or the last one, which the incident went back from.
fn proposal_heading(incident: &Incident) -> &'static str {
    match incident.status {
        IncidentStatus::Proposed if incident.needs_you => "Proposal · needs you",
        IncidentStatus::Proposed => "Proposal · not applied",
        IncidentStatus::Applying => "Proposal · being applied",
        IncidentStatus::Verifying => "Proposal · applied, verifying",
        _ => "Last proposal",
    }
}

/// The amber mark of an incident whose current task waits for you.
fn needs_you<'a>(tokens: Tokens) -> El<'a> {
    pill_with(Icon::Alert, Tone::Wait, "needs you", tokens)
}

/// The status, and under it the mark when the incident needs you.
fn status_and_wait<'a>(incident: &Incident, tokens: Tokens) -> El<'a> {
    if incident.needs_you {
        column![status(incident, tokens), needs_you(tokens)].spacing(4).into()
    } else {
        status(incident, tokens)
    }
}

/// The decision the incident waits for: which task asks to call which
/// tool, with Approve and Deny, the Tasks screen's own. Approve lets the
/// apply run and moves the incident to Applying; Deny sends it back to
/// Open (the second denial leaves it to a person).
fn decision_card<'a>(app: &'a App, incident: &'a Incident, tokens: Tokens) -> Option<El<'a>> {
    let (task, _) = incident.decision()?;
    let current = incident.current_task.as_ref()?;
    let tool = current.waiting_call().map_or("a write", |w| w.tool.as_str());
    let what = match current.phase {
        IncidentPhase::Apply => "Approve to apply the proposal; Deny sends the incident back to Open.",
        _ => "Approve to let the call run; Deny refuses it.",
    };
    let buttons: El<'a> = if app.deciding.as_ref() == Some(task) {
        row![
            super::widgets::icon(Icon::Hourglass, 18.0, tokens.wait),
            bold("Sending your answer…")
        ]
        .spacing(10)
        .align_y(Alignment::Center)
        .into()
    } else {
        row![
            action(
                Kind::Good,
                Icon::Check,
                "Approve",
                Some(Message::Approve(task.clone())),
                tokens
            ),
            action(
                Kind::Danger,
                Icon::Cross,
                "Deny",
                Some(Message::AskDeny(task.clone())),
                tokens
            ),
            Space::new().width(Length::Fill),
            link(
                format!("{} · {}", current.phase.name(), current.bot_id),
                Message::Go(Screen::Task(task.clone())),
                tokens
            ),
        ]
        .spacing(12)
        .align_y(Alignment::Center)
        .into()
    };
    Some(
        card(
            column![
                super::widgets::section_title(Icon::Alert, "Needs you", tokens),
                row![
                    muted("The task asks to call", tokens),
                    bold(tool.to_string()).size(20.0)
                ]
                .spacing(10)
                .align_y(Alignment::Center),
                muted(what, tokens),
                buttons,
            ]
            .spacing(12),
            tokens,
        )
        .into(),
    )
}

fn severity<'a>(incident: &Incident, tokens: Tokens) -> El<'a> {
    pill_with(
        Icon::of_severity(incident.severity),
        Tone::of_severity(incident.severity),
        incident.severity.name(),
        tokens,
    )
}

fn status<'a>(incident: &Incident, tokens: Tokens) -> El<'a> {
    pill_with(
        Icon::of_incident(incident.status),
        Tone::of_incident(incident.status),
        incident.status.name(),
        tokens,
    )
}

/// The alerts still firing, then those the source said went green.
fn alerts<'a>(app: &'a App, incident: &'a Incident, tokens: Tokens) -> El<'a> {
    if incident.fingerprints.is_empty() {
        return empty(Icon::Alert, "No alert listed", tokens);
    }
    let fingerprint = |f: &str| {
        reader(
            &app.readers,
            ReaderKey::Fingerprint(f.to_string()),
            f.to_string(),
            Voice::Mono,
            tokens,
        )
    };
    let mut body = column![].spacing(8);
    let firing = incident.firing();
    if !firing.is_empty() {
        let mut list = column![muted("Firing", tokens)].spacing(4);
        for f in firing {
            list = list.push(row![pill(Tone::Bad, "firing", tokens), fingerprint(f)].spacing(8));
        }
        body = body.push(list);
    }
    if !incident.resolved_fingerprints.is_empty() {
        let mut list = column![muted("Went green", tokens)].spacing(4);
        for f in &incident.resolved_fingerprints {
            list = list.push(row![pill(Tone::Good, "green", tokens), fingerprint(f)].spacing(8));
        }
        body = body.push(list);
    }
    body.into()
}

/// The tasks of each phase, oldest first, with their status when the
/// fleet's read has them; a click opens one.
fn tasks<'a>(app: &'a App, incident: &'a Incident, tokens: Tokens) -> El<'a> {
    if incident.task_ids.is_empty() {
        return empty(Icon::Tasks, "No task worked on it", tokens);
    }
    let mut list = column![].spacing(8);
    for id in &incident.task_ids {
        list = list.push(task_line(app, incident, id, tokens));
    }
    list.into()
}

/// A task by what it was asked, its phase, bot and age; by its id only
/// when the fleet's read no longer holds it.
fn task_line<'a>(app: &'a App, incident: &'a Incident, id: &'a TaskId, tokens: Tokens) -> El<'a> {
    let task = app.fleet.as_ref().and_then(|f| f.tasks.iter().find(|t| &t.id == id));
    let go = Message::Go(Screen::Task(id.clone()));
    let phase = incident
        .phase_of(id)
        .map(|p| format!("{} · ", p.name()))
        .unwrap_or_default();
    match task {
        Some(task) => row![
            slot(pill(Tone::of_task(task.status), task.status.label(), tokens), PILL_SLOT),
            container(link(grid::shorten(task.ask(), 60), go, tokens)).width(Length::Fill),
            muted(
                format!("{phase}{} · {}", task.bot_id, format::age(task.created_at, app.now)),
                tokens
            )
            .wrapping(Wrapping::None),
        ]
        .spacing(10)
        .align_y(Alignment::Center)
        .into(),
        None => row![
            link(id.to_string(), go, tokens),
            muted("older than the tasks listed", tokens)
        ]
        .spacing(10)
        .align_y(Alignment::Center)
        .into(),
    }
}

/// The reason box and the button that closes the incident. The buttons
/// sit on their own line, so the box never takes their room.
fn resolve_form<'a>(app: &'a App, tokens: Tokens) -> El<'a> {
    let form = &app.incidents.resolve;
    let reason = text_input("Why it is over (optional)", &form.reason)
        .on_input(Message::ResolveReason)
        .on_submit(Message::ResolveSend)
        .padding(12)
        .size(BODY)
        .style(styles::field(tokens));
    card(
        column![
            super::widgets::section_title(Icon::Check, "Resolve this incident", tokens),
            muted(
                "It closes now. An alert of its group in the next 30 minutes opens it again.",
                tokens
            ),
            reason,
            row![
                action(
                    Kind::Primary,
                    Icon::Check,
                    if form.busy { "Resolving…" } else { "Resolve" },
                    (!form.busy).then_some(Message::ResolveSend),
                    tokens
                ),
                action(
                    Kind::Ghost,
                    Icon::Back,
                    "Keep it open",
                    Some(Message::ResolveClose),
                    tokens
                ),
                Space::new().width(Length::Fill),
            ]
            .spacing(12)
            .align_y(Alignment::Center),
        ]
        .spacing(12),
        tokens,
    )
    .into()
}
