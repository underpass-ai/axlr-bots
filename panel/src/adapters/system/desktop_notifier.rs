use std::thread;
use std::time::Duration;

use futures::channel::oneshot;
use notify_rust::{Notification, Timeout};

use crate::application::{Notice, Notifier, PortFuture};

/// How long a notice stays before it goes away on its own.
const SHOWN_FOR: Duration = Duration::from_secs(30);

/// The desktop's notices, through the session bus. A click on the notice
/// counts; a desktop without notices, or one that closes it, does not.
pub struct DesktopNotifier;

impl Notifier for DesktopNotifier {
    fn show(&self, notice: Notice) -> PortFuture<'_, bool> {
        let (sender, receiver) = oneshot::channel();
        // Waiting for the click blocks: it waits on its own thread.
        thread::spawn(move || {
            let shown = Notification::new()
                .appname("AXLR Bots")
                .summary(&notice.title)
                .body(&notice.body)
                .icon("axlr-bots-panel")
                .action("default", "Open")
                .timeout(Timeout::Milliseconds(SHOWN_FOR.as_millis() as u32))
                .show();
            let mut clicked = false;
            if let Ok(handle) = shown {
                handle.wait_for_action(|action| clicked = action == "default");
            }
            let _ = sender.send(clicked);
        });
        Box::pin(async move { Ok(receiver.await.unwrap_or(false)) })
    }
}
