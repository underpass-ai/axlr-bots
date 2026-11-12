use std::cmp::Ordering;

/// A release version the panel can compare: `MAJOR.MINOR.PATCH` with an
/// optional `-prerelease`, as Semantic Versioning orders them, read from
/// what the control plane says (`1.4.0`, `v1.4.0`, `2.0.0-rc.1`; build
/// metadata after `+` is left out). A text it cannot place is no version:
/// empty, `(devel)`, a Go pseudo-version (`0.0.0-20261112223000-4924ad731450`)
/// or a build with local changes (`+dirty`) never ranks, so the panel draws
/// no mark from it.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Version {
    major: u64,
    minor: u64,
    patch: u64,
    prerelease: Vec<String>,
    /// As written, without a leading `v`.
    text: String,
}

impl Version {
    /// The version the text names, or none when it cannot be compared.
    pub fn parse(raw: &str) -> Option<Self> {
        let raw = raw.trim();
        let text = raw.strip_prefix('v').unwrap_or(raw);
        let (core, build) = match text.split_once('+') {
            Some((core, build)) => (core, Some(build)),
            None => (text, None),
        };
        if build.is_some_and(|b| b.is_empty() || b.split('.').any(|part| part == "dirty" || !identifier(part))) {
            return None;
        }
        let (numbers, prerelease) = match core.split_once('-') {
            Some((numbers, pre)) => (numbers, Some(pre)),
            None => (core, None),
        };
        let mut parts = numbers.split('.');
        let (major, minor, patch) = (number(parts.next()?)?, number(parts.next()?)?, number(parts.next()?)?);
        if parts.next().is_some() {
            return None;
        }
        let prerelease: Vec<String> = match prerelease {
            Some(pre) => {
                let ids: Vec<&str> = pre.split('.').collect();
                if ids
                    .iter()
                    .any(|id| !identifier(id) || (numeric(id) && id.len() > 1 && id.starts_with('0')))
                {
                    return None;
                }
                if pseudo(pre) {
                    return None;
                }
                ids.into_iter().map(str::to_string).collect()
            }
            None => Vec::new(),
        };
        Some(Self {
            major,
            minor,
            patch,
            prerelease,
            text: text.to_string(),
        })
    }

    /// The version in a line that names a program before it, as
    /// `axlr-tui --version` prints (`axlr-tui 0.14.0`, `nanobot-0.1.0`):
    /// the first word, or the part of a word after a hyphen, that is one.
    pub fn find_in(line: &str) -> Option<Self> {
        line.split_whitespace().find_map(|word| {
            Self::parse(word).or_else(|| {
                word.match_indices('-')
                    .map(|(at, _)| &word[at + 1..])
                    .find(|rest| rest.starts_with(|c: char| c.is_ascii_digit()) || rest.starts_with('v'))
                    .and_then(Self::parse)
            })
        })
    }

    /// The version as the panel writes it: `v1.4.0`.
    pub fn label(&self) -> String {
        format!("v{}", self.text)
    }
}

impl PartialOrd for Version {
    fn partial_cmp(&self, other: &Self) -> Option<Ordering> {
        Some(self.cmp(other))
    }
}

impl Ord for Version {
    /// Semantic Versioning's precedence: the numbers, then a release over
    /// any of its prereleases, then the prerelease identifiers one by one.
    fn cmp(&self, other: &Self) -> Ordering {
        (self.major, self.minor, self.patch)
            .cmp(&(other.major, other.minor, other.patch))
            .then_with(|| match (self.prerelease.is_empty(), other.prerelease.is_empty()) {
                (true, true) => Ordering::Equal,
                (true, false) => Ordering::Greater,
                (false, true) => Ordering::Less,
                (false, false) => prerelease(&self.prerelease, &other.prerelease),
            })
    }
}

fn prerelease(a: &[String], b: &[String]) -> Ordering {
    for (x, y) in a.iter().zip(b) {
        let order = match (x.parse::<u64>(), y.parse::<u64>()) {
            (Ok(x), Ok(y)) => x.cmp(&y),
            (Ok(_), Err(_)) => Ordering::Less,
            (Err(_), Ok(_)) => Ordering::Greater,
            (Err(_), Err(_)) => x.cmp(y),
        };
        if order != Ordering::Equal {
            return order;
        }
    }
    a.len().cmp(&b.len())
}

