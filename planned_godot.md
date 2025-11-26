A useful way to think about “what next” is:  
1) what an agent actually needs to do in Godot during real workflows, and  
2) what’s still clunky / manual in the current integration.

Here’s a prioritized backlog of features that would meaningfully level this up.

---

## 1. Higher‑level, idempotent “ensure” operations

Right now we expose mostly *primitive* actions (`node_create`, `node_set_prop`, etc.). For agent workflows it’s very useful to have idempotent, declarative ops:

- **`ensure_node`**
  - Inputs: `path`, `type`, optional `props`.
  - Behavior: if node exists and type matches → no-op (maybe update props); if missing → create; if type mismatch → error with hint.
  - Internally uses `node_inspect` + `node_create` + `node_set_prop`.

- **`ensure_signal_connected`**
  - Given `source_path`, `signal_name`, `target_path`, `method_name`, only connect if not already connected.
  - You already partially do this in `signal_connect`; explicitly expose it as “ensure” for agents.

- **`ensure_script_attached`**
  - Combine `node_attach_script` + checks so re-running plans is safe.

These significantly reduce “state dance” in agents and make plans repeatable.

---

## 2. Scene & prefab workflows

You’re building an actual game (Overcharge), so scene/prefab flows matter:

- **`scene_list`**
  - List scenes under `res://Scenes` (or configurable roots), with tags like “main menu”, “gameplay”.
- **`scene_open`**
  - Open a specific scene (by path) in the editor; fail with good hint if unsaved changes, etc.
- **`scene_duplicate` / `scene_instance`**
  - Duplicate a scene as a template (e.g., new map).
  - Instance a scene as a child node at a given path (prefab-style).

These let an agent create new maps / levels / screens by composing existing scenes.

---

## 3. Script scaffolding (GDScript side only)

Staying within your “no arbitrary code exec” boundary, but giving agents better hooks:

- **`script_scaffold`**
  - Create a new `.gd` file with a safe template:
    - `extends <BaseClass>`
    - optional exported variables
    - stub methods for signals you request.
  - Use conservative patterns (no dynamic eval, no reflection, etc.).
- **`attach_and_stub_signal`**
  - For a signal on a node, ensure:
    - script exists and is attached.
    - method stub exists in that script.

This is a big productivity boost when wiring UI / gameplay signals.

---

## 4. Editor UX / navigation helpers

These are mostly human‑facing but help “explainability” and debugging:

- **`focus_node`**
  - Select and frame a node in the editor (like clicking it in the Scene tree).
- **`camera_view` helpers**
  - Save / restore named editor camera bookmarks (e.g., “spawn area”, “boss arena”).
- **`selection_state`**
  - Report what is currently selected in the editor so the agent can say “I modified the node you have selected”.

This improves the hybrid human+agent loop: you can see exactly what the agent touched.

---

## 5. Test & play workflows

Given Overcharge has client/server and tests:

- **`run_scene` / `run_scene_once`**
  - Play a specific scene (not just main scene).
- **`run_tests` (Godot test layer)**
  - If you end up adding Godot unit tests, the plugin can trigger them and surface structured results to agentctl.
- **`capture_logs`**
  - Tail recent in‑engine logs for the last play session (distinct from editor errors).

These enable “try this change and see if the scene still boots / tests pass”.

---

## 6. Richer introspection & search

You already have `class_info` and `resource_list`. Some natural follow‑ups:

- **`search_nodes`**
  - Find nodes by name, type, or property pattern (e.g., all `Area2D` with `monitorable=true`).
- **`search_resources`**
  - Search `res://` for resources by type (`PackedScene`, `Texture2D`, etc.) or name patterns.
- **`references_to_resource`**
  - Given a resource path, list scenes/nodes that reference it (helps with refactors).

This gives agents better “global” understanding of a project, which is crucial in non‑trivial games.

---

## 7. Change previews & dry‑run

Improve safety and agent debugging:

- **`dry_run` toggle in skill input**
  - Plugin returns a “plan” of what would be changed (list of nodes, properties, scripts) without applying Undo/Redo actions.
- **`change_summary` metadata**
  - For mutating actions, plugin returns a concise summary (you already do some; you could standardize a minimal diff structure).

This would pair nicely with agentctl’s CAS + summaries for review workflows.

---

## 8. Overcharge‑specific helpers (later)

Once Overcharge’s architecture is more solid, some tailored helpers could be very powerful:

- **`ensure_network_manager`**
  - Confirm that the right `NetworkManager` node, script, and signals are present.
- **`spawn_pattern_editor`**
  - Helpers that edit spawn points / map markers in a normalized format the server/client share.

These are more opinionated but can turn multi‑step design tasks into single high‑level commands.

---

If you’d like, I can next:

- Sketch a **v3 feature spec** with 3–5 of these (e.g. `ensure_node`, `scene_open`, `scene_list`, `script_scaffold`, `search_nodes`), or  
- Pick one category (probably “ensure_*” or `scene_*`) and start implementing it end‑to‑end.