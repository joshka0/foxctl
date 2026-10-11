<file_map>
/Users/joshka/repos/worktrees/feat-longcot-eval-dry-run
├── internal
│   ├── rlm
│   │   ├── env
│   │   │   ├── adapter.go * +
│   │   │   ├── bootstrap.go * +
│   │   │   ├── code_search_ensemble.go * +
│   │   │   ├── memory_ensemble.go * +
│   │   │   └── tools.go * +
│   │   ├── optdata
│   │   ├── repl
│   │   ├── runtime
│   │   ├── lambda_classify.go * +
│   │   ├── lambda_runner.go * +
│   │   ├── llm_runner.go * +
│   │   ├── plan.go * +
│   │   └── run_spec.go * +
│   ├── adapters
│   │   ├── artifacts
│   │   └── skillslib
│   │       ├── ci
│   │       ├── codeblocks
│   │       ├── codeedit
│   │       ├── diffutil
│   │       ├── editutil
│   │       ├── executil
│   │       ├── fs
│   │       ├── fsutil
│   │       ├── gitutil
│   │       ├── hashutil
│   │       ├── hookutil
│   │       ├── htmledit
│   │       ├── langutil
│   │       ├── lsp
│   │       ├── mathutil
│   │       ├── mcputil
│   │       ├── memoryutil
│   │       ├── mobileutil
│   │       ├── obs
│   │       ├── oputil
│   │       ├── pathutil
│   │       ├── rgutil
│   │       ├── runner
│   │       ├── secretutil
│   │       ├── skillerr
│   │       ├── skillmain
│   │       ├── skillout
│   │       ├── skilltest
│   │       ├── sliceutil
│   │       ├── stringutil
│   │       ├── symbolutil
│   │       ├── textmatch
│   │       ├── textreplace
│   │       ├── textutil
│   │       └── workspaceutil
│   ├── agent
│   │   ├── daemon
│   │   ├── optimization
│   │   │   └── testdata
│   │   ├── prompts
│   │   ├── runtime
│   │   ├── toolnames
│   │   ├── tools
│   │   └── types
│   ├── atcp
│   │   ├── adapter
│   │   │   ├── generictty
│   │   │   └── profiles
│   │   ├── addressing
│   │   ├── broker
│   │   │   ├── lease
│   │   │   ├── modetrack
│   │   │   ├── room
│   │   │   ├── router
│   │   │   ├── safeprompt
│   │   │   ├── session
│   │   │   ├── storage
│   │   │   │   └── sqlite
│   │   │   ├── termcaps
│   │   │   └── vtscreen
│   │   ├── client
│   │   ├── daemon
│   │   ├── envelope
│   │   ├── intents
│   │   ├── kinds
│   │   └── transport
│   │       ├── httpjson
│   │       └── unixsocket
│   ├── auth
│   │   └── broker
│   ├── console
│   │   └── app
│   ├── context
│   │   ├── calibration
│   │   ├── companion
│   │   │   └── testdata
│   │   ├── contextplane
│   │   │   └── taskhistory
│   │   ├── knowledge
│   │   │   └── builtin
│   │   │       └── data
│   │   │           ├── agents
│   │   │           └── droids
│   │   ├── sessionkit
│   │   │   ├── archive
│   │   │   ├── claudejsonl
│   │   │   ├── codexjsonl
│   │   │   ├── snapshot
│   │   │   └── summary
│   │   ├── todosync
│   │   ├── transcriptpipeline
│   │   │   └── history
│   │   └── updater
│   ├── domain
│   │   ├── agent
│   │   ├── backup
│   │   ├── console
│   │   ├── envelope
│   │   ├── hook
│   │   ├── identity
│   │   ├── policy
│   │   └── skill
│   ├── intelligence
│   │   ├── analysis
│   │   │   ├── overseer
│   │   │   └── tasksgraph
│   │   ├── codecontext
│   │   │   ├── adapters
│   │   │   ├── autoselect
│   │   │   ├── expander
│   │   │   ├── files
│   │   │   ├── guard
│   │   │   ├── lang
│   │   │   └── selector
│   │   ├── codemap
│   │   │   ├── context
│   │   │   └── tools
│   │   ├── indexing
│   │   │   ├── atomic
│   │   │   ├── codefilter
│   │   │   ├── embedding
│   │   │   ├── embeddingtext
│   │   │   ├── filesummary
│   │   │   ├── postreview
│   │   │   ├── repoindex
│   │   │   │   └── parser
│   │   │   ├── rerank
│   │   │   ├── semantic
│   │   │   └── symbol
│   │   │       └── testdata
│   │   ├── planning
│   │   │   └── llm
│   │   ├── refactor
│   │   │   ├── changes
│   │   │   ├── deps
│   │   │   ├── evidence
│   │   │   ├── hot
│   │   │   ├── scope
│   │   │   ├── snapshot
│   │   │   ├── snapshotstore
│   │   │   └── status
│   │   ├── repoquery
│   │   │   └── adapters
│   │   ├── retrieval
│   │   │   └── v2
│   │   ├── searchindex
│   │   ├── searchquery
│   │   ├── searchrank
│   │   └── verification
│   ├── interfaces
│   │   ├── chatadapter
│   │   │   ├── discord
│   │   │   ├── teams
│   │   │   └── telegram
│   │   ├── gateway
│   │   │   ├── sshterm
│   │   │   ├── static
│   │   │   └── webterm
│   │   ├── jira
│   │   ├── openapi
│   │   │   ├── auth
│   │   │   ├── builder
│   │   │   ├── client
│   │   │   ├── loader
│   │   │   │   └── testdata
│   │   │   ├── pagination
│   │   │   ├── plugin
│   │   │   └── retry
│   │   ├── tui
│   │   │   ├── components
│   │   │   │   ├── cmd
│   │   │   │   │   ├── detailpane
│   │   │   │   │   ├── drawer
│   │   │   │   │   ├── streamviewer
│   │   │   │   │   └── tabs
│   │   │   │   └── testdata
│   │   │   │       ├── snapshots
│   │   │   │       └── tuistory
│   │   │   ├── entities
│   │   │   ├── runtime
│   │   │   ├── testfixture
│   │   │   └── theme
│   │   └── web
│   │       ├── api
│   │       ├── consolews
│   │       └── sse
│   ├── platform
│   │   ├── buildinfo
│   │   ├── config
│   │   ├── deviceid
│   │   ├── env
│   │   ├── errors
│   │   ├── fsutil
│   │   ├── jsoncompat
│   │   ├── logging
│   │   ├── lsp
│   │   │   ├── gopls
│   │   │   └── jsonrpc
│   │   ├── maputil
│   │   ├── metrics
│   │   ├── pathutil
│   │   ├── secrets
│   │   ├── symbolutil
│   │   ├── timeutil
│   │   ├── workspace
│   │   └── worktree
│   ├── protocol
│   ├── providers
│   │   ├── claude
│   │   │   └── todos
│   │   ├── llm
│   │   └── llmcompat
│   ├── runtime
│   │   ├── actor
│   │   │   ├── handlers
│   │   │   └── memory
│   │   ├── agentpolicy
│   │   ├── agentprompt
│   │   ├── daemon
│   │   ├── engine
│   │   ├── execution
│   │   │   ├── agentmanager
│   │   │   ├── circuitbreaker
│   │   │   ├── exec
│   │   │   ├── quotas
│   │   │   ├── runner
│   │   │   ├── scheduler
│   │   │   └── wasi
│   │   ├── hooks
│   │   │   ├── analysisflow
│   │   │   ├── contextflow
│   │   │   ├── inboxflow
│   │   │   ├── lifecycle
│   │   │   ├── memoryflow
│   │   │   ├── operationalflow
│   │   │   ├── pathutil
│   │   │   ├── promptflow
│   │   │   ├── sessionmode
│   │   │   ├── taskflow
│   │   │   └── toolutil
│   │   ├── jobs
│   │   │   └── workers
│   │   ├── observability
│   │   │   └── testdata
│   │   ├── orchestration
│   │   │   ├── roomruntime
│   │   │   └── workflow
│   │   │       └── frontmatter
│   │   ├── runservice
│   │   ├── sandbox
│   │   │   ├── opensandbox
│   │   │   └── smolvm
│   │   ├── terminal
│   │   │   ├── agentpane
│   │   │   ├── tmuxbridge
│   │   │   └── zellijbridge
│   │   └── trajectorycapture
│   ├── storage
│   │   ├── agents
│   │   ├── annotations
│   │   ├── backup
│   │   ├── blackboard
│   │   ├── cache
│   │   ├── cas
│   │   ├── console
│   │   ├── contextbuffer
│   │   ├── contextvar
│   │   ├── conversationsettings
│   │   ├── convref
│   │   ├── coordination
│   │   ├── dbdriver
│   │   ├── dbutil
│   │   ├── graph
│   │   ├── jobs
│   │   │   ├── executor
│   │   │   ├── fsutil
│   │   │   ├── persist
│   │   │   └── types
│   │   ├── knowledge
│   │   ├── mailbox
│   │   ├── memory
│   │   ├── memvid
│   │   ├── obsidianindex
│   │   ├── plans
│   │   ├── queue
│   │   ├── quotas
│   │   ├── sessions
│   │   ├── sqliteutil
│   │   ├── sqlutil
│   │   ├── tasks
│   │   ├── teams
│   │   ├── testwatch
│   │   ├── trajectory
│   │   ├── transcriptcache
│   │   └── vector
│   ├── tooling
│   │   ├── evals
│   │   │   ├── correctioneval
│   │   │   ├── longcotbridge
│   │   │   ├── longcoteval
│   │   │   ├── retrievaleval
│   │   │   └── transcriptmemoryeval
│   │   ├── evolve
│   │   │   ├── model
│   │   │   ├── store
│   │   │   └── view
│   │   ├── shellreduce
│   │   ├── skillrun
│   │   │   └── ephemeral
│   │   ├── testwatch
│   │   └── tools
│   │       ├── obsidian
│   │       │   └── testdata
│   │       │       └── vaults
│   │       │           └── basic
│   │       │               ├── .obsidian
│   │       │               ├── 00-home
│   │       │               ├── atlas
│   │       │               ├── inbox
│   │       │               │   └── drafted-from-foxctl
│   │       │               └── notes
│   │       │                   ├── adr
│   │       │                   ├── moc
│   │       │                   └── patterns
│   │       └── ripgrep
│   └── v2
│       ├── adapters
│       │   ├── eino
│       │   ├── goruntime
│       │   ├── jido
│       │   ├── libsql
│       │   │   ├── events
│       │   │   ├── idmap
│       │   │   ├── orchestration
│       │   │   ├── projections
│       │   │   ├── turns
│       │   │   └── workers
│       │   ├── llm
│       │   ├── mailbox
│       │   ├── sourceimport
│       │   ├── sqlite
│       │   ├── telemetry
│       │   └── toolbridge
│       ├── core
│       │   ├── ask
│       │   ├── errors
│       │   ├── events
│       │   ├── kill
│       │   ├── list
│       │   ├── orchestration
│       │   ├── policy
│       │   ├── run
│       │   ├── services
│       │   ├── spawn
│       │   ├── tool
│       │   └── worker
│       ├── runtime
│       │   ├── contextbuilder
│       │   ├── enrichers
│       │   ├── events
│       │   ├── maintenance
│       │   ├── orchestration
│       │   ├── profiles
│       │   ├── runner
│       │   │   └── testdata
│       │   │       └── golden_events
│       │   ├── snapshots
│       │   ├── supervisor
│       │   ├── toolnames
│       │   ├── tools
│       │   └── workers
│       ├── services
│       └── testkit
│           ├── fakes
│           └── golden
├── skills
│   ├── memory_query
│   │   └── skill.yaml *
│   ├── session_recall
│   │   └── skill.yaml *
│   ├── agent_handbook
│   ├── build_godot
│   ├── build_unity
│   ├── calibration_feedback
│   ├── calibration_generate
│   ├── calibration_get
│   ├── ci_checks
│   ├── ci_prcomments
│   ├── code_complexity
│   ├── code_context_grep
│   ├── code_context_ripgrep
│   ├── code_counsel
│   ├── code_dag_grep
│   ├── code_diff
│   ├── code_git
│   ├── code_greenlight
│   ├── code_imports
│   ├── code_incremental_index
│   ├── code_llm_search
│   ├── code_refactor_advisor
│   ├── code_refactor_scout
│   ├── code_security
│   ├── code_semantic_search
│   ├── code_smart_read
│   ├── code_smart_search
│   ├── code_smart_write
│   ├── code_snippet_extract
│   ├── code_stats
│   ├── code_symbols
│   ├── codemap_check
│   ├── codemap_generate
│   ├── codemap_get
│   ├── codemap_import
│   ├── codemap_list
│   ├── context_filter
│   ├── cove_verify
│   ├── data_jq
│   ├── editor_godot
│   │   └── godot_plugin
│   │       └── handlers
│   ├── embedding_memories
│   ├── embedding_queue
│   ├── embedding_refresh
│   ├── embedding_tasks
│   ├── embedding_worker
│   ├── epic_complete
│   ├── exa
│   ├── expo
│   ├── fs_apply_edit
│   ├── fs_cas_get
│   ├── fs_find
│   ├── fs_ls
│   ├── fs_read
│   ├── fs_tree
│   ├── fs_write
│   ├── git_status
│   ├── git_worktree
│   ├── graph
│   ├── graph_cleanup
│   ├── graph_pagerank
│   ├── heartwood_action
│   ├── heartwood_state
│   ├── hooks_bash_guard
│   ├── hooks_context_drain
│   ├── hooks_context_enqueue
│   ├── hooks_dispatch
│   ├── hooks_file_guard
│   ├── hooks_impact_analysis
│   ├── hooks_knowledge_router
│   ├── hooks_mail_router
│   ├── hooks_overseer_inbox
│   ├── hooks_session_end
│   ├── hooks_stop_guard
│   ├── hooks_subagent_start
│   ├── hooks_subagent_stop
│   ├── hooks_task_guard
│   ├── hooks_test_feedback
│   ├── html_edit
│   ├── http_openapi
│   ├── jira_board
│   ├── jira_issue
│   ├── json_transform
│   ├── libsql_migrate
│   ├── lsp_gopls
│   ├── lsp_pylsp
│   ├── lsp_tsserver
│   ├── mailbox
│   ├── mcp_bridge
│   ├── mcp_install
│   ├── mobile
│   ├── mobile_android
│   ├── mobile_ios
│   ├── obs_logs
│   ├── obs_replay
│   ├── optimize_analyze
│   ├── optimize_bootstrap
│   ├── optimize_feedback
│   ├── optimize_from_feedback
│   ├── optimize_patterns
│   └── optimize_reflect
├── .claude
│   ├── agents
│   ├── commands
│   ├── skills
│   └── workflows
├── .codex
│   └── prompts
├── .devcontainer
├── .factory
│   ├── library
│   ├── skills
│   │   ├── tui-docs-worker
│   │   └── tui-engineer-worker
│   └── validation
│       ├── components
│       │   ├── scrutiny
│       │   │   └── reviews
│       │   └── user-testing
│       │       └── flows
│       ├── cross-feature
│       │   └── user-testing
│       │       └── flows
│       ├── docs
│       │   ├── scrutiny
│       │   │   └── reviews
│       │   └── user-testing
│       │       └── flows
│       ├── gateway-terminal
│       │   ├── scrutiny
│       │   │   └── reviews
│       │   └── user-testing
│       │       └── flows
│       ├── room-sandbox
│       │   ├── scrutiny
│       │   │   └── reviews
│       │   └── user-testing
│       │       └── flows
│       ├── tailscale
│       └── worktree-manager
│           ├── scrutiny
│           │   └── reviews
│           └── user-testing
│               └── flows
├── .github
│   ├── ISSUE_TEMPLATE
│   └── workflows
├── .windsurf
│   ├── rules
│   └── workflows
├── archive
│   ├── cmd
│   │   └── foxctl_viewer
│   └── packages
│       ├── tui-agent
│       │   └── src
│       └── tui-legacy
│           └── src
│               ├── hooks
│               └── views
├── cmd
│   ├── atcp-live
│   ├── atcp-room-smoke
│   ├── atcp-smoke
│   ├── atcpctl
│   ├── atcpd
│   ├── companion_mailbox_test
│   ├── companion_memory_test
│   ├── companion_test
│   ├── foxctl
│   │   └── cmd
│   │       ├── godot_plugin
│   │       ├── memorycmd
│   │       └── sessionscmd
│   ├── foxctl-mail
│   ├── foxctl_tui
│   ├── foxctl_web
│   └── synctest
├── configs
│   ├── agents
│   ├── agents-condensed
│   ├── codex
│   ├── commands
│   ├── gemini
│   │   └── commands
│   ├── hooks
│   │   ├── claude
│   │   ├── lib
│   │   └── optional
│   ├── launchd
│   ├── opencode
│   │   ├── agents-pack
│   │   └── plugin
│   ├── opencode-hooks
│   │   └── lib
│   ├── providers
│   ├── skills
│   │   ├── comment-skill
│   │   ├── dev-docs
│   │   ├── dev-docs-update
│   │   ├── foxctl-agents
│   │   ├── foxctl-ci
│   │   ├── foxctl-code
│   │   ├── foxctl-codemap
│   │   ├── foxctl-companion
│   │   ├── foxctl-complexity
│   │   ├── foxctl-daemon
│   │   ├── foxctl-friend
│   │   ├── foxctl-fs
│   │   ├── foxctl-godot
│   │   ├── foxctl-hooks
│   │   ├── foxctl-html-edit
│   │   ├── foxctl-jq
│   │   ├── foxctl-mailbox
│   │   ├── foxctl-mobile
│   │   ├── foxctl-mobile-android
│   │   ├── foxctl-mobile-ios
│   │   ├── foxctl-openapi
│   │   ├── foxctl-overseer
│   │   ├── foxctl-preimpl
│   │   ├── foxctl-reservations
│   │   ├── foxctl-run
│   │   ├── foxctl-search
│   │   ├── foxctl-semantic-search
│   │   ├── foxctl-sessions
│   │   ├── foxctl-swe-grep
│   │   ├── foxctl-test
│   │   ├── foxctl-todo
│   │   ├── foxctl-unity
│   │   ├── foxctl-workflow
│   │   ├── hybrid-research
│   │   ├── plan-sync
│   │   ├── pre-impl
│   │   ├── remember
│   │   ├── route-research-for-testing
│   │   ├── session-restore
│   │   └── session-save
│   ├── skills-condensed
│   │   ├── ask-questions-if-underspecified
│   │   ├── dev-docs
│   │   ├── dev-docs-update
│   │   ├── foxctl-agents
│   │   ├── foxctl-ci
│   │   ├── foxctl-code
│   │   ├── foxctl-code-intelligence
│   │   ├── foxctl-codemap
│   │   ├── foxctl-context
│   │   ├── foxctl-data
│   │   ├── foxctl-dev-workflow
│   │   ├── foxctl-friend
│   │   ├── foxctl-fs
│   │   ├── foxctl-godot
│   │   ├── foxctl-hooks
│   │   ├── foxctl-html-edit
│   │   ├── foxctl-jq
│   │   ├── foxctl-mailbox
│   │   ├── foxctl-mobile
│   │   ├── foxctl-mobile-android
│   │   ├── foxctl-mobile-ios
│   │   ├── foxctl-obs
│   │   ├── foxctl-openapi
│   │   ├── foxctl-orchestration
│   │   ├── foxctl-overseer
│   │   ├── foxctl-preimpl
│   │   ├── foxctl-providers
│   │   ├── foxctl-reservations
│   │   ├── foxctl-run
│   │   ├── foxctl-search
│   │   ├── foxctl-semantic-search
│   │   ├── foxctl-sessions
│   │   ├── foxctl-smart-edit
│   │   ├── foxctl-test
│   │   ├── foxctl-todo
│   │   ├── foxctl-workflow
│   │   ├── plan-sync
│   │   ├── pre-impl
│   │   ├── remember
│   │   ├── route-research-for-testing
│   │   ├── session-restore
│   │   └── session-save
│   └── skills-pack
│       ├── agent-ci
│       ├── foxctl-all
│       ├── foxctl-code
│       ├── foxctl-context
│       ├── foxctl-core
│       ├── foxctl-dev
│       ├── foxctl-epic-grader
│       │   └── references
│       ├── foxctl-epic-pipeline
│       ├── foxctl-factory-epic-creation
│       │   └── references
│       ├── foxctl-integrations
│       ├── foxctl-mobile
│       ├── foxctl-orchestrate
│       ├── foxctl-room
│       ├── foxctl-room-agent
│       ├── foxctl-room-agile
│       ├── foxctl-room-operator
│       ├── foxctl-room-view
│       ├── foxctl-tmux
│       │   └── scripts
│       ├── hard-cut
│       └── tmux-bridge
│           └── scripts
├── deploy
│   ├── kubernetes
│   │   ├── base
│   │   └── overlays
│   │       ├── local
│   │       ├── postgres
│   │       ├── public-gui
│   │       └── public-gui-local
│   └── libsql
├── docs
│   ├── architecture
│   ├── archive
│   │   ├── designs
│   │   ├── impl_plan
│   │   │   └── impl_plan
│   │   │       └── viewer
│   │   ├── migrations
│   │   └── specs
│   ├── atcp
│   ├── changelogs
│   ├── checklists
│   ├── ci
│   ├── codemaps
│   ├── designs
│   ├── examples
│   ├── general
│   ├── godot
│   ├── guides
│   ├── knowledge
│   │   ├── backend-dev-guidelines
│   │   │   └── resources
│   │   ├── code-retrieval
│   │   ├── error-tracking
│   │   ├── frontend-dev-guidelines
│   │   │   └── resources
│   │   ├── resources
│   │   ├── route-tester
│   │   └── skill-developer
│   ├── notes
│   ├── plans
│   │   ├── archive
│   │   ├── embedding-quality-roadmap
│   │   ├── features
│   │   ├── gui-v2
│   │   ├── k8s
│   │   ├── migration_to_v2
│   │   │   └── phase2
│   │   └── tui-redesign
│   │       └── adrs
│   ├── research
│   ├── spec
│   │   ├── skills_spec
│   │   └── v1
│   └── start
├── packages
│   ├── data
│   │   └── src
│   ├── gui-agent
│   │   ├── public
│   │   └── src
│   │       ├── api
│   │       ├── assets
│   │       ├── components
│   │       │   ├── actions
│   │       │   ├── activity
│   │       │   ├── agents
│   │       │   ├── chat
│   │       │   ├── conversations
│   │       │   ├── layout
│   │       │   ├── rooms
│   │       │   ├── ui
│   │       │   └── v2
│   │       ├── hooks
│   │       ├── lib
│   │       └── stores
│   └── gui-auth-gateway
│       └── src
├── plugins
│   ├── auth-hmac
│   ├── paging-custom
│   └── zellij-room-relay
│       └── src
├── scripts
│   ├── checkmanifests
│   ├── compare_retrieval_modes
│   ├── hooks
│   ├── reindex_searchindex
│   └── skills_impact
├── test
│   ├── e2e
│   ├── golden
│   │   ├── envelopes
│   │   ├── openapi
│   │   ├── skills
│   │   ├── symbol_index
│   │   │   └── fixtures
│   │   └── trajectories
│   │       └── fixtures
│   └── integration
├── testdata
│   └── evals
│       ├── code-search-ensemble
│       ├── corrections
│       ├── longcot
│       ├── refactor-scout
│       ├── retrieval
│       └── transcriptmemory
├── tests
│   └── regression
│       └── 001-room-runtime-critical-invariants
├── workflows
└── AGENTS.md *


(* denotes selected files)
(+ denotes code-map available)
Config: directory-only view; selected files shown.

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/rlm/lambda.go
Imports:
  - import "math"
---
Classes:
  - LambdaConfig
    Methods:
      - L119: func (c LambdaConfig) Defaults() LambdaConfig
    Properties:
      - ContextBudget
      - MaxK
      - CSearch
      - CInspect
      - PerPhaseAccuracy
      - AccuracyTarget
      - LLM
      - EphemeralSkills
      - ExtractSolutionLine
      - EphemeralSkillAttempts
  - LambdaPlan
    Properties:
      - TaskType
      - ComposeOp
      - KStar
      - TauStar
      - Depth
      - CostEstimate
      - N

Functions:
  - L17: func NormalizeTaskType(value string) TaskType
  - L62: func ComposeOpForTask(tt TaskType) ComposeOp
  - L70: func SearchToolForTask(tt TaskType) string
  - L162: func PlanLambda(taskType TaskType, n int, cfg LambdaConfig) LambdaPlan
  - L210: func capLambdaPlanForTask(plan LambdaPlan, task Task, cfg LambdaConfig) (LambdaPlan, map[string]any)
  - L237: func ceilDivInt(n, d int) int
  - L247: func clampInt(v, lo, hi int) int

Global vars:
  - TaskTypeCodeLocate
  - TaskTypeCodeUnderstand
  - TaskTypeMemoryRecall
  - TaskTypeEvidenceAudit
  - TaskTypeGeneral
  - ComposeUnion
  - compositionTable
  - searchToolByTaskType
---


File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/cmd/foxctl/cmd/rlm.go
Imports:
  - import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/context/companion"
	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/rlm"
	rlmenv "github.com/joshka0/foxctl/internal/rlm/env"
	rlmruntime "github.com/joshka0/foxctl/internal/rlm/runtime"
	"github.com/joshka0/foxctl/internal/storage/dbutil"
	"github.com/spf13/cobra"
)
---
Classes:
  - Environment
    Methods:
      - L315: func newRLMREPLQueryFactory(adapter *rlmenv.ReadOnlyAdapter) func(parentTask rlm.Task, env rlm.Environment) rlmruntime.RLMQueryRunFunc