/// A number with no leading zero.
fn number(text: &str) -> Option<u64> {
    if text.is_empty() || !numeric(text) || (text.len() > 1 && text.starts_with('0')) {
        return None;
    }
    text.parse().ok()
}

fn numeric(text: &str) -> bool {
    !text.is_empty() && text.bytes().all(|b| b.is_ascii_digit())
}

/// A prerelease or build identifier: letters, digits and hyphens.
fn identifier(text: &str) -> bool {
    !text.is_empty() && text.bytes().all(|b| b.is_ascii_alphanumeric() || b == b'-')
}

/// A Go pseudo-version's prerelease: it ends in a 14-digit time and a
/// 12-hex commit (`20261112223000-4924ad731450`, after `0.` or `pre.0.`
/// in its other forms).
fn pseudo(prerelease: &str) -> bool {
    let last = prerelease.rsplit('.').next().unwrap_or(prerelease);
    match last.split_once('-') {
        Some((time, commit)) => {
            time.len() == 14
                && numeric(time)
                && commit.len() == 12
                && commit.bytes().all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
        }
        None => false,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn order(a: &str, b: &str) -> Option<Ordering> {
        Some(Version::parse(a)?.cmp(&Version::parse(b)?))
    }

    #[test]
    fn compares_as_semantic_versioning_orders() {
        use Ordering::{Equal, Greater, Less};
        let table = [
            ("1.2.0", "1.3.0", Some(Less)),
            ("1.10.0", "1.9.0", Some(Greater)),
            ("2.0.0", "1.99.99", Some(Greater)),
            ("v1.2.0", "1.2.0", Some(Equal)),
            ("v0.4.0", "0.3.9", Some(Greater)),
            ("1.2.0-rc.1", "1.2.0", Some(Less)),
            ("1.2.0-alpha", "1.2.0-alpha.1", Some(Less)),
            ("1.2.0-alpha.1", "1.2.0-alpha.beta", Some(Less)),
            ("1.2.0-beta.2", "1.2.0-beta.11", Some(Less)),
            ("1.2.0-rc.1", "1.2.0-beta.11", Some(Greater)),
            ("1.2.0+build.7", "1.2.0", Some(Equal)),
            ("0.14.0-dev", "0.14.0", Some(Less)),
            // What the panel cannot place: no mark either way.
            ("", "1.0.0", None),
            ("(devel)", "1.0.0", None),
            ("dev", "1.0.0", None),
            ("0.0.0-20261112223000-4924ad731450", "0.4.0", None),
            ("v0.4.1-0.20261112223000-4924ad731450", "0.4.0", None),
            ("v0.4.0-rc.1.0.20261112223000-4924ad731450", "0.4.0", None),
            ("1.2.3+dirty", "1.2.3", None),
            ("1.2", "1.2.0", None),
            ("1.2.3.4", "1.2.3", None),
            ("01.2.3", "1.2.3", None),
            ("1.2.3-01", "1.2.3", None),
            ("1.2.3-", "1.2.3", None),
            ("1.2.3+", "1.2.3", None),
            ("1.2.3-a..b", "1.2.3", None),
        ];
        for (a, b, want) in table {
            assert_eq!(order(a, b), want, "{a} against {b}");
        }
    }

    #[test]
    fn finds_the_version_in_a_version_line() {
        let found = |line: &str| Version::find_in(line).map(|v| v.label());
        assert_eq!(found("axlr-tui 0.14.0-dev").as_deref(), Some("v0.14.0-dev"));
        assert_eq!(found("axlr-tui 0.20.1").as_deref(), Some("v0.20.1"));
        assert_eq!(found("nanobot-0.1.0").as_deref(), Some("v0.1.0"));
        assert_eq!(found("axlr v0.14.0").as_deref(), Some("v0.14.0"));
        assert_eq!(found("0.14.0").as_deref(), Some("v0.14.0"));
        assert_eq!(found("dev"), None);
        assert_eq!(found(""), None);
        assert_eq!(found("axlr-tui"), None);
    }

    #[test]
    fn writes_itself_with_a_v() {
        assert_eq!(Version::parse("1.3.0").unwrap().label(), "v1.3.0");
        assert_eq!(Version::parse(" v1.3.0 ").unwrap().label(), "v1.3.0");
        assert!(Version::parse("1.3.0") > Version::parse("1.2.9"));
    }
}
