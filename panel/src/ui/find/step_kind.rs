/// What a step of a task's activity is, for the chips over it: a tool
/// call, an event the task emitted, or words (what it said, what a
/// person wrote to it, a bot it asked, a call that waits).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum StepKind {
    Tool,
    Event,
    Note,
}
