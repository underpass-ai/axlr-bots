use super::{StepKind, StepText};

/// Which steps of a task's activity show: all of them, the tool calls,
/// the calls that failed or the events it emitted. The chips over the
/// activity.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub enum StepFilter {
    #[default]
    All,
    Tools,
    Failed,
    Events,
}

impl StepFilter {
    /// Every chip, in the order shown.
    pub const ALL: [StepFilter; 4] = [Self::All, Self::Tools, Self::Failed, Self::Events];

    /// The chip's word.
    pub fn label(self) -> &'static str {
        match self {
            Self::All => "All",
            Self::Tools => "Tools",
            Self::Failed => "Failed",
            Self::Events => "Events",
        }
    }

    /// Whether the step shows under this chip.
    pub fn shows(self, step: &StepText) -> bool {
        match self {
            Self::All => true,
            Self::Tools => step.kind == StepKind::Tool,
            Self::Failed => step.failed,
            Self::Events => step.kind == StepKind::Event,
        }
    }

    /// How many of the steps show under this chip: its number.
    pub fn count(self, steps: &[StepText]) -> usize {
        steps.iter().filter(|step| self.shows(step)).count()
    }

    /// The chips to draw, each with its number: All always, the others
    /// only when a step shows under them.
    pub fn chips(steps: &[StepText]) -> Vec<(StepFilter, usize)> {
        Self::ALL
            .into_iter()
            .map(|chip| (chip, chip.count(steps)))
            .filter(|(chip, count)| *chip == Self::All || *count > 0)
            .collect()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn steps() -> Vec<StepText> {
        vec![
            StepText::tool("local_list", "", ".", false),
            StepText::tool("local_read", "", "missing.txt", true),
            StepText::note("The file is not there."),
            StepText::event("report.ready", "nothing to report"),
            StepText::tool("local_read", "", "a.txt", false),
        ]
    }

    #[test]
    fn each_chip_shows_its_steps_and_counts_them() {
        let steps = steps();
        let counts: Vec<usize> = StepFilter::ALL.into_iter().map(|c| c.count(&steps)).collect();
        assert_eq!(counts, vec![5, 3, 1, 1]);
        assert!(StepFilter::Failed.shows(&steps[1]) && !StepFilter::Failed.shows(&steps[0]));
        assert!(StepFilter::Events.shows(&steps[3]) && !StepFilter::Tools.shows(&steps[3]));
        assert!(StepFilter::All.shows(&steps[2]) && !StepFilter::Tools.shows(&steps[2]));
        assert_eq!(StepFilter::default(), StepFilter::All);
        let labels: Vec<&str> = StepFilter::ALL.into_iter().map(StepFilter::label).collect();
        assert_eq!(labels, vec!["All", "Tools", "Failed", "Events"]);
    }

    #[test]
    fn draws_only_the_chips_that_show_something_and_all() {
        assert_eq!(
            StepFilter::chips(&steps()),
            vec![
                (StepFilter::All, 5),
                (StepFilter::Tools, 3),
                (StepFilter::Failed, 1),
                (StepFilter::Events, 1)
            ]
        );
        let said = [StepText::note("Done.")];
        assert_eq!(StepFilter::chips(&said), vec![(StepFilter::All, 1)]);
        assert_eq!(StepFilter::chips(&[]), vec![(StepFilter::All, 0)]);
    }
}
