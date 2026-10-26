/// A desktop notice: a title and a line, shown outside the panel's window.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Notice {
    pub title: String,
    pub body: String,
}
