use std::ops::Range;

/// A text with its case folded, each folded character knowing the bytes
/// of the character it came from: `İ` folds to two characters, and a
/// match on either marks the whole `İ` in the text as it was written.
#[derive(Debug, Clone, PartialEq, Eq)]
pub(super) struct Folded {
    chars: Vec<char>,
    /// Where each folded character's source starts and ends, in bytes.
    sources: Vec<(usize, usize)>,
}

impl Folded {
    pub fn of(text: &str) -> Self {
        let mut chars = Vec::with_capacity(text.len());
        let mut sources = Vec::with_capacity(text.len());
        for (at, c) in text.char_indices() {
            for lower in c.to_lowercase() {
                chars.push(lower);
                sources.push((at, at + c.len_utf8()));
            }
        }
        Self { chars, sources }
    }

    /// The characters a search for `text` looks for: folded the same way.
    pub fn needle(text: &str) -> Vec<char> {
        text.chars().flat_map(char::to_lowercase).collect()
    }

    pub fn len(&self) -> usize {
        self.chars.len()
    }

    pub fn at(&self, index: usize) -> Option<char> {
        self.chars.get(index).copied()
    }

    /// Where `needle` first starts at or after `from`, in folded
    /// characters. An empty needle is found nowhere.
    pub fn find(&self, needle: &[char], from: usize) -> Option<usize> {
        if needle.is_empty() || needle.len() > self.chars.len() {
            return None;
        }
        (from..=self.chars.len() - needle.len()).find(|&start| self.chars[start..start + needle.len()] == *needle)
    }

    /// The bytes of the written text the folded characters `start..end`
    /// came from: always whole characters.
    pub fn bytes(&self, start: usize, end: usize) -> Range<usize> {
        match (
            self.sources.get(start),
            end.checked_sub(1).and_then(|last| self.sources.get(last)),
        ) {
            (Some(first), Some(last)) if start < end => first.0..last.1,
            _ => 0..0,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn finds_without_case_and_points_at_the_written_text() {
        let text = "Deploy ÁRBOL now";
        let folded = Folded::of(text);
        let at = folded.find(&Folded::needle("árbol"), 0).unwrap();
        assert_eq!(&text[folded.bytes(at, at + 5)], "ÁRBOL");
        assert_eq!(folded.find(&Folded::needle("DEPLOY"), 0), Some(0));
        assert_eq!(folded.find(&Folded::needle("deploy"), 1), None);
        assert_eq!(folded.find(&Folded::needle("absent"), 0), None);
        assert_eq!(folded.find(&[], 0), None);
        assert_eq!(folded.find(&Folded::needle("longer than the whole text"), 0), None);
        assert_eq!(folded.len(), text.chars().count());
        assert_eq!(folded.at(0), Some('d'));
        assert_eq!(folded.at(99), None);
    }

    #[test]
    fn a_letter_that_folds_to_two_marks_the_whole_letter() {
        let text = "İstanbul 🚀 go";
        let folded = Folded::of(text);
        assert_eq!(folded.len(), text.chars().count() + 1, "İ folds to i and a dot above");
        let at = folded.find(&Folded::needle("i"), 0).unwrap();
        assert_eq!(&text[folded.bytes(at, at + 1)], "İ");
        let rocket = folded.find(&Folded::needle("🚀"), 0).unwrap();
        assert_eq!(&text[folded.bytes(rocket, rocket + 1)], "🚀");
        let go = folded.find(&Folded::needle("GO"), 0).unwrap();
        assert_eq!(&text[folded.bytes(go, go + 2)], "go");
    }

    #[test]
    fn no_characters_are_no_bytes() {
        let folded = Folded::of("abc");
        assert_eq!(folded.bytes(1, 1), 0..0);
        assert_eq!(folded.bytes(2, 1), 0..0);
        assert_eq!(folded.bytes(0, 9), 0..0);
        assert_eq!(Folded::of("").len(), 0);
    }
}
