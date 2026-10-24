use serde::{Deserialize, Serialize};

/// The limits of one task of a bot. Zero means the control plane's default.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Budgets {
    #[serde(default)]
    pub max_model_calls_per_task: u32,
    #[serde(default)]
    pub max_tool_calls_per_task: u32,
    #[serde(default)]
    pub max_delegation_depth: u32,
    #[serde(default)]
    pub task_timeout_seconds: u32,
}

impl Budgets {
    /// The timeout as minutes and seconds, `5m`, `1m30s`, `45s`.
    pub fn timeout_label(&self) -> String {
        let (minutes, seconds) = (self.task_timeout_seconds / 60, self.task_timeout_seconds % 60);
        match (minutes, seconds) {
            (0, s) => format!("{s}s"),
            (m, 0) => format!("{m}m"),
            (m, s) => format!("{m}m{s}s"),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn labels_the_timeout() {
        let mut budgets = Budgets {
            task_timeout_seconds: 300,
            ..Default::default()
        };
        assert_eq!(budgets.timeout_label(), "5m");
        budgets.task_timeout_seconds = 90;
        assert_eq!(budgets.timeout_label(), "1m30s");
        budgets.task_timeout_seconds = 45;
        assert_eq!(budgets.timeout_label(), "45s");
    }
}
