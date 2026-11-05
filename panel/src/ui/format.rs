//! Numbers and times in few characters.

use chrono::{DateTime, Duration, Local, Utc};

use crate::domain::{Cost, Event, NodeWait, Usage};

/// `now`, `12s`, `5m`, `3h`, `2d` ago.
pub fn age(from: DateTime<Utc>, now: DateTime<Utc>) -> String {
    span(now - from)
}

/// `just now`, `12s ago`, `5m ago`.
pub fn ago(from: DateTime<Utc>, now: DateTime<Utc>) -> String {
    match age(from, now).as_str() {
        "now" => "just now".to_string(),
        other => format!("{other} ago"),
    }
}

/// A duration as one unit: `45s`, `3m`, `2h`, `5d`.
pub fn span(duration: Duration) -> String {
    let seconds = duration.num_seconds().max(0);
    match seconds {
        0..=4 => "now".to_string(),
        5..=59 => format!("{seconds}s"),
        60..=3599 => format!("{}m", seconds / 60),
        3600..=86_399 => format!("{}h", seconds / 3600),
        _ => format!("{}d", seconds / 86_400),
    }
}

/// A run's length: `7s`, `1m 30s`, `2h 5m`.
pub fn run_length(duration: Duration) -> String {
    let seconds = duration.num_seconds().max(0);
    match seconds {
        0..=59 => format!("{seconds}s"),
        60..=3599 => format!("{}m {}s", seconds / 60, seconds % 60),
        _ => format!("{}h {}m", seconds / 3600, (seconds % 3600) / 60),
    }
}

/// Dollars with as many decimals as a small cost needs: `$0`, `$0.0012`,
/// `$0.42`, `$12.30`.
pub fn cost(usd: f64) -> String {
    if usd <= 0.0 {
        "$0".to_string()
    } else if usd < 0.01 {
        format!("${usd:.4}")
    } else {
        format!("${usd:.2}")
    }
}

/// What runs cost together: `$0.42`, or `≥ $0.42` when some run did not
/// tell its cost.
pub fn spend(cost: Cost) -> String {
    if cost.is_floor() {
        format!("≥ {}", self::cost(cost.usd))
    } else {
        self::cost(cost.usd)
    }
}

/// What one run cost: `$0.0012`, or `cost unknown` when it stopped
/// before it said.
pub fn run_cost(usage: &Usage) -> String {
    if usage.cost_unknown {
        "cost unknown".to_string()
    } else {
        cost(usage.cost_usd)
    }
}

/// A file's size as `512 B`, `2.0 KB`, `10.0 MB`, in steps of 1024.
pub fn bytes(count: u64) -> String {
    const KB: u64 = 1024;
    match count {
        0..KB => format!("{count} B"),
        KB..=1_048_575 => format!("{:.1} KB", count as f64 / KB as f64),
        _ => format!("{:.1} MB", count as f64 / (KB * KB) as f64),
    }
}

/// Tokens as `950`, `12.3k`, `1.2M`.
pub fn tokens(count: u64) -> String {
    match count {
        0..=999 => count.to_string(),
        1_000..=999_999 => format!("{:.1}k", count as f64 / 1_000.0),
        _ => format!("{:.1}M", count as f64 / 1_000_000.0),
    }
}

/// The local wall-clock time, `14:05:09`.
pub fn clock(moment: DateTime<Utc>) -> String {
    moment.with_timezone(&Local).format("%H:%M:%S").to_string()
}

/// A task's Activity one entry per line, `14:05:09 list_pods · ok ·
/// 120 ms`, then the model's words: what a copy of the Activity takes.
pub fn activity(activity: &crate::application::Activity) -> String {
    let mut lines: Vec<String> = activity
        .entries
        .iter()
        .map(|entry| format!("{} {}", clock(entry.at()), entry.headline()))
        .collect();
    if !activity.thinking.trim().is_empty() {
        lines.push(String::new());
        lines.push(format!("Thinking: {}", activity.thinking.trim()));
    }
    if !activity.text.trim().is_empty() {
        lines.push(String::new());
        lines.push(activity.text.trim().to_string());
    }
    lines.join("\n")
}

