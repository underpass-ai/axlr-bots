use super::{Folded, Marks, Query, Score, TextMatch};

/// The shortest word looked for letter by letter: shorter ones find too
/// much.
const FUZZY_FROM: usize = 3;

/// How a query looks for its words in a text. Every word must be found.
/// A list's filter is strict: each word as written, whatever its case.
/// A search that ranks is loose: a word of three letters or more that is
/// not written anywhere is also found letter by letter, when its letters
/// come in order within less than twice its length (`dvj` finds
/// `dev-jobs`).
#[derive(Debug, Clone, Copy)]
pub struct Matcher<'a> {
    query: &'a Query,
    fuzzy: bool,
}

impl<'a> Matcher<'a> {
    /// Each word as written.
    pub fn strict(query: &'a Query) -> Self {
        Self { query, fuzzy: false }
    }

    /// Each word as written or, failing that, letter by letter.
    pub fn loose(query: &'a Query) -> Self {
        Self { query, fuzzy: true }
    }

    pub fn matches(&self, text: &str) -> bool {
        self.find(text).is_some()
    }

    /// The text when every word is found in it: its score and the first
    /// place of each word to highlight. An empty query finds any text.
    pub fn find(&self, text: &str) -> Option<TextMatch> {
        if self.query.is_empty() {
            return Some(TextMatch::any());
        }
        let folded = Folded::of(text);
        let mut exact = true;
        let mut spread = 0;
        let mut start = usize::MAX;
        let mut ranges = Vec::new();
        for word in self.query.needles() {
            if let Some(at) = folded.find(word, 0) {
                spread += word.len();
                start = start.min(at);
                ranges.push(folded.bytes(at, at + word.len()));
            } else if self.fuzzy && word.len() >= FUZZY_FROM {
                let letters = tightest(&folded, word)?;
                let (first, last) = (*letters.first()?, *letters.last()?);
                exact = false;
                spread += last - first + 1;
                start = start.min(first);
                ranges.extend(letters.into_iter().map(|at| folded.bytes(at, at + 1)));
            } else {
                return None;
            }
        }
        Some(TextMatch {
            score: Score::of(exact, spread, start, folded.len()),
            marks: Marks::of(ranges),
        })
    }

    /// Every place a word of the query is written in the text, to
    /// highlight all of them. Letter-by-letter finds are not marked here.
    pub fn marks(&self, text: &str) -> Marks {
        let folded = Folded::of(text);
        let mut ranges = Vec::new();
        for word in self.query.needles() {
            let mut from = 0;
            while let Some(at) = folded.find(word, from) {
                ranges.push(folded.bytes(at, at + word.len()));
                from = at + word.len();
            }
        }
        Marks::of(ranges)
    }
}

/// Where the word's letters come in order in the tightest stretch of the
/// text, the earliest when two are as tight; none when no stretch is
/// shorter than twice the word.
fn tightest(text: &Folded, word: &[char]) -> Option<Vec<usize>> {
    let reach = word.len() * 2;
    let mut best: Option<Vec<usize>> = None;
    for start in 0..text.len() {
        if text.at(start) != word.first().copied() {
            continue;
        }
        let mut letters = vec![start];
        let mut at = start + 1;
        for letter in &word[1..] {
            while at < text.len() && at - start < reach && text.at(at) != Some(*letter) {
                at += 1;
            }
            if at >= text.len() || at - start >= reach {
                break;
            }
            letters.push(at);
            at += 1;
        }
        let tighter = best
            .as_ref()
            .is_none_or(|b| letters[letters.len() - 1] - start < b[b.len() - 1] - b[0]);
        if letters.len() == word.len() && tighter {
            best = Some(letters);
        }
    }
    best
}

#[cfg(test)]
mod tests {
    use super::*;

    fn marked(found: &TextMatch, text: &str) -> Vec<String> {
        found
            .marks
            .pieces(text)
            .into_iter()
            .filter(|(_, on)| *on)
            .map(|(piece, _)| piece.to_string())
            .collect()
    }

