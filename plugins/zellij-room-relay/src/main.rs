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
    #[serde(default)]
    action: String,
    room_id: String,
    sender: String,
    content: String,
    interrupt: bool,
    targets: Vec<String>,
}

#[derive(Debug, Serialize)]
struct PaneDebugEntry {
    pane_id: String,
    title: String,
    is_plugin: bool,
    is_focused: bool,
    is_suppressed: bool,
}

#[derive(Debug, Serialize)]
struct DebugResponse {
    backend: &'static str,
    action: &'static str,
    permission_granted: bool,
    pane_count: usize,
    panes: Vec<PaneDebugEntry>,
    error: String,
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
                if request.action == "debug_panes" {
                    let response =
                        debug_panes_response(self.permission_granted, self.pane_manifest.as_ref());
                    cli_pipe_output(
                        &pipe_message.name,
                        &serde_json::to_string(&response).unwrap(),
                    );
                    return false;
                }
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

fn debug_panes_response(
    permission_granted: bool,
    pane_manifest: Option<&PaneManifest>,
) -> DebugResponse {
    let mut panes = vec![];
    if let Some(manifest) = pane_manifest {
        for pane_group in manifest.panes.values() {
            for pane in pane_group {
                panes.push(PaneDebugEntry {
                    pane_id: format!("terminal_{}", pane.id),
                    title: pane.title.clone(),
                    is_plugin: pane.is_plugin,
                    is_focused: pane.is_focused,
                    is_suppressed: pane.is_suppressed,
                });
            }
        }
    }
    panes.sort_by(|a, b| a.pane_id.cmp(&b.pane_id));
    DebugResponse {
        backend: "zellij",
        action: "debug_panes",
        permission_granted,
        pane_count: panes.len(),
        panes,
        error: String::new(),
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

/// When true, send a leading Escape before paste for `interrupt` deliveries (tmux parity).
/// Skip for composer-style panes — Escape often clears the composer there.
fn interrupt_escape_before_paste(target: &str) -> bool {
    !target_uses_composer_submit(target)
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
    let original_focus = current_focus(pane_manifest);

    for target in request.targets {
        if target == request.sender {
            response.skipped_members.push(target);
            continue;
        }
        match find_terminal_pane_by_title(pane_manifest, &target) {
            Some(PaneId::Terminal(pane_id)) => {
                let target_pane = PaneId::Terminal(pane_id);
                if original_focus.as_ref() != Some(&target_pane) {
                    focus_pane_with_id(target_pane, false);
                }
                if request.interrupt && interrupt_escape_before_paste(&target) {
                    write_chars("\u{1b}");
                }
                write_chars(&request.content);
                // Match tmuxbridge relay: composer-style TUIs need Ctrl+Enter. Kitty keyboard CSI
                // works when the outer terminal/session supports it (Zellij enables this by default
                // for capable terminals).
                if target_uses_composer_submit(&target) {
                    write_chars("\x1b[13;5u");
                } else {
                    write_chars("\n");
                }
                if let Some(original_focus) = original_focus {
                    if original_focus != target_pane {
                        focus_pane_with_id(original_focus, false);
                    }
                }
                response.delivered_count += 1;
                response.delivered_to.push(target);
            }
            Some(_) => {
                response.failed_count += 1;
                response.failed_members.push(target);
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

fn current_focus(pane_manifest: &PaneManifest) -> Option<PaneId> {
    for panes in pane_manifest.panes.values() {
        for pane in panes {
            if pane.is_focused {
                return Some(if pane.is_plugin {
                    PaneId::Plugin(pane.id)
                } else {
                    PaneId::Terminal(pane.id)
                });
            }
        }
    }
    None
}

fn find_terminal_pane_by_title(pane_manifest: &PaneManifest, title: &str) -> Option<PaneId> {
    if title.trim() == "__singleton__" {
        return find_single_terminal_pane(pane_manifest);
    }
    for panes in pane_manifest.panes.values() {
        for pane in panes {
            if pane.is_plugin {
                continue;
            }
            if pane_target_matches(&target_key(title), pane.id) {
                return Some(PaneId::Terminal(pane.id));
            }
            if pane_title_matches(&target_key(title), &pane.title) {
                return Some(PaneId::Terminal(pane.id));
            }
        }
    }
    None
}

fn find_single_terminal_pane(pane_manifest: &PaneManifest) -> Option<PaneId> {
    let mut terminal_id: Option<u32> = None;
    for panes in pane_manifest.panes.values() {
        for pane in panes {
            if pane.is_plugin {
                continue;
            }
            if terminal_id.is_some() {
                return None;
            }
            terminal_id = Some(pane.id);
        }
    }
    terminal_id.map(PaneId::Terminal)
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

fn pane_title_matches(target: &str, title: &str) -> bool {
    let title = title.trim();
    if title.is_empty() {
        return false;
    }
    if target == title {
        return true;
    }
    if let Some(rest) = target.strip_prefix("zellij:") {
        if let Some((_, pane_part)) = rest.split_once(':') {
            return pane_part == title;
        }
    }
    false
}

fn target_uses_composer_submit(target: &str) -> bool {
    let t = target.trim().to_lowercase();
    t.starts_with("droid")
        || t.starts_with("codex")
        || t.starts_with("cursor")
        || t.starts_with("claude")
        || t.starts_with("gemini")
}