Functions:
  - L23: func newRLMCommand() *cobra.Command
  - L32: func newRLMRunCommand() *cobra.Command
  - L182: func normalizedRLMExecutor(value string) string
  - L193: func chooseRLMRunner(
 executor string, adapter *rlmenv.ReadOnlyAdapter, task rlm.Task, env rlm.Environment, llmProvider, llmModel, llmBaseURL, llmAPIKey string, llmTimeout time.Duration, requireToolUse bool, routeProfile string, planMode string, sandboxKind string, ephemeralSkills bool, extractSolution bool,
  - L288: func buildRLMREPLCLISystemPrompt(kind rlmruntime.SandboxKind, helperEnabled bool) string
  - L292: func buildRLMREPLCLISystemPromptForPolicy(kind rlmruntime.SandboxKind, helperEnabled bool, recursionEnabled bool) string
  - L356: func applyRLMScoutRole(task rlm.Task, env rlm.Environment) (rlm.Task, rlm.Environment)
  - L367: func init()
  - L371: func openRLMCompanionDB(ctx context.Context, cfg config.Config) (*sql.DB, func() error, error)
---


File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/agent/optimization/bootstrap.go
Imports:
  - import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/storage/trajectory"
)
---
Classes:
  - BootstrapConfig
    Properties:
      - MinSuccessRate
      - MaxExamples
      - DiversityWeight
      - MinHumanRating
      - LookbackDays
  - Example
    Properties:
      - Input
      - Output
      - Tools
      - Rating
      - diversityScore
  - BootstrapOptimizer
    Methods:
      - L86: func (b *BootstrapOptimizer) GenerateExamples(ctx context.Context, workspaceID, agentRole string) ([]Example, error)
      - L136: func (b *BootstrapOptimizer) extractExample(ctx context.Context, traj trajectory.Trajectory) (*Example, error)
      - L197: func (b *BootstrapOptimizer) selectDiverse(examples []Example) []Example
      - L252: func (b *BootstrapOptimizer) computeDiversityScore(example Example, all []Example) float64
      - L275: func (b *BootstrapOptimizer) combinedScore(example Example) float64
      - L295: func (b *BootstrapOptimizer) similarity(textA, textB string) float64
      - L330: func (b *BootstrapOptimizer) FormatExamplesForPrompt(examples []Example) string
      - L365: func (b *BootstrapOptimizer) GetExampleStats(ctx context.Context, workspaceID, agentRole string) (*ExampleStats, error)
    Properties:
      - trajStore
      - patternStore
      - config
  - ExampleStats
    Properties:
      - TotalAvailable
      - AvgToolsPerExample
      - HasRatings
      - AvgRating

Functions:
  - L33: func DefaultBootstrapConfig() BootstrapConfig
  - L68: func NewBootstrapOptimizer(trajStore trajectory.Store, patternStore PatternStore, config BootstrapConfig) *BootstrapOptimizer
  - L353: func truncate(s string, maxLen int) string
---


File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/context/transcriptpipeline/worker.go
Imports:
  - import (
	"context"
	"fmt"
	"strings"
	"time"

	actormemory "github.com/joshka0/foxctl/internal/runtime/actor/memory"
	"github.com/joshka0/foxctl/internal/runtime/engine"
)
---
Classes:
  - WorkerConfig
    Properties:
      - Provider
      - APIKey
      - AuthMode
      - AuthHeader
      - AuthPrefix
      - Model
      - BaseURL
      - MaxContextTokens
      - Timeout
  - Task
    Properties:
      - Stage
      - InputKind
      - PromptVersion
      - SystemPrompt
      - ArtifactText
      - MaxTokens
  - Result
    Methods:
      - L133: func RunLLMTaskWithFallbackModel(ctx context.Context, primary WorkerConfig, fallback WorkerConfig, task Task, accept func(Result) bool, run llmTaskRunner) (Result, bool)
    Properties:
      - Stage
      - InputKind
      - PromptVersion
      - ModelID
      - OutputText

Functions:
  - L60: func RunLLMTask(ctx context.Context, cfg WorkerConfig, task Task) (Result, error)
  - L154: func BoundArtifactText(text string, maxContextTokens int) string
  - L169: func truncateInline(text string, max int) string
  - L180: func sameWorkerTarget(a, b WorkerConfig) bool

Global vars:
  - StageCompress
  - StageDerive
  - StageBridge
  - StageDistill
  - StagePolish
  - StageClassify
  - StageReview
  - StageAlign
---

</file_map>
<file_contents>
File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/rlm/lambda_classify.go
```go
package rlm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/joshka0/foxctl/internal/runtime/engine"
)

const classifyPromptTemplate = `Based on the query below, select the most appropriate retrieval task type.

Query: %s

Reply with ONLY a single digit:
1. code_locate - find specific files, functions, or symbols in the repository
2. code_understand - explain how code works, trace execution, or understand architecture
3. memory_recall - recover past decisions, sessions, context, or timeline
4. evidence_audit - cross-check claims across multiple sources for consistency
5. general - mixed or other retrieval

Single digit:`

var taskDigitMap = map[string]TaskType{
	"1": TaskTypeCodeLocate,
	"2": TaskTypeCodeUnderstand,
	"3": TaskTypeMemoryRecall,
	"4": TaskTypeEvidenceAudit,
	"5": TaskTypeGeneral,
}

// classifyTask makes one bounded LLM call to classify the query into a task type.
// Returns TaskTypeGeneral on any error.
func classifyTask(ctx context.Context, cfg LLMConfig, prompt string) (TaskType, error) {
	llmCfg := lambdaLLMChatConfig(cfg)
	llmCfg.MaxIterations = 1
	llmCfg.MaxTokens = 16

	llm, err := engine.NewLLMChatEngine(llmCfg)
	if err != nil {
		return TaskTypeGeneral, fmt.Errorf("lambda classify: init LLM: %w", err)
	}

	output, err := llm.Run(ctx, engine.EngineInput{
		SystemPrompt: "You are a task classifier. Reply with exactly one digit and nothing else.",
		Messages:     []engine.Message{engine.NewUserMessage(fmt.Sprintf(classifyPromptTemplate, truncateRLMText(prompt, 500)))},
	})
	if err != nil {
		return TaskTypeGeneral, fmt.Errorf("lambda classify: LLM call: %w", err)
	}

	resp := strings.TrimSpace(output.AssistantText)
	return parseTaskType(resp), nil
}

func lambdaLLMChatConfig(cfg LLMConfig) engine.LLMChatConfig {
	llmCfg := engine.DefaultLLMChatConfig()
	if strings.TrimSpace(cfg.Provider) != "" {
		llmCfg.Provider = strings.TrimSpace(cfg.Provider)
	}
	if strings.TrimSpace(cfg.APIKey) != "" {
		llmCfg.APIKey = strings.TrimSpace(cfg.APIKey)
	}
	if strings.TrimSpace(cfg.BaseURL) != "" {
		llmCfg.BaseURL = strings.TrimSpace(cfg.BaseURL)
	}
	if strings.TrimSpace(cfg.AuthMode) != "" {
		llmCfg.AuthMode = strings.TrimSpace(cfg.AuthMode)
	}
	if strings.TrimSpace(cfg.AuthHeader) != "" {
		llmCfg.AuthHeader = strings.TrimSpace(cfg.AuthHeader)
	}
	if cfg.AuthPrefix != "" {
		llmCfg.AuthPrefix = cfg.AuthPrefix
	}
	if strings.TrimSpace(cfg.Model) != "" {
		llmCfg.Model = strings.TrimSpace(cfg.Model)
	}
	if cfg.Timeout > 0 {
		llmCfg.Timeout = cfg.Timeout
	}
	if cfg.MaxTokens > 0 {
		llmCfg.MaxTokens = cfg.MaxTokens
	}
	if cfg.Temperature != 0 {
		llmCfg.Temperature = cfg.Temperature
	}
	if cfg.MaxIterations > 0 {
		llmCfg.MaxIterations = cfg.MaxIterations
	}
	return llmCfg
}

// parseTaskType extracts the first digit from the response and maps it.
func parseTaskType(response string) TaskType {
	for _, ch := range response {
		if ch >= '1' && ch <= '5' {
			if tt, ok := taskDigitMap[string(ch)]; ok {
				return tt
			}
		}
	}
	return TaskTypeGeneral
}

// estimateProblemSize estimates the candidate search space size for planning.
//
// For code retrieval tasks, this approximates the number of potentially relevant
// files/symbols. For memory tasks, it estimates the number of memory entries.
// Returns a rough integer used by PlanLambda to compute k* and depth.
func estimateProblemSize(task Task, env Environment) int {
	// Use repo handle count and tool surface as a proxy for problem space.
	n := len(env.RepoHandles) + len(env.VaultHandles) + len(env.SceneHandles) + len(env.ArtifactHandles)
	if n == 0 {
		// Default heuristic: assume a moderate-sized search problem.
		n = 50
	}
	// Scale by prompt length as a rough complexity proxy.
	words := len(strings.Fields(task.Prompt))
	if words > 20 {
		n = n * 2
	}
	if n < 5 {
		n = 5
	}
	return n
}

// queryVariants produces k varied search formulations from the original query.
// Each variant targets a different search strategy.
func queryVariants(query string, k int) []string {
	if k <= 1 {
		return []string{query}
	}
	variants := make([]string, 0, k)
	variants = append(variants, query) // original

	// Variant 2: content-bearing terms. This is more useful than the prompt tail
	// for instruction-shaped queries whose final words are often output format.
	keywords := filterStopWords(query)
	if keywords != "" && keywords != query && !containsString(variants, keywords) {
		variants = append(variants, keywords)
	}

	// Variant 3: extract CamelCase / underscore tokens for symbol search.
	symbols := extractSymbolTokens(query)
	if symbols != "" && symbols != query && !containsString(variants, symbols) {
		variants = append(variants, symbols)
	}

	words := strings.Fields(query)
	if len(words) > 2 {
		// Variant 4: last N words as a fallback for short question-shaped prompts.
		tail := strings.Join(words[maxInt(0, len(words)-4):], " ")
		if tail != query && !containsString(variants, tail) {
			variants = append(variants, tail)
		}
	}

	// Pad or trim to exactly k.
	if len(variants) > k {
		variants = variants[:k]
	}
	for len(variants) < k {
		variants = append(variants, query)
	}
	return variants
}

func extractSymbolTokens(query string) string {
	var parts []string
	var current strings.Builder
	prevLower := false
	for _, ch := range query {
		if ch >= 'A' && ch <= 'Z' && prevLower {
			if current.Len() > 1 {
				parts = append(parts, current.String())
			}
			current.Reset()
		}
		if isIdentChar(ch) {
			current.WriteRune(ch)
			prevLower = ch >= 'a' && ch <= 'z'
		} else {
			if current.Len() > 1 {
				parts = append(parts, current.String())
			}
			current.Reset()
			prevLower = false
		}
	}
	if current.Len() > 1 {
		parts = append(parts, current.String())
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ")
}

func isIdentChar(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_'
}

var stopWords = map[string]struct{}{
	"the": {}, "a": {}, "an": {}, "is": {}, "are": {}, "was": {}, "were": {},
	"be": {}, "been": {}, "being": {}, "have": {}, "has": {}, "had": {},
	"do": {}, "does": {}, "did": {}, "will": {}, "would": {}, "could": {},
	"should": {}, "may": {}, "might": {}, "can": {}, "shall": {},
	"what": {}, "where": {}, "when": {}, "how": {}, "which": {}, "who": {},
	"that": {}, "this": {}, "these": {}, "those": {}, "it": {},
	"in": {}, "on": {}, "at": {}, "to": {}, "for": {}, "of": {}, "with": {},
	"by": {}, "from": {}, "up": {}, "about": {}, "into": {}, "through": {},
	"and": {}, "or": {}, "but": {}, "not": {}, "no": {},
}

func filterStopWords(query string) string {
	words := strings.Fields(query)
	var filtered []string
	for _, w := range words {
		if _, ok := stopWords[strings.ToLower(w)]; !ok && len(w) > 1 {
			filtered = append(filtered, w)
		}
	}
	return strings.Join(filtered, " ")
}

// jsonArgs builds a json.RawMessage from key-value pairs.
func jsonArgs(kv map[string]any) json.RawMessage {
	b, _ := json.Marshal(kv)
	return b
}

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/rlm/lambda_runner.go
```go
package rlm

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/joshka0/foxctl/internal/runtime/engine"
)

// LambdaRunner executes deterministic recursive decomposition over the tool surface.
//
// Unlike LLMRunner (which uses an LLM tool loop for every iteration), LambdaRunner:
//  1. Classifies the task (1 LLM call)
//  2. Computes an optimal decomposition plan (0 LLM calls, pure math)
//  3. Executes Phi = Split -> Map(Phi) -> Reduce deterministically
//  4. Only calls the LLM at leaf nodes (evidence judgment) and for composition
//
// Guarantees:
//   - Termination: depth is analytically bounded
//   - Bounded cost: cost estimate computed before execution
//   - No LLM-generated control code: structure is deterministic Go
type LambdaRunner struct {
	Config LambdaConfig
	Tools  ToolExecutor
}

// Run implements Runner.
func (r LambdaRunner) Run(ctx context.Context, task Task, env Environment) (Result, error) {
	if err := ValidateTask(task); err != nil {
		return Result{}, err
	}
	if err := ValidateEnvironment(env); err != nil {
		return Result{}, err
	}
	if r.Tools == nil {
		return Result{}, fmt.Errorf("rlm lambda runner requires tool adapter")
	}

	cfg := r.Config.Defaults()
	if cfg.EphemeralSkills {
		result, err := r.runEphemeralHelper(ctx, task, cfg)
		if err != nil {
			return Result{}, err
		}
		if result.Metadata == nil {
			result.Metadata = map[string]any{}
		}
		result.Metadata["plan_mode"] = string(PlanModeLambda)
		result.Metadata["lambda_mode"] = "ephemeral_helper"
		return result, nil
	}

	// Phase 1: Classify task (1 LLM call).
	taskType, classifyErr := classifyTask(ctx, cfg.LLM, task.Prompt)
	if classifyErr != nil {
		taskType = TaskTypeGeneral
	}

	// Phase 2: Estimate problem size and compute plan (0 LLM calls).
	n := estimateProblemSize(task, env)
	plan := PlanLambda(taskType, n, cfg)
	plan, planCaps := capLambdaPlanForTask(plan, task, cfg)

	// Phase 3: Execute Phi.
	result, err := r.executePhi(ctx, task, env, plan)
	if err != nil {
		return Result{}, err
	}

	// Attach execution metadata.
	if result.Metadata == nil {
		result.Metadata = map[string]any{}
	}
	result.Metadata["lambda_task_type"] = string(taskType)
	result.Metadata["lambda_k_star"] = plan.KStar
	result.Metadata["lambda_tau_star"] = plan.TauStar
	result.Metadata["lambda_depth"] = plan.Depth
	result.Metadata["lambda_cost_estimate"] = plan.CostEstimate
	result.Metadata["lambda_n"] = plan.N
	result.Metadata["lambda_compose_op"] = string(plan.ComposeOp)
	result.Metadata["plan_mode"] = string(PlanModeLambda)
	for key, value := range planCaps {
		result.Metadata[key] = value
	}
	if classifyErr != nil {
		result.Metadata["lambda_classify_error"] = classifyErr.Error()
	}
	return result, nil
}

// executePhi runs the recursive combinator chain.
//
//	Phi(P):
//	  if |P| <= tau*: return leaf(P)       // LLM judges evidence
//	  chunks = split(P, k*)
//	  partials = [Phi(chunk) for chunk in chunks]
//	  return reduce(composeOp, partials)    // deterministic or 1 LLM call
func (r LambdaRunner) executePhi(ctx context.Context, task Task, env Environment, plan LambdaPlan) (Result, error) {
	if plan.Depth == 0 || plan.KStar <= 1 {
		return r.leaf(ctx, task, env, plan)
	}

	// Split: produce k* subproblems by running parallel search strategies.
	chunks := r.split(ctx, task, env, plan)

	// Map: recurse into each chunk. Store results by split index so concurrent
	// completion order cannot affect reduce input order.
	var mu sync.Mutex
	partialsByIndex := make([]Result, len(chunks))
	partialOK := make([]bool, len(chunks))
	childErrorCount := 0
	var wg sync.WaitGroup
	for i, chunk := range chunks {
		wg.Add(1)
		go func(index int, childTask Task) {
			defer wg.Done()
			childPlan := LambdaPlan{
				TaskType:  plan.TaskType,
				ComposeOp: plan.ComposeOp,
				KStar:     plan.KStar,
				TauStar:   plan.TauStar,
				Depth:     plan.Depth - 1,
			}
			childResult, err := r.executePhi(ctx, childTask, env, childPlan)
			if err != nil {
				mu.Lock()
				childErrorCount++
				mu.Unlock()
				return // graceful degradation
			}
			mu.Lock()
			partialsByIndex[index] = childResult
			partialOK[index] = true
			mu.Unlock()
		}(i, chunk)
	}
	wg.Wait()

	partials := make([]Result, 0, len(chunks))
	for i := range partialsByIndex {
		if partialOK[i] {
			partials = append(partials, partialsByIndex[i])
		}
	}

	if len(partials) == 0 {
		return Result{Answer: "No evidence found after recursive decomposition.", Metadata: map[string]any{
			"lambda_no_partials":       true,
			"lambda_child_error_count": childErrorCount,
		}}, nil
	}

	// Reduce: compose partial results.
	result, err := r.reduce(ctx, task, plan, partials)
	if err != nil {
		return Result{}, err
	}
	if result.Metadata == nil {
		result.Metadata = map[string]any{}
	}
	if childErrorCount > 0 {
		result.Metadata["lambda_child_error_count"] = childErrorCount
	}
	return result, nil
}

// leaf runs one bounded search + LLM judgment cycle.
//
// This is the ONLY place the LLM is used for evidence reasoning:
//  1. Call the primary search tool deterministically
//  2. Load top candidates deterministically
//  3. Ask the LLM: "Given this evidence, answer the question" (1 call)
func (r LambdaRunner) leaf(ctx context.Context, task Task, env Environment, plan LambdaPlan) (Result, error) {
	cfg := r.Config.Defaults()
	searchTool := SearchToolForTask(plan.TaskType)

	// Step 1: Run search deterministically (no LLM).
	searchResult, err := r.Tools.Execute(ctx, searchTool, jsonArgs(map[string]any{
		"query":  task.Prompt,
		"limit":  plan.TauStar,
		"budget": map[string]any{"max_candidates": plan.TauStar},
	}))
	if err != nil {
		return Result{}, fmt.Errorf("lambda leaf search: %w", err)
	}

	// Extract candidate paths from search result.
	candidates := extractCandidatePaths(searchResult, task.WorkspaceRoot)
	evidenceRefs := extractEvidenceRefs(searchResult)

	// Step 2: Load top candidates deterministically (no LLM).
	var loadedSnippets []string
	for i, path := range candidates {
		if i >= plan.TauStar {
			break
		}
		loadResult, err := r.Tools.Execute(ctx, "load_file", jsonArgs(map[string]any{
			"path": path,
		}))
		if err != nil {
			continue
		}
		if snippet := extractTextFromToolResult(loadResult, 2000); snippet != "" {
			loadedSnippets = append(loadedSnippets, fmt.Sprintf("File: %s\n%s", path, snippet))
		}
	}

	// Step 3: Ask the LLM to judge the evidence (1 call).
	var evidenceBlock string
	if len(loadedSnippets) > 0 {
		evidenceBlock = strings.Join(loadedSnippets, "\n---\n")
	} else {
		evidenceBlock = formatMapAsText(searchResult)
	}

	answer, err := r.judgeEvidence(ctx, cfg.LLM, task.Prompt, evidenceBlock)
	if err != nil {
		// Fallback: return the search result summary without LLM judgment.
		return Result{
			Answer:         formatMapAsText(searchResult),
			EvidenceRefs:   evidenceRefs,
			RetrievedPaths: candidates,
			Iterations:     1,
			Subcalls:       1,
			Metadata: map[string]any{
				"lambda_leaf":           true,
				"lambda_judge_error":    err.Error(),
				"search_tool":           searchTool,
				"candidates_found":      len(candidates),
				"candidate_paths":       candidates,
				"files_loaded":          len(loadedSnippets),
				"retrieved_path_source": "candidate_paths_fallback",
			},
		}, nil
	}

	retrievedPaths, answerPaths, pathSource := selectLambdaRetrievedPaths(answer, candidates, task.WorkspaceRoot)

	return Result{
		Answer:         answer,
		EvidenceRefs:   evidenceRefs,
		RetrievedPaths: retrievedPaths,
		Iterations:     1,
		Subcalls:       1 + len(loadedSnippets),
		Metadata: map[string]any{
			"lambda_leaf":           true,
			"search_tool":           searchTool,
			"candidates_found":      len(candidates),
			"candidate_paths":       candidates,
			"answer_paths":          answerPaths,
			"retrieved_path_source": pathSource,
			"files_loaded":          len(loadedSnippets),
		},
	}, nil
}

// split produces k* subproblems by running parallel searches with varied query formulations.
// This is deterministic Go code -- no LLM involved.
func (r LambdaRunner) split(ctx context.Context, task Task, _ Environment, plan LambdaPlan) []Task {
	variants := queryVariants(task.Prompt, plan.KStar)
	tasks := make([]Task, 0, len(variants))
	for _, variant := range variants {
		tasks = append(tasks, Task{
			Prompt:        variant,
			WorkspaceID:   task.WorkspaceID,
			WorkspaceRoot: task.WorkspaceRoot,
			MaxDepth:      task.MaxDepth,
			MaxIterations: 1,
			MaxSubcalls:   1,
		})
	}
	return tasks
}

// reduce merges partial results using the composition operator.
func (r LambdaRunner) reduce(ctx context.Context, task Task, plan LambdaPlan, partials []Result) (Result, error) {
	cfg := r.Config.Defaults()

	var answer string
	var evidence []string
	var paths []string
	var candidatePaths []string
	var totalIterations int
	var totalSubcalls int

	for _, p := range partials {
		evidence = append(evidence, p.EvidenceRefs...)
		paths = append(paths, p.RetrievedPaths...)
		candidatePaths = append(candidatePaths, stringSliceFromAny(p.Metadata["candidate_paths"])...)
		totalIterations += p.Iterations
		totalSubcalls += p.Subcalls
	}
	evidence = uniqueStringsRLM(evidence)
	paths = uniqueStringsRLM(paths)
	candidatePaths = uniqueStringsRLM(candidatePaths)
	if len(candidatePaths) == 0 {
		candidatePaths = paths
	}

	switch plan.ComposeOp {
	case ComposeUnion:
		answer = composeUnion(partials)

	case ComposeIntersection:
		answer, paths = composeIntersection(partials, paths)

	case ComposeChronological:
		answer = composeChronological(partials)

	case ComposeRerank:
		// 1 LLM call to re-rank all collected evidence.
		synthesized, err := r.judgeEvidence(ctx, cfg.LLM, task.Prompt,
			formatPartialsAsEvidence(partials))
		if err != nil {
			answer = composeUnion(partials)
		} else {
			answer = synthesized
		}

	case ComposeSynthesize:
		// 1 LLM call to synthesize partial answers.
		synthesized, err := r.synthesizePartials(ctx, cfg.LLM, task.Prompt, partials)
		if err != nil {
			answer = composeUnion(partials)
		} else {
			answer = synthesized
		}

	default:
		answer = composeUnion(partials)
	}
	finalPaths, answerPaths, pathSource := selectLambdaRetrievedPaths(answer, candidatePaths, task.WorkspaceRoot)

	return Result{
		Answer:         answer,
		EvidenceRefs:   evidence,
		RetrievedPaths: finalPaths,
		Iterations:     totalIterations,
		Subcalls:       totalSubcalls,
		Metadata: map[string]any{
			"lambda_reduce":         true,
			"compose_op":            string(plan.ComposeOp),
			"partial_count":         len(partials),
			"total_iterations":      totalIterations,
			"candidate_path_count":  len(candidatePaths),
			"answer_paths":          answerPaths,
			"retrieved_path_source": pathSource,
		},
	}, nil
}

// judgeEvidence asks the LLM to answer a question given collected evidence (1 call).
func (r LambdaRunner) judgeEvidence(ctx context.Context, cfg LLMConfig, query, evidence string) (string, error) {
	llmCfg := lambdaLLMChatConfig(cfg)
	llmCfg.MaxIterations = 1

	llm, err := engine.NewLLMChatEngine(llmCfg)
	if err != nil {
		return "", fmt.Errorf("lambda judge: init LLM: %w", err)
	}

	prompt := fmt.Sprintf(`Based on the following evidence, answer the question concisely.
Cite specific file paths when evidence supports your answer.
If the evidence is insufficient, say so explicitly.

Question: %s

Evidence:
%s

Answer:`, query, truncateRLMText(evidence, 8000))

	output, err := llm.Run(ctx, engine.EngineInput{
		SystemPrompt: "You are an evidence-based reasoning assistant. Answer only from the provided evidence. Be concise.",
		Messages:     []engine.Message{engine.NewUserMessage(prompt)},
	})
	if err != nil {
		return "", fmt.Errorf("lambda judge: LLM call: %w", err)
	}
	return strings.TrimSpace(output.AssistantText), nil
}

// synthesizePartials asks the LLM to merge multiple partial answers (1 call).
func (r LambdaRunner) synthesizePartials(ctx context.Context, cfg LLMConfig, query string, partials []Result) (string, error) {
	llmCfg := lambdaLLMChatConfig(cfg)
	llmCfg.MaxIterations = 1

	llm, err := engine.NewLLMChatEngine(llmCfg)
	if err != nil {
		return "", fmt.Errorf("lambda synthesize: init LLM: %w", err)
	}

	var partialTexts []string
	for i, p := range partials {
		partialTexts = append(partialTexts, fmt.Sprintf("Partial %d:\n%s", i+1, truncateRLMText(p.Answer, 2000)))
	}

	prompt := fmt.Sprintf(`Synthesize these partial findings into one complete, accurate answer.

Question: %s

%s

Synthesized answer:`, query, strings.Join(partialTexts, "\n\n"))

	output, err := llm.Run(ctx, engine.EngineInput{
		SystemPrompt: "Synthesize multiple partial findings into one coherent answer. Preserve all key facts.",
		Messages:     []engine.Message{engine.NewUserMessage(prompt)},
	})
	if err != nil {
		return "", fmt.Errorf("lambda synthesize: LLM call: %w", err)
	}
	return strings.TrimSpace(output.AssistantText), nil
}

// --- Deterministic composition helpers ---

func composeUnion(partials []Result) string {
	var parts []string
	for _, p := range partials {
		if strings.TrimSpace(p.Answer) != "" {
			parts = append(parts, p.Answer)
		}
	}
	return strings.Join(parts, "\n\n")
}

