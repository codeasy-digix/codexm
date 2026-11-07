use crate::desktop::Owner;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use std::fs::{self, OpenOptions};
use std::io::{Read, Write};
use std::os::unix::fs::{MetadataExt, OpenOptionsExt, PermissionsExt};
use std::path::{Path, PathBuf};

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Receipt {
    pub schema: u32,
    pub request_id: String,
    pub thread_id: String,
    pub message_sha256: String,
    pub created_at_ms: u128,
    pub state: String,
    pub owner: Option<Owner>,
    pub queue_id: Option<String>,
    pub turn_id: Option<String>,
    pub detail: String,
}

pub fn uuid() -> Result<String, String> {
    let mut bytes = [0; 16];
    getrandom::fill(&mut bytes).map_err(|e| e.to_string())?;
    bytes[6] = (bytes[6] & 15) | 64;
    bytes[8] = (bytes[8] & 63) | 128;
    let hex: String = bytes.iter().map(|b| format!("{b:02x}")).collect();
    Ok(format!(
        "{}-{}-{}-{}-{}",
        &hex[..8],
        &hex[8..12],
        &hex[12..16],
        &hex[16..20],
        &hex[20..]
    ))
}

pub fn valid_uuid(value: &str) -> bool {
    value.len() == 36
        && value.bytes().enumerate().all(|(i, b)| {
            if [8, 13, 18, 23].contains(&i) {
                b == b'-'
            } else {
                b.is_ascii_hexdigit() && !b.is_ascii_uppercase()
            }
        })
}

pub fn message_hash(text: &str) -> String {
    format!("{:x}", Sha256::digest(text.as_bytes()))
}

impl Receipt {
    pub fn new(thread: &str, request: &str, text: &str) -> Self {
        Self {
            schema: 1,
            request_id: request.into(),
            thread_id: thread.into(),
            message_sha256: message_hash(text),
            created_at_ms: std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap_or_default()
                .as_millis(),
            state: "prepared".into(),
            owner: None,
            queue_id: None,
            turn_id: None,
            detail: "No acknowledgement recorded. This request ID will never be resubmitted."
                .into(),
        }
    }
    pub fn outcome(&mut self, state: &str, detail: &str) {
        self.state = state.into();
        self.detail = detail.into();
    }
}

pub struct Store {
    root: PathBuf,
}

impl Store {
    pub fn open(root: PathBuf) -> Result<Self, String> {
        if !root.is_absolute() {
            return Err("receipt directory must be absolute".into());
        }
        fs::create_dir_all(&root).map_err(|e| e.to_string())?;
        let m = fs::symlink_metadata(&root).map_err(|e| e.to_string())?;
        if !m.is_dir() || m.uid() != nix::unistd::Uid::current().as_raw() {
            return Err("receipt directory has unexpected ownership/type".into());
        }
        fs::set_permissions(&root, fs::Permissions::from_mode(0o700)).map_err(|e| e.to_string())?;
        Ok(Self { root })
    }
    fn path(&self, id: &str) -> Result<PathBuf, String> {
        if !valid_uuid(id) {
            return Err("request ID must be a lowercase UUID".into());
        }
        Ok(self.root.join(format!("{id}.json")))
    }
    pub fn read(&self, id: &str) -> Result<Option<Receipt>, String> {
        let path = self.path(id)?;
        let mut file = match OpenOptions::new()
            .read(true)
            .custom_flags(nix::libc::O_NOFOLLOW | nix::libc::O_NONBLOCK)
            .open(path)
        {
            Ok(f) => f,
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(None),
            Err(e) => return Err(format!("cannot read receipt: {e}")),
        };
        let m = file.metadata().map_err(|e| e.to_string())?;
        if !m.is_file()
            || m.uid() != nix::unistd::Uid::current().as_raw()
            || m.mode() & 0o077 != 0
            || m.len() > 16384
        {
            return Err("receipt has unexpected ownership/type/permissions/size".into());
        }
        let mut text = String::new();
        file.read_to_string(&mut text).map_err(|e| e.to_string())?;
        let r: Receipt = serde_json::from_str(&text).map_err(|_| "invalid receipt")?;
        if r.schema != 1
            || r.request_id != id
            || !valid_uuid(&r.thread_id)
            || r.message_sha256.len() != 64
        {
            return Err("receipt identity is invalid".into());
        }
        Ok(Some(r))
    }
    /// Exclusive reservation: even concurrent invocations can enqueue this ID only once.
    pub fn reserve(&self, receipt: &Receipt) -> Result<(), String> {
        Self::write_new(&self.path(&receipt.request_id)?, receipt)
    }
    fn write_new(path: &Path, receipt: &Receipt) -> Result<(), String> {
        let mut file = OpenOptions::new()
            .write(true)
            .create_new(true)
            .mode(0o600)
            .open(path)
            .map_err(|e| format!("cannot reserve receipt (inspect status; do not resend): {e}"))?;
        let bytes = serde_json::to_vec(receipt).map_err(|e| e.to_string())?;
        file.write_all(&bytes)
            .and_then(|()| file.sync_all())
            .map_err(|e| e.to_string())
    }
    pub fn save(&self, receipt: &Receipt) -> Result<(), String> {
        let destination = self.path(&receipt.request_id)?;
        let tmp = self.root.join(format!(".{}.tmp", uuid()?));
        Self::write_new(&tmp, receipt)?;
        if let Err(e) = fs::rename(&tmp, destination) {
            let _ = fs::remove_file(tmp);
            return Err(e.to_string());
        }
        // Persist the rename, not just the file contents, before a mutation.
        fs::File::open(&self.root)
            .and_then(|d| d.sync_all())
            .map_err(|e| e.to_string())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn receipt_reservation_is_exclusive_and_contains_no_prompt() {
        let root = std::env::temp_dir().join(format!("codexm-gui-test-{}", uuid().unwrap()));
        let store = Store::open(root.clone()).unwrap();
        let id = uuid().unwrap();
        let mut r = Receipt::new(&uuid().unwrap(), &id, "private text never stored");
        store.reserve(&r).unwrap();
        assert!(store.reserve(&r).is_err());
        r.outcome("unknown", "Do not resend");
        store.save(&r).unwrap();
        let data = fs::read_to_string(store.path(&id).unwrap()).unwrap();
        assert!(!data.contains("private text"));
        assert_eq!(store.read(&id).unwrap().unwrap().state, "unknown");
        assert_eq!(
            fs::metadata(store.path(&id).unwrap()).unwrap().mode() & 0o077,
            0
        );
        let evil = uuid().unwrap();
        std::os::unix::fs::symlink(store.path(&id).unwrap(), store.path(&evil).unwrap()).unwrap();
        assert!(store.read(&evil).is_err());
        assert!(store.read("../elsewhere").is_err());
        fs::remove_dir_all(root).unwrap();
    }
    #[test]
    fn generated_ids_and_rejected_inputs() {
        assert!(valid_uuid(&uuid().unwrap()));
        for id in [
            "--all",
            "../../../tmp/lock",
            "a0a0a0a0-a0a0-a0a0-a0a0-a0a0a0a0a0aZ",
            "",
        ] {
            assert!(!valid_uuid(id));
        }
    }
}
