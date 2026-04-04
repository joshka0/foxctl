use serde::{Deserialize, Serialize};
use std::collections::BTreeMap;
use zellij_tile::prelude::*;

register_plugin!(State);

#[derive(Default)]
struct State {
    permission_granted: bool,
    pane_manifest: Option<PaneManifest>,
    pending: Vec<PendingDelivery>,
}

#[derive(Clone)]
struct PendingDelivery {
    pipe_name: String,
    request: RelayRequest,
}

#[derive(Debug, Clone, Deserialize)]
struct RelayRequest {
    room_id: String,
    sender: String,
    content: String,
    targets: Vec<String>,
}

#[derive(Debug, Serialize)]
struct RelayResponse {
    backend: &'static str,
    delivered_count: usize,
    failed_count: usize,
    delivered_to: Vec<String>,
    failed_members: Vec<String>,
    skipped_members: Vec<String>,
    error: String,
}

impl ZellijPlugin for State {
    fn load(&mut self, _configuration: BTreeMap<String, String>) {
        subscribe(&[EventType::PermissionRequestResult, EventType::Timer]);
        request_permission(&[
            PermissionType::ReadApplicationState,
            PermissionType::WriteToStdin,
            PermissionType::ReadCliPipes,
        ]);
    }

    fn update(&mut self, event: Event) -> bool {
        match event {
            Event::PermissionRequestResult(PermissionStatus::Granted) => {
                self.permission_granted = true;
                subscribe(&[
                    EventType::PermissionRequestResult,
                    EventType::PaneUpdate,
                    EventType::Timer,
                ]);
            }
            Event::PermissionRequestResult(PermissionStatus::Denied) => {
                self.flush_error("zellij relay plugin permissions were denied");
            }
            Event::PaneUpdate(pane_manifest) => {
                self.pane_manifest = Some(pane_manifest);
                self.flush_pending();
            }
            Event::Timer(_) => {
                if !self.pending.is_empty()
                    && (self.pane_manifest.is_none() || !self.permission_granted)
                {
                    self.flush_error("zellij relay plugin is waiting for pane state or permissions; retry once permissions are granted");
                }
            }
            _ => {}
        }
        false
    }

    fn pipe(&mut self, pipe_message: PipeMessage) -> bool {
        let payload = match pipe_message.payload {
            Some(payload) => payload,
            None => return false,
        };
        match serde_json::from_str::<RelayRequest>(&payload) {
            Ok(request) => {
                self.pending.push(PendingDelivery {
                    pipe_name: pipe_message.name,
                    request,
                });
                self.flush_pending();
                if !self.pending.is_empty() {
                    set_timeout(0.75);
                }
            }
            Err(err) => {
                let response = RelayResponse {
                    backend: "zellij",
                    delivered_count: 0,
                    failed_count: 0,
                    delivered_to: vec![],
                    failed_members: vec![],
                    skipped_members: vec![],
                    error: format!("invalid relay payload: {err}"),
                };
                cli_pipe_output(
                    &pipe_message.name,
                    &serde_json::to_string(&response).unwrap(),
                );
            }
        }
        false
    }
}

impl State {
    fn flush_pending(&mut self) {
        if !self.permission_granted {
            return;
        }
        let Some(pane_manifest) = self.pane_manifest.as_ref() else {
            return;
        };
        let pending = std::mem::take(&mut self.pending);
        for delivery in pending {
            let response = deliver_to_panes(pane_manifest, delivery.request);
            cli_pipe_output(
                &delivery.pipe_name,
                &serde_json::to_string(&response).unwrap(),
            );
        }
    }

    fn flush_error(&mut self, error: &str) {
        let pending = std::mem::take(&mut self.pending);
        for delivery in pending {
            let response = RelayResponse {
                backend: "zellij",
                delivered_count: 0,
                failed_count: delivery.request.targets.len(),
                delivered_to: vec![],
                failed_members: delivery.request.targets,
                skipped_members: vec![],
                error: error.to_owned(),
            };
            cli_pipe_output(
                &delivery.pipe_name,
                &serde_json::to_string(&response).unwrap(),
            );
        }
    }
}

fn deliver_to_panes(pane_manifest: &PaneManifest, request: RelayRequest) -> RelayResponse {
    let mut response = RelayResponse {
        backend: "zellij",
        delivered_count: 0,
        failed_count: 0,
        delivered_to: vec![],
        failed_members: vec![],
        skipped_members: vec![],
        error: String::new(),
    };

    for target in request.targets {
        if target == request.sender {
            response.skipped_members.push(target);
            continue;
        }
        match find_terminal_pane_by_title(pane_manifest, &target) {
            Some(pane_id) => {
                write_chars_to_pane_id(&request.content, pane_id);
                write_chars_to_pane_id("\n", pane_id);
                response.delivered_count += 1;
                response.delivered_to.push(target);
            }
            None => {
                response.failed_count += 1;
                response.failed_members.push(target);
            }
        }
    }

    if response.delivered_count == 0 && response.failed_count > 0 {
        response.error = format!(
            "room {} targets were not found by pane title",
            request.room_id
        );
    }

    response
}

fn find_terminal_pane_by_title(pane_manifest: &PaneManifest, title: &str) -> Option<PaneId> {
    for panes in pane_manifest.panes.values() {
        for pane in panes {
            if pane.is_plugin {
                continue;
            }
            if pane_target_matches(&target_key(title), pane.id) {
                return Some(PaneId::Terminal(pane.id));
            }
            if pane.title == title {
                return Some(PaneId::Terminal(pane.id));
            }
        }
    }
    None
}

fn target_key(target: &str) -> String {
    target.trim().to_string()
}

fn pane_target_matches(target: &str, pane_id: u32) -> bool {
    let terminal = format!("terminal_{pane_id}");
    if target == terminal || target == pane_id.to_string() {
        return true;
    }
    if let Some(rest) = target.strip_prefix("zellij:") {
        if let Some((_, pane_part)) = rest.split_once(':') {
            return pane_part == terminal || pane_part == pane_id.to_string();
        }
    }
    false
}