func composeIntersection(partials []Result, paths []string) (string, []string) {
	// Keep only paths that appear in 2+ partials.
	pathCount := make(map[string]int)
	for _, p := range partials {
		seen := make(map[string]struct{})
		for _, path := range p.RetrievedPaths {
			if _, ok := seen[path]; !ok {
				pathCount[path]++
				seen[path] = struct{}{}
			}
		}
	}
	var shared []string
	for path, count := range pathCount {
		if count >= 2 {
			shared = append(shared, path)
		}
	}
	sort.Strings(shared)

	var parts []string
	if len(shared) > 0 {
		parts = append(parts, "Cross-confirmed paths: "+strings.Join(shared, ", "))
	}
	for _, p := range partials {
		if strings.TrimSpace(p.Answer) != "" {
			parts = append(parts, p.Answer)
		}
	}
	return strings.Join(parts, "\n\n"), shared
}

func composeChronological(partials []Result) string {
	// Sort partials by any timestamp metadata, then concatenate.
	sorted := make([]Result, len(partials))
	copy(sorted, partials)
	sort.SliceStable(sorted, func(i, j int) bool {
		ti, _ := time.Parse(time.RFC3339, stringFromAny(sorted[i].Metadata["timestamp"]))
		tj, _ := time.Parse(time.RFC3339, stringFromAny(sorted[j].Metadata["timestamp"]))
		return ti.Before(tj)
	})
	var parts []string
	for _, p := range sorted {
		if strings.TrimSpace(p.Answer) != "" {
			parts = append(parts, p.Answer)
		}
	}
	return strings.Join(parts, "\n\n")
}

// --- Helpers ---

func formatPartialsAsEvidence(partials []Result) string {
	var parts []string
	for i, p := range partials {
		var pathsStr string
		if len(p.RetrievedPaths) > 0 {
			pathsStr = fmt.Sprintf("\nRetrieved paths: %s", strings.Join(p.RetrievedPaths, ", "))
		}
		parts = append(parts, fmt.Sprintf("--- Finding %d ---%s\n%s",
			i+1, pathsStr, truncateRLMText(p.Answer, 3000)))
	}
	return strings.Join(parts, "\n\n")
}

func extractCandidatePaths(result map[string]any, workspaceRoot string) []string {
	var paths []string
	collectPathsRecursive(result, workspaceRoot, &paths)
	if len(paths) == 0 {
		var decoded any
		if body, err := json.Marshal(result); err == nil && json.Unmarshal(body, &decoded) == nil {
			collectPathsRecursive(decoded, workspaceRoot, &paths)
		}
	}
	return uniqueStringsRLM(paths)
}

func extractEvidenceRefs(result map[string]any) []string {
	if refs, ok := result["evidence_refs"].([]string); ok {
		return refs
	}
	if refs, ok := result["evidence_refs"].([]any); ok {
		var out []string
		for _, r := range refs {
			if s, ok := r.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func extractTextFromToolResult(result map[string]any, maxLen int) string {
	// Try common field names from the tool result shape.
	for _, key := range []string{"content", "text", "snippet", "body", "output"} {
		if v, ok := result[key]; ok {
			if s, ok := v.(string); ok {
				return truncateRLMText(s, maxLen)
			}
		}
	}
	// Fallback: serialize the whole result.
	return truncateRLMText(formatMapAsText(result), maxLen)
}

func formatMapAsText(m map[string]any) string {
	if m == nil {
		return ""
	}
	b, err := json.Marshal(m)
	if err != nil {
		return fmt.Sprintf("%v", m)
	}
	return string(b)
}

func stringFromAny(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

// extractAnswerPaths pulls file paths from the LLM answer text using backtick-wrapped
// paths (e.g. `internal/rlm/runner.go`) and code-fenced paths, plus bare paths on
// lines starting with list markers.
var answerPathRE = regexp.MustCompile(`(?:^|[\s"` + "`" + `(]|\*)([A-Za-z0-9_./-]+\.(?:go|md|yaml|yml|tf|sh|json|sql|toml|txt))(?:[` + "`" + `\s)"',:;]|$)`)

func extractAnswerPaths(answer, workspaceRoot string) []string {
	matches := answerPathRE.FindAllStringSubmatch(answer, -1)
	var paths []string
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		p := normalizeRetrievedPath(m[1], workspaceRoot)
		if p != "" {
			paths = append(paths, p)
		}
	}
	return uniqueStringsRLM(paths)
}

func selectLambdaRetrievedPaths(answer string, candidatePaths []string, workspaceRoot string) ([]string, []string, string) {
	answerPaths := extractAnswerPaths(answer, workspaceRoot)
	if len(answerPaths) == 0 {
		return uniqueStringsRLM(candidatePaths), nil, "candidate_paths_fallback"
	}
	return answerPaths, answerPaths, "answer_paths"
}

func stringSliceFromAny(value any) []string {
	switch v := value.(type) {
	case []string:
		return append([]string(nil), v...)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/skills/session_recall/skill.yaml
```yaml
apiVersion: foxctl/v1
kind: Skill
metadata:
  name: session/recall
  version: 0.1.0
  description: "Semantic retrieval of relevant past sessions using embeddings"
  tags: ["session", "memory", "retrieval", "embeddings", "semantic-search"]
distribution:
  type: exec
  exec:
    entry: skills/session_recall/session_recall
io:
  format: JSON
  inline_output_kb: 32
signature:
  command: session/recall
  parameters:
    - name: query
      type: string
      required: false
      description: "Natural language query to find relevant sessions (required unless list_sessions or toc mode is used)"
    - name: limit
      type: integer
      required: false
      description: "Maximum number of sessions to return (default: 5)"
    - name: min_similarity
      type: number
      required: false
      description: "Minimum cosine similarity threshold (default: 0.3)"
    - name: workspace
      type: string
      required: false
      description: "Filter by workspace path (optional)"
    - name: project
      type: string
      required: false
      description: "Filter by project name (optional)"
    - name: session_id
      type: string
      required: false
      description: "Restrict search to a specific session ID"
    - name: list_sessions
      type: boolean
      required: false
      description: "List available sessions instead of searching"
    - name: toc
      type: boolean
      required: false
      description: "Return TOC labels for a session (requires session_id)"
    - name: window_granularity
      type: boolean
      required: false
      description: "Search at context window level (default: false)"
    - name: chunk_granularity
      type: boolean
      required: false
      description: "Search at chunk level (requires embeddings)"
    - name: annotation_granularity
      type: boolean
      required: false
      description: "Search at annotation level (requires embeddings)"
    - name: filter_category
      type: string
      required: false
      description: "Annotation category filter (decision, debug, code_change, etc.); annotation mode only"
    - name: sort_by
      type: string
      required: false
      description: "Comma-separated annotation sort keys: similarity, date, recent (annotation mode only)"
  help:
    short: "Recall relevant sessions via semantic search."
    long: |
      Use session/recall to find past sessions matching a query.
      Example commands:
        foxctl run session/recall --input '{"query":"authentication flow","limit":3}'
        foxctl skills run session/recall --query "authentication flow" --limit 3
    workflows:
      - id: recall-query
        description: "Recall sessions for a query."
        example_input:
          query: "authentication flow"
          limit: 3
  returns:
    - name: query
      type: string
      description: "The search query"
    - name: matches
      type: array
      description: "Matching sessions with similarity scores"
    - name: window_matches
      type: array
      description: "Matching context windows with similarity scores"
    - name: chunk_matches
      type: array
      description: "Matching chunks with similarity scores"
    - name: annotation_matches
      type: array
      description: "Matching turn annotations with similarity scores"
    - name: total_with_embeddings
      type: integer
      description: "Total sessions with embeddings available"
    - name: status
      type: string
      description: "Status of the recall operation"
    - name: message
      type: string
      description: "Human-readable status message"
capabilities:
  network: "egress"
  filesystem:
    - type: workdir
  pure: false
examples:
  - name: "Find authentication sessions"
    input:
      query: "How did we implement authentication?"
      limit: 3
    description: "Find past sessions related to authentication implementation"
  - name: "Find error handling patterns"
    input:
      query: "sessions where we fixed errors or handled exceptions"
      min_similarity: 0.4
    description: "Find sessions dealing with error handling"
  - name: "Project-specific recall"
    input:
      query: "database migrations"
      project: "foxctl"
      limit: 5
    description: "Find database migration sessions for a specific project"
  - name: "Decision evolution"
    input:
      query: "database migration strategy"
      annotation_granularity: true
      filter_category: "decision"
      sort_by: "date,similarity"
      limit: 20
    description: "Track how database migration decisions evolved over time"
openapi:
  enabled: true
  methods:
    GET: "true"
    POST: "true"

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/rlm/env/bootstrap.go
```go
package env

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/joshka0/foxctl/internal/context/contextplane"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex"
	"github.com/joshka0/foxctl/internal/intelligence/repoquery"
	"github.com/joshka0/foxctl/internal/platform/config"
	ws "github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/rlm"
	"github.com/joshka0/foxctl/internal/storage/obsidianindex"
	"github.com/joshka0/foxctl/internal/storage/trajectory"
)

const (
	defaultRepoHandleLimit  = 5
	defaultVaultHandleLimit = 5
	defaultThreadLimit      = 5
	defaultArtifactLimit    = 5
)

// BootstrapConfig configures the environment bootstrap.
type BootstrapConfig struct {
	AppConfig        config.Config
	VaultPath        string
	CompanionDB      *sql.DB
	RepoHandleLimit  int
	VaultHandleLimit int
	ThreadLimit      int
	ArtifactLimit    int
}

// Bootstrapper builds a typed read-only RLM environment from current foxctl state.
type Bootstrapper struct {
	cfg BootstrapConfig
}

// NewBootstrapper creates a new environment bootstrapper.
func NewBootstrapper(cfg BootstrapConfig) *Bootstrapper {
	if cfg.RepoHandleLimit <= 0 {
		cfg.RepoHandleLimit = defaultRepoHandleLimit
	}
	if cfg.VaultHandleLimit <= 0 {
		cfg.VaultHandleLimit = defaultVaultHandleLimit
	}
	if cfg.ThreadLimit <= 0 {
		cfg.ThreadLimit = defaultThreadLimit
	}
	if cfg.ArtifactLimit <= 0 {
		cfg.ArtifactLimit = defaultArtifactLimit
	}
	return &Bootstrapper{cfg: cfg}
}

// Build prepares an RLM environment from ACA, repo index, vault index, and optional companion state.
func (b *Bootstrapper) Build(ctx context.Context, task rlm.Task) (rlm.Environment, error) {
	env := rlm.Environment{
		Tools: DefaultTools(),
	}

	workspaceRoot := ws.Normalize(strings.TrimSpace(task.WorkspaceRoot))
	if workspaceRoot == "" {
		return env, nil
	}
	store := contextplane.NewWorkspaceStore(workspaceRoot)
	layout := store.Layout()

	top, err := readJSONFile(layout.TopOfMindPath)
	if err != nil {
		return env, err
	}
	env.TopOfMind = top

	handoff, handoffRefs, err := latestHandoff(layout.HandoffsDir)
	if err != nil {
		return env, err
	}
	env.LatestHandoff = handoff
	env.ArtifactHandles = uniqueStrings(append(env.ArtifactHandles, handoffRefs...))
	env.ArtifactHandles = uniqueStrings(append(env.ArtifactHandles, topOfMindRefs(top)...))

	threadIDs, sceneHandles, err := loadCompanionHandles(ctx, b.cfg.CompanionDB, b.cfg.ThreadLimit)
	if err != nil {
		return env, err
	}
	env.ActiveThreadIDs = threadIDs
	env.SceneHandles = sceneHandles

	repoHandles, err := b.loadRepoHandles(ctx, workspaceRoot, firstNonEmpty(task.Prompt, fmt.Sprint(top["objective"])))
	if err != nil {
		return env, err
	}
	env.RepoHandles = repoHandles

	vaultHandles, err := b.loadVaultHandles(ctx, firstNonEmpty(task.Prompt, fmt.Sprint(top["objective"])))
	if err != nil {
		return env, err
	}
	env.VaultHandles = vaultHandles

	artifactHandles, err := b.loadTrajectoryHandles(ctx, workspaceRoot)
	if err != nil {
		return env, err
	}
	env.ArtifactHandles = uniqueStrings(append(env.ArtifactHandles, artifactHandles...))

	return env, nil
}

func (b *Bootstrapper) loadRepoHandles(ctx context.Context, workspaceRoot, query string) ([]string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	store, err := repoindex.Open(ctx, b.cfg.AppConfig.Storage.Root, workspaceRoot)
	if err != nil {
		return nil, nil
	}
	defer func() { _ = store.Close() }()
	output, err := repoquery.NewQueryService(repoindex.NewQueryEngine(store)).SearchWithProjection(ctx, repoquery.SearchRequest{
		Query: query,
		Limit: b.cfg.RepoHandleLimit,
	})
	if err != nil {
		return nil, nil
	}
	out := make([]string, 0, len(output.Anchors))
	for _, anchor := range output.Anchors {
		if strings.TrimSpace(anchor.Path) != "" {
			out = append(out, "path:"+filepath.ToSlash(anchor.Path))
		}
	}
	if len(out) == 0 {
		for _, node := range output.Nodes {
			if strings.TrimSpace(node.ID) != "" {
				out = append(out, "repo:"+node.ID)
			}
		}
	}
	return uniqueStrings(out), nil
}

func (b *Bootstrapper) loadVaultHandles(ctx context.Context, query string) ([]string, error) {
	query = strings.TrimSpace(query)
	vaultPath := firstNonEmpty(strings.TrimSpace(b.cfg.VaultPath),
		strings.TrimSpace(os.Getenv("FOXCTL_RLM_VAULT_PATH")),
		strings.TrimSpace(os.Getenv("FOXCTL_ACA_VAULT_PATH")),
		strings.TrimSpace(os.Getenv("FOXCTL_OBSIDIAN_VAULT_PATH")),
	)
	if vaultPath == "" || query == "" {
		return nil, nil
	}
	index, err := obsidianindex.Open(ctx, b.cfg.AppConfig.Storage.Root, vaultPath)
	if err != nil {
		return nil, nil
	}
	defer func() { _ = index.Close() }()
	hits, err := index.SearchNotes(ctx, query, b.cfg.VaultHandleLimit)
	if err != nil {
		return nil, nil
	}
	out := make([]string, 0, len(hits))
	for _, hit := range hits {
		if strings.TrimSpace(hit.Path) != "" {
			out = append(out, "note:"+filepath.ToSlash(hit.Path))
		}
	}
	return uniqueStrings(out), nil
}

func readJSONFile(path string) (map[string]any, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(body) == 0 {
		return nil, nil
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func latestHandoff(dir string) (map[string]any, []string, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() > entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, err
		}
		var handoff contextplane.Handoff
		if err := json.Unmarshal(body, &handoff); err != nil {
			return nil, nil, err
		}
		payload := map[string]any{
			"path":       path,
			"task_id":    handoff.TaskID,
			"phase":      handoff.Phase,
			"outcome":    handoff.Outcome,
			"summary":    handoff.Summary,
			"created_at": handoff.CreatedAt,
		}
		return payload, append([]string(nil), handoff.EvidenceRefs...), nil
	}
	return nil, nil, nil
}

func topOfMindRefs(top map[string]any) []string {
	if top == nil {
		return nil
	}
	raw, ok := top["relevant_refs"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
			out = append(out, text)
		}
	}
	return out
}

func loadCompanionHandles(ctx context.Context, db *sql.DB, limit int) ([]string, []string, error) {
	if db == nil || limit <= 0 {
		return nil, nil, nil
	}
	rows, err := db.QueryContext(ctx, `
		SELECT conversation_id, MAX(created_at) AS latest
		FROM companion_turns
		GROUP BY conversation_id
		ORDER BY latest DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, nil, nil
	}
	defer rows.Close()
	threadIDs := make([]string, 0, limit)
	sceneHandles := make([]string, 0, limit*2)
	for rows.Next() {
		var conversationID string
		var latest string
		if err := rows.Scan(&conversationID, &latest); err != nil {
			return nil, nil, err
		}
		conversationID = strings.TrimSpace(conversationID)
		if conversationID == "" {
			continue
		}
		threadIDs = append(threadIDs, conversationID)
		sceneHandles = append(sceneHandles, "conversation:"+conversationID)
		episodes, err := loadEpisodeHandles(ctx, db, conversationID, 2)
		if err == nil {
			sceneHandles = append(sceneHandles, episodes...)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return uniqueStrings(threadIDs), uniqueStrings(sceneHandles), nil
}

func loadEpisodeHandles(ctx context.Context, db *sql.DB, conversationID string, limit int) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id
		FROM companion_soft_episodes
		WHERE conversation_id = ?
		ORDER BY end_event_id DESC
		LIMIT ?`, conversationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]string, 0, limit)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, fmt.Sprintf("episode:%d", id))
	}
	return out, rows.Err()
}

func (b *Bootstrapper) loadTrajectoryHandles(ctx context.Context, workspaceRoot string) ([]string, error) {
	if strings.TrimSpace(b.cfg.AppConfig.Storage.Root) == "" || strings.TrimSpace(workspaceRoot) == "" {
		return nil, nil
	}
	store, err := trajectory.Open(ctx, b.cfg.AppConfig.Storage.Root)
	if err != nil {
		return nil, nil
	}
	defer func() { _ = store.Close() }()
	trajs, err := store.ListTrajectories(ctx, trajectory.ListFilter{
		WorkspaceID: ws.ID(workspaceRoot),
		Limit:       b.cfg.ArtifactLimit,
	})
	if err != nil {
		return nil, nil
	}
	out := make([]string, 0, len(trajs)*2)
	for _, traj := range trajs {
		if strings.TrimSpace(traj.ID) != "" {
			out = append(out, "trajectory:"+traj.ID)
		}
		if strings.TrimSpace(traj.ArtifactDigest) != "" {
			out = append(out, "artifact:"+traj.ArtifactDigest)
		}
	}
	return uniqueStrings(out), nil
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/rlm/plan.go
```go
package rlm

import "strings"

type RouteProfile string

const (
	RouteProfileAuto          RouteProfile = "auto"
	RouteProfileCodeRetrieval RouteProfile = "code_retrieval"
	RouteProfileMemoryRecall  RouteProfile = "memory_recall"
	RouteProfileMixed         RouteProfile = "mixed"
	RouteProfileEvidenceAudit RouteProfile = "evidence_audit"
)

type PlanMode string

const (
	PlanModeFree   PlanMode = "free"
	PlanModeGuided PlanMode = "guided"
	PlanModeStaged PlanMode = "staged"
	PlanModeHard   PlanMode = "hard"
	PlanModeLambda PlanMode = "lambda-retrieval"
)

type Plan struct {
	RouteProfile RouteProfile `json:"route_profile"`
	Mode         PlanMode     `json:"mode"`
	Phases       []Phase      `json:"phases,omitempty"`
}

type Phase struct {
	Name           string   `json:"name"`
	Objective      string   `json:"objective"`
	AllowedTools   []string `json:"allowed_tools,omitempty"`
	RequireOneOf   []string `json:"require_one_of,omitempty"`
	MaxIterations  int      `json:"max_iterations,omitempty"`
	RequireToolUse bool     `json:"require_tool_use,omitempty"`
}

func NormalizeRouteProfile(value string) RouteProfile {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(RouteProfileAuto):
		return RouteProfileAuto
	case string(RouteProfileCodeRetrieval):
		return RouteProfileCodeRetrieval
	case string(RouteProfileMemoryRecall):
		return RouteProfileMemoryRecall
	case string(RouteProfileMixed):
		return RouteProfileMixed
	case string(RouteProfileEvidenceAudit):
		return RouteProfileEvidenceAudit
	default:
		return RouteProfileAuto
	}
}

func NormalizePlanMode(value string) PlanMode {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(PlanModeFree):
		return PlanModeFree
	case string(PlanModeGuided):
		return PlanModeGuided
	case string(PlanModeStaged):
		return PlanModeStaged
	case string(PlanModeHard):
		return PlanModeHard
	case string(PlanModeLambda):
		return PlanModeLambda
	default:
		return PlanModeFree
	}
}

func ClassifyRouteProfile(prompt string) RouteProfile {
	query := strings.ToLower(strings.TrimSpace(prompt))
	switch {
	case containsAny(query, "thread", "scene", "handoff", "session", "decision", "artifact", "timeline"):
		return RouteProfileMemoryRecall
	case containsAny(query, "code", "repo", "package", "file", "handler", "api", "storage", "auth", "runtime", "function", "symbol", "path"):
		return RouteProfileCodeRetrieval
	default:
		return RouteProfileCodeRetrieval
	}
}

func ResolveRouteProfile(prompt string, requested RouteProfile) RouteProfile {
	requested = NormalizeRouteProfile(string(requested))
	if requested != RouteProfileAuto {
		return requested
	}
	return ClassifyRouteProfile(prompt)
}

func BuildPlan(prompt string, requestedRoute RouteProfile, requestedMode PlanMode) Plan {
	mode := NormalizePlanMode(string(requestedMode))
	route := ResolveRouteProfile(prompt, requestedRoute)
	return buildPlan(route, mode)
}

func buildPlan(route RouteProfile, mode PlanMode) Plan {
	plan := Plan{
		RouteProfile: route,
		Mode:         mode,
	}
	if mode != PlanModeStaged {
		return plan
	}
	switch route {
	case RouteProfileCodeRetrieval:
		plan.Phases = []Phase{
			{
				Name:           "discovery",
				Objective:      "Find likely repository files or canonical notes that answer the query.",
				AllowedTools:   []string{"code_search_ensemble", "semantic_search_code", "smart_search_code", "search_repo", "search_vault"},
				RequireOneOf:   []string{"code_search_ensemble"},
				MaxIterations:  3,
				RequireToolUse: true,
			},
			{
				Name:           "inspection",
				Objective:      "Open and inspect the strongest candidates from discovery.",
				AllowedTools:   []string{"load_file", "read_note", "ripgrep_code"},
				RequireOneOf:   []string{"load_file", "read_note"},
				MaxIterations:  3,
				RequireToolUse: true,
			},
			{
				Name:           "verification",
				Objective:      "Cross-check the strongest candidate and confirm the best supporting paths.",
				AllowedTools:   []string{"load_file", "read_note", "ripgrep_code", "expand_repo_graph"},
				RequireOneOf:   []string{"load_file", "read_note", "ripgrep_code", "expand_repo_graph"},
				MaxIterations:  2,
				RequireToolUse: true,
			},
		}
	}
	return plan
}

func filterToolsByNames(tools []Tool, allowed []string) []Tool {
	if len(allowed) == 0 {
		return append([]Tool(nil), tools...)
	}
	allow := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		allow[strings.TrimSpace(name)] = struct{}{}
	}
	out := make([]Tool, 0, len(tools))
	for _, tool := range tools {
		if _, ok := allow[strings.TrimSpace(tool.Name)]; ok {
			out = append(out, tool)
		}
	}
	return out
}

func containsAny(query string, parts ...string) bool {
	for _, part := range parts {
		if strings.Contains(query, part) {
			return true
		}
	}
	return false
}

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/rlm/env/tools.go
```go
package env

import (
	"encoding/json"

	"github.com/joshka0/foxctl/internal/rlm"
)

// DefaultTools returns the initial read-only tool surface for the experimental RLM runtime.
func DefaultTools() []rlm.Tool {
	return []rlm.Tool{
		{
			Name:        "get_top_of_mind",
			Description: "Load the ACA top-of-mind bundle for the workspace.",
			Parameters:  emptyObjectSchema(),
			ReadOnly:    true,
		},
		{
			Name:        "get_latest_handoff",
			Description: "Load the latest ACA handoff for the workspace.",
			Parameters:  emptyObjectSchema(),
			ReadOnly:    true,
		},
		{
			Name:        "search_scenes",
			Description: "Search companion scenes or episodes by summary/topic.",
			Parameters: objectSchema(map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Text query for matching scene or episode summaries.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum number of results to return. Defaults to 10.",
				},
			}),
			ReadOnly: true,
		},
		{
			Name:        "get_scene",
			Description: "Load one companion scene or episode by handle.",
			Parameters: objectSchema(map[string]any{
				"handle": map[string]any{
					"type":        "string",
					"description": "Scene handle such as episode:42 or conversation:<id>.",
				},
			}, "handle"),
			ReadOnly: true,
		},
		{
			Name:        "search_artifacts",
			Description: "Search persisted artifacts and trajectory handles by query.",
			Parameters: objectSchema(map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Text query to match artifact or trajectory handles.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum number of handles to return. Defaults to 10.",
				},
			}),
			ReadOnly: true,
		},
		{
			Name:        "load_artifact",
			Description: "Load one bounded artifact, trajectory, event, file, or note by handle.",
			Parameters: objectSchema(map[string]any{
				"handle": map[string]any{
					"type":        "string",
					"description": "Artifact handle such as trajectory:<id>, artifact:<digest>, event:<trajectory>:<event>, path:<file>, or note:<path>.",
				},
			}, "handle"),
			ReadOnly: true,
		},
		{
			Name:        "semantic_search_code",
			Description: "Preferred first-pass repo understanding tool. Run the repo-aware code semantic search skill over the current workspace.",
			Parameters: objectSchema(map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Semantic code query for files, symbols, packages, or architectural concepts.",
				},
				"scope": map[string]any{
					"type":        "array",
					"description": "Optional semantic_search scopes such as symbols, codemaps, sessions, memories, tasks, or context.",
					"items":       map[string]any{"type": "string"},
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum number of semantic search results to return.",
				},
				"repo_index_mode": map[string]any{
					"type":        "string",
					"description": "Optional repo index mode override, such as off, search, or dag.",
				},
			}, "query"),
			ReadOnly: true,
		},
		{
			Name:        "smart_search_code",
			Description: "Preferred follow-up repo understanding tool. Run the smart code search pipeline to produce candidate files and snippets.",
			Parameters: objectSchema(map[string]any{
				"question": map[string]any{
					"type":        "string",
					"description": "Natural-language coding question to answer with code candidates and snippets.",
				},
				"repo_index_mode": map[string]any{
					"type":        "string",
					"description": "Optional repo index mode override, such as off, search, or dag.",
				},
				"max_candidates": map[string]any{
					"type":        "integer",
					"description": "Optional maximum number of candidate files to consider.",
				},
				"max_snippets": map[string]any{
					"type":        "integer",
					"description": "Optional maximum number of snippets to emit.",
				},
			}, "question"),
			ReadOnly: true,
		},
		{
			Name:        "ripgrep_code",
			Description: "Use for exact text, symbol, or literal pattern searches after semantic search narrows the area.",
			Parameters: objectSchema(map[string]any{
				"pattern": map[string]any{
					"type":        "string",
					"description": "Regex or literal search pattern for code/context_ripgrep.",
				},
				"path": map[string]any{
					"type":        "string",
					"description": "Optional workspace-relative path to limit the search.",
				},
				"glob": map[string]any{
					"type":        "array",
					"description": "Optional file glob filters.",
					"items":       map[string]any{"type": "string"},
				},
				"glob_not": map[string]any{
					"type":        "array",
					"description": "Optional negative glob filters.",
					"items":       map[string]any{"type": "string"},
				},
				"max_matches": map[string]any{
					"type":        "integer",
					"description": "Optional maximum raw matches.",
				},
				"max_blocks": map[string]any{
					"type":        "integer",
					"description": "Optional maximum expanded code blocks.",
				},
			}, "pattern"),
			ReadOnly: true,
		},
		{
			Name:        "search_repo",
			Description: "Fallback shallow repo graph search over projected file anchors. Prefer semantic_search_code or smart_search_code first for repo understanding queries.",
			Parameters: objectSchema(map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Repo query text, such as a subsystem, symbol, path fragment, or concept.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum number of projected repo results to return.",
				},
			}, "query"),
			ReadOnly: true,
		},
		{
			Name:        "expand_repo_graph",
			Description: "Expand the repo graph around one seed handle.",
			Parameters: objectSchema(map[string]any{
				"seed": map[string]any{
					"type":        "string",
					"description": "Repo seed handle to expand from, usually a repo:<node> or path:<file> style handle.",
				},
				"depth": map[string]any{
					"type":        "integer",
					"description": "Graph expansion depth. Defaults to the adapter defaults when omitted.",
				},
				"budget": map[string]any{
					"type":        "integer",
					"description": "Maximum expansion budget for returned anchors.",
				},
			}, "seed"),
			ReadOnly: true,
		},
		{
			Name:        "load_file",
			Description: "Load a bounded file slice from the workspace.",
			Parameters: objectSchema(map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Absolute or workspace-relative file path.",
				},
				"start_line": map[string]any{
					"type":        "integer",
					"description": "Optional 1-based start line for slicing.",
				},
				"end_line": map[string]any{
					"type":        "integer",
					"description": "Optional 1-based end line for slicing.",
				},
			}, "path"),
			ReadOnly: true,
		},
		{
			Name:        "search_vault",
			Description: "Search the Obsidian or vault knowledge plane for relevant notes.",
			Parameters: objectSchema(map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Vault search query text.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum number of note hits to return.",
				},
			}, "query"),
			ReadOnly: true,
		},
		{
			Name:        "read_note",
			Description: "Load one durable note by note handle or vault-relative path.",
			Parameters: objectSchema(map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Vault-relative note path or note:<path> handle.",
				},
			}, "path"),
			ReadOnly: true,
		},
		{
			Name:        "memory_ensemble_retrieve",
			Description: "Run one bounded memory scout ensemble over the current environment using focused scout roles.",
			Parameters: objectSchema(map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "The memory question or retrieval objective.",
				},
				"lanes": map[string]any{
					"type":        "array",
					"description": "Optional lane hints: facts, timeline, aca.",
					"items":       map[string]any{"type": "string"},
				},
				"max_scouts": map[string]any{
					"type":        "integer",
					"description": "Maximum number of scout subcalls to run.",
				},
				"max_iterations_per_scout": map[string]any{
					"type":        "integer",
					"description": "Maximum iteration budget per scout subcall.",
				},
				"max_subcalls_per_scout": map[string]any{
					"type":        "integer",
					"description": "Maximum subcall budget per scout subcall.",
				},
				"limit_per_lane": map[string]any{
					"type":        "integer",
					"description": "Optional result limit hint carried into scout prompts.",
				},
			}, "query"),
			ReadOnly: true,
		},
		{
			Name:        "code_search_ensemble",
			Description: "Run a staged code-search ensemble that finds candidate files, grounds them with repo evidence, and returns a compact evidence pack.",
			Parameters: objectSchema(map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "The code-search question or retrieval objective.",
				},
				"task_type": map[string]any{
					"type":        "string",
					"description": "Optional task type: file_locate, execution_trace, symbol_inspect, change_impact, or registration_trace. Defaults to file_locate when omitted.",
				},
				"candidate_paths": map[string]any{
					"type":        "array",
					"description": "Optional repo-relative candidate files to treat as already known.",
					"items":       map[string]any{"type": "string"},
				},
				"constraints": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"exclude_paths": map[string]any{
							"type":        "array",
							"description": "Optional repo-relative glob-style exclusions.",
							"items":       map[string]any{"type": "string"},
						},
						"include_history": map[string]any{
							"type":        "boolean",
							"description": "Reserved for later historical augmentation. Ignored in the first slice.",
						},
						"include_aca": map[string]any{
							"type":        "boolean",
							"description": "Reserved for later ACA augmentation. Ignored in the first slice.",
						},
						"require_grounding": map[string]any{
							"type":        "boolean",
							"description": "Whether the ensemble should require at least one grounded file/snippet before returning a confident answer. Defaults to true.",
						},
					},
					"additionalProperties": false,
				},
				"budget": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"max_steps": map[string]any{
							"type":        "integer",
							"description": "Reserved step budget placeholder for later staged planning.",
						},
						"max_candidates": map[string]any{
							"type":        "integer",
							"description": "Maximum candidate files to keep during candidate generation.",
						},
						"max_files": map[string]any{
							"type":        "integer",
							"description": "Maximum grounded files to return.",
						},
						"max_snippets": map[string]any{
							"type":        "integer",
							"description": "Maximum snippets to return.",
						},
						"max_tokens_out": map[string]any{
							"type":        "integer",
							"description": "Reserved output-budget placeholder for later compaction policies.",
						},
						"allow_scouts": map[string]any{
							"type":        "boolean",
							"description": "Reserved for later scout escalation. Ignored in the first slice.",
						},
					},
					"additionalProperties": false,
				},
			}, "query"),
			ReadOnly: true,
		},
		{
			Name:        "subcall",
			Description: "Issue one bounded recursive subcall over selected repo, vault, scene, or artifact handles.",
			Parameters: objectSchema(map[string]any{
				"prompt": map[string]any{
					"type":        "string",
					"description": "Focused child prompt for the recursive subcall.",
				},
				"role": map[string]any{
					"type":        "string",
					"description": "Optional specialized child scout role such as memory_fact_scout, memory_timeline_scout, or aca_context_scout.",
				},
				"repo_handles": map[string]any{
					"type":        "array",
					"description": "Optional narrowed repo handles for the child call.",
					"items":       map[string]any{"type": "string"},
				},
				"vault_handles": map[string]any{
					"type":        "array",
					"description": "Optional narrowed vault handles for the child call.",
					"items":       map[string]any{"type": "string"},
				},
				"scene_handles": map[string]any{
					"type":        "array",
					"description": "Optional narrowed scene handles for the child call.",
					"items":       map[string]any{"type": "string"},
				},
				"artifact_handles": map[string]any{
					"type":        "array",
					"description": "Optional narrowed artifact handles for the child call.",
					"items":       map[string]any{"type": "string"},
				},
				"max_depth": map[string]any{
					"type":        "integer",
					"description": "Child max recursive depth budget.",
				},
				"max_iterations": map[string]any{
					"type":        "integer",
					"description": "Child max iteration budget.",
				},
				"max_subcalls": map[string]any{
					"type":        "integer",
					"description": "Child max subcall budget.",
				},
			}, "prompt"),
			ReadOnly: true,
		},
	}
}