    #[test]
    fn strict_wants_every_word_as_written() {
        let query = Query::new("  Deploy  WEB ");
        let strict = Matcher::strict(&query);
        let text = "The web team asked to deploy it";
        let found = strict.find(text).unwrap();
        assert_eq!(marked(&found, text), vec!["web", "deploy"]);
        assert!(found.score.is_exact());
        assert!(strict.matches("deploy-web-1"));
        assert!(!strict.matches("deploy the api"));
        assert!(!strict.matches("dply web"), "a list's filter never guesses letters");
        assert!(!strict.matches(""));
    }

    #[test]
    fn an_empty_query_finds_any_text() {
        let query = Query::new("   ");
        assert_eq!(Matcher::strict(&query).find("anything"), Some(TextMatch::any()));
        assert_eq!(Matcher::loose(&query).find(""), Some(TextMatch::any()));
        assert!(Matcher::strict(&query).marks("anything").is_empty());
    }

    #[test]
    fn loose_finds_a_word_letter_by_letter_when_it_is_tight() {
        let query = Query::new("dvj");
        let loose = Matcher::loose(&query);
        let text = "dev-jobs";
        let found = loose.find(text).unwrap();
        assert!(!found.score.is_exact());
        assert_eq!(marked(&found, text), vec!["d", "v", "j"]);
        assert!(!loose.matches("d-----v-----j"), "the letters are too far apart");
        assert!(!loose.matches("jvd"), "the letters come in order");
        let short = Query::new("dj");
        assert!(
            !Matcher::loose(&short).matches("dev-jobs"),
            "two letters are too few to guess"
        );
        assert!(Matcher::loose(&short).matches("adj"));
    }

    #[test]
    fn loose_takes_the_tightest_stretch_and_the_earliest_of_two() {
        let query = Query::new("abc");
        let loose = Matcher::loose(&query);
        let text = "a-b-c then a-bc";
        let found = loose.find(text).unwrap();
        assert_eq!(found.marks.ranges(), &[11..12, 13..15]);
        let twice = "a.bc a.bc";
        assert_eq!(loose.find(twice).unwrap().marks.ranges(), &[0..1, 2..4]);
        assert!(!loose.matches("ab"), "the text ends before the word does");
        assert!(!loose.matches("a b"));
    }

    #[test]
    fn ranks_as_written_first_then_tighter_then_earlier() {
        let query = Query::new("node");
        let loose = Matcher::loose(&query);
        let score = |text: &str| loose.find(text).unwrap().score;
        assert!(score("the node") > score("n-o-d-e"));
        assert!(score("node one") > score("the node"));
        assert!(score("no-de") > score("n-o-d-e"));
        assert!(score("node") > score("node one"), "the shorter text is the closer one");
    }

    #[test]
    fn mixes_words_as_written_and_letter_by_letter() {
        let query = Query::new("web dvj");
        let text = "dev-jobs on web";
        let found = Matcher::loose(&query).find(text).unwrap();
        assert!(!found.score.is_exact());
        assert_eq!(marked(&found, text), vec!["d", "v", "j", "web"]);
        assert!(!Matcher::loose(&query).matches("dev-jobs"), "every word must be found");
    }

    #[test]
    fn marks_every_place_a_word_is_written() {
        let query = Query::new("an");
        let text = "Banana AND an ant";
        let marks = Matcher::strict(&query).marks(text);
        assert_eq!(marks.ranges(), &[1..5, 7..9, 11..13, 14..16]);
        assert!(Matcher::strict(&query).marks("nothing here").is_empty());
    }

    #[test]
    fn highlights_stay_on_whole_characters_of_the_written_text() {
        for (typed, text) in [
            ("i", "İstanbul İ"),
            ("stra", "STRASSE straße 🚀"),
            ("🚀", "go 🚀 now 🚀"),
            ("ñu", "El ÑU y el ñu"),
            ("ist", "İ-s-t"),
        ] {
            let query = Query::new(typed);
            let found = Matcher::loose(&query).find(text).unwrap();
            let all = Matcher::strict(&query).marks(text);
            for range in found.marks.ranges().iter().chain(all.ranges()) {
                assert!(text.get(range.clone()).is_some(), "{typed} in {text}: {range:?}");
            }
            assert!(!found.marks.is_empty());
        }
    }
}
