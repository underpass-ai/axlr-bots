use std::collections::BTreeMap;
use std::path::PathBuf;
use std::sync::Mutex;

use crate::application::{DefinitionReader, FilePicker, PanelError, PortFuture};

/// Files in memory, the one the operator "picks", and the downloads
/// they save, under `/saved/<name>` unless they cancel.
#[derive(Default)]
pub struct MemoryFiles {
    files: Mutex<BTreeMap<PathBuf, String>>,
    picked: Mutex<Option<PathBuf>>,
    saved: Mutex<BTreeMap<PathBuf, Vec<u8>>>,
    cancel_saves: Mutex<bool>,
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

    /// Adds a file, or replaces its text.
    pub fn put(&self, path: &str, text: &str) {
        self.files.lock().unwrap().insert(PathBuf::from(path), text.to_string());
    }

    pub fn pick(&self, path: Option<&str>) {
        *self.picked.lock().unwrap() = path.map(PathBuf::from);
    }

    /// From now on the operator cancels where to save.
    pub fn cancel_saves(&self) {
        *self.cancel_saves.lock().unwrap() = true;
    }

    /// What was saved so far, by path.
    pub fn saved(&self) -> BTreeMap<PathBuf, Vec<u8>> {
        self.saved.lock().unwrap().clone()
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

    fn pick_settings(&self) -> PortFuture<'_, Option<PathBuf>> {
        self.pick_definition()
    }

    fn save_as(&self, name: String, bytes: Vec<u8>) -> PortFuture<'_, Option<PathBuf>> {
        let path = (!*self.cancel_saves.lock().unwrap()).then(|| PathBuf::from("/saved").join(name));
        if let Some(path) = &path {
            self.saved.lock().unwrap().insert(path.clone(), bytes);
        }
        Box::pin(async move { Ok(path) })
    }
}
