use std::collections::BTreeMap;
use std::path::PathBuf;
use std::sync::Mutex;

use crate::application::{DefinitionReader, FilePicker, PanelError, PortFuture};

/// Files in memory, and the one the operator "picks".
#[derive(Default)]
pub struct MemoryFiles {
    files: Mutex<BTreeMap<PathBuf, String>>,
    picked: Mutex<Option<PathBuf>>,
}

impl MemoryFiles {
    pub fn with(path: &str, text: &str) -> Self {
        let files = Self::default();
        files
            .files
            .lock()
            .unwrap()
            .insert(PathBuf::from(path), text.to_string());
        files
    }

    pub fn pick(&self, path: Option<&str>) {
        *self.picked.lock().unwrap() = path.map(PathBuf::from);
    }
}

impl DefinitionReader for MemoryFiles {
    fn read(&self, path: PathBuf) -> PortFuture<'_, String> {
        let result = self
            .files
            .lock()
            .unwrap()
            .get(&path)
            .cloned()
            .ok_or_else(|| PanelError::File(format!("{}: no such file", path.display())));
        Box::pin(async move { result })
    }
}

impl FilePicker for MemoryFiles {
    fn pick_definition(&self) -> PortFuture<'_, Option<PathBuf>> {
        let picked = self.picked.lock().unwrap().clone();
        Box::pin(async move { Ok(picked) })
    }
}
