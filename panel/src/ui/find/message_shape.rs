/// An event's message with its dates and times taken out: what two
/// messages of a periodic event share. `hourly sync
/// 2026-11-16T09:00:00Z` and `hourly sync 2026-11-16T08:00:00Z` have
/// one shape; `item-4198 · gate failed` and `item-4182 · gate failed`
/// keep apart, since a number that is not a date or a time can be what
/// tells two pieces of work apart.
#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord)]
pub struct MessageShape(String);

impl MessageShape {
    /// What stands in the place of a date or a time.
    const MOMENT: char = '⧖';

    pub fn of(message: &str) -> Self {
        let chars: Vec<char> = message.trim().chars().collect();
        let mut shape = String::with_capacity(message.len());
        let mut at = 0;
        while at < chars.len() {
            match moment(&chars, at) {
                Some(end) => {
                    shape.push(Self::MOMENT);
                    at = end;
                }
                None => {
                    shape.push(chars[at]);
                    at += 1;
                }
            }
        }
        Self(shape)
    }
}

/// Where a date or a time that starts at `at` ends, when one does:
/// `2026-11-16`, with or without `T09:00`, `:00`, a fraction and `Z`,
/// or a time alone, `09:00` or `09:00:00`. It starts a word: a digit
/// right before it means it is the tail of a longer number.
fn moment(chars: &[char], at: usize) -> Option<usize> {
    if at > 0 && chars[at - 1].is_ascii_digit() {
        return None;
    }
    if let Some(end) = date(chars, at) {
        let mut end = end;
        if chars.get(end) == Some(&'T')
            && let Some(time_end) = time(chars, end + 1)
        {
            end = time_end;
        }
        return Some(zone(chars, end));
    }
    time(chars, at).map(|end| zone(chars, end))
}

/// `yyyy-mm-dd` at `at`: where it ends.
fn date(chars: &[char], at: usize) -> Option<usize> {
    let end = digits(chars, at, 4)?;
    let end = digits(chars, sep(chars, end, '-')?, 2)?;
    let end = digits(chars, sep(chars, end, '-')?, 2)?;
    whole(chars, end)
}

/// `hh:mm`, `hh:mm:ss` and an optional fraction at `at`: where it ends.
fn time(chars: &[char], at: usize) -> Option<usize> {
    let mut end = digits(chars, sep(chars, digits(chars, at, 2)?, ':')?, 2)?;
    if let Some(seconds) = sep(chars, end, ':').and_then(|s| digits(chars, s, 2)) {
        end = seconds;
    }
    if chars.get(end) == Some(&'.') {
        let mut fraction = end + 1;
        while chars.get(fraction).is_some_and(char::is_ascii_digit) {
            fraction += 1;
        }
        if fraction > end + 1 {
            end = fraction;
        }
    }
    whole(chars, end)
}

/// A trailing `Z` after a time, taken with it.
fn zone(chars: &[char], end: usize) -> usize {
    if chars.get(end) == Some(&'Z') { end + 1 } else { end }
}

/// Exactly `count` digits at `at`: where they end.
fn digits(chars: &[char], at: usize, count: usize) -> Option<usize> {
    let end = at + count;
    (end <= chars.len() && chars[at..end].iter().all(char::is_ascii_digit)).then_some(end)
}

/// The separator `mark` at `at`: where what follows it starts.
fn sep(chars: &[char], at: usize, mark: char) -> Option<usize> {
    (chars.get(at) == Some(&mark)).then_some(at + 1)
}

/// The end of a date or time, when no digit follows it: `2026-11-160`
/// is no date.
fn whole(chars: &[char], end: usize) -> Option<usize> {
    (!chars.get(end).is_some_and(char::is_ascii_digit)).then_some(end)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn same(a: &str, b: &str) -> bool {
        MessageShape::of(a) == MessageShape::of(b)
    }

    #[test]
    fn messages_that_differ_only_by_a_date_or_a_time_share_a_shape() {
        assert!(same(
            "hourly sync 2026-11-16T09:00:00Z",
            "hourly sync 2026-11-16T08:00:00Z"
        ));
        assert!(same(
            "run at 2026-11-16T09:00:00.125Z done",
            "run at 2026-11-15T23:59:01.5Z done"
        ));
        assert!(same("report for 2026-11-16", "report for 2026-11-15"));
        assert!(same("tick 09:00", "tick 21:30"));
        assert!(same("tick 09:00:00", "tick 21:30:59"));
        assert!(same("  same words  ", "same words"));
    }

    #[test]
    fn other_numbers_keep_messages_apart() {
        assert!(!same("item-4198 · gate failed", "item-4182 · gate failed"));
        assert!(!same("3 left of A-12", "5 left of A-12"));
        assert!(!same("PR 231 merged", "PR 232 merged"));
        assert!(!same(
            "hourly sync 2026-11-16T09:00:00Z",
            "daily sync 2026-11-16T09:00:00Z"
        ));
    }

    #[test]
    fn a_number_that_only_looks_like_a_moment_stays() {
        assert!(
            !same("build 12026-11-16", "build 12026-11-15"),
            "five digits are no year"
        );
        assert!(!same("v 2026-11-160", "v 2026-11-161"), "a third digit is no day");
        assert!(!same("ratio 123:45", "ratio 124:45"), "three digits are no hour");
        assert_eq!(MessageShape::of("at 09:00Zulu"), MessageShape::of("at 10:15Zulu"));
    }

    #[test]
    fn keeps_the_words_around_a_moment() {
        assert_eq!(
            MessageShape::of("sync 2026-11-16T09:00Z for qa"),
            MessageShape("sync ⧖ for qa".to_string())
        );
        assert_eq!(MessageShape::of(""), MessageShape(String::new()));
        assert_eq!(MessageShape::of("é 2026-11-16"), MessageShape("é ⧖".to_string()));
    }
}
