//! Reads the running control plane of this host, when `AXLR_PANEL_LIVE=1`:
//! every list and one task's detail and activity tree, to prove the panel
//! reads the real gRPC answers. Nothing is changed. The settings are the local install's
//! (`~/.local/state/axlr-bots`, `https://127.0.0.1:7880`, the gRPC address) unless
//! `AXLR_PANEL_API`, `AXLR_PANEL_CA` and `AXLR_PANEL_TOKEN_FILE` say
//! otherwise.

use std::path::PathBuf;
use std::sync::Arc;

use axlr_bots_panel::adapters::grpc::GrpcControlPlane;
use axlr_bots_panel::adapters::system::SystemClock;
use axlr_bots_panel::application::{ControlPlane, LoadTaskDetail, PanelConfig, RefreshFleet, ShowBot};

fn live_config() -> Option<PanelConfig> {
    if std::env::var("AXLR_PANEL_LIVE").ok().as_deref() != Some("1") {
        return None;
    }
    let state = PathBuf::from(std::env::var("HOME").unwrap()).join(".local/state/axlr-bots");
    let var = |name: &str| std::env::var(name).ok();
    Some(PanelConfig {
        api_url: var("AXLR_PANEL_API").unwrap_or_else(|| "https://127.0.0.1:7880".into()),
        ca_path: var("AXLR_PANEL_CA")
            .map(PathBuf::from)
            .unwrap_or_else(|| state.join("ca/ca.crt")),
        token_file: var("AXLR_PANEL_TOKEN_FILE")
            .map(PathBuf::from)
            .unwrap_or_else(|| state.join("api.token")),
        actor: None,
        cert_path: None,
        key_path: None,
    })
}

#[tokio::test]
async fn reads_the_running_control_plane() {
    let Some(config) = live_config() else {
        eprintln!("skipped: set AXLR_PANEL_LIVE=1 to read the running control plane");
        return;
    };
    let api: Arc<dyn ControlPlane> = Arc::new(GrpcControlPlane::new(&config).unwrap());
    let fleet = RefreshFleet::new(api.clone(), Arc::new(SystemClock))
        .execute()
        .await
        .unwrap();
    println!(
        "nodes {} bots {} tasks {} executions today {} cost today {:.4}",
        fleet.nodes.len(),
        fleet.bots.len(),
        fleet.tasks.len(),
        fleet.overview.executions_since,
        fleet.overview.cost_usd_since
    );
    for node in &fleet.nodes {
        println!(
            "node {} {:?} cert until {:?}",
            node.id, node.status, node.certificate_not_after
        );
    }
    for bot in &fleet.bots {
        let (shown, events) = ShowBot::new(api.clone()).execute(bot.id.clone()).await.unwrap();
        println!("bot {} {:?} {} events", shown.id, shown.status, events.len());
    }
    for task in fleet.tasks.iter().take(25) {
        let detail = LoadTaskDetail::new(api.clone()).execute(task.id.clone()).await.unwrap();
        assert_eq!(detail.task.id, task.id);
        let tree = api.task_activity(task.id.clone()).await.unwrap();
        assert_eq!(tree.task.id, task.id);
    }
    println!("read {} task details", fleet.tasks.len().min(25));
}
