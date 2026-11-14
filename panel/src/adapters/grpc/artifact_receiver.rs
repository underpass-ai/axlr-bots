use sha2::{Digest, Sha256};

use super::proto::{self, get_artifact_response::Content};
use crate::application::PanelError;

/// Collects the messages of a `GetArtifact` stream and checks the result
/// against what the control plane said about it: the byte count and the
/// sha256 (lowercase hex), hashed while the chunks arrive.
///
/// Bytes are only handed back when both hold. A mismatch is an error and
/// the bytes are dropped, so they are never saved as good. When the
/// control plane reports no sha256 the download is refused and says so:
/// content that cannot be verified is not passed off as verified.
#[derive(Default)]
pub struct ArtifactReceiver {
    bytes: Vec<u8>,
    hasher: Sha256,
    said: Option<proto::Artifact>,
}

impl ArtifactReceiver {
    pub fn accept(&mut self, content: Option<Content>) {
        match content {
            Some(Content::Artifact(artifact)) => self.said = Some(artifact),
            Some(Content::Chunk(chunk)) => {
                self.hasher.update(&chunk);
                self.bytes.extend_from_slice(&chunk);
            }
            None => {}
        }
    }

    pub fn finish(self) -> Result<Vec<u8>, PanelError> {
        let Some(said) = self.said else {
            return Err(PanelError::Unreadable(
                "the control plane sent the artifact's bytes without saying what it is, so they cannot be verified"
                    .into(),
            ));
        };
        if usize::try_from(said.size).ok() != Some(self.bytes.len()) {
            return Err(PanelError::Unreadable(format!(
                "the artifact came with {} bytes of the {} it says it has",
                self.bytes.len(),
                said.size
            )));
        }
        let expected = said.sha256.trim().to_ascii_lowercase();
        if expected.is_empty() {
            return Err(PanelError::Unreadable(format!(
                "the control plane reports no sha256 for artifact {}, so its content cannot be verified and was not saved",
                said.name
            )));
        }
        let got = format!("{:x}", self.hasher.finalize());
        if got != expected {
            return Err(PanelError::Unreadable(format!(
                "artifact {} is corrupt: its bytes hash to {got} but the control plane kept it as {expected}; it was not saved",
                said.name
            )));
        }
        Ok(self.bytes)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    // sha256("hello world")
    const HELLO: &str = "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9";

    fn receive(size: i64, sha256: &str, chunks: &[&[u8]]) -> Result<Vec<u8>, PanelError> {
        let mut receiver = ArtifactReceiver::default();
        receiver.accept(Some(Content::Artifact(proto::Artifact {
            name: "report.md".into(),
            size,
            sha256: sha256.into(),
            stored: true,
            ..Default::default()
        })));
        for chunk in chunks {
            receiver.accept(Some(Content::Chunk(chunk.to_vec())));
        }
        receiver.accept(None);
        receiver.finish()
    }

    fn reason(result: Result<Vec<u8>, PanelError>) -> String {
        match result {
            Err(PanelError::Unreadable(reason)) => reason,
            other => panic!("expected an unreadable answer, got {other:?}"),
        }
    }

    #[test]
    fn hands_back_bytes_whose_hash_matches_across_chunks() {
        assert_eq!(receive(11, HELLO, &[b"hello", b" world"]).unwrap(), b"hello world");
        assert!(receive(11, &HELLO.to_uppercase(), &[b"hello world"]).is_ok());
    }

    #[test]
    fn refuses_a_hash_mismatch_and_names_both_digests() {
        let said = reason(receive(11, HELLO, &[b"hello worlD"]));
        assert!(
            said.contains("corrupt") && said.contains(HELLO) && said.contains("not saved"),
            "{said}"
        );
    }

    #[test]
    fn refuses_content_the_control_plane_gave_no_hash_for() {
        let said = reason(receive(11, "", &[b"hello world"]));
        assert!(said.contains("no sha256") && said.contains("report.md"), "{said}");
    }

    #[test]
    fn refuses_a_short_download_before_looking_at_the_hash() {
        assert!(reason(receive(11, HELLO, &[b"hello"])).contains("5 bytes of the 11"));
    }
}
