//! A runtime for tests: runs the effects through the services at once
//! (the fakes answer immediately) and feeds the messages back.

use super::view::tests::app;
use super::{App, Effect, Message};

/// Runs the effects and every effect their messages ask for; returns what
/// went to the clipboard.
pub(crate) fn drive(app: &mut App, effects: Vec<Effect>) -> Vec<String> {
    let mut copied = Vec::new();
    let mut queue = effects;
    let mut rounds = 0;
    while !queue.is_empty() {
        rounds += 1;
        assert!(rounds < 50, "effects never settle");
        let mut next = Vec::new();
        for effect in queue {
            if let Effect::Copy(text) = &effect {
                copied.push(text.clone());
            }
            if let Some(message) = futures::executor::block_on(app.services.clone().execute(effect)) {
                next.extend(app.apply(message));
            }
        }
        queue = next;
    }
    copied
}

/// Applies a message and runs what follows.
pub(crate) fn send(app: &mut App, message: Message) -> Vec<String> {
    let effects = app.apply(message);
    drive(app, effects)
}

/// The panel connected to the sample fleet, on Home, the fleet loaded.
pub(crate) fn connected() -> App {
    let mut app = app();
    send(&mut app, Message::SettingsSave);
    assert!(app.connected(), "the sample settings connect");
    app
}
