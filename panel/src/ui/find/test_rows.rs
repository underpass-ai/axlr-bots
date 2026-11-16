//! What the find kit's tests share: rows of each list, made short.

use crate::domain::{Bot, Node, Task};

/// A task of a bot with its status (the control plane's word), the
/// trigger that made it (`timer:clock`) and the message of the event
/// that started it, when one did. Its minute sets its age: the list
/// comes newest first.
pub(crate) fn task(id: &str, bot: &str, status: &str, trigger: &str, message: Option<&str>) -> Task {
    let (kind, name) = trigger.split_once(':').unwrap_or((trigger, trigger));
    let mut task: Task = serde_json::from_value(serde_json::json!({
        "id": id, "botId": bot, "objective": format!("Do the work of {bot}.\nThen stop."), "status": status,
        "correlationId": format!("trace-of-{id}"), "trigger": {"type": kind, "name": name},
        "createdAt": "2026-10-25T09:00:00Z", "updatedAt": "2026-10-25T09:00:07Z"
    }))
    .unwrap();
    task.input = message.map(|m| serde_json::json!({"from": "watcher", "name": "stock.low", "message": m}));
    task
}

/// A bot of the teams named (`ops,lab`; empty for none), on or paused.
pub(crate) fn bot(id: &str, teams: &str, status: &str) -> Bot {
    let groups: Vec<&str> = teams.split(',').filter(|t| !t.is_empty()).collect();
    serde_json::from_value(serde_json::json!({
        "id": id, "name": format!("The {id} bot"), "mission": "Do what each task asks.", "status": status,
        "groups": groups, "level": "act-with-approval", "model": {"model": "vendor/small-model"},
        "placement": {"nodes": ["host-a"]},
        "createdAt": "2026-10-25T09:00:00Z", "updatedAt": "2026-10-25T09:00:00Z"
    }))
    .unwrap()
}

/// A node on a host, with its status (the control plane's word).
pub(crate) fn node(id: &str, host: &str, status: &str) -> Node {
    serde_json::from_value(serde_json::json!({
        "id": id, "hostName": host, "status": status, "labels": {"zone": "eu"},
        "capabilities": {"axlrVersion": "0.18.3", "providers": ["openrouter"]},
        "registeredAt": "2026-10-25T09:00:00Z", "lastSeenAt": "2026-10-25T09:59:00Z"
    }))
    .unwrap()
}