func emptyObjectSchema() json.RawMessage {
	return objectSchema(nil)
}

func objectSchema(properties map[string]any, required ...string) json.RawMessage {
	if properties == nil {
		properties = map[string]any{}
	}
	schema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	body, _ := json.Marshal(schema)
	return body
}

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/rlm/run_spec.go
```go
package rlm

import (
	"fmt"
	"strings"
)

type ToolProfile string

const (
	ToolProfileDefault               ToolProfile = "default"
	ToolProfileCodeIntel             ToolProfile = "code-intel"
	ToolProfileLongCoTNoModelTools   ToolProfile = "longcot-no-model-tools"
)

// ToolPolicy captures the effective model-visible tool policy for one run.
type ToolPolicy struct {
	Profile      ToolProfile `json:"profile"`
	AllowedTools []string    `json:"allowed_tools,omitempty"`
	Tools        []Tool      `json:"tools,omitempty"`
}

// RunSpec is the canonical resolved runtime contract for one RLM run.
type RunSpec struct {
	RouteProfile RouteProfile `json:"route_profile"`
	PlanMode     PlanMode     `json:"plan_mode"`
	Plan         Plan         `json:"plan"`
	ToolPolicy   ToolPolicy   `json:"tool_policy"`
}

// ResolveRunSpecInput describes unresolved route/mode/tool-policy inputs.
type ResolveRunSpecInput struct {
	Prompt               string
	RequestedRoute       RouteProfile
	RequestedPlanMode    PlanMode
	RequestedToolProfile string
	AvailableTools       []Tool
}

// ResolveRunSpec canonicalizes route, planning mode, plan, and tool policy.
func ResolveRunSpec(input ResolveRunSpecInput) (RunSpec, error) {
	route, err := resolveRunRouteProfile(input.Prompt, input.RequestedRoute)
	if err != nil {
		return RunSpec{}, err
	}
	mode, err := resolveRunPlanMode(input.RequestedPlanMode)
	if err != nil {
		return RunSpec{}, err
	}
	policy, err := ResolveToolPolicy(input.AvailableTools, input.RequestedToolProfile)
	if err != nil {
		return RunSpec{}, err
	}
	plan := buildPlan(route, mode)
	return RunSpec{
		RouteProfile: route,
		PlanMode:     mode,
		Plan:         plan,
		ToolPolicy:   policy,
	}, nil
}

// ResolveToolPolicy resolves one canonical tool profile against available tools.
func ResolveToolPolicy(available []Tool, profile string) (ToolPolicy, error) {
	resolvedProfile, err := NormalizeToolProfile(profile)
	if err != nil {
		return ToolPolicy{}, err
	}

	switch resolvedProfile {
	case ToolProfileDefault:
		tools := append([]Tool(nil), available...)
		return ToolPolicy{
			Profile:      resolvedProfile,
			AllowedTools: collectToolNames(tools),
			Tools:        tools,
		}, nil
	case ToolProfileCodeIntel:
		allow := map[string]struct{}{
			"semantic_search_code": {},
			"smart_search_code":    {},
			"ripgrep_code":         {},
			"code_search_ensemble": {},
			"load_file":            {},
			"search_vault":         {},
			"read_note":            {},
			"subcall":              {},
		}
		tools := filterToolsBySet(available, allow)
		return ToolPolicy{
			Profile:      resolvedProfile,
			AllowedTools: collectToolNames(tools),
			Tools:        tools,
		}, nil
	case ToolProfileLongCoTNoModelTools:
		return ToolPolicy{
			Profile:      resolvedProfile,
			AllowedTools: nil,
			Tools:        []Tool{},
		}, nil
	default:
		return ToolPolicy{}, fmt.Errorf("rlm: unsupported tool profile %q", profile)
	}
}

// NormalizeToolProfile validates a tool profile against canonical profile names.
func NormalizeToolProfile(value string) (ToolProfile, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(ToolProfileDefault):
		return ToolProfileDefault, nil
	case string(ToolProfileCodeIntel):
		return ToolProfileCodeIntel, nil
	case string(ToolProfileLongCoTNoModelTools):
		return ToolProfileLongCoTNoModelTools, nil
	default:
		return "", fmt.Errorf("rlm: unsupported tool profile %q", value)
	}
}

func resolveRunRouteProfile(prompt string, requested RouteProfile) (RouteProfile, error) {
	switch strings.ToLower(strings.TrimSpace(string(requested))) {
	case "", string(RouteProfileAuto):
		return ClassifyRouteProfile(prompt), nil
	case string(RouteProfileCodeRetrieval):
		return RouteProfileCodeRetrieval, nil
	case string(RouteProfileMemoryRecall):
		return RouteProfileMemoryRecall, nil
	case string(RouteProfileMixed):
		return RouteProfileMixed, nil
	case string(RouteProfileEvidenceAudit):
		return RouteProfileEvidenceAudit, nil
	default:
		return "", fmt.Errorf("rlm: unsupported route profile %q", requested)
	}
}

func resolveRunPlanMode(requested PlanMode) (PlanMode, error) {
	switch strings.ToLower(strings.TrimSpace(string(requested))) {
	case "", string(PlanModeFree):
		return PlanModeFree, nil
	case string(PlanModeGuided):
		return PlanModeGuided, nil
	case string(PlanModeStaged):
		return PlanModeStaged, nil
	case string(PlanModeHard):
		return PlanModeHard, nil
	case string(PlanModeLambda):
		return PlanModeLambda, nil
	default:
		return "", fmt.Errorf("rlm: unsupported plan mode %q", requested)
	}
}

func filterToolsBySet(tools []Tool, allow map[string]struct{}) []Tool {
	out := make([]Tool, 0, len(tools))
	for _, tool := range tools {
		if _, ok := allow[strings.TrimSpace(tool.Name)]; ok {
			out = append(out, tool)
		}
	}
	return out
}

func collectToolNames(tools []Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	return names
}

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/rlm/env/memory_ensemble.go
```go
package env

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/joshka0/foxctl/internal/rlm"
)

type memoryEnsembleInput struct {
	Query                 string   `json:"query"`
	Lanes                 []string `json:"lanes"`
	MaxScouts             int      `json:"max_scouts"`
	MaxIterationsPerScout int      `json:"max_iterations_per_scout"`
	MaxSubcallsPerScout   int      `json:"max_subcalls_per_scout"`
	LimitPerLane          int      `json:"limit_per_lane"`
}

type memoryScoutClaim struct {
	Key          string   `json:"key,omitempty"`
	Value        string   `json:"value,omitempty"`
	Status       string   `json:"status,omitempty"`
	Source       string   `json:"source,omitempty"`
	EvidenceRefs []string `json:"evidence_refs,omitempty"`
	Confidence   float64  `json:"confidence,omitempty"`
}

type memoryScoutTimelineItem struct {
	TS           string   `json:"ts,omitempty"`
	Kind         string   `json:"kind,omitempty"`
	Value        string   `json:"value,omitempty"`
	Source       string   `json:"source,omitempty"`
	EvidenceRefs []string `json:"evidence_refs,omitempty"`
	Supersedes   string   `json:"supersedes,omitempty"`
	Confidence   float64  `json:"confidence,omitempty"`
}

type memoryScoutContextBlock struct {
	Lane    string   `json:"lane,omitempty"`
	Summary string   `json:"summary,omitempty"`
	Refs    []string `json:"refs,omitempty"`
}

type memoryScoutOutput struct {
	Summary         string                    `json:"summary,omitempty"`
	Claims          []memoryScoutClaim        `json:"claims,omitempty"`
	CurrentBestView string                    `json:"current_best_view,omitempty"`
	Timeline        []memoryScoutTimelineItem `json:"timeline,omitempty"`
	ContextBlocks   []memoryScoutContextBlock `json:"context_blocks,omitempty"`
	Gaps            []string                  `json:"gaps,omitempty"`
}

func (a *ReadOnlyAdapter) memoryEnsembleRetrieve(ctx context.Context, args json.RawMessage) (map[string]any, error) {
	if a.subcall == nil {
		return map[string]any{
			"supported": false,
			"message":   "memory_ensemble_retrieve requires subcall support",
		}, nil
	}

	var input memoryEnsembleInput
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, err
	}
	input.Query = strings.TrimSpace(input.Query)
	if input.Query == "" {
		return nil, fmt.Errorf("query is required")
	}
	if input.MaxScouts <= 0 {
		input.MaxScouts = 3
	}
	if input.MaxIterationsPerScout <= 0 {
		input.MaxIterationsPerScout = 3
	}
	if input.MaxSubcallsPerScout < 0 {
		input.MaxSubcallsPerScout = 0
	}

	roles := selectMemoryScoutRoles(input.Lanes, input.MaxScouts)
	if len(roles) == 0 {
		return map[string]any{
			"summary":  "",
			"scouts":   []map[string]any{},
			"metadata": map[string]any{"scouts_run": []string{}, "lanes_run": []string{}},
		}, nil
	}

	scouts := make([]map[string]any, 0, len(roles))
	summaries := make([]string, 0, len(roles))
	evidenceRefs := make([]string, 0, len(roles)*4)
	retrievedPaths := make([]string, 0, len(roles)*4)
	claims := make([]memoryScoutClaim, 0, len(roles)*4)
	timeline := make([]memoryScoutTimelineItem, 0, len(roles)*4)
	contextBlocks := make([]memoryScoutContextBlock, 0, len(roles)*4)
	gaps := make([]string, 0, len(roles)*2)
	currentBestViews := make([]string, 0, len(roles))

	for _, role := range roles {
		taskPrompt := buildMemoryScoutPrompt(role, input.Query, input.LimitPerLane)
		result, err := a.subcall(ctx, rlm.Task{
			Prompt:        taskPrompt,
			Role:          role,
			WorkspaceRoot: a.workspaceRoot,
			MaxDepth:      0,
			MaxIterations: input.MaxIterationsPerScout,
			MaxSubcalls:   input.MaxSubcallsPerScout,
		}, a.environment)
		scout := map[string]any{
			"role": role,
		}
		if err != nil {
			scout["ok"] = false
			scout["error"] = err.Error()
			scouts = append(scouts, scout)
			continue
		}

		parsed := parseMemoryScoutOutput(result.Answer)
		parsed = hydrateMemoryScoutOutput(parsed, result.EvidenceRefs)

		scout["ok"] = true
		scout["summary"] = scoutStructuredSummary(parsed)
		scout["raw_answer"] = strings.TrimSpace(result.Answer)
		scout["evidence_refs"] = append([]string(nil), result.EvidenceRefs...)
		scout["retrieved_paths"] = append([]string(nil), result.RetrievedPaths...)
		if len(parsed.Claims) > 0 {
			scout["claims"] = parsed.Claims
		}
		if len(parsed.Timeline) > 0 {
			scout["timeline"] = parsed.Timeline
		}
		if parsed.CurrentBestView != "" {
			scout["current_best_view"] = parsed.CurrentBestView
		}
		if len(parsed.ContextBlocks) > 0 {
			scout["context_blocks"] = parsed.ContextBlocks
		}
		if len(parsed.Gaps) > 0 {
			scout["gaps"] = parsed.Gaps
		}
		if len(result.Metadata) > 0 {
			scout["metadata"] = result.Metadata
		}
		scouts = append(scouts, scout)

		if summary := scoutStructuredSummary(parsed); summary != "" {
			summaries = append(summaries, role+": "+summary)
		}
		if parsed.CurrentBestView != "" {
			currentBestViews = append(currentBestViews, parsed.CurrentBestView)
		}
		claims = append(claims, parsed.Claims...)
		timeline = append(timeline, parsed.Timeline...)
		contextBlocks = append(contextBlocks, parsed.ContextBlocks...)
		gaps = append(gaps, parsed.Gaps...)
		evidenceRefs = append(evidenceRefs, result.EvidenceRefs...)
		retrievedPaths = append(retrievedPaths, result.RetrievedPaths...)
	}

	basis := recommendAnswerBasis(roles, claims, timeline, contextBlocks, currentBestViews)
	return map[string]any{
		"summary":                  buildMemoryEnsembleSummary(basis, summaries, claims, timeline, contextBlocks, currentBestViews),
		"scouts":                   scouts,
		"recommended_answer_basis": basis,
		"claims":                   claims,
		"timeline":                 timeline,
		"context_blocks":           contextBlocks,
		"gaps":                     uniqueStrings(gaps),
		"evidence_refs":            uniqueStrings(evidenceRefs),
		"retrieved_paths":          uniqueStrings(retrievedPaths),
		"metadata": map[string]any{
			"scouts_run": roles,
			"lanes_run":  lanesForRoles(roles),
		},
	}, nil
}

func selectMemoryScoutRoles(lanes []string, maxScouts int) []string {
	if maxScouts <= 0 {
		maxScouts = 3
	}
	roles := make([]string, 0, 3)
	addRole := func(role string) {
		role = NormalizeScoutRole(role)
		if role == "" {
			return
		}
		for _, existing := range roles {
			if existing == role {
				return
			}
		}
		roles = append(roles, role)
	}

	for _, lane := range lanes {
		switch strings.ToLower(strings.TrimSpace(lane)) {
		case "facts", "fact":
			addRole(ScoutRoleMemoryFact)
		case "timeline", "time":
			addRole(ScoutRoleMemoryTimeline)
		case "aca", "context":
			addRole(ScoutRoleACAContext)
		}
	}
	if len(roles) == 0 {
		for _, role := range []string{ScoutRoleMemoryFact, ScoutRoleMemoryTimeline, ScoutRoleACAContext} {
			addRole(role)
		}
	}
	if len(roles) > maxScouts {
		return roles[:maxScouts]
	}
	return roles
}

func buildMemoryScoutPrompt(role, query string, limit int) string {
	limitHint := ""
	if limit > 0 {
		limitHint = fmt.Sprintf(" Limit yourself to about %d high-signal items.", limit)
	}
	switch NormalizeScoutRole(role) {
	case ScoutRoleMemoryFact:
		return strings.TrimSpace("Find the current explicit facts, preferences, decisions, goals, or technical context that answer this query: " + query + "." + limitHint + ` Return JSON only with this shape: {"summary":"...","claims":[{"key":"...","value":"...","status":"current|candidate","source":"tool-name","evidence_refs":["..."],"confidence":0.0}],"gaps":["..."]}.`)
	case ScoutRoleMemoryTimeline:
		return strings.TrimSpace("Reconstruct the update timeline for this query and identify the current best view: " + query + "." + limitHint + ` Return JSON only with this shape: {"summary":"...","current_best_view":"...","timeline":[{"ts":"...","kind":"statement|update|retraction|decision","value":"...","source":"tool-name","evidence_refs":["..."],"supersedes":"...","confidence":0.0}],"gaps":["..."]}.`)
	case ScoutRoleACAContext:
		return strings.TrimSpace("Gather the durable ACA, handoff, and vault-backed context relevant to this query: " + query + "." + limitHint + ` Return JSON only with this shape: {"summary":"...","context_blocks":[{"lane":"top_of_mind|task_continuity|vault|related_note","summary":"...","refs":["..."]}],"gaps":["..."]}.`)
	default:
		return strings.TrimSpace(query)
	}
}

func parseMemoryScoutOutput(raw string) memoryScoutOutput {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return memoryScoutOutput{}
	}
	var out memoryScoutOutput
	if err := json.Unmarshal([]byte(raw), &out); err == nil {
		return out
	}
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start >= 0 && end > start {
		if err := json.Unmarshal([]byte(raw[start:end+1]), &out); err == nil {
			return out
		}
	}
	return memoryScoutOutput{Summary: raw}
}

func hydrateMemoryScoutOutput(out memoryScoutOutput, fallbackEvidence []string) memoryScoutOutput {
	for i := range out.Claims {
		out.Claims[i].EvidenceRefs = fillStringSlice(out.Claims[i].EvidenceRefs, fallbackEvidence)
	}
	for i := range out.Timeline {
		out.Timeline[i].EvidenceRefs = fillStringSlice(out.Timeline[i].EvidenceRefs, fallbackEvidence)
	}
	for i := range out.ContextBlocks {
		out.ContextBlocks[i].Refs = fillStringSlice(out.ContextBlocks[i].Refs, fallbackEvidence)
	}
	return out
}

func fillStringSlice(existing, fallback []string) []string {
	if len(existing) > 0 {
		return uniqueStrings(existing)
	}
	if len(fallback) == 0 {
		return nil
	}
	return uniqueStrings(append([]string(nil), fallback...))
}

func scoutStructuredSummary(out memoryScoutOutput) string {
	if strings.TrimSpace(out.CurrentBestView) != "" {
		return strings.TrimSpace(out.CurrentBestView)
	}
	if strings.TrimSpace(out.Summary) != "" {
		return strings.TrimSpace(out.Summary)
	}
	if len(out.Claims) > 0 {
		lines := make([]string, 0, min(3, len(out.Claims)))
		for _, claim := range out.Claims[:min(3, len(out.Claims))] {
			lines = append(lines, strings.TrimSpace(claim.Key+": "+claim.Value))
		}
		return strings.Join(lines, "; ")
	}
	if len(out.ContextBlocks) > 0 {
		lines := make([]string, 0, min(2, len(out.ContextBlocks)))
		for _, block := range out.ContextBlocks[:min(2, len(out.ContextBlocks))] {
			lines = append(lines, strings.TrimSpace(block.Lane+": "+block.Summary))
		}
		return strings.Join(lines, "; ")
	}
	return ""
}

func recommendAnswerBasis(roles []string, claims []memoryScoutClaim, timeline []memoryScoutTimelineItem, contextBlocks []memoryScoutContextBlock, currentBestViews []string) string {
	switch {
	case len(timeline) > 0 || len(currentBestViews) > 0:
		return "timeline"
	case len(claims) > 0 && len(contextBlocks) == 0:
		return "facts"
	case len(contextBlocks) > 0 && len(claims) == 0:
		return "aca"
	case len(roles) == 1:
		switch roles[0] {
		case ScoutRoleMemoryFact:
			return "facts"
		case ScoutRoleMemoryTimeline:
			return "timeline"
		case ScoutRoleACAContext:
			return "aca"
		}
	}
	return "combined"
}

