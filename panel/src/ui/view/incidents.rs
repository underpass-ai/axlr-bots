//! Incidents: the grouped faults the bots triage, one incident's alerts,
//! diagnosis and tasks, and closing one by hand.

use iced::widget::text::Wrapping;
use iced::widget::{Space, column, container, mouse_area, row, scrollable, table, text, text_input};
use iced::{Alignment, Length, mouse};

use super::widgets::{
    BODY, El, Voice, action, bold, card, chip, empty, fact, muted, pill, pill_with, reader, section, title,
};
use crate::domain::{Incident, TaskId};
use crate::ui::format;
use crate::ui::styles::{self, Kind};
use crate::ui::{App, Icon, Message, ReaderKey, Screen, Tokens, Tone};

/// Every incident, the last seen first.
pub fn list<'a>(app: &'a App, tokens: Tokens) -> El<'a> {
    let mut page = column![row![title(Icon::Alert, "Incidents", tokens)].align_y(Alignment::Center)].spacing(18);
    let Some(incidents) = &app.incidents.list else {
        return page
            .push(empty(Icon::Hourglass, "Loading the incidents…", tokens))
            .into();
    };
    let now = app.now;
    let columns = vec![
        table::column(bold("Severity"), move |i: &'a Incident| severity(i, tokens)),
        table::column(bold("Status"), move |i: &'a Incident| status(i, tokens)),
        // The title takes what the other columns leave and wraps there,
        // so the smallest window keeps the age and Open in sight.
        table::column(bold("Incident"), move |i: &'a Incident| {
            text(i.title.clone()).wrapping(Wrapping::WordOrGlyph)
        })
        .width(Length::Fill),
        table::column(bold("Bot"), move |i: &'a Incident| bold(i.bot_id.to_string())),
        table::column(bold("Repeats"), move |i: &'a Incident| text(i.repeats.to_string())),
        table::column(bold("Last seen"), move |i: &'a Incident| {
            text(format::age(i.last_seen_at, now)).wrapping(Wrapping::None)
        }),
        table::column(text(""), move |i: &'a Incident| {
            action(
                Kind::Ghost,
                Icon::Play,
                "Open",
                Some(Message::Go(Screen::Incident(i.id.clone()))),
                tokens,
            )
        }),
    ];
    let body: El<'a> = if incidents.is_empty() {
        empty(Icon::Check, "No incident: nothing is firing", tokens)
    } else {
        table::table(columns, incidents)
            .padding_x(12)
            .padding_y(8)
            .width(Length::Fill)
            .into()
    };
    page = page.push(card(body, tokens));
    scrollable(page.padding(iced::Padding {
        right: 12.0,
        ..Default::default()
    }))
    .id(super::widgets::PAGE)
    .height(Length::Fill)
    .into()
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
    if form.open && incident.status.can_resolve() {
        page = page.push(resolve_form(app, tokens));
    }
    page = page.push(facts(app, incident, tokens));
    if !incident.reason.is_empty() {
        let why = match incident.status {
            crate::domain::IncidentStatus::Resolved => "Why it was resolved",
            crate::domain::IncidentStatus::Escalated => "Why it was escalated",
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
            mouse_area(bold(incident.bot_id.to_string()).color(tokens.accent))
                .on_press(Message::Go(Screen::Bot(incident.bot_id.clone())))
                .interaction(mouse::Interaction::Pointer),
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
        fact("Cost", text(format::cost(incident.cost_usd)), tokens),
        fact("Repeats", text(incident.repeats.to_string()), tokens),
        fact("Retries", text(incident.retries.to_string()), tokens),
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
    card(row![left, right].spacing(20), tokens).into()
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

/// The triage tasks, one per cycle, with their status when the fleet's
/// read has them; a click opens one.
fn tasks<'a>(app: &'a App, incident: &'a Incident, tokens: Tokens) -> El<'a> {
    if incident.task_ids.is_empty() {
        return empty(Icon::Tasks, "No task worked on it", tokens);
    }
    let mut list = column![].spacing(8);
    for id in &incident.task_ids {
        list = list.push(task_line(app, id, tokens));
    }
    list.into()
}

fn task_line<'a>(app: &'a App, id: &'a TaskId, tokens: Tokens) -> El<'a> {
    let task = app.fleet.as_ref().and_then(|f| f.tasks.iter().find(|t| &t.id == id));
    let mut line = row![].spacing(10).align_y(Alignment::Center);
    if let Some(task) = task {
        line = line.push(pill(Tone::of_task(task.status), task.status.label(), tokens));
    }
    line = line.push(
        mouse_area(bold(id.to_string()).color(tokens.accent))
            .on_press(Message::Go(Screen::Task(id.clone())))
            .interaction(mouse::Interaction::Pointer),
    );
    if let Some(task) = task {
        line = line.push(chip(format::age(task.created_at, app.now), tokens));
    }
    line.into()
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
