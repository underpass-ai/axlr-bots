use serde::{Deserialize, Serialize};

/// Which trigger created a task; empty means by hand.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct TriggerRef {
    #[serde(default, rename = "type", skip_serializing_if = "String::is_empty")]
    pub trigger_type: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub name: String,
}

impl TriggerRef {
    /// `manual`, `webhook deploy`, `timer clock`.
    pub fn describe(&self) -> String {
        let kind = if self.trigger_type.is_empty() {
            "manual"
        } else {
            &self.trigger_type
        };
        if self.name.is_empty() || self.name == kind {
            kind.to_string()
        } else {
            format!("{kind} {}", self.name)
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn describes_the_origin() {
        assert_eq!(TriggerRef::default().describe(), "manual");
        let manual = TriggerRef {
            trigger_type: "manual".into(),
            name: "manual".into(),
        };
        assert_eq!(manual.describe(), "manual");
        let timer = TriggerRef {
            trigger_type: "timer".into(),
            name: "clock".into(),
        };
        assert_eq!(timer.describe(), "timer clock");
    }
}