func buildMemoryEnsembleSummary(basis string, scoutSummaries []string, claims []memoryScoutClaim, timeline []memoryScoutTimelineItem, contextBlocks []memoryScoutContextBlock, currentBestViews []string) string {
	switch basis {
	case "timeline":
		if len(currentBestViews) > 0 {
			return strings.TrimSpace(currentBestViews[0])
		}
		if len(timeline) > 0 {
			return strings.TrimSpace(timeline[len(timeline)-1].Value)
		}
	case "facts":
		if len(claims) > 0 {
			lines := make([]string, 0, min(3, len(claims)))
			for _, claim := range claims[:min(3, len(claims))] {
				lines = append(lines, strings.TrimSpace(claim.Key+": "+claim.Value))
			}
			return strings.Join(lines, "\n")
		}
	case "aca":
		if len(contextBlocks) > 0 {
			lines := make([]string, 0, min(2, len(contextBlocks)))
			for _, block := range contextBlocks[:min(2, len(contextBlocks))] {
				lines = append(lines, strings.TrimSpace(block.Lane+": "+block.Summary))
			}
			return strings.Join(lines, "\n")
		}
	}
	return strings.Join(scoutSummaries, "\n\n")
}

func lanesForRoles(roles []string) []string {
	out := make([]string, 0, len(roles))
	for _, role := range roles {
		switch role {
		case ScoutRoleMemoryFact:
			out = append(out, "facts")
		case ScoutRoleMemoryTimeline:
			out = append(out, "timeline")
		case ScoutRoleACAContext:
			out = append(out, "aca")
		}
	}
	return uniqueStrings(out)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/skills/memory_query/skill.yaml
```yaml
apiVersion: foxctl/v1
kind: Skill
metadata:
  name: memory/query
  description: Query memories with type filtering, file association, and semantic search
  version: 0.1.0
  author: foxctl
  tags: [memory, search, gotcha, decision, pattern, embedding]
distribution:
  type: exec
  exec:
    entry: bin
io:
  format: JSON
  inline_output_kb: 32
signature:
  command: memory/query
  parameters:
    - name: query
      type: string
      required: false
      description: Natural language or text query to search for
    - name: file
      type: string
      required: false
      description: Filter memories associated with this file path
    - name: types
      type: string
      required: false
      description: Comma-separated memory types to filter (gotcha, decision, pattern, user_pref, time_sink, edit, codemap, code_symbol)
    - name: workspace
      type: string
      required: false
      description: Workspace path (defaults to current directory)
    - name: session_id
      type: string
      required: false
      description: Filter memories by session_id
    - name: limit
      type: integer
      required: false
      default: 10
      description: Maximum number of results to return per page (max 100)
    - name: offset
      type: integer
      required: false
      default: 0
      description: Offset for pagination (skip this many results)
    - name: min_similarity
      type: number
      required: false
      default: 0.3
      description: Minimum similarity threshold for vector search (0-1)
    - name: include_content
      type: boolean
      required: false
      default: false
      description: Include full memory content in results (can be large)
  returns:
    - name: memories
      type: array
      description: Matching memory entries with metadata and scores
    - name: pagination
      type: object
      description: Pagination info (total, offset, limit, has_more)
    - name: stats
      type: object
      description: Search statistics (total, filtered, search method used)
  help:
    short: "Query memories by type, file, or semantic similarity"
    long: |
      memory/query provides filtered access to the foxctl memory store,
      designed for plugin/hook integration where you need to surface relevant
      context before operations.

      Use Cases:
      - Recall gotchas before editing a file
      - Find past decisions related to a topic
      - Surface patterns for similar code
      - Get file-specific context for AI assistants

      Search Methods:
      1. If query is provided: semantic vector search (requires VOYAGE_API_KEY/GEMINI_API_KEY)
      2. If only file is provided: filter by file association
      3. If only types is provided: filter by memory type
      4. Combinations are AND-ed together

      Memory Types:
      - gotcha: Lessons learned, pitfalls to avoid
      - decision: Architecture/design decisions with rationale
      - pattern: Code patterns to follow or avoid
      - user_pref: Explicit user preferences (from session summaries)
      - time_sink: Time-consuming issues to avoid (from session summaries)
      - edit: Recent file edits (auto-captured)
      - code_symbol: Indexed code symbols
      - codemap: Semantic codemaps
      - result: Generic skill results
      - task_embedding: Task context

      File Association:
      Memories can be associated with files through:
      - Explicit file path in memory metadata
      - File references in memory content (auto-detected)
      - Directory prefix matching (e.g., "src/" matches "src/foo/bar.go")

      Examples:
      - Query gotchas for a file: { "file": "internal/storage/memory/store.go", "types": "gotcha,decision" }
      - Search patterns semantically: { "query": "error handling patterns", "types": "pattern" }
      - Get all decisions: { "types": "decision", "limit": 20 }

      Example commands:
        foxctl run memory/query --input '{"file":"internal/storage/memory/store.go","types":"gotcha,decision","limit":5}'
        foxctl skills run memory/query --file internal/storage/memory/store.go --types gotcha,decision --limit 5
        foxctl run memory/query --input '{"query":"authentication middleware","types":"pattern","limit":10}'
        foxctl skills run memory/query --query "authentication middleware" --types pattern --limit 10
    workflows:
      - id: file-gotchas
        description: "Get gotchas before editing a file"
        example_input:
          file: "internal/storage/memory/store.go"
          types: "gotcha,decision"
          limit: 5
      - id: search-patterns
        description: "Find relevant patterns semantically"
        example_input:
          query: "authentication middleware"
          types: "pattern"
          limit: 10
      - id: recent-decisions
        description: "List recent architecture decisions"
        example_input:
          types: "decision"
          limit: 20
capabilities:
  network: "egress"
  filesystem:
    - type: workdir
  pure: false
memory:
  recommend: false
openapi:
  enabled: true
  methods:
    GET: "true"
    POST: "true"

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/rlm/llm_runner.go
(lines 1-260: LLM runner setup and run entrypoint: run-spec resolution, tool-policy enforcement, single-pass execution config, and metadata about parent tool usage.)
```go
package rlm

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/joshka0/foxctl/internal/runtime/engine"
)

// LLMToolExecutor adapts the RLM tool surface to the engine.ToolExecutor contract.
type LLMToolExecutor struct {
	adapter ToolExecutor
	tools   []Tool
}

func NewLLMToolExecutor(adapter ToolExecutor, tools []Tool) *LLMToolExecutor {
	return &LLMToolExecutor{adapter: adapter, tools: tools}
}

func (e *LLMToolExecutor) Execute(ctx context.Context, name string, args json.RawMessage) (string, error) {
	if e == nil || e.adapter == nil {
		return "", fmt.Errorf("rlm llm tool executor is not configured")
	}
	result, err := e.adapter.Execute(ctx, name, args)
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (e *LLMToolExecutor) List() []engine.ToolDef {
	if e == nil {
		return nil
	}
	defs := make([]engine.ToolDef, 0, len(e.tools))
	for _, tool := range e.tools {
		schema := tool.Parameters
		if len(schema) == 0 {
			schema, _ = json.Marshal(map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"additionalProperties": false,
			})
		}
		defs = append(defs, engine.ToolDef{
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  schema,
		})
	}
	return defs
}

// LLMConfig configures the model-backed experimental RLM runner.
type LLMConfig struct {
	Provider       string
	APIKey         string
	BaseURL        string
	AuthMode       string
	AuthHeader     string
	AuthPrefix     string
	Model          string
	Timeout        time.Duration
	MaxTokens      int
	Temperature    float64
	MaxIterations  int
	RequireToolUse bool
	QwenNoThink    bool
	ExtraBody      map[string]any
	RouteProfile   RouteProfile
	PlanMode       PlanMode
}

// LLMRunner uses the existing engine.LLMChatEngine as an experimental read-only RLM backend.
type LLMRunner struct {
	Config LLMConfig
	Tools  ToolExecutor
}

func (r LLMRunner) Run(ctx context.Context, task Task, env Environment) (Result, error) {
	if err := ValidateTask(task); err != nil {
		return Result{}, err
	}
	if err := ValidateEnvironment(env); err != nil {
		return Result{}, err
	}
	if r.Tools == nil {
		return Result{}, fmt.Errorf("rlm llm runner requires tool adapter")
	}
	spec, err := ResolveRunSpec(ResolveRunSpecInput{
		Prompt:               task.Prompt,
		RequestedRoute:       r.Config.RouteProfile,
		RequestedPlanMode:    r.Config.PlanMode,
		RequestedToolProfile: string(ToolProfileDefault),
		AvailableTools:       env.Tools,
	})
	if err != nil {
		return Result{}, err
	}
	effectiveEnv := env
	effectiveEnv.Tools = append([]Tool(nil), spec.ToolPolicy.Tools...)
	if spec.PlanMode == PlanModeStaged && len(spec.Plan.Phases) > 0 {
		return r.runStaged(ctx, task, effectiveEnv, spec)
	}
	return r.runSinglePass(ctx, task, effectiveEnv, runPassConfig{
		Prompt:         task.Prompt,
		Tools:          effectiveEnv.Tools,
		SystemPrompt:   BuildLLMSystemPrompt(effectiveEnv, task),
		RequireToolUse: r.Config.RequireToolUse,
		MaxIterations:  task.MaxIterations,
		Metadata: map[string]any{
			"effective_route_profile": spec.RouteProfile,
			"effective_plan_mode":     spec.PlanMode,
			"tool_policy_profile":     spec.ToolPolicy.Profile,
			"allowed_tools":           append([]string(nil), spec.ToolPolicy.AllowedTools...),
		},
	})
}

type runPassConfig struct {
	Prompt         string
	Tools          []Tool
	SystemPrompt   string
	RequireToolUse bool
	MaxIterations  int
	Metadata       map[string]any
}

func (r LLMRunner) runSinglePass(ctx context.Context, task Task, env Environment, pass runPassConfig) (Result, error) {
	if pass.RequireToolUse && len(pass.Tools) == 0 {
		return Result{}, fmt.Errorf("rlm llm runner: require-tool-use is enabled but no tools are available")
	}
	llmCfg := engine.DefaultLLMChatConfig()
	if strings.TrimSpace(r.Config.Provider) != "" {
		llmCfg.Provider = strings.TrimSpace(r.Config.Provider)
	}
	if strings.TrimSpace(r.Config.APIKey) != "" {
		llmCfg.APIKey = strings.TrimSpace(r.Config.APIKey)
	}
	if strings.TrimSpace(r.Config.BaseURL) != "" {
		llmCfg.BaseURL = strings.TrimSpace(r.Config.BaseURL)
	}
	if strings.TrimSpace(r.Config.AuthMode) != "" {
		llmCfg.AuthMode = strings.TrimSpace(r.Config.AuthMode)
	}
	if strings.TrimSpace(r.Config.AuthHeader) != "" {
		llmCfg.AuthHeader = strings.TrimSpace(r.Config.AuthHeader)
	}
	if r.Config.AuthPrefix != "" {
		llmCfg.AuthPrefix = r.Config.AuthPrefix
	}
	if strings.TrimSpace(r.Config.Model) != "" {
		llmCfg.Model = strings.TrimSpace(r.Config.Model)
	}
	if r.Config.Timeout > 0 {
		llmCfg.Timeout = r.Config.Timeout
	}
	if r.Config.MaxTokens > 0 {
		llmCfg.MaxTokens = r.Config.MaxTokens
	}
	if r.Config.Temperature != 0 {
		llmCfg.Temperature = r.Config.Temperature
	}
	llmCfg.ExtraBody = cloneStringAnyMap(r.Config.ExtraBody)
	if pass.MaxIterations > 0 {
		llmCfg.MaxIterations = pass.MaxIterations
	} else if r.Config.MaxIterations > 0 {
		llmCfg.MaxIterations = r.Config.MaxIterations
	} else if task.MaxIterations > 0 {
		llmCfg.MaxIterations = task.MaxIterations
	}
	if pass.RequireToolUse {
		llmCfg.ToolChoice = json.RawMessage(`"required"`)
	}

	llm, err := engine.NewLLMChatEngine(llmCfg)
	if err != nil {
		return Result{}, err
	}
	toolExec := NewLLMToolExecutor(r.Tools, pass.Tools)
	llm.SetToolRunner(engine.NewToolRunner(toolExec, nil, engine.ToolRunnerConfig{
		Workspace:   task.WorkspaceRoot,
		WorkspaceID: task.WorkspaceID,
	}))

	output, err := llm.Run(ctx, engine.EngineInput{
		SystemPrompt: pass.SystemPrompt,
		Messages:     []engine.Message{engine.NewUserMessage(pass.Prompt)},
		Tools:        toolExec.List(),
		Workspace:    task.WorkspaceRoot,
		MaxTokens:    llmCfg.MaxTokens,
		Temperature:  llmCfg.Temperature,
	})
	if err != nil {
		return Result{}, err
	}
	if output.StopReason == engine.StopReasonError {
		return Result{}, fmt.Errorf("rlm llm runner: %s", strings.TrimSpace(output.Error))
	}
	if pass.RequireToolUse && len(output.ToolCalls) == 0 {
		return Result{}, fmt.Errorf("rlm llm runner: model answered without using tools")
	}
	answer := strings.TrimSpace(output.AssistantText)
	if answer == "" {
		if output.StopReason != "" && output.StopReason != engine.StopReasonEndTurn {
			detail := strings.TrimSpace(output.Error)
			if detail != "" {
				return Result{}, fmt.Errorf("rlm llm runner: %s before assistant response: %s", output.StopReason, detail)
			}
			return Result{}, fmt.Errorf("rlm llm runner: %s before assistant response", output.StopReason)
		}
		return Result{}, fmt.Errorf("rlm llm runner: empty assistant response")
	}
	evidence := collectEvidenceRefs(env)
	retrievedPaths := collectRetrievedPaths(output.ToolResults, task.WorkspaceRoot, answer)
	parentUsage := summarizeParentToolUsage(output.Iterations, "code_search_ensemble")
	metadata := map[string]any{
		"stop_reason":            output.StopReason,
		"provider":               llmCfg.Provider,
		"model":                  llmCfg.Model,
		"tool_calls":             len(output.ToolCalls),
		"tool_names":             toolCallNames(output.ToolCalls),
		"llm_error":              output.Error,
		"require_tool_use":       pass.RequireToolUse,
		"retrieved_paths":        retrievedPaths,
		"parent_input_tokens":    output.Tokens.InputTokens,
		"parent_output_tokens":   output.Tokens.OutputTokens,
		"parent_total_tokens":    output.Tokens.TotalTokens,
		"parent_iteration_count": len(output.Iterations),
		"parent_tool_usage":      parentUsage,
	}
	for key, value := range pass.Metadata {
		metadata[key] = value
	}
	return Result{
		Answer:         answer,
		EvidenceRefs:   evidence,
		RetrievedPaths: retrievedPaths,
		Iterations:     1,
		Subcalls:       len(output.ToolCalls),
		Metadata:       metadata,
	}, nil
}

func BuildLLMSystemPrompt(env Environment, task Task) string {
	var b strings.Builder
	b.WriteString("You are an experimental read-only recursive reasoning runtime.\n")
	b.WriteString("Use tools to inspect external state before answering. Do not invent unavailable evidence.\n")
	b.WriteString("Prefer repo, scene, vault, and artifact handles already present in the environment.\n")

```

(lines 820-1047: Staged-phase guidance and query/path reranking heuristics (queryGuidanceHints, scoreCandidatePath, tokenizeQueryTerms), including keyword-based routing weights.)
```go
			return true
		}
	}
	return false
}

func truncateRLMText(value string, max int) string {
	value = strings.TrimSpace(value)
	if max <= 0 || len(value) <= max {
		return value
	}
	if max <= 3 {
		return value[:max]
	}
	return value[:max-3] + "..."
}

func phaseGuidance(phaseName string) string {
	switch phaseName {
	case "discovery":
		return "End discovery with 2-5 plausible relative paths. If the first search is weak or off-domain, run a second discovery tool before finishing."
	case "inspection":
		return "Do not do another broad search if candidate paths already exist. Open the 1-2 strongest candidates directly."
	case "verification":
		return "Re-open or literal-check the single strongest inspected path and confirm it really matches the query."
	default:
		return ""
	}
}

func queryGuidanceHints(query, phaseName string, candidatePaths []string) []string {
	q := strings.ToLower(strings.TrimSpace(query))
	hints := make([]string, 0, 4)
	if strings.Contains(q, "package") {
		hints = append(hints, "Prefer directory or package matches over basename-only coincidences.")
	}
	if strings.Contains(q, "semantic") && strings.Contains(q, "index") {
		hints = append(hints, "Prefer paths under internal/intelligence/indexing/semantic or closely related indexing directories.")
		hints = append(hints, "Avoid generic repository or events files unless they explicitly mention semantic indexing.")
	}
	if containsAny(q, "web", "api", "handler", "transport") {
		hints = append(hints, "Prefer handler, server, transport, bridge, http, or api paths over unrelated tool clients.")
		hints = append(hints, "Avoid obsidian/tooling paths unless the query explicitly mentions them.")
	}
	if phaseName == "inspection" && len(candidatePaths) > 0 {
		hints = append(hints, "Choose the candidate whose directory and filename most directly match the query terms.")
	}
	return hints
}

func rerankCandidatePaths(query string, paths []string) []string {
	type scoredPath struct {
		Path  string
		Score int
	}
	normalized := dedupePathsCaseInsensitive(paths)
	if len(normalized) <= 1 {
		return normalized
	}
	scored := make([]scoredPath, 0, len(normalized))
	for _, path := range normalized {
		scored = append(scored, scoredPath{
			Path:  path,
			Score: scoreCandidatePath(query, path),
		})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Score == scored[j].Score {
			if pathDepth(scored[i].Path) == pathDepth(scored[j].Path) {
				return scored[i].Path < scored[j].Path
			}
			return pathDepth(scored[i].Path) > pathDepth(scored[j].Path)
		}
		return scored[i].Score > scored[j].Score
	})
	out := make([]string, 0, len(scored))
	for _, item := range scored {
		out = append(out, item.Path)
	}
	return out
}

func scoreCandidatePath(query, path string) int {
	q := strings.ToLower(strings.TrimSpace(query))
	p := strings.ToLower(strings.TrimSpace(filepath.ToSlash(path)))
	if p == "" {
		return -1000
	}
	score := 0
	tokens := tokenizeQueryTerms(q)
	for _, token := range tokens {
		if token == "" {
			continue
		}
		switch {
		case strings.Contains("/"+p+"/", "/"+token+"/"):
			score += 8
		case strings.Contains(p, token):
			score += 4
		}
	}
	if strings.Contains(q, "package") {
		if strings.Count(p, "/") >= 2 {
			score += 2
		}
		if !strings.Contains(p, "/") {
			score -= 4
		}
	}
	if containsAny(q, "semantic", "index") {
		if strings.Contains(p, "indexing/semantic") {
			score += 12
		}
		if strings.Contains(p, "repository") || strings.Contains(p, "events/") || strings.Contains(p, "builder") {
			score -= 6
		}
	}
	if containsAny(q, "web", "api", "handler", "transport") {
		if containsAny(p, "web/", "/web", "api", "handler", "http", "transport", "server", "bridge") {
			score += 8
		}
		if containsAny(p, "obsidian", "godot", "gui-agent") {
			score -= 8
		}
	}
	if containsAny(q, "storage", "memory") {
		if containsAny(p, "storage/memory", "/memory/", "memoryflow", "stores.go", "store.go") {
			score += 10
		}
	}
	if containsAny(q, "skill", "output", "envelope", "cas") {
		if containsAny(p, "skillout", "cas", "envelope", "skillmain") {
			score += 8
		}
	}
	if containsAny(q, "platform", "config") {
		if containsAny(p, "config", "platform") {
			score += 8
		}
	}
	if containsAny(q, "cmd", "runtime") {
		if containsAny(p, "cmd/", "/cmd", "runtime") {
			score += 8
		}
	}
	score += minInt(pathDepth(p), 6)
	return score
}

func tokenizeQueryTerms(query string) []string {
	parts := strings.FieldsFunc(query, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	out := make([]string, 0, len(parts)+8)
	for _, part := range parts {
		part = strings.TrimSpace(strings.ToLower(part))
		if len(part) < 3 {
			continue
		}
		switch part {
		case "handlers":
			part = "handler"
		case "settings":
			part = "config"
		}
		out = append(out, part)
	}
	if containsAny(query, "web", "api", "handler", "transport") {
		out = append(out, "web", "api", "handler", "transport", "http", "server")
	}
	if containsAny(query, "semantic", "index") {
		out = append(out, "semantic", "index", "indexing", "provider", "indexer")
	}
	if containsAny(query, "storage", "memory") {
		out = append(out, "storage", "memory", "store")
	}
	if containsAny(query, "skill", "output", "envelope", "cas") {
		out = append(out, "skill", "output", "envelope", "cas")
	}
	if containsAny(query, "platform", "config") {
		out = append(out, "platform", "config")
	}
	return uniqueStringsRLM(out)
}

func dedupePathsCaseInsensitive(paths []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(filepath.ToSlash(path))
		if path == "" {
			continue
		}
		key := strings.ToLower(path)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, path)
	}
	return out
}