/// An event's detail as a person reads it: what a decision queued for a
/// node waits for, with the time in the person's own clock, or the
/// event's own detail.
pub fn event_detail(event: &Event) -> String {
    NodeWait::of(event)
        .map(|wait| self::wait(&wait))
        .unwrap_or_else(|| event.detail())
}

/// What a decision queued for a node waits for, in a sentence: `Waiting
/// for node web-1, offline since 11:09`, in local time.
pub fn wait(wait: &NodeWait) -> String {
    let node = &wait.node;
    match (&wait.since, wait.busy_with.is_empty(), wait.status.as_str()) {
        (Some(since), _, status) if !status.is_empty() => format!(
            "Waiting for node {node}, {} since {}",
            status.to_lowercase(),
            since.with_timezone(&Local).format("%H:%M")
        ),
        (_, false, _) => format!("Waiting for node {node}, busy with task {}", wait.busy_with.join(", ")),
        (_, true, "gone") => format!("Waiting for node {node}, which is gone"),
        (_, true, "") => upper_first(&wait.said),
        (_, true, status) => format!("Waiting for node {node}, which is {status}"),
    }
}

fn upper_first(words: &str) -> String {
    let mut chars = words.chars();
    match chars.next() {
        Some(first) => first.to_uppercase().chain(chars).collect(),
        None => String::new(),
    }
}

/// Events one per line, `14:05:09 Failed · the file does not exist`:
/// what a copy of a timeline takes.
pub fn events<'a>(events: impl IntoIterator<Item = &'a Event>) -> String {
    events
        .into_iter()
        .map(|event| {
            let detail = event_detail(event);
            let head = format!("{} {}", clock(event.created_at), event.label());
            if detail.is_empty() {
                head
            } else {
                format!("{head} · {detail}")
            }
        })
        .collect::<Vec<_>>()
        .join("\n")
}

/// An address without its scheme and its trailing slash:
/// `https://127.0.0.1:7879/` is `127.0.0.1:7879`.
pub fn endpoint(url: &str) -> String {
    let url = url.trim();
    let bare = url.split_once("://").map_or(url, |(_, rest)| rest);
    bare.trim_end_matches('/').to_string()
}

/// The local date and time, `25 Oct 14:05`.
pub fn when(moment: DateTime<Utc>) -> String {
    moment.with_timezone(&Local).format("%d %b %H:%M").to_string()
}

