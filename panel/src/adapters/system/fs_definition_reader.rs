use std::path::PathBuf;

use crate::application::{DefinitionReader, PanelError, PortFuture};

/// Reads a definition file from disk, at most 1 MiB, as the control plane
/// bounds a body.
#[derive(Debug, Default, Clone, Copy)]
pub struct FsDefinitionReader;

const MAX_DEFINITION_BYTES: u64 = 1 << 20;

impl DefinitionReader for FsDefinitionReader {
    fn read(&self, path: PathBuf) -> PortFuture<'_, String> {
        Box::pin(async move {
            let shown = path.display().to_string();
            let metadata = std::fs::metadata(&path).map_err(|e| PanelError::File(format!("{shown}: {e}")))?;
            if metadata.len() > MAX_DEFINITION_BYTES {
                return Err(PanelError::File(format!("{shown}: larger than 1 MiB")));
            }
            std::fs::read_to_string(&path).map_err(|e| PanelError::File(format!("{shown}: {e}")))
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn reads_small_files_and_refuses_the_rest() {
        let dir = tempfile::tempdir().unwrap();
        let file = dir.path().join("bot.json");
        std::fs::write(&file, "{}").unwrap();
        let run = futures::executor::block_on;
        assert_eq!(run(FsDefinitionReader.read(file)).unwrap(), "{}");
        let big = dir.path().join("big.json");
        std::fs::write(&big, vec![b' '; (MAX_DEFINITION_BYTES + 1) as usize]).unwrap();
        assert!(matches!(run(FsDefinitionReader.read(big)), Err(PanelError::File(m)) if m.contains("1 MiB")));
        assert!(matches!(
            run(FsDefinitionReader.read(dir.path().join("none"))),
            Err(PanelError::File(_))
        ));
    }
}
