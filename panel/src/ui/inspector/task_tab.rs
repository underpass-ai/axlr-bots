/// A tab of a task in the inspector: what it did step by step, its
/// answer, the files it delivered, its runs, its timeline and the task
/// as the control plane keeps it.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub enum TaskTab {
    #[default]
    Activity,
    Answer,
    Files,
    Runs,
    Timeline,
    Raw,
}

impl TaskTab {
    /// Every tab, in the order shown.
    pub const ALL: [Self; 6] = [
        Self::Activity,
        Self::Answer,
        Self::Files,
        Self::Runs,
        Self::Timeline,
        Self::Raw,
    ];

    /// The tab's word.
    pub fn label(self) -> &'static str {
        match self {
            Self::Activity => "Activity",
            Self::Answer => "Answer",
            Self::Files => "Files",
            Self::Runs => "Runs",
            Self::Timeline => "Timeline",
            Self::Raw => "Raw",
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn opens_on_the_activity_and_names_each_tab() {
        assert_eq!(TaskTab::default(), TaskTab::Activity);
        let labels: Vec<&str> = TaskTab::ALL.into_iter().map(TaskTab::label).collect();
        assert_eq!(labels, ["Activity", "Answer", "Files", "Runs", "Timeline", "Raw"]);
    }
}