func pathDepth(path string) int {
	path = strings.Trim(path, "/")
	if path == "" {
		return 0
	}
	return strings.Count(path, "/")
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func cloneStringAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/rlm/env/code_search_ensemble.go
(lines 130-1279: Primary code_search_ensemble orchestration: input normalization, lane bootstrapping (path/exact/semantic/repo/motif/ACA/graph), gap tracking, grounding via load_file, evidence packaging, and telemetry compaction metrics.)
```go

type codeSearchRouteFamily string

const (
	codeSearchRouteCode            codeSearchRouteFamily = "code"
	codeSearchRouteInfraResource   codeSearchRouteFamily = "infra_resource"
	codeSearchRoutePackageOwner    codeSearchRouteFamily = "package_ownership"
	codeSearchRouteCochangeHistory codeSearchRouteFamily = "cochange_history"
)

type codeSearchCandidateTrace struct {
	Path          string   `json:"path"`
	Why           string   `json:"why,omitempty"`
	SupportScore  float64  `json:"support_score"`
	Sources       []string `json:"sources,omitempty"`
	LineHints     []int    `json:"line_hints,omitempty"`
	Symbols       []string `json:"symbols,omitempty"`
	EvidenceClass string   `json:"evidence_class,omitempty"`
	AnchorRole    string   `json:"anchor_role,omitempty"`
	RepoNodeCount int      `json:"repo_node_count,omitempty"`
	Selected      bool     `json:"selected,omitempty"`
	SelectedRank  int      `json:"selected_rank,omitempty"`
	PruneReason   string   `json:"prune_reason,omitempty"`
}

type codeSearchTraceAnchors struct {
	SourceQuery       string
	TargetQuery       string
	SourceExactProbes []string
	TargetExactProbes []string
	SourcePathProbes  []string
	TargetPathProbes  []string
}

func (a *ReadOnlyAdapter) codeSearchEnsemble(ctx context.Context, args json.RawMessage) (map[string]any, error) {
	started := time.Now()
	var input codeSearchEnsembleInput
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, err
	}
	input.Query = strings.TrimSpace(input.Query)
	if input.Query == "" {
		return nil, fmt.Errorf("query is required")
	}

	taskType, defaulted := normalizeCodeSearchTaskType(input.TaskType)
	if input.Budget.MaxCandidates <= 0 {
		input.Budget.MaxCandidates = 8
	}
	if input.Budget.MaxFiles <= 0 {
		input.Budget.MaxFiles = 4
	}
	if input.Budget.MaxSnippets <= 0 {
		input.Budget.MaxSnippets = 4
	}
	requireGrounding := true
	if input.Constraints.RequireGrounding != nil {
		requireGrounding = *input.Constraints.RequireGrounding
	}

	normalizedCandidates := normalizeCodeSearchPaths(input.CandidatePaths)
	excluded := normalizeCodeSearchPaths(input.Constraints.ExcludePaths)
	phraseIdentifierProbes := codeSearchPhraseIdentifierProbes(input.Query)
	traceAnchors := deriveExecutionTraceAnchors(input.Query, taskType)
	exactProbes := codeSearchTaskExactProbes(input.Query, taskType)
	pathProbes := codeSearchTaskPathProbes(input.Query, taskType, exactProbes)
	preferredSymbolProbes := codeSearchSymbolProbes(input.Query)
	metadata := map[string]any{
		"task_type_defaulted": defaulted,
		"lanes_used":          []string{},
		"grounded":            false,
		"scouts_used":         []string{},
		"history_requested":   input.Constraints.IncludeHistory,
		"aca_requested":       input.Constraints.IncludeACA,
	}

	candidates := map[string]*codeSearchCandidate{}
	gaps := make([]string, 0, 4)
	var routeFamily codeSearchRouteFamily
	var acaHits []contextplane.RetrievalHit
	plannerOut := codeSearchLLMPlannerOutput{}
	if input.Planner.Enabled {
		stageStart := time.Now()
		var plannerMeta map[string]any
		var plannerUsage *skillobs.TokenUsage
		var plannerErr error
		plannerOut, plannerMeta, plannerUsage, plannerErr = a.codeSearchLLMPlanner(ctx, input.Query, taskType, exactProbes, pathProbes, input.Planner)
		a.telemetry.record("llm_planner", time.Since(stageStart), plannerUsage)
		if plannerErr != nil {
			gaps = append(gaps, "llm planner failed: "+plannerErr.Error())
		} else {
			metadata["llm_planner"] = plannerMeta
			exactProbes = mergeCodeSearchProbeLists(exactProbes, plannerOut.SeedQueries)
			pathProbes = mergeCodeSearchProbeLists(pathProbes, plannerOut.PathBiases)
			if taskType == codeSearchTaskExecutionTrace {
				traceAnchors.SourceExactProbes = mergeCodeSearchProbeLists(traceAnchors.SourceExactProbes, plannerOut.SeedQueries)
				traceAnchors.TargetExactProbes = mergeCodeSearchProbeLists(traceAnchors.TargetExactProbes, plannerOut.SeedQueries)
				traceAnchors.SourcePathProbes = mergeCodeSearchProbeLists(traceAnchors.SourcePathProbes, plannerOut.PathBiases)
				traceAnchors.TargetPathProbes = mergeCodeSearchProbeLists(traceAnchors.TargetPathProbes, plannerOut.PathBiases)
			}
		}
	}
	phraseProbeSet := map[string]struct{}{}
	if taskType == codeSearchTaskExecutionTrace || taskType == codeSearchTaskChangeImpact {
		for _, probe := range phraseIdentifierProbes {
			probe = strings.TrimSpace(strings.ToLower(probe))
			if probe == "" {
				continue
			}
			phraseProbeSet[probe] = struct{}{}
		}
	}
	sourcePathProbeSet := stringSet(traceAnchors.SourcePathProbes)
	targetPathProbeSet := stringSet(traceAnchors.TargetPathProbes)
	sourceExactProbeSet := stringSet(traceAnchors.SourceExactProbes)
	targetExactProbeSet := stringSet(traceAnchors.TargetExactProbes)
	if traceAnchors.SourceQuery != "" {
		metadata["trace_source_query"] = traceAnchors.SourceQuery
	}
	if traceAnchors.TargetQuery != "" {
		metadata["trace_target_query"] = traceAnchors.TargetQuery
	}
	addLane := func(name string) {
		raw, _ := metadata["lanes_used"].([]string)
		for _, existing := range raw {
			if existing == name {
				return
			}
		}
		metadata["lanes_used"] = append(raw, name)
	}

	for _, candidate := range normalizedCandidates {
		if isExcludedCodeSearchPath(candidate, excluded) {
			continue
		}
		addCodeSearchCandidate(candidates, candidate, "caller supplied candidate", "caller", 1.0, 0, "", "")
	}

	for _, probe := range pathProbes {
		stageStart := time.Now()
		hits, err := codeSearchPathProbeSearch(a.workspaceRoot, probe, input.Budget.MaxCandidates)
		a.telemetry.record("path_probe", time.Since(stageStart), nil)
		if err != nil {
			gaps = append(gaps, "path probe search failed for "+probe+": "+err.Error())
			continue
		}
		if len(hits) == 0 {
			continue
		}
		addLane("path_probe")
		for _, hit := range hits {
			if hit.Path == "" || isExcludedCodeSearchPath(hit.Path, excluded) {
				continue
			}
			why := "path probe: " + probe
			support := 0.92
			if _, ok := phraseProbeSet[strings.TrimSpace(strings.ToLower(probe))]; ok {
				support = 1.25
			}
			addCodeSearchCandidate(candidates, hit.Path, why, "path_probe", support, 0, "", "")
			if _, ok := sourcePathProbeSet[probe]; ok {
				addCodeSearchCandidate(candidates, hit.Path, "trace source anchor: "+probe, "trace_source_anchor", 0.45, 0, "", "")
			}
			if _, ok := targetPathProbeSet[probe]; ok {
				addCodeSearchCandidate(candidates, hit.Path, "trace target anchor: "+probe, "trace_target_anchor", 0.3, 0, "", "")
			}
		}
	}

	for _, probe := range exactProbes {
		stageStart := time.Now()
		hits, err := exactCodeProbeSearch(a.workspaceRoot, probe, input.Budget.MaxCandidates)
		a.telemetry.record("exact_probe", time.Since(stageStart), nil)
		if err != nil {
			gaps = append(gaps, "exact probe search failed for "+probe+": "+err.Error())
			continue
		}
		if len(hits) == 0 {
			continue
		}
		addLane("exact_probe")
		for _, hit := range hits {
			if hit.Path == "" || isExcludedCodeSearchPath(hit.Path, excluded) {
				continue
			}
			why := "exact code probe: " + probe
			support := codeSearchFileWeight(hit.Path)
			if _, ok := phraseProbeSet[strings.TrimSpace(strings.ToLower(probe))]; ok && support < 1.05 {
				support = 1.05
			}
			addCodeSearchCandidate(candidates, hit.Path, why, "exact_probe", support, hit.Line, "", "")
			if _, ok := sourceExactProbeSet[probe]; ok && codeSearchPathTermScore(hit.Path, splitCodeSearchProbe(probe)) > 0 {
				addCodeSearchCandidate(candidates, hit.Path, "trace source anchor: "+probe, "trace_source_anchor", 0.55, hit.Line, "", "")
			}
			if _, ok := targetExactProbeSet[probe]; ok {
				addCodeSearchCandidate(candidates, hit.Path, "trace target anchor: "+probe, "trace_target_anchor", 0.35, hit.Line, "", "")
			}
		}
	}

	if taskType == codeSearchTaskFileLocate || taskType == codeSearchTaskSymbolInspect {
		semanticCtx, cancel := context.WithTimeout(ctx, codeSearchSemanticStageTimeout)
		defer cancel()
		if results, err := a.semanticSearchCode(semanticCtx, mustJSON(map[string]any{
			"query":   strings.TrimSpace(input.Query),
			"limit":   input.Budget.MaxCandidates,
			"profile": "code",
			"scope":   []string{"symbols"},
		})); err == nil {
			if bundleItems := semanticBundleMaps(results["candidate_bundles"]); len(bundleItems) > 0 {
				addLane("semantic_bundle")
				for _, item := range bundleItems {
					primaryPath := normalizeCodeSearchPath(stringValue(item["primary_path"]))
					if primaryPath == "" || isExcludedCodeSearchPath(primaryPath, excluded) {
						continue
					}
					why := firstNonEmpty(stringValue(item["match_reason"]), "semantic bundle primary")
					score := scoreValue(item["score"], 0.75)
					primaryAffinity := codeSearchBasenameProbeScore(primaryPath, pathProbes, exactProbes)
					if score < 0.72 {
						score = 0.72
					}
					if primaryAffinity > 0 && score < 0.82 {
						score = 0.82
					}
					addCodeSearchCandidate(candidates, primaryPath, why, "semantic_bundle_primary", score, 0, "", "")
					for _, related := range stringSliceValue(item["related_paths"]) {
						related = normalizeCodeSearchPath(related)
						if related == "" || isExcludedCodeSearchPath(related, excluded) {
							continue
						}
						relatedScore := score * 0.82
						if relatedScore < 0.58 {
							relatedScore = 0.58
						}
						if primaryAffinity > 0 && relatedScore < 0.72 {
							relatedScore = 0.72
						}
						addCodeSearchCandidate(candidates, related, "semantic bundle companion for "+primaryPath, "semantic_bundle_related", relatedScore, 0, "", "")
					}
				}
			}
			for _, item := range repoResultMaps(results["results"]) {
				pathValue := normalizeCodeSearchPath(stringValue(item["path"]))
				if pathValue == "" || isExcludedCodeSearchPath(pathValue, excluded) {
					continue
				}
				why := firstNonEmpty(stringValue(item["summary"]), "semantic candidate")
				addCodeSearchCandidate(candidates, pathValue, why, "semantic_search_code", scoreValue(item["similarity"], 0.6), 0, stringValue(item["name"]), "")
			}
		} else {
			gaps = append(gaps, "semantic_search_code failed: "+err.Error())
		}
	}

	if taskType == codeSearchTaskExecutionTrace || taskType == codeSearchTaskChangeImpact {
		for _, probe := range exactProbes {
			stageStart := time.Now()
			hits, err := executionBridgeSearch(a.workspaceRoot, probe, input.Budget.MaxCandidates)
			a.telemetry.record("execution_bridge", time.Since(stageStart), nil)
			if err != nil {
				gaps = append(gaps, "execution bridge search failed for "+probe+": "+err.Error())
				continue
			}
			if len(hits) == 0 {
				continue
			}
			addLane("execution_bridge")
			for _, hit := range hits {
				if hit.Path == "" || isExcludedCodeSearchPath(hit.Path, excluded) {
					continue
				}
				why := "execution bridge for " + probe
				addCodeSearchCandidate(candidates, hit.Path, why, "execution_bridge", 1.15, hit.Line, "", "")
			}
		}
	}

	var motifHits []contextplane.RepoMotifSearchHit
	motifPathScores := map[string]float64{}
	if taskType == codeSearchTaskExecutionTrace || taskType == codeSearchTaskFileLocate {
		stageStart := time.Now()
		var motifErr error
		motifHits, motifErr = a.searchRepoMotifs(ctx, input.Query, input.Budget.MaxCandidates)
		a.telemetry.record("repo_motif", time.Since(stageStart), nil)
		if motifErr != nil {
			gaps = append(gaps, "repo motif search failed: "+motifErr.Error())
		} else if len(motifHits) > 0 {
			motifPathScores = repoMotifPathScores(motifHits)
		}
	}

	store, err := repoindex.Open(ctx, a.cfg.Storage.Root, a.workspaceRoot)
	if err != nil {
		return nil, err
	}
	defer func() { _ = store.Close() }()
	querySvc := repoquery.NewQueryService(repoindex.NewQueryEngine(store))

	for _, probe := range codeSearchRepoProbes(input.Query) {
		stageStart := time.Now()
		searchOut, err := querySvc.SearchWithProjection(ctx, repoquery.SearchRequest{
			Query: probe,
			Limit: input.Budget.MaxCandidates,
		})
		a.telemetry.record("repo_index", time.Since(stageStart), nil)
		if err != nil {
			gaps = append(gaps, "repo search failed for "+probe+": "+err.Error())
			continue
		}
		if len(searchOut.Anchors) == 0 {
			continue
		}
		addLane("repo_index")
		for i, anchor := range searchOut.Anchors {
			pathValue := normalizeCodeSearchPath(anchor.Path)
			if pathValue == "" || isExcludedCodeSearchPath(pathValue, excluded) {
				continue
			}
			nodeID := ""
			if i < len(searchOut.Nodes) {
				nodeID = strings.TrimSpace(searchOut.Nodes[i].ID)
			}
			why := firstNonEmpty(strings.TrimSpace(anchor.Summary), "repo-index anchor")
			addCodeSearchCandidate(candidates, pathValue, why, "search_repo", anchor.Score, anchor.LineHint, anchor.SymbolName, nodeID)
		}
	}
	if taskType == codeSearchTaskExecutionTrace {
		for _, side := range []struct {
			query       string
			exactProbes []string
			source      string
			label       string
		}{
			{query: traceAnchors.SourceQuery, exactProbes: traceAnchors.SourceExactProbes, source: "trace_source_repo", label: "trace source repo anchor"},
			{query: traceAnchors.TargetQuery, exactProbes: traceAnchors.TargetExactProbes, source: "trace_target_repo", label: "trace target repo anchor"},
		} {
			probes := codeSearchExecutionTraceSideRepoProbes(side.query, side.exactProbes)
			for _, probe := range probes {
				stageStart := time.Now()
				searchOut, err := querySvc.SearchWithProjection(ctx, repoquery.SearchRequest{
					Query: probe,
					Limit: input.Budget.MaxCandidates,
				})
				a.telemetry.record("repo_index", time.Since(stageStart), nil)
				if err != nil {
					gaps = append(gaps, "repo search failed for "+probe+": "+err.Error())
					continue
				}
				if len(searchOut.Anchors) == 0 {
					continue
				}
				addLane("trace_anchor_repo")
				for i, anchor := range searchOut.Anchors {
					pathValue := normalizeCodeSearchPath(anchor.Path)
					if pathValue == "" || isExcludedCodeSearchPath(pathValue, excluded) {
						continue
					}
					nodeID := ""
					if i < len(searchOut.Nodes) {
						nodeID = strings.TrimSpace(searchOut.Nodes[i].ID)
					}
					why := firstNonEmpty(strings.TrimSpace(anchor.Summary), side.label)
					addCodeSearchCandidate(candidates, pathValue, why, "search_repo", anchor.Score*0.45, anchor.LineHint, anchor.SymbolName, nodeID)
					addCodeSearchCandidate(candidates, pathValue, side.label+": "+probe, side.source, 0.95, anchor.LineHint, anchor.SymbolName, nodeID)
				}
			}
		}
	}

	if len(motifHits) > 0 {
		if applyRepoMotifSupport(candidates, motifHits, taskType, excluded) > 0 {
			addLane("repo_motif")
		}
	}

	if taskType == codeSearchTaskExecutionTrace && len(candidates) > 0 {
		stageStart := time.Now()
		bridgeHits, bridgeQueries, bridgeGaps := a.codeSearchBridgeTupleAugment(ctx, querySvc, candidates, input.Query, input.Budget.MaxCandidates)
		a.telemetry.record("bridge_tuple", time.Since(stageStart), nil)
		gaps = append(gaps, bridgeGaps...)
		if len(bridgeQueries) > 0 {
			addLane("bridge_tuple")
			metadata["bridge_queries"] = bridgeQueries
			for _, hit := range bridgeHits {
				if hit.Path == "" || isExcludedCodeSearchPath(hit.Path, excluded) {
					continue
				}
				addCodeSearchCandidate(candidates, hit.Path, "bridge tuple from "+hit.SourcePath, "bridge_tuple", 1.35, hit.Line, hit.Symbol, hit.NodeID)
			}
		}
	}

	if len(candidates) == 0 && taskType != codeSearchTaskExecutionTrace {
		semanticCtx, cancel := context.WithTimeout(ctx, codeSearchSemanticStageTimeout)
		defer cancel()
		if results, err := a.semanticSearchCode(semanticCtx, mustJSON(map[string]any{
			"query": strings.TrimSpace(input.Query),
			"limit": input.Budget.MaxCandidates,
			"scope": []string{"symbols", "codemaps"},
		})); err == nil {
			addLane("semantic_code")
			for _, item := range repoResultMaps(results["results"]) {
				pathValue := normalizeCodeSearchPath(stringValue(item["path"]))
				if pathValue == "" || isExcludedCodeSearchPath(pathValue, excluded) {
					continue
				}
				why := firstNonEmpty(stringValue(item["summary"]), "semantic candidate")
				addCodeSearchCandidate(candidates, pathValue, why, "semantic_search_code", scoreValue(item["similarity"], 0.6), 0, stringValue(item["name"]), "")
			}
		} else {
			gaps = append(gaps, "semantic_search_code failed: "+err.Error())
		}
	}

	if taskType == codeSearchTaskRegistrationTrace {
		stageStart := time.Now()
		registrationHits, registrationGaps := codeSearchRegistrationAugment(a.workspaceRoot, candidates, input.Budget.MaxCandidates)
		a.telemetry.record("registration_trace", time.Since(stageStart), nil)
		gaps = append(gaps, registrationGaps...)
		if len(registrationHits) > 0 {
			addLane("registration_trace")
			for _, hit := range registrationHits {
				if hit.Path == "" || isExcludedCodeSearchPath(hit.Path, excluded) {
					continue
				}
				addCodeSearchCandidate(candidates, hit.Path, "registration site for "+hit.Symbol, "registration_trace", 1.25, hit.Line, hit.Symbol, "")
			}
		}
	}

	routeFamily = inferCodeSearchRouteFamilyFromCandidates(taskType, input.Query, candidates)
	metadata["route_family"] = string(routeFamily)
	autoACA := taskType == codeSearchTaskFileLocate || taskType == codeSearchTaskChangeImpact
	metadata["aca_auto_enabled"] = autoACA
	if input.Constraints.IncludeACA || autoACA {
		stageStart := time.Now()
		var acaErr error
		acaHits, acaErr = a.searchACAGuidance(ctx, input.Query, routeFamily, minInt(input.Budget.MaxCandidates, 6))
		a.telemetry.record("aca_guidance", time.Since(stageStart), nil)
		if acaErr != nil {
			gaps = append(gaps, "aca guidance failed: "+acaErr.Error())
		} else if len(acaHits) > 0 {
			acaRouteFamily := inferCodeSearchRouteFamily(taskType, acaHits)
			if taskType == codeSearchTaskFileLocate && routeFamily != codeSearchRouteInfraResource {
				packageHits, packageErr := a.searchACAGuidance(ctx, input.Query, codeSearchRoutePackageOwner, minInt(input.Budget.MaxCandidates, 6))
				if packageErr != nil {
					gaps = append(gaps, "aca package guidance failed: "+packageErr.Error())
				} else if shouldUsePackageACAGuidance(routeFamily, input.Query, packageHits) {
					acaHits = packageHits
					acaRouteFamily = codeSearchRoutePackageOwner
				}
			}
			routeFamily = promoteCodeSearchRouteFamily(routeFamily, input.Query, taskType, acaHits, acaRouteFamily)
			metadata["route_family"] = string(routeFamily)
			metadata["aca_guidance"] = summarizeACAGuidanceHits(acaHits, 4)
		}
	}

	if len(acaHits) > 0 && len(candidates) > 0 {
		if applied := applyACAGuidanceSupport(input.Query, candidates, acaHits, routeFamily, taskType, excluded); applied > 0 {
			addLane("aca_guidance")
		}
	}

	if taskType == codeSearchTaskExecutionTrace {
		stageStart := time.Now()
		bridgeHits, bridgeAnchors, bridgeGaps := codeSearchExecutionTraceAugment(ctx, querySvc, candidates, input.Query, input.Budget.MaxCandidates)
		a.telemetry.record("execution_graph", time.Since(stageStart), nil)
		gaps = append(gaps, bridgeGaps...)
		if len(bridgeAnchors) > 0 {
			addLane("execution_graph")
			for _, hit := range bridgeHits {
				if hit.Path == "" || isExcludedCodeSearchPath(hit.Path, excluded) {
					continue
				}
				addCodeSearchCandidate(candidates, hit.Path, "execution graph from "+hit.SourcePath, "execution_graph", 1.15, hit.Line, hit.Symbol, hit.NodeID)
				for _, edgeType := range hit.EdgeTypes {
					switch edgeType {
					case repoindex.EdgeImplements:
						addCodeSearchCandidate(candidates, hit.Path, "execution graph implements from "+hit.SourcePath, "execution_graph_implements", 0.95, hit.Line, hit.Symbol, hit.NodeID)
					case repoindex.EdgeUsesSymbol:
						addCodeSearchCandidate(candidates, hit.Path, "execution graph uses symbol from "+hit.SourcePath, "execution_graph_uses_symbol", 0.95, hit.Line, hit.Symbol, hit.NodeID)
					case repoindex.EdgeRefersTo:
						addCodeSearchCandidate(candidates, hit.Path, "execution graph refers-to from "+hit.SourcePath, "execution_graph_refers_to", 0.55, hit.Line, hit.Symbol, hit.NodeID)
					}
				}
			}
		}
	}

	if taskType == codeSearchTaskChangeImpact {
		stageStart := time.Now()
		impactHits, impactAnchors, impactGaps := codeSearchImpactAugment(ctx, querySvc, candidates, input.Budget.MaxCandidates)
		a.telemetry.record("impact_graph", time.Since(stageStart), nil)
		gaps = append(gaps, impactGaps...)
		if len(impactAnchors) > 0 {
			addLane("impact_graph")
			for _, hit := range impactHits {
				if hit.Path == "" || isExcludedCodeSearchPath(hit.Path, excluded) {
					continue
				}
				addCodeSearchCandidate(candidates, hit.Path, "impact graph from "+hit.SourcePath, "impact_graph", 1.1, hit.Line, hit.Symbol, hit.NodeID)
			}
		}
	}
	if taskType == codeSearchTaskExecutionTrace {
		stageStart := time.Now()
		adjacentHits, adjacentGaps := codeSearchAdjacentImplementationAugment(a.workspaceRoot, candidates, input.Query, pathProbes, exactProbes, input.Budget.MaxCandidates)
		a.telemetry.record("adjacent_impl", time.Since(stageStart), nil)
		gaps = append(gaps, adjacentGaps...)
		if len(adjacentHits) > 0 {
			addLane("adjacent_impl")
			for _, hit := range adjacentHits {
				if hit.Path == "" || isExcludedCodeSearchPath(hit.Path, excluded) {
					continue
				}
				addCodeSearchCandidate(candidates, hit.Path, "adjacent implementation near "+hit.SourcePath, "adjacent_impl", 1.05, hit.Line, "", "")
			}
		}
	}

	rankLimit := input.Budget.MaxCandidates
	if taskType == codeSearchTaskExecutionTrace {
		rankLimit = maxInt(input.Budget.MaxCandidates*3, 12)
	}
	replanOut := codeSearchLLMReplannerOutput{}
	replanEdgePrioritySet := map[string]struct{}{}
	replanMustCoverSet := map[string]struct{}{}
	preRanked := rankCodeSearchCandidatesWithProbes(candidates, input.Query, taskType, rankLimit, pathProbes, exactProbes)
	if taskType == codeSearchTaskExecutionTrace && input.Planner.Enabled && input.Planner.EnableReplan && shouldRunCodeSearchLLMReplanner(preRanked, input.Budget.MaxFiles, motifPathScores) {
		stageStart := time.Now()
		replanMeta, replanUsage, replanErr := map[string]any(nil), (*skillobs.TokenUsage)(nil), error(nil)
		replanOut, replanMeta, replanUsage, replanErr = a.codeSearchLLMReplanner(ctx, input.Query, taskType, preRanked, input.Budget.MaxFiles, motifPathScores, input.Planner, plannerOut)
		a.telemetry.record("llm_replanner", time.Since(stageStart), replanUsage)
		if replanErr != nil {
			gaps = append(gaps, "llm replanner failed: "+replanErr.Error())
		} else {
			metadata["llm_replanner"] = replanMeta
			replanEdgePrioritySet = stringSet(replanOut.RaiseEdgePriorities)
			replanMustCoverSet = stringSet(replanOut.MustCover)
			if !replanOut.StopBroadening {
				newExact := mergeCodeSearchProbeLists(nil, replanOut.AddSeedQueries)
				newPaths := mergeCodeSearchProbeLists(nil, replanOut.AddPathBiases)
				for _, probe := range newPaths {
					stageStart := time.Now()
					hits, err := codeSearchPathProbeSearch(a.workspaceRoot, probe, input.Budget.MaxCandidates)
					a.telemetry.record("replan_path_probe", time.Since(stageStart), nil)
					if err != nil {
						gaps = append(gaps, "replan path probe failed for "+probe+": "+err.Error())
						continue
					}
					for _, hit := range hits {
						if hit.Path == "" || isExcludedCodeSearchPath(hit.Path, excluded) {
							continue
						}
						addCodeSearchCandidate(candidates, hit.Path, "replan path probe: "+probe, "replan_path_probe", 0.92, 0, "", "")
					}
				}
				for _, probe := range newExact {
					stageStart := time.Now()
					hits, err := exactCodeProbeSearch(a.workspaceRoot, probe, input.Budget.MaxCandidates)
					a.telemetry.record("replan_exact_probe", time.Since(stageStart), nil)
					if err != nil {
						gaps = append(gaps, "replan exact probe failed for "+probe+": "+err.Error())
						continue
					}
					for _, hit := range hits {
						if hit.Path == "" || isExcludedCodeSearchPath(hit.Path, excluded) {
							continue
						}
						addCodeSearchCandidate(candidates, hit.Path, "replan exact probe: "+probe, "replan_exact_probe", codeSearchFileWeight(hit.Path), hit.Line, "", "")
					}
				}
				for _, probe := range newExact {
					stageStart := time.Now()
					searchOut, err := querySvc.SearchWithProjection(ctx, repoquery.SearchRequest{Query: probe, Limit: input.Budget.MaxCandidates})
					a.telemetry.record("replan_repo_index", time.Since(stageStart), nil)
					if err != nil {
						gaps = append(gaps, "replan repo search failed for "+probe+": "+err.Error())
						continue
					}
					for i, anchor := range searchOut.Anchors {
						pathValue := normalizeCodeSearchPath(anchor.Path)
						if pathValue == "" || isExcludedCodeSearchPath(pathValue, excluded) {
							continue
						}
						nodeID := ""
						if i < len(searchOut.Nodes) {
							nodeID = strings.TrimSpace(searchOut.Nodes[i].ID)
						}
						addCodeSearchCandidate(candidates, pathValue, "replan repo-index anchor", "replan_repo_search", anchor.Score, anchor.LineHint, anchor.SymbolName, nodeID)
					}
				}
			}
		}
	}
	if taskType == codeSearchTaskExecutionTrace && input.Selector.Enabled {
		preRanked := rankCodeSearchCandidatesWithPlan(candidates, input.Query, taskType, rankLimit, pathProbes, exactProbes, replanEdgePrioritySet, replanMustCoverSet, replanOut.AmbiguityFamily)
		if shouldRunCodeSearchLLMSelector(preRanked, input.Budget.MaxFiles) {
			stageStart := time.Now()
			selectorOut, selectorMeta, selectorUsage, selectorErr := a.codeSearchLLMSelector(ctx, input.Query, taskType, preRanked, input.Selector, input.Budget.MaxFiles)
			a.telemetry.record("llm_selector", time.Since(stageStart), selectorUsage)
			if selectorErr != nil {
				gaps = append(gaps, "llm selector failed: "+selectorErr.Error())
			} else {
				metadata["llm_selector"] = selectorMeta
				for _, keepPath := range selectorOut.KeepPaths {
					keepPath = normalizeCodeSearchPath(keepPath)
					if keepPath == "" {
						continue
					}
					if candidate := candidates[keepPath]; candidate != nil {
						if candidate.Sources == nil {
							candidate.Sources = map[string]struct{}{}
						}
						candidate.Sources["llm_selector_keep"] = struct{}{}
					}
				}
			}
		}
	}
	ranked := rankCodeSearchCandidatesWithPlan(candidates, input.Query, taskType, rankLimit, pathProbes, exactProbes, replanEdgePrioritySet, replanMustCoverSet, replanOut.AmbiguityFamily)
	if len(ranked) == 0 {
		return map[string]any{
			"summary":            "",
			"task_type":          taskType,
			"answer_basis":       "none",
			"confidence":         0.0,
			"files":              []codeSearchEvidenceFile{},
			"symbols":            []codeSearchEvidenceSymbol{},
			"snippets":           []codeSearchEvidenceSnippet{},
			"call_paths":         []map[string]any{},
			"supporting_context": []any{},
			"gaps":               append(gaps, "no candidate files found"),
			"metadata":           metadata,
		}, nil
	}

	files := make([]codeSearchEvidenceFile, 0, input.Budget.MaxFiles)
	symbols := make([]codeSearchEvidenceSymbol, 0, input.Budget.MaxFiles)
	snippets := make([]codeSearchEvidenceSnippet, 0, input.Budget.MaxSnippets)
	callPaths := make([]map[string]any, 0, input.Budget.MaxFiles*2)
	groundedCount := 0
	loadedChars := 0
	selectedPaths := map[string]int{}
	groundingQueue := append([]*codeSearchCandidate(nil), prioritizedGroundingCandidates(ranked, input.Query, taskType)...)
	rankedByPath := make(map[string]*codeSearchCandidate, len(ranked))
	for _, candidate := range ranked {
		if candidate == nil || candidate.Path == "" {
			continue
		}
		rankedByPath[candidate.Path] = candidate
	}
	if taskType == codeSearchTaskExecutionTrace && len(groundingQueue) > 0 {
		groundingQueue = applyCodeSearchResolverHintsToQueue(groundingQueue, replanOut.PreferPaths, replanOut.DemotePaths, codeSearchSameRoleAmbiguities(ranked, input.Budget.MaxFiles, motifPathScores), input.Budget.MaxFiles)
	}

	for idx := 0; idx < len(groundingQueue); idx++ {
		if len(files) >= input.Budget.MaxFiles {
			break
		}
		candidate := groundingQueue[idx]
		filePath := candidate.Path
		if filePath == "" || isExcludedCodeSearchPath(filePath, excluded) {
			continue
		}

		lineHint := firstPositive(candidate.LineHints...)
		if taskType == codeSearchTaskSymbolInspect {
			lineHint = lastPositive(candidate.LineHints...)
		}
		startLine, endLine := codeSearchSliceBounds(lineHint, taskType)
		stageStart := time.Now()
		loaded, loadErr := a.loadFile(mustJSON(map[string]any{
			"path":       filePath,
			"start_line": startLine,
			"end_line":   endLine,
		}))
		a.telemetry.record("load_file", time.Since(stageStart), nil)
		if loadErr != nil {
			gaps = append(gaps, fmt.Sprintf("load_file failed for %s: %v", filePath, loadErr))
			continue
		}

		groundedCount++
		addLane("load_file")
		confirmedBy := sortedSourceKeys(candidate.Sources)
		confirmedBy = appendIfMissing(confirmedBy, "load_file")
		files = append(files, codeSearchEvidenceFile{
			Path:         filePath,
			Why:          candidate.Why,
			SupportScore: clampScore(candidate.Support),
			ConfirmedBy:  confirmedBy,
		})
		if _, ok := selectedPaths[filePath]; !ok {
			selectedPaths[filePath] = len(files)
		}

		content := strings.TrimSpace(stringValue(loaded["content"]))
		extractedSymbols := extractGroundedEvidenceSymbols(
			filePath,
			content,
			intValue(loaded["start_line"]),
			lineHint,
			preferredSymbolProbes,
			candidate.Symbols,
			taskType,
		)
		for _, symbol := range extractedSymbols {
			symbol.Why = candidate.Why
			symbols = append(symbols, symbol)
		}
		if len(symbols) > input.Budget.MaxFiles {
			symbols = symbols[:input.Budget.MaxFiles]
		}

		if len(snippets) < input.Budget.MaxSnippets {
			if content != "" {
				loadedChars += len(content)
				snippetStart := startLine
				snippetEnd := endLine
				if taskType == codeSearchTaskSymbolInspect {
					for _, symbol := range extractedSymbols {
						if symbol.Line <= 0 {
							continue
						}
						snippetStart, snippetEnd = codeSearchSliceBounds(symbol.Line, taskType)
						break
					}
				}
				snippets = append(snippets, codeSearchEvidenceSnippet{
					Path:      filePath,
					StartLine: snippetStart,
					EndLine:   snippetEnd,
					Reason:    candidate.Why,
				})
			}
		}

		if taskType == codeSearchTaskExecutionTrace || taskType == codeSearchTaskChangeImpact {
			neighborPaths := make([]string, 0, 8)
			if taskType == codeSearchTaskExecutionTrace {
				protocolImplPaths := codeSearchProtocolImplementationPaths(a.workspaceRoot, filePath, content, 2)
				for _, pathValue := range protocolImplPaths {
					groundingQueue = moveCandidateEarlier(groundingQueue, idx+1, pathValue)
				}
			}
			for _, seed := range uniqueStrings(candidate.RepoNodeIDs) {
				if strings.TrimSpace(seed) == "" {
					continue
				}
				directions := []repoindex.Direction{repoindex.DirOut}
				if taskType == codeSearchTaskChangeImpact {
					directions = []repoindex.Direction{repoindex.DirOut, repoindex.DirIn}
				}
				for _, direction := range directions {
					stageStart := time.Now()
					expanded, expandErr := querySvc.ExpandWithProjection(ctx, repoquery.ExpandRequest{
						Seeds:      []string{seed},
						EdgeTypes:  repoindex.EdgeSetStructural,
						Direction:  direction,
						Depth:      2,
						Budget:     input.Budget.MaxCandidates * 3,
						PerNodeCap: 20,
					})
					a.telemetry.record("repo_graph", time.Since(stageStart), nil)
					if expandErr != nil {
						gaps = append(gaps, fmt.Sprintf("expand_repo_graph failed for %s: %v", seed, expandErr))
						continue
					}
					addLane("repo_graph")
					for _, anchor := range expanded.Anchors {
						pathValue := normalizeCodeSearchPath(anchor.Path)
						if pathValue == "" || isExcludedCodeSearchPath(pathValue, excluded) {
							continue
						}
						neighborPaths = append(neighborPaths, pathValue)
						callPaths = append(callPaths, map[string]any{
							"path":        pathValue,
							"symbol_name": strings.TrimSpace(anchor.SymbolName),
							"line_hint":   anchor.LineHint,
							"source":      "expand_repo_graph",
							"direction":   string(direction),
						})
					}
				}
				if len(callPaths) > input.Budget.MaxFiles*3 {
					callPaths = callPaths[:input.Budget.MaxFiles*3]
				}
				break
			}
			groundingQueue = promoteGroundedRepoGraphNeighbors(groundingQueue, idx+1, rankedByPath, selectedPaths, neighborPaths, input.Query, taskType)
		}
	}

	metadata["grounded"] = groundedCount > 0
	if requireGrounding && groundedCount == 0 {
		gaps = append(gaps, "no grounded files were loaded")
	}

	if len(files) == 0 && len(ranked) > 0 {
		for _, candidate := range ranked[:minInt(len(ranked), input.Budget.MaxFiles)] {
			files = append(files, codeSearchEvidenceFile{
				Path:         candidate.Path,
				Why:          candidate.Why,
				SupportScore: clampScore(candidate.Support * 0.7),
				ConfirmedBy:  sortedSourceKeys(candidate.Sources),
			})
			if _, ok := selectedPaths[candidate.Path]; !ok {
				selectedPaths[candidate.Path] = len(files)
			}
		}
	}

	metadata["candidate_trace"] = buildCodeSearchCandidateTrace(ranked, selectedPaths, input.Budget.MaxFiles, taskType)
	if taskType == codeSearchTaskFileLocate {
		metadata["file_locate_evidence_buckets"] = buildFileLocateEvidenceBuckets(files, rankedByPath)
	}

	answerBasis := buildCodeSearchAnswerBasis(metadata["lanes_used"].([]string))
	confidence := codeSearchConfidence(taskType, len(files), groundedCount, len(callPaths), requireGrounding)
	telemetry := a.telemetry.snapshot()
	evidencePayload := map[string]any{
		"summary":            buildCodeSearchSummary(taskType, files, callPaths, gaps),
		"task_type":          taskType,
		"answer_basis":       answerBasis,
		"confidence":         confidence,
		"files":              files,
		"symbols":            dedupeCodeSearchSymbols(symbols),
		"snippets":           snippets,
		"call_paths":         callPaths,
		"supporting_context": []any{},
		"gaps":               uniqueStrings(gaps),
	}
	if taskType == codeSearchTaskExecutionTrace || taskType == codeSearchTaskRegistrationTrace {
		buckets := buildExecutionTraceBuckets(ranked, selectedPaths)
		evidencePayload["direct_dispatch_files"] = buckets["direct_dispatch_files"]
		evidencePayload["exposure_files"] = buckets["exposure_files"]
		evidencePayload["structural_support_files"] = buckets["structural_support_files"]
		evidencePayload["registration_files"] = buckets["registration_files"]
	}
	emittedBytes, _ := json.Marshal(evidencePayload)
	loadedTokenEstimate := loadedChars / memtokens.CharsPerToken
	emittedTokenEstimate := len(emittedBytes) / memtokens.CharsPerToken
	telemetry["loaded_chars"] = loadedChars
	telemetry["loaded_token_estimate"] = loadedTokenEstimate
	telemetry["emitted_chars"] = len(emittedBytes)
	telemetry["emitted_token_estimate"] = emittedTokenEstimate
	telemetry["parent_input_token_savings_estimate"] = maxInt(0, loadedTokenEstimate-emittedTokenEstimate)
	if loadedTokenEstimate > 0 {
		telemetry["compaction_ratio"] = float64(emittedTokenEstimate) / float64(loadedTokenEstimate)
	}
	metadata["telemetry"] = telemetry
	_ = observability.EmitSync(ctx, observability.NewEvent("context.code_search_ensemble").
		WithComponent(observability.ComponentContextBuilder).
		WithCommand("code_search_ensemble").
		WithWorkspace(a.workspaceRoot).
		WithData("query_hash", observability.HashQuestion(input.Query)).
		WithData("task_type", taskType).
		WithData("answer_basis", answerBasis).
		WithData("grounded", groundedCount > 0).
		WithData("files_count", len(files)).
		WithData("snippets_count", len(snippets)).
		WithData("call_paths_count", len(callPaths)).
		WithDataMap(telemetry).
		Success(time.Since(started)))
	evidencePayload["metadata"] = metadata
	return evidencePayload, nil
}

func (a *ReadOnlyAdapter) searchRepoMotifs(ctx context.Context, query string, limit int) ([]contextplane.RepoMotifSearchHit, error) {
	if strings.TrimSpace(query) == "" || strings.TrimSpace(a.workspaceRoot) == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 8
	}
	memStore, err := memorystore.OpenWithConfig(ctx, a.cfg)
	if err != nil {
		return nil, err
	}
	defer func() { _ = memStore.Close() }()
	return contextplane.SearchRepoMotifArtifacts(ctx, a.workspaceRoot, query, limit, memStore, nil)
}

func (a *ReadOnlyAdapter) searchACAGuidance(ctx context.Context, query string, routeFamily codeSearchRouteFamily, limit int) ([]contextplane.RetrievalHit, error) {
	if strings.TrimSpace(query) == "" || strings.TrimSpace(a.workspaceRoot) == "" {
		return nil, nil
	}
	vaultPath := firstNonEmpty(strings.TrimSpace(a.vaultPath),
		strings.TrimSpace(os.Getenv("FOXCTL_RLM_VAULT_PATH")),
		strings.TrimSpace(os.Getenv("FOXCTL_ACA_VAULT_PATH")),
		strings.TrimSpace(os.Getenv("FOXCTL_OBSIDIAN_VAULT_PATH")),
	)
	if vaultPath == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 6
	}
	index, err := obsidianindex.Open(ctx, a.cfg.Storage.Root, vaultPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = index.Close() }()
	if routeFamily == codeSearchRoutePackageOwner {
		return searchPackageACAGuidance(index, a.workspaceRoot, query, limit)
	}
	repoStore, err := repoindex.Open(ctx, a.cfg.Storage.Root, a.workspaceRoot)
	if err != nil {
		return nil, err
	}
	defer func() { _ = repoStore.Close() }()
	memStore, err := memorystore.OpenWithConfig(ctx, a.cfg)
	if err != nil {
		return nil, err
	}
	defer func() { _ = memStore.Close() }()
	workspaceStore := contextplane.NewWorkspaceStore(a.workspaceRoot)
	opts := workspaceStore.CurrentRetrievalOptions()
	opts.IncludeTopOfMindResult = false
	opts.IncludeLatestHandoff = false
	opts.IncludeVaultHits = true
	opts.UseRelevantRefBoost = false
	opts.UseHandoffRefBoost = false
	opts.IncludeControlPlaneRefs = false
	opts.UseSemanticVaultSearch = false
	if routeFamily == codeSearchRoutePackageOwner {
		opts.UseRepoMotifPrior = false
		opts.UseCodeHints = false
		opts.UseCoChangePrior = false
	}
	opts.AllowedTrusts = []string{"canonical"}
	result, err := workspaceStore.RetrieveWithOptionsAndMemory(ctx, index, repoStore, nil, memStore, query, limit, opts)
	if err != nil {
		return nil, err
	}
	return result.VaultHits, nil
}

func searchPackageACAGuidance(index obsidianindex.Store, workspaceRoot, query string, limit int) ([]contextplane.RetrievalHit, error) {
	if index == nil || strings.TrimSpace(query) == "" || strings.TrimSpace(workspaceRoot) == "" {
		return nil, nil
	}
	searchLimit := maxInt(limit*12, 24)
	hits, err := index.SearchNotes(context.Background(), strings.TrimSpace(query), searchLimit)
	if err != nil {
		return nil, err
	}
	return packageACAGuidanceHitsFromSearchHits(workspaceRoot, query, hits, limit), nil
}

func packageACAGuidanceHitsFromSearchHits(workspaceRoot, query string, hits []obsidianindex.SearchHit, limit int) []contextplane.RetrievalHit {
	if len(hits) == 0 || strings.TrimSpace(workspaceRoot) == "" {
		return nil
	}
	repoName := strings.ToLower(strings.TrimSpace(filepath.Base(workspaceRoot)))
	if repoName == "" {
		return nil
	}
	prefix := "notes/repo/" + repoName + "/packages/"
	type scoredHit struct {
		hit   obsidianindex.SearchHit
		score int
	}
	scored := make([]scoredHit, 0, len(hits))
	queryTerms := codeSearchPathTerms(query)
	for _, hit := range hits {
		pathValue := strings.ToLower(strings.TrimSpace(hit.Path))
		if !strings.HasPrefix(pathValue, prefix) {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(hit.Trust), "canonical") {
			continue
		}
		score := maxInt(0, hit.Score)
		score += int(acaGuidanceTitleAffinity(hit.Title, query) * 120)
		anchorPaths := preferredPackageAnchorPaths(contextplane.RetrievalHit{
			PrimaryAnchorPath: hit.PrimaryAnchorPath,
			AnchorPaths:       hit.AnchorPaths,
			AnchorRoles:       cloneAnchorRoles(hit.AnchorRoles),
		})
		if len(anchorPaths) == 0 {
			anchorPaths = normalizeCodeSearchPaths(hit.AnchorPaths)
			if primary := normalizeCodeSearchPath(hit.PrimaryAnchorPath); primary != "" {
				anchorPaths = append([]string{primary}, anchorPaths...)
			}
		}
		score += int(anchorPathAffinityScore(anchorPaths, queryTerms) * 120)
		if primary := normalizeCodeSearchPath(hit.PrimaryAnchorPath); primary != "" {
			score += int(packageAnchorBasenameQueryAffinity(primary, queryTerms) * 60)
		}
		if repoPathCount := len(normalizeCodeSearchPaths(hit.RepoPaths)); repoPathCount > 0 {
			score += maxInt(0, 10-repoPathCount)
		}
		scored = append(scored, scoredHit{hit: hit, score: score})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return strings.TrimSpace(scored[i].hit.Path) < strings.TrimSpace(scored[j].hit.Path)
	})
	if limit <= 0 {
		limit = 1
	}
	if len(scored) > limit {
		scored = scored[:limit]
	}
	out := make([]contextplane.RetrievalHit, 0, len(scored))
	for _, item := range scored {
		out = append(out, contextplane.RetrievalHit{
			Path:              strings.TrimSpace(item.hit.Path),
			Title:             strings.TrimSpace(item.hit.Title),
			Type:              strings.TrimSpace(item.hit.Type),
			Trust:             strings.TrimSpace(item.hit.Trust),
			Score:             item.score,
			Snippet:           strings.TrimSpace(item.hit.Snippet),
			PrimaryAnchorPath: strings.TrimSpace(item.hit.PrimaryAnchorPath),
			RepoPaths:         append([]string(nil), item.hit.RepoPaths...),
			AnchorPaths:       append([]string(nil), item.hit.AnchorPaths...),
			AnchorRoles:       cloneAnchorRoles(item.hit.AnchorRoles),
			Symbols:           append([]string(nil), item.hit.Symbols...),
		})
	}
	return out
}

func shouldUsePackageACAGuidance(routeFamily codeSearchRouteFamily, query string, hits []contextplane.RetrievalHit) bool {
	if len(hits) == 0 {
		return false
	}
	if routeFamily == codeSearchRoutePackageOwner {
		return true
	}
	return packageACAGuidanceSpecificityScore(query, hits[0]) >= 1.25
}

func promoteCodeSearchRouteFamily(current codeSearchRouteFamily, query, taskType string, hits []contextplane.RetrievalHit, inferred codeSearchRouteFamily) codeSearchRouteFamily {
	if current != codeSearchRouteCode || inferred == codeSearchRouteCode {
		return current
	}
	if inferred == codeSearchRoutePackageOwner && !shouldUsePackageACAGuidance(current, query, hits) {
		return current
	}
	return inferred
}

func applyACAGuidanceCandidates(workspaceRoot, query string, candidates map[string]*codeSearchCandidate, hits []contextplane.RetrievalHit, taskType string, excluded []string) int {
	if len(candidates) == 0 && len(hits) == 0 {
		return 0
	}
	applied := 0
	for _, hit := range hits {
		if acaGuidanceMatchCount(hit, taskType, query) < 2 {
			continue
		}
		repoPaths := limitedACARepoPaths(workspaceRoot, hit, 6)
		if len(repoPaths) == 0 {
			continue
		}
		support := codeSearchACAGuidanceSupport(taskType, hit)
		why := "aca guidance from " + strings.TrimSpace(hit.Path)
		for _, repoPath := range repoPaths {
			repoPath = normalizeCodeSearchPath(repoPath)
			if repoPath == "" || isExcludedCodeSearchPath(repoPath, excluded) {
				continue
			}
			symbolName := firstCodeLikeACASymbol(hit.Symbols)
			addCodeSearchCandidate(candidates, repoPath, why, "aca_guidance", support, 0, symbolName, "")
			applied++
		}
	}
	return applied
}

func applyACAGuidanceSupport(query string, candidates map[string]*codeSearchCandidate, hits []contextplane.RetrievalHit, routeFamily codeSearchRouteFamily, taskType string, excluded []string) int {
	if len(candidates) == 0 || len(hits) == 0 {
		return 0
	}
	exactSource, exactSupport, symbolSource, symbolSupport := acaGuidanceSupportWeights(routeFamily, taskType)
	if exactSupport <= 0 && symbolSupport <= 0 {
		return 0
	}
	hits = topACAGuidanceSupportHits(query, hits, routeFamily, taskType)
	if len(hits) == 0 {
		return 0
	}
	applied := 0
	minMatches := acaGuidanceMinimumMatchCount(routeFamily, taskType)
	queryTerms := codeSearchPathTerms(query)
	exactProbes := codeSearchExactProbes(query)
	pathProbes := codeSearchPathProbes(query)
	for _, hit := range hits {
		if acaGuidanceMatchCount(hit, taskType, query) < minMatches {
			continue
		}
		repoPaths := normalizeCodeSearchPaths(hit.RepoPaths)
		if routeFamily == codeSearchRoutePackageOwner {
			packageAnchors := preferredPackageAnchorPaths(hit)
			if len(packageAnchors) == 0 {
				packageAnchors = selectACAPackageAnchorPaths(repoPaths, candidates, hit.Symbols, queryTerms, pathProbes, exactProbes, 2)
			}
			primaryAnchor := normalizeCodeSearchPath(hit.PrimaryAnchorPath)
			for idx, repoPath := range packageAnchors {
				if isExcludedCodeSearchPath(repoPath, excluded) {
					continue
				}
				support := 1.1
				if idx == 0 {
					support = 1.3
				}
				if primaryAnchor != "" && repoPath == primaryAnchor {
					support = 1.35
				}
				symbolName := bestACASymbolForPath(repoPath, hit.Symbols)
				anchorRoleSource := "aca_route_package_secondary_anchor"
				if idx == 0 || (primaryAnchor != "" && repoPath == primaryAnchor) {
					anchorRoleSource = "aca_route_package_primary_anchor"
				}
				if candidates[repoPath] == nil {
					if !shouldIntroduceACAPackageAnchorCandidate(repoPath, primaryAnchor, candidates) {
						continue
					}
					addCodeSearchCandidate(candidates, repoPath, "aca package anchor from "+strings.TrimSpace(hit.Path), "aca_route_package_anchor", support, 0, symbolName, "")
					addCodeSearchCandidate(candidates, repoPath, "aca package anchor role from "+strings.TrimSpace(hit.Path), anchorRoleSource, 0, 0, "", "")
					applied++
					continue
				}
				candidate := candidates[repoPath]
				if candidate.Sources == nil {
					candidate.Sources = map[string]struct{}{}
				}
				if _, ok := candidate.Sources["aca_route_package_anchor"]; !ok {
					candidate.Sources["aca_route_package_anchor"] = struct{}{}
					applied++
				}
				candidate.Sources[anchorRoleSource] = struct{}{}
				candidate.Support += support
				if symbolName != "" {
					candidate.Symbols = append(candidate.Symbols, symbolName)
				}
			}
		}
		exactPaths := repoPaths
		primaryExactPath := ""
		if routeFamily == codeSearchRouteInfraResource {
			if resourcePaths := preferredInfraAnchorPaths(hit); len(resourcePaths) > 0 {

```

(lines 2149-2335: Task-type normalization and core exact/repo probe derivation heuristics for lane fan-out.)
```go
func normalizeCodeSearchTaskType(taskType string) (string, bool) {
	switch strings.TrimSpace(taskType) {
	case codeSearchTaskFileLocate:
		return codeSearchTaskFileLocate, false
	case codeSearchTaskExecutionTrace:
		return codeSearchTaskExecutionTrace, false
	case codeSearchTaskSymbolInspect:
		return codeSearchTaskSymbolInspect, false
	case codeSearchTaskChangeImpact:
		return codeSearchTaskChangeImpact, false
	case codeSearchTaskRegistrationTrace:
		return codeSearchTaskRegistrationTrace, false
	case "":
		return codeSearchTaskFileLocate, true
	default:
		return codeSearchTaskFileLocate, true
	}
}

func repoResultMaps(raw any) []map[string]any {
	items, ok := raw.([]map[string]any)
	if ok {
		return items
	}
	body, _ := json.Marshal(raw)
	var out []map[string]any
	_ = json.Unmarshal(body, &out)
	return out
}

func semanticBundleMaps(raw any) []map[string]any {
	items, ok := raw.([]map[string]any)
	if ok {
		return items
	}
	body, _ := json.Marshal(raw)
	var out []map[string]any
	_ = json.Unmarshal(body, &out)
	return out
}

var (
	codeLikeTokenPattern          = regexp.MustCompile(`[\pL\pN_./:-]+`)
	groundedTypeSymbolPattern     = regexp.MustCompile(`^\s*type\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
	groundedFuncSymbolPattern     = regexp.MustCompile(`^\s*func\s*(?:\([^)]+\)\s*)?([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	groundedVarConstSymbolPattern = regexp.MustCompile(`^\s*(?:var|const)\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
	bridgeCtorAssignPattern       = regexp.MustCompile(`(?m)([A-Za-z_][A-Za-z0-9_]*)\s*(?::=|=)\s*(?:[A-Za-z_][A-Za-z0-9_]*\.)?New([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	bridgeMethodLiteralPattern    = regexp.MustCompile(`(?m)([A-Za-z_][A-Za-z0-9_]*)\.(Execute)\s*\([^\n]*"([A-Za-z_][A-Za-z0-9_]*)"`)
	elixirProtocolPattern         = regexp.MustCompile(`(?m)^\s*defprotocol\s+([A-Za-z0-9_.]+)\s+do`)
)

func codeSearchExactProbes(query string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 4)
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	for _, token := range codeLikeTokenPattern.FindAllString(query, -1) {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		if isGenericCodeSearchPathWord(token) {
			continue
		}
		if !looksLikeCodeProbe(token) {
			continue
		}
		add(token)
		if len(out) >= 4 {
			break
		}
	}
	return out
}

func codeSearchTaskExactProbes(query, taskType string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 6)
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	for _, probe := range codeSearchExactProbes(query) {
		add(probe)
	}
	if taskType == codeSearchTaskExecutionTrace || taskType == codeSearchTaskChangeImpact {
		for _, probe := range normalizedExactProbePrefixes(codeSearchExactProbes(query)) {
			add(probe)
			if len(out) >= 6 {
				return out
			}
		}
		normalizedExisting := normalizedExactProbePrefixes(out)
		for _, probe := range codeSearchPhraseIdentifierProbes(query) {
			joined := strings.Join(splitCodeSearchProbe(probe), "_")
			skip := false
			for _, existing := range normalizedExisting {
				if existing == "" || existing == joined {
					continue
				}
				if strings.HasPrefix(existing, joined+"_") {
					skip = true
					break
				}
			}
			if skip {
				continue
			}
			add(probe)
			if len(out) >= 6 {
				break
			}
		}
	}
	return out
}

func codeSearchRepoProbes(query string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 6)
	normalizedExact := normalizedExactProbePrefixes(codeSearchExactProbes(query))
	isStrictExactPrefix := func(value string) bool {
		parts := splitCodeSearchProbe(value)
		if len(parts) == 0 {
			return false
		}
		joined := strings.Join(parts, "_")
		for _, exact := range normalizedExact {
			if exact == "" || exact == joined {
				continue
			}
			if strings.HasPrefix(exact, joined+"_") {
				return true
			}
		}
		return false
	}
	add := func(value string) {
		value = normalizeRepoSearchProbe(value)
		if value == "" {
			return
		}
		if isStrictExactPrefix(value) {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	add(query)
	for _, probe := range codeSearchExactProbes(query) {
		add(probe)
		if strings.Contains(probe, "_") {
			add(strings.ReplaceAll(probe, "_", " "))
		}
		if idx := strings.LastIndex(probe, "_"); idx > 0 {
			add(probe[:idx])
		}
	}
	if len(out) == 0 {
		add(query)
	}
	return out
}

func codeSearchExecutionTraceSideRepoProbes(query string, exactProbes []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 8)
	add := func(value string) {

```

(lines 5360-5670: LLM selector/planner/replanner integration for the ensemble, including provider/model resolution, bounded JSON schemas, and replanning trigger context.)
```go
	}
	for _, edgeType := range incoming {
		if _, ok := seen[edgeType]; ok {
			continue
		}
		seen[edgeType] = struct{}{}
		out = append(out, edgeType)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func (a *ReadOnlyAdapter) codeSearchLLMSelector(
	ctx context.Context,
	query, taskType string,
	ranked []*codeSearchCandidate,
	selector codeSearchLLMSelectorInput,
	maxFiles int,
) (codeSearchLLMSelectorOutput, map[string]any, *skillobs.TokenUsage, error) {
	var empty codeSearchLLMSelectorOutput
	if len(ranked) == 0 || !selector.Enabled {
		return empty, nil, nil, nil
	}

	provider := strings.TrimSpace(selector.Provider)
	if provider == "" {
		if strings.TrimSpace(a.cfg.LLM.OpenRouterAPIKey) != "" {
			provider = "openrouter"
		} else {
			provider = strings.TrimSpace(a.cfg.LLM.Provider)
		}
	}
	if provider == "" {
		return empty, nil, nil, fmt.Errorf("no llm provider configured")
	}
	apiKey := a.cfg.LLM.ResolveAPIKey(provider)
	if strings.TrimSpace(apiKey) == "" {
		return empty, nil, nil, fmt.Errorf("no api key configured for provider %q", provider)
	}
	model := strings.TrimSpace(selector.Model)
	if model == "" {
		if provider == "openrouter" {
			model = "openai/gpt-5.4-nano"
		} else {
			model = a.cfg.LLM.ResolveModel(provider)
		}
	}
	if model == "" {
		return empty, nil, nil, fmt.Errorf("no model configured for provider %q", provider)
	}

	maxCandidates := selector.MaxCandidates
	if maxCandidates <= 0 {
		maxCandidates = 8
	}
	if maxCandidates > len(ranked) {
		maxCandidates = len(ranked)
	}
	candidates := make([]map[string]any, 0, maxCandidates)
	for _, candidate := range ranked[:maxCandidates] {
		if candidate == nil || candidate.Path == "" {
			continue
		}
		edgeHints := make([]string, 0, 3)
		for _, source := range []string{"execution_graph_implements", "execution_graph_uses_symbol", "execution_graph_refers_to"} {
			if candidateHasSource(candidate, source) {
				edgeHints = append(edgeHints, source)
			}
		}
		candidates = append(candidates, map[string]any{
			"path":        candidate.Path,
			"why":         candidate.Why,
			"sources":     sortedSourceKeys(candidate.Sources),
			"symbols":     uniqueStrings(candidate.Symbols),
			"line_hints":  uniquePositiveInts(candidate.LineHints),
			"edge_hints":  edgeHints,
			"support":     candidate.Support,
			"repo_nodes":  len(uniqueStrings(candidate.RepoNodeIDs)),
			"code_weight": codeSearchFileWeight(candidate.Path),
		})
	}
	if len(candidates) == 0 {
		return empty, nil, nil, nil
	}

	reqPayload := map[string]any{
		"query":           strings.TrimSpace(query),
		"task_type":       taskType,
		"max_files":       maxFiles,
		"candidate_count": len(candidates),
		"candidates":      candidates,
	}
	body, err := json.Marshal(reqPayload)
	if err != nil {
		return empty, nil, nil, err
	}

	responseFormat := json.RawMessage(`{
		"type":"json_schema",
		"json_schema":{
			"name":"code_search_selector",
			"schema":{
				"type":"object",
				"additionalProperties":false,
				"properties":{
					"keep_paths":{"type":"array","items":{"type":"string"}},
					"rationale":{"type":"string"}
				},
				"required":["keep_paths","rationale"]
			}
		}
	}`)
	llmCfg := engine.DefaultLLMChatConfig()
	llmCfg.Provider = provider
	llmCfg.APIKey = apiKey
	llmCfg.BaseURL = a.cfg.LLM.ResolveBaseURL(provider)
	llmCfg.Model = model
	llmCfg.Timeout = 20 * time.Second
	llmCfg.MaxIterations = 1
	llmCfg.MaxTokens = 500
	llmCfg.Temperature = 0
	llmCfg.ResponseFormat = responseFormat

	llm, err := engine.NewLLMChatEngine(llmCfg)
	if err != nil {
		return empty, nil, nil, err
	}
	systemPrompt := "You are a bounded code retrieval adjudicator. Choose up to max_files repo-relative paths that best complete the answer. Prefer direct source/target coverage, protocol implementation pairs, and typed graph edges like IMPLEMENTS and USES_SYMBOL. Avoid helper or documentation noise. Return only structured JSON."
	output, err := llm.Run(ctx, engine.EngineInput{
		SystemPrompt: systemPrompt,
		Messages:     []engine.Message{engine.NewUserMessage(string(body))},
		MaxTokens:    llmCfg.MaxTokens,
		Temperature:  llmCfg.Temperature,
	})
	if err != nil {
		return empty, nil, nil, err
	}
	if output.StopReason == engine.StopReasonError {
		return empty, nil, nil, errors.New(strings.TrimSpace(output.Error))
	}
	usage := skillobs.CalculateTokenCost(llmCfg.Model, output.Tokens.InputTokens, output.Tokens.OutputTokens)
	var selectorOut codeSearchLLMSelectorOutput
	if err := json.Unmarshal([]byte(strings.TrimSpace(output.AssistantText)), &selectorOut); err != nil {
		return empty, nil, &usage, fmt.Errorf("parse selector output: %w", err)
	}
	selectorOut.KeepPaths = normalizeCodeSearchPaths(selectorOut.KeepPaths)
	if maxFiles > 0 && len(selectorOut.KeepPaths) > maxFiles {
		selectorOut.KeepPaths = selectorOut.KeepPaths[:maxFiles]
	}
	meta := map[string]any{
		"provider":   provider,
		"model":      model,
		"keep_paths": append([]string(nil), selectorOut.KeepPaths...),
		"rationale":  strings.TrimSpace(selectorOut.Rationale),
	}
	return selectorOut, meta, &usage, nil
}

func (a *ReadOnlyAdapter) codeSearchLLMPlanner(
	ctx context.Context,
	query, taskType string,
	exactProbes, pathProbes []string,
	planner codeSearchLLMPlannerInput,
) (codeSearchLLMPlannerOutput, map[string]any, *skillobs.TokenUsage, error) {
	var empty codeSearchLLMPlannerOutput
	if !planner.Enabled {
		return empty, nil, nil, nil
	}
	provider := strings.TrimSpace(planner.Provider)
	if provider == "" {
		if strings.TrimSpace(a.cfg.LLM.OpenRouterAPIKey) != "" {
			provider = "openrouter"
		} else {
			provider = strings.TrimSpace(a.cfg.LLM.Provider)
		}
	}
	if provider == "" {
		return empty, nil, nil, fmt.Errorf("no llm provider configured")
	}
	apiKey := a.cfg.LLM.ResolveAPIKey(provider)
	if strings.TrimSpace(apiKey) == "" {
		return empty, nil, nil, fmt.Errorf("no api key configured for provider %q", provider)
	}
	model := strings.TrimSpace(planner.Model)
	if model == "" {
		if provider == "openrouter" {
			model = "openai/gpt-5.4-nano"
		} else {
			model = a.cfg.LLM.ResolveModel(provider)
		}
	}
	if model == "" {
		return empty, nil, nil, fmt.Errorf("no model configured for provider %q", provider)
	}
	maxCandidates := planner.MaxCandidates
	if maxCandidates <= 0 {
		maxCandidates = 8
	}
	if len(exactProbes) > maxCandidates {
		exactProbes = exactProbes[:maxCandidates]
	}
	if len(pathProbes) > maxCandidates {
		pathProbes = pathProbes[:maxCandidates]
	}

	reqPayload := map[string]any{
		"query":          strings.TrimSpace(query),
		"task_type":      taskType,
		"existing_exact": append([]string(nil), exactProbes...),
		"existing_paths": append([]string(nil), pathProbes...),
		"max_candidates": maxCandidates,
		"allowed_edge_types": []string{
			string(repoindex.EdgeCalls),
			string(repoindex.EdgeImports),
			string(repoindex.EdgeRefersTo),
			string(repoindex.EdgeImplements),
			string(repoindex.EdgeUsesSymbol),
		},
	}
	body, err := json.Marshal(reqPayload)
	if err != nil {
		return empty, nil, nil, err
	}
	responseFormat := json.RawMessage(`{
		"type":"json_schema",
		"json_schema":{
			"name":"code_search_planner",
			"schema":{
				"type":"object",
				"additionalProperties":false,
				"properties":{
					"seed_queries":{"type":"array","items":{"type":"string"}},
					"path_biases":{"type":"array","items":{"type":"string"}},
					"edge_priorities":{"type":"array","items":{"type":"string"}},
					"must_cover":{"type":"array","items":{"type":"string"}},
					"ambiguity_family":{"type":"string"},
					"max_widen_steps":{"type":"integer"},
					"rationale":{"type":"string"}
				},
				"required":["seed_queries","path_biases","edge_priorities","must_cover","ambiguity_family","max_widen_steps","rationale"]
			}
		}
	}`)
	llmCfg := engine.DefaultLLMChatConfig()
	llmCfg.Provider = provider
	llmCfg.APIKey = apiKey
	llmCfg.BaseURL = a.cfg.LLM.ResolveBaseURL(provider)
	llmCfg.Model = model
	llmCfg.Timeout = 20 * time.Second
	llmCfg.MaxIterations = 1
	llmCfg.MaxTokens = 400
	llmCfg.Temperature = 0
	llmCfg.ResponseFormat = responseFormat

	llm, err := engine.NewLLMChatEngine(llmCfg)
	if err != nil {
		return empty, nil, nil, err
	}
	systemPrompt := "You are a bounded code retrieval planner. Given a repo question and current probe candidates, choose a few better seed queries, path biases, structural edge priorities, required coverage classes, and one ambiguity family. Do not return file paths. Use must_cover values only from source,target,implementation,bridge. Use ambiguity_family only from none,protocol_impl,root_vs_subtype,router_executor. Prefer terse repo terms and edge names only. Return only structured JSON."
	output, err := llm.Run(ctx, engine.EngineInput{
		SystemPrompt: systemPrompt,
		Messages:     []engine.Message{engine.NewUserMessage(string(body))},
		MaxTokens:    llmCfg.MaxTokens,
		Temperature:  llmCfg.Temperature,
	})
	if err != nil {
		return empty, nil, nil, err
	}
	if output.StopReason == engine.StopReasonError {
		return empty, nil, nil, errors.New(strings.TrimSpace(output.Error))
	}
	usage := skillobs.CalculateTokenCost(llmCfg.Model, output.Tokens.InputTokens, output.Tokens.OutputTokens)
	var plannerOut codeSearchLLMPlannerOutput
	if err := json.Unmarshal([]byte(strings.TrimSpace(output.AssistantText)), &plannerOut); err != nil {
		return empty, nil, &usage, fmt.Errorf("parse planner output: %w", err)
	}
	plannerOut.SeedQueries = mergeCodeSearchProbeLists(nil, plannerOut.SeedQueries)
	plannerOut.PathBiases = mergeCodeSearchProbeLists(nil, plannerOut.PathBiases)
	plannerOut.EdgePriorities = mergeCodeSearchProbeLists(nil, plannerOut.EdgePriorities)
	plannerOut.MustCover = mergeCodeSearchProbeLists(nil, plannerOut.MustCover)
	plannerOut.AmbiguityFamily = strings.TrimSpace(strings.ToLower(plannerOut.AmbiguityFamily))
	if plannerOut.AmbiguityFamily == "" {
		plannerOut.AmbiguityFamily = "none"
	}
	if plannerOut.MaxWidenSteps < 0 {
		plannerOut.MaxWidenSteps = 0
	}
	meta := map[string]any{
		"provider":         provider,
		"model":            model,
		"seed_queries":     append([]string(nil), plannerOut.SeedQueries...),
		"path_biases":      append([]string(nil), plannerOut.PathBiases...),
		"edge_priorities":  append([]string(nil), plannerOut.EdgePriorities...),
		"must_cover":       append([]string(nil), plannerOut.MustCover...),
		"ambiguity_family": plannerOut.AmbiguityFamily,
		"max_widen_steps":  plannerOut.MaxWidenSteps,
		"rationale":        strings.TrimSpace(plannerOut.Rationale),
	}
	return plannerOut, meta, &usage, nil
}

func (a *ReadOnlyAdapter) codeSearchLLMReplanner(
	ctx context.Context,
	query, taskType string,
	ranked []*codeSearchCandidate,
	maxFiles int,
	motifPathScores map[string]float64,
	plannerCfg codeSearchLLMPlannerInput,
	initial codeSearchLLMPlannerOutput,
) (codeSearchLLMReplannerOutput, map[string]any, *skillobs.TokenUsage, error) {
	var empty codeSearchLLMReplannerOutput

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/rlm/env/adapter.go
(lines 133-220: Model-visible allowlist enforcement and executeInternal tool-dispatch switch showing how environment tool names map to lane implementations and subcall support.)
```go
func (a *ReadOnlyAdapter) Execute(ctx context.Context, name string, args json.RawMessage) (map[string]any, error) {
	name = strings.TrimSpace(name)
	if !a.isModelToolAllowed(name) {
		return nil, fmt.Errorf("rlm env adapter: tool %q is not allowed by the current model-visible policy", name)
	}
	return a.executeInternal(ctx, name, args)
}

// ExecuteInternal bypasses model-visible allowlist checks for deterministic controller/eval paths.
func (a *ReadOnlyAdapter) ExecuteInternal(ctx context.Context, name string, args json.RawMessage) (map[string]any, error) {
	return a.executeInternal(ctx, strings.TrimSpace(name), args)
}

func (a *ReadOnlyAdapter) isModelToolAllowed(name string) bool {
	if name == "" {
		return false
	}
	for _, tool := range a.environment.Tools {
		if strings.TrimSpace(tool.Name) == name {
			return true
		}
	}
	return false
}

func (a *ReadOnlyAdapter) executeInternal(ctx context.Context, name string, args json.RawMessage) (map[string]any, error) {
	switch strings.TrimSpace(name) {
	case "get_top_of_mind":
		return a.getTopOfMind(), nil
	case "get_latest_handoff":
		return a.getLatestHandoff(), nil
	case "search_artifacts":
		return a.searchArtifacts(ctx, args)
	case "load_artifact":
		return a.loadArtifact(ctx, args)
	case "search_repo":
		return a.searchRepo(ctx, args)
	case "semantic_search_code":
		return a.semanticSearchCode(ctx, args)
	case "smart_search_code":
		return a.smartSearchCode(ctx, args)
	case "ripgrep_code":
		return a.ripgrepCode(ctx, args)
	case "expand_repo_graph":
		return a.expandRepoGraph(ctx, args)
	case "load_file":
		return a.loadFile(args)
	case "search_vault":
		return a.searchVault(ctx, args)
	case "read_note":
		return a.readNote(args)
	case "memory_ensemble_retrieve":
		return a.memoryEnsembleRetrieve(ctx, args)
	case "code_search_ensemble":
		return a.codeSearchEnsemble(ctx, args)
	case "search_scenes":
		return a.searchScenes(ctx, args)
	case "get_scene":
		return a.getScene(ctx, args)
	case "subcall":
		return a.subcallTool(ctx, args)
	default:
		return nil, fmt.Errorf("rlm env adapter: unknown tool %q", name)
	}
}

type semanticSearchToolOutput struct {
	Results []struct {
		Path       string  `json:"path"`
		Name       string  `json:"name"`
		Source     string  `json:"source"`
		Summary    string  `json:"summary"`
		Similarity float64 `json:"similarity"`
	} `json:"results"`
	CandidateBundles []struct {
		Key          string   `json:"key"`
		PrimaryPath  string   `json:"primary_path"`
		RelatedPaths []string `json:"related_paths"`
		Symbols      []string `json:"symbols"`
		Sources      []string `json:"sources"`
		Score        float64  `json:"score"`
		MatchReason  string   `json:"match_reason"`
		Ambiguity    string   `json:"ambiguity"`
	} `json:"candidate_bundles"`
}

type smartSearchToolOutput struct {
	Candidates []struct {

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/AGENTS.md
(lines 80-83: Rule 16 policy excerpt: forbids keyword/substr heuristics for routing/classification decisions.)
```md
15. **Task continuity split** — use `foxctl context task-history-summary` for Codex/agents/scripts *(structured summary + artifact pointer)* and `configs/hooks/task-continuity-summary.sh` for hook injection *(prompt-ready wrapper output)*
16. **Never use keyword heuristics** — do not route, classify, promote, or suppress behavior using ad hoc substring/keyword matching; these heuristics are brittle. Prefer explicit schemas, typed signals, scored features, tests, or learned policies.
17. **`internal/*` placement rule** — before adding a new `internal/*` package or extending `internal/v2/*`, read [docs/architecture/package-topology.md](docs/architecture/package-topology.md) and place the work by family model, not by local preference


```
</file_contents>
<meta prompt 1 = "[Architect]">
You are producing an implementation-ready technical plan. The implementer will work from your plan without asking clarifying questions, so every design decision must be resolved, every touched component must be identified, and every behavioral change must be specified precisely.

Your job:
1. Analyze the requested change against the provided code — identify the relevant architecture, constraints, data flow, and extension points.
2. Decide whether this is best solved by a targeted change or a broader refactor, and justify that decision.
3. Produce a plan detailed enough that an engineer can implement it file-by-file without making design decisions of their own.

Hard constraints:
- Do not write production code, patches, diffs, or copy-paste-ready implementations.
- Stay in analysis and architecture mode only.
- Use illustrative snippets, interface shapes, sample signatures, state/data shapes, or pseudocode when they communicate the design more precisely than prose. Keep them partial — enough to remove ambiguity, not enough to copy-paste.
- Scale your response to the complexity of the request. Small, localized changes need short plans; only expand sections for changes that genuinely require the detail.

─── ANALYSIS ───

Current-state analysis (always include):
- Map the existing responsibilities, type relationships, ownership, data flow, and mutation points relevant to the request.
- Identify existing code that should be reused or extended — never duplicate what already exists without justification.
- Note hard constraints: API contracts, protocol conformances, state ownership rules, thread/actor isolation, persistence schemas, UI update mechanisms.
- When multiple subsystems interact, trace the call chain end-to-end and identify each transformation boundary.

─── DESIGN ───

Design standards — address only the standards relevant to the change; skip sections that don't apply:

1. New and modified components/types: For each, specify:
   - The name, kind (for example: class, interface, enum, record, service, module, controller), and why that kind fits the codebase and language.
   - The fields/properties/state it owns, including data shape, mutability, and ownership/lifecycle semantics.
   - Key callable interfaces or signatures, including inputs, outputs, and whether execution is synchronous/asynchronous or can fail.
   - Contracts it implements, extends, composes with, or depends on.
   - For closed sets of variants (for example enums, tagged unions, discriminated unions): all cases/variants and any attached data.
   - Where the component lives (file path) and who creates/owns its instances.

2. State and data flow: For each state change the plan introduces or modifies:
   - What triggers the change (user action, callback, notification, timer, stream event).
   - The exact path the data travels: source → transformations → destination.
   - Thread/actor/queue context at each step.
   - How downstream consumers observe the change (published property, delegate, notification, binding, callback).
   - What happens if the change arrives out of order, is duplicated, or is dropped.

3. API and interface changes: For each modified public/internal interface:
   - The before and after signatures (or new signature if additive).
   - Every call site that must be updated, grouped by file.
   - Backward-compatibility strategy if the interface is used by external consumers or persisted data.

4. Persistence and serialization: When the plan touches stored data:
   - Schema changes with exact field names, types, and defaults.
   - Migration strategy: how existing data is read, transformed, and re-persisted.
   - What happens when new code reads old data and when old code reads new data (if rollback is possible).

5. Concurrency and lifecycle:
   - Specify the execution model and safety boundaries for each new/modified component: thread affinity, event-loop/runtime constraints, isolation boundaries, queue/worker discipline, or thread-safety expectations as applicable.
   - Identify potential races, leaked references/resources, or lifecycle mismatches introduced by the change.
   - When operations are asynchronous, specify cancellation/abort behavior and what state remains after interruption.

6. Error handling and edge cases:
   - For each operation that can fail, specify what failures are possible and how they propagate.
   - Describe degraded-mode behavior: what the user sees, what state is preserved, what recovery is available.
   - Identify boundary conditions: empty collections, missing/null/optional values, first-run states, interrupted operations.

7. Algorithmic and logic-heavy work (include whenever the change involves non-trivial control flow, state machines, data transformations, or performance-sensitive paths):
   - Describe the algorithm step-by-step: inputs, outputs, invariants, and data structures.
   - Cover edge cases, failure modes, and performance characteristics (time/space complexity if relevant).
   - Explain why this approach over the most plausible alternatives.

8. Avoid unnecessary complexity:
   - Do not add layers, abstractions, or indirection without a concrete benefit identified in the plan.
   - Do not create parallel code paths — unify where possible.
   - Reuse existing patterns unless those patterns are themselves the problem.

─── OUTPUT ───

Structure your response as:

1. **Summary** — One paragraph: what changes, why, and the high-level approach.

2. **Current-state analysis** — How the relevant code works today. Trace the data/control flow end-to-end. Identify what is reusable and what is blocking.

3. **Design** — The core of the plan. Apply every applicable standard from above. Organize by logical component or subsystem, not by standard number. Each component section should cover types, state flow, interfaces, persistence, concurrency, and error handling as relevant to that component.

4. **File-by-file impact** — For every file that changes, list:
   - What changes (added/modified/removed types, methods, properties).
   - Why (which design decision drives this change).
   - Dependencies on other changes in this plan (ordering constraints).

5. **Risks and migration** — Include only when the change introduces breaking changes, data migration, or rollback concerns. Omit for additive or non-breaking work.

6. **Implementation order** — A numbered sequence of steps. Each step should be independently compilable and testable where possible. Call out steps that must be atomic (landed together).

Response discipline:
- Be specific to the provided code — reference actual type names, file paths, method names, and property names.
- Make every assumption explicit.
- Flag unknowns that must be validated during implementation, with a suggested validation approach.
- When a design decision has a non-obvious rationale, explain it in one sentence.
- Do not pad with generic advice. Every sentence should convey information the implementer needs.

Please proceed with your analysis based on the following <user instructions>
</meta prompt 1>
<user_instructions>
<taskname="RLM Retrieval Lanes"/>
<task>Produce a structured analysis of RLM retrieval across Code, Memory, and Context lanes: tools per lane, bootstrap flow, retrieval funnel, failure modes, cross-lane interactions, and ranked recommendations to unify memory↔code↔tasks retrieval. Also assess: DefaultTools surface bloat, ClassifyRouteProfile routing heuristics, composite-vs-atomic decomposition, missing skills not exposed to RLM, CAS/token efficiency, and LambdaRunner vs LLMRunner split. Keep all recommendations read-only.</task>

<architecture>
- Policy/routing: `internal/rlm/run_spec.go`, `internal/rlm/plan.go`.
- Tool surface + env wiring: `internal/rlm/env/tools.go`, `internal/rlm/env/bootstrap.go`.
- Dispatch/allowlist gate: `internal/rlm/env/adapter.go`.
- Code lane composite: `internal/rlm/env/code_search_ensemble.go`.
- Memory lane composite: `internal/rlm/env/memory_ensemble.go`.
- Runner split: `internal/rlm/llm_runner.go` vs `internal/rlm/lambda_runner.go` (+ `internal/rlm/lambda_classify.go`).
- Rule constraint on heuristics: `AGENTS.md` Rule 16 excerpt.
- Skill availability evidence (not model-exposed in DefaultTools): `skills/memory_query/skill.yaml`, `skills/session_recall/skill.yaml`.
</architecture>

<selected_context>
internal/rlm/env/tools.go: `DefaultTools()` (18-tool model-visible surface) + schemas/descriptions.
internal/rlm/env/bootstrap.go: `BootstrapReadOnlyEnvironment` and loaders (`loadRepoHandles`, `loadVaultHandles`, `loadTrajectoryHandles`) showing lane bootstrap and soft-fail behavior.
internal/rlm/env/adapter.go#133-220: model-visible allowlist check and tool dispatch mapping.
internal/rlm/env/code_search_ensemble.go slices:
- 130-1279: end-to-end ensemble funnel (probe lanes, motif/ACA/graph augmentation, grounding with `load_file`, evidence payload, gaps, telemetry compaction estimates).
- 2149-2335: core task/probe normalization and lane fan-out heuristics.
- 5360-5670: bounded LLM selector/planner/replanner integration.
internal/rlm/env/memory_ensemble.go: scout-role orchestration and merged memory evidence output.
internal/rlm/run_spec.go: run-spec resolution + tool-profile policy (`default`, `code-intel`, `longcot-no-model-tools`).
internal/rlm/plan.go: route/plan definitions and keyword-based `ClassifyRouteProfile`.
internal/rlm/llm_runner.go slices:
- 1-260: run-spec/tool-policy application and tool-loop metadata.
- 820-1047: keyword/path scoring heuristics (`queryGuidanceHints`, `scoreCandidatePath`, `tokenizeQueryTerms`).
internal/rlm/lambda_runner.go: decomposition execution runtime.
internal/rlm/lambda_classify.go: LLM-based task-shape classification.
skills/memory_query/skill.yaml: memory-query capability manifest.
skills/session_recall/skill.yaml: session-recall capability manifest.
AGENTS.md#80-83: Rule 16 prohibits keyword heuristics for routing/classification behavior.
</selected_context>

<relationships>
- `ResolveRunSpec` applies route profile + tool profile before runner execution.
- `DefaultTools` defines model-visible tools; `adapter.isModelToolAllowed` enforces them.
- `llm_runner` steers repo retrieval toward `code_search_ensemble` and records parent token/call stats.
- `code_search_ensemble` composes atomic lanes into a single grounded evidence contract.
- `memory_ensemble_retrieve` aggregates role-based subcalls into facts/timeline/context outputs.
- Lambda and LLM runners share environment/tool surface but differ in control decomposition.
- Memory/session skill manifests indicate available capabilities that are absent from RLM `DefaultTools` exposure.
</relationships>

<ambiguities>
- Design docs referenced in the original brief are not selected due token budget; ground analysis in implementation evidence from selected files.
- Composite (`code_search_ensemble`) and atomic tools coexist; policy intent vs implementation layering must be inferred.
- Selected code contains keyword heuristics while selected AGENTS policy forbids them; treat as an explicit policy/runtime mismatch to analyze.
</ambiguities>
</user_instructions>
