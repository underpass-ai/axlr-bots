//! Numbers and times in few characters.

use chrono::{DateTime, Duration, Local, Utc};

use crate::domain::Event;

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

/// Events one per line, `14:05:09 Failed · the file does not exist`:
/// what a copy of a timeline takes.
pub fn events<'a>(events: impl IntoIterator<Item = &'a Event>) -> String {
    events
        .into_iter()
        .map(|event| {
            let detail = event.detail();
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
        assert_eq!(short_id("task-68edb0ffeae524e9", 13), "task-68edb0ff…");
        assert_eq!(short_id("bot", 13), "bot");
        let moment: DateTime<Utc> = "2026-10-25T10:00:00Z".parse().unwrap();
        assert_eq!(clock(moment).len(), 8);
        assert!(when(moment).contains("Oct"));
    }
}