/// An id cut to its first characters, `task-68edb0ff…`.
pub fn short_id(id: &str, keep: usize) -> String {
    if id.chars().count() <= keep {
        id.to_string()
    } else {
        format!("{}…", id.chars().take(keep).collect::<String>())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_cost_some_run_did_not_tell_is_a_floor() {
        assert_eq!(spend(Cost::new(0.42, 0)), "$0.42");
        assert_eq!(spend(Cost::new(0.42, 2)), "≥ $0.42");
        assert_eq!(spend(Cost::new(0.0, 1)), "≥ $0");
        let mut usage = Usage {
            cost_usd: 0.0012,
            ..Default::default()
        };
        assert_eq!(run_cost(&usage), "$0.0012");
        usage = Usage {
            cost_unknown: true,
            ..Default::default()
        };
        assert_eq!(run_cost(&usage), "cost unknown");
    }

    #[test]
    fn says_what_a_queued_decision_waits_for_in_local_time() {
        let since: DateTime<Utc> = "2026-11-05T10:09:26Z".parse().unwrap();
        let mut wait = NodeWait {
            node: "web-1".into(),
            status: "Offline".into(),
            since: Some(since),
            busy_with: Vec::new(),
            said: "waiting for node web-1, offline since 10:09 UTC".into(),
            execution: "exec-2".into(),
        };
        let local = since.with_timezone(&Local).format("%H:%M").to_string();
        assert_eq!(
            super::wait(&wait),
            format!("Waiting for node web-1, offline since {local}")
        );
        wait.since = None;
        assert_eq!(super::wait(&wait), "Waiting for node web-1, which is Offline");
        wait.status = "gone".into();
        assert_eq!(super::wait(&wait), "Waiting for node web-1, which is gone");
        wait.status.clear();
        wait.busy_with = vec!["task-9".into(), "task-8".into()];
        assert_eq!(
            super::wait(&wait),
            "Waiting for node web-1, busy with task task-9, task-8"
        );
        wait.busy_with.clear();
        assert_eq!(super::wait(&wait), "Waiting for node web-1, offline since 10:09 UTC");
        assert_eq!(upper_first(""), "");
        let event: Event = serde_json::from_value(serde_json::json!({
            "eventId": "e1", "type": "bot.task.decided", "tenantId": "t", "createdAt": "2026-11-05T10:13:37Z",
            "payload": {"decision": "approve", "nodeId": "web-1", "nodeStatus": "Offline",
                "waiting": "waiting for node web-1, which is Offline"}
        }))
        .unwrap();
        assert_eq!(event_detail(&event), "Waiting for node web-1, which is Offline");
        assert_eq!(event.detail(), "waiting for node web-1, which is Offline");
    }

    #[test]
    fn events_one_per_line() {
        let failed: Event = serde_json::from_str(
            r#"{"eventId":"e1","type":"bot.task.failed","createdAt":"2026-10-25T10:00:00Z","payload":{"reason":"no file"}}"#,
        )
        .unwrap();
        let started: Event =
            serde_json::from_str(r#"{"eventId":"e2","type":"bot.task.started","createdAt":"2026-10-25T10:00:05Z"}"#)
                .unwrap();
        assert_eq!(
            events([&failed, &started]),
            format!(
                "{} Failed · no file\n{} Started",
                clock(failed.created_at),
                clock(started.created_at)
            )
        );
        assert_eq!(events(&[]), "");
    }

    #[test]
    fn endpoints_lose_the_scheme() {
        assert_eq!(endpoint("https://127.0.0.1:17879/"), "127.0.0.1:17879");
        assert_eq!(endpoint(" cp.example:7879 "), "cp.example:7879");
    }

    #[test]
    fn ages_and_lengths() {
        let now: DateTime<Utc> = "2026-10-25T10:00:00Z".parse().unwrap();
        assert_eq!(age(now, now), "now");
        assert_eq!(age(now - Duration::seconds(12), now), "12s");
        assert_eq!(age(now - Duration::minutes(5), now), "5m");
        assert_eq!(age(now - Duration::hours(3), now), "3h");
        assert_eq!(age(now - Duration::days(2), now), "2d");
        assert_eq!(age(now + Duration::minutes(1), now), "now");
        assert_eq!(ago(now, now), "just now");
        assert_eq!(ago(now - Duration::minutes(5), now), "5m ago");
        assert_eq!(run_length(Duration::seconds(7)), "7s");
        assert_eq!(run_length(Duration::seconds(90)), "1m 30s");
        assert_eq!(run_length(Duration::minutes(125)), "2h 5m");
    }

    #[test]
    fn money_tokens_and_ids() {
        assert_eq!(cost(0.0), "$0");
        assert_eq!(cost(0.0012), "$0.0012");
        assert_eq!(cost(0.42), "$0.42");
        assert_eq!(cost(12.3), "$12.30");
        assert_eq!(tokens(950), "950");
        assert_eq!(tokens(12_300), "12.3k");
        assert_eq!(tokens(1_250_000), "1.2M");
        assert_eq!(bytes(512), "512 B");
        assert_eq!(bytes(2048), "2.0 KB");
        assert_eq!(bytes(10_485_760), "10.0 MB");
        assert_eq!(short_id("task-68edb0ffeae524e9", 13), "task-68edb0ff…");
        assert_eq!(short_id("bot", 13), "bot");
        let moment: DateTime<Utc> = "2026-10-25T10:00:00Z".parse().unwrap();
        assert_eq!(clock(moment).len(), 8);
        assert!(when(moment).contains("Oct"));
    }
}
