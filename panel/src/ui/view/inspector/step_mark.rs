/// How a line of a task's activity stands to the find and to the
/// person: a match, the match the find is at, open.
#[derive(Debug, Clone, Copy)]
pub struct StepMark {
    pub hit: bool,
    pub current: bool,
    pub open: bool,
}
