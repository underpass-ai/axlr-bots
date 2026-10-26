//! Stand-ins for the ports: the control plane, the clock, the keys, the
//! files and the settings in memory. Tests run against them.

mod counting_keys;
mod fake_connector;
mod fake_control_plane;
mod fixed_clock;
mod memory_config_store;
mod memory_files;
mod recording_notifier;

pub use counting_keys::CountingKeys;
pub use fake_connector::FakeConnector;
pub use fake_control_plane::FakeControlPlane;
pub use fixed_clock::FixedClock;
pub use memory_config_store::MemoryConfigStore;
pub use memory_files::MemoryFiles;
pub use recording_notifier::RecordingNotifier;
