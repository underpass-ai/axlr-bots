use std::path::PathBuf;

use crate::application::{FilePicker, PanelError, PortFuture};

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

    fn pick_settings(&self) -> PortFuture<'_, Option<PathBuf>> {
        Box::pin(async move {
            let picked = rfd::AsyncFileDialog::new()
                .set_title("Panel settings to import")
                .add_filter("JSON", &["json"])
                .pick_file()
                .await;
            Ok(picked.map(|handle| handle.path().to_path_buf()))
        })
    }

    fn save_as(&self, name: String, bytes: Vec<u8>) -> PortFuture<'_, Option<PathBuf>> {
        Box::pin(async move {
            let Some(handle) = rfd::AsyncFileDialog::new()
                .set_title("Save as")
                .set_file_name(&name)
                .save_file()
                .await
            else {
                return Ok(None);
            };
            let path = handle.path().to_path_buf();
            std::fs::write(&path, &bytes)
                .map_err(|e| PanelError::File(format!("cannot save to {}: {e}", path.display())))?;
            Ok(Some(path))
        })
    }
}
