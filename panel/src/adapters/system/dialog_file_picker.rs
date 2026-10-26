use std::path::PathBuf;

use crate::application::{FilePicker, PortFuture};

/// The desktop's own file dialog, through the XDG portal.
#[derive(Debug, Default, Clone, Copy)]
pub struct DialogFilePicker;

impl FilePicker for DialogFilePicker {
    fn pick_definition(&self) -> PortFuture<'_, Option<PathBuf>> {
        Box::pin(async move {
            let picked = rfd::AsyncFileDialog::new()
                .set_title("Bot definition")
                .add_filter("JSON", &["json"])
                .pick_file()
                .await;
            Ok(picked.map(|handle| handle.path().to_path_buf()))
        })
    }
}
