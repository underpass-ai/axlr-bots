use std::sync::Mutex;
use std::sync::atomic::{AtomicBool, Ordering};

use crate::application::{Notice, Notifier, PortFuture};

/// Keeps the notices shown; each counts as clicked when told so.
#[derive(Default)]
pub struct RecordingNotifier {
    shown: Mutex<Vec<Notice>>,
    clicking: AtomicBool,
}

impl RecordingNotifier {
    /// From now on every notice is clicked, or not.
    pub fn click(&self, clicking: bool) {
        self.clicking.store(clicking, Ordering::SeqCst);
    }

    /// The notices shown so far.
    pub fn shown(&self) -> Vec<Notice> {
        self.shown.lock().unwrap().clone()
    }
}

impl Notifier for RecordingNotifier {
    fn show(&self, notice: Notice) -> PortFuture<'_, bool> {
        self.shown.lock().unwrap().push(notice);
        let clicked = self.clicking.load(Ordering::SeqCst);
        Box::pin(async move { Ok(clicked) })
    }
}
