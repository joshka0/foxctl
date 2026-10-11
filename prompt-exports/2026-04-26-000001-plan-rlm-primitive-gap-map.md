<file_map>
/Users/joshka/repos/worktrees/feat-longcot-eval-dry-run
├── internal
│   ├── context
│   │   ├── companion
│   │   │   ├── testdata
│   │   │   ├── hybrid_evidence.go * +
│   │   │   ├── hybrid_pipeline.go * +
│   │   │   └── hybrid_types.go * +
│   │   ├── contextplane
│   │   │   ├── taskhistory
│   │   │   │   └── taskhistory.go * +
│   │   │   ├── proposals.go * +
│   │   │   ├── retrieval_inspect.go * +
│   │   │   ├── store.go * +
│   │   │   └── types.go * +
│   │   ├── calibration
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
│   ├── intelligence
│   │   ├── retrieval
│   │   │   └── v2
│   │   │       ├── query_plan.go * +
│   │   │       └── types.go * +
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
│   │   ├── searchindex
│   │   ├── searchquery
│   │   ├── searchrank
│   │   └── verification
│   ├── rlm
│   │   ├── runtime
│   │   │   └── node.go * +
│   │   ├── env
│   │   ├── optdata
│   │   └── repl
│   ├── runtime
│   │   ├── observability
│   │   │   ├── testdata
│   │   │   └── wide_event.go * +
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
│   │   ├── tasks
│   │   │   └── store.go * +
│   │   ├── trajectory
│   │   │   └── types.go * +
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
│   │   ├── teams
│   │   ├── testwatch
│   │   ├── transcriptcache
│   │   ├── vector
│   │   └── interfaces.go * +
│   ├── tooling
│   │   ├── evals
│   │   │   ├── longcoteval
│   │   │   │   └── types.go * +
│   │   │   ├── retrievaleval
│   │   │   │   ├── eval.go * +
│   │   │   │   └── policy.go * +
│   │   │   ├── correctioneval
│   │   │   ├── longcotbridge
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
│   ├── domain
│   │   ├── agent
│   │   ├── backup
│   │   ├── console
│   │   ├── envelope
│   │   ├── hook
│   │   ├── identity
│   │   ├── policy
│   │   └── skill
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
├── prompt-exports
├── scripts
│   ├── checkmanifests
│   ├── compare_retrieval_modes
│   ├── hooks
│   ├── reindex_searchindex
│   └── skills_impact
├── skills
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
│   ├── memory_query
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
│   ├── optimize_reflect
│   └── optimize_weights
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
└── workflows


(* denotes selected files)
(+ denotes code-map available)
Config: directory-only view; selected files shown.
</file_map>
<file_contents>
File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/context/contextplane/types.go
```go
package contextplane

import "time"

// RecentDecision captures a bounded decision item in top-of-mind.
type RecentDecision struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	Ref  string `json:"ref,omitempty"`
}

// TopOfMind is the derived orientation bundle for the current workspace frontier.
type TopOfMind struct {
	WorkspaceID     string           `json:"workspace_id"`
	Objective       string           `json:"objective"`
	Phase           string           `json:"phase"`
	ActiveTaskIDs   []string         `json:"active_task_ids,omitempty"`
	HardConstraints []string         `json:"hard_constraints,omitempty"`
	Blockers        []string         `json:"blockers,omitempty"`
	RecentDecisions []RecentDecision `json:"recent_decisions,omitempty"`
	OpenLoops       []string         `json:"open_loops,omitempty"`
	NextActions     []string         `json:"next_actions,omitempty"`
	RelevantRefs    []string         `json:"relevant_refs,omitempty"`
	UpdatedAt       time.Time        `json:"updated_at"`
}

// Handoff captures the compact output of a bounded work phase.
type Handoff struct {
	TaskID              string    `json:"task_id"`
	Phase               string    `json:"phase"`
	Outcome             string    `json:"outcome"`
	Summary             string    `json:"summary"`
	EvidenceRefs        []string  `json:"evidence_refs,omitempty"`
	FilesTouched        []string  `json:"files_touched,omitempty"`
	Observations        []string  `json:"observations,omitempty"`
	Tensions            []string  `json:"tensions,omitempty"`
	NextActions         []string  `json:"next_actions,omitempty"`
	PromotionCandidates []string  `json:"promotion_candidates,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
}

// Observation records a repeatable system learning.
type Observation struct {
	ID           string    `json:"id"`
	Statement    string    `json:"statement"`
	Confidence   float64   `json:"confidence"`
	Count        int       `json:"count"`
	Project      string    `json:"project,omitempty"`
	Area         string    `json:"area,omitempty"`
	EvidenceRefs []string  `json:"evidence_refs,omitempty"`
	FirstSeen    time.Time `json:"first_seen"`
	LastSeen     time.Time `json:"last_seen"`
}

// Tension records a contradiction or drag source.
type Tension struct {
	ID          string    `json:"id"`
	Kind        string    `json:"kind"`
	Statement   string    `json:"statement"`
	Impact      string    `json:"impact"`
	RelatedRefs []string  `json:"related_refs,omitempty"`
	Status      string    `json:"status"`
	Count       int       `json:"count,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	LastSeen    time.Time `json:"last_seen,omitempty"`
}

// HandoffRecord wraps a handoff with its persisted path.
type HandoffRecord struct {
	Path    string  `json:"path"`
	Handoff Handoff `json:"handoff"`
}

// PromotionJob records a draft promotion event.
type PromotionJob struct {
	ID         string    `json:"id"`
	SourceRef  string    `json:"source_ref"`
	SourceKind string    `json:"source_kind"`
	NoteType   string    `json:"note_type"`
	Title      string    `json:"title"`
	DraftPath  string    `json:"draft_path"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

// PromotionDraftResult describes a generated draft note.
type PromotionDraftResult struct {
	DraftPath string       `json:"draft_path"`
	Job       PromotionJob `json:"job"`
}

// PromotionMergeResult describes an explicit reviewed merge of a draft into the knowledge plane.
type PromotionMergeResult struct {
	DraftPath   string       `json:"draft_path"`
	TargetPath  string       `json:"target_path"`
	Heading     string       `json:"heading,omitempty"`
	MergedAs    string       `json:"merged_as"`
	Job         PromotionJob `json:"job"`
	MergedAtUTC time.Time    `json:"merged_at_utc"`
}

// Report is a synthesized current-view projection of the control plane.
type Report struct {
	WorkspaceID        string         `json:"workspace_id"`
	Objective          string         `json:"objective"`
	Phase              string         `json:"phase"`
	ActiveTaskIDs      []string       `json:"active_task_ids,omitempty"`
	LatestHandoff      *HandoffRecord `json:"latest_handoff,omitempty"`
	TopObservations    []Observation  `json:"top_observations,omitempty"`
	OpenTensions       []Tension      `json:"open_tensions,omitempty"`
	RecommendedActions []string       `json:"recommended_actions,omitempty"`
	GeneratedAt        time.Time      `json:"generated_at"`
}

// MaintenanceTask captures a generated maintenance action derived from control-plane drift.
type MaintenanceTask struct {
	ID         string              `json:"id"`
	Title      string              `json:"title"`
	Kind       string              `json:"kind"`
	Priority   int                 `json:"priority"`
	Reason     string              `json:"reason"`
	SourceRefs []string            `json:"source_refs,omitempty"`
	WorkPacket *ProposalWorkPacket `json:"work_packet,omitempty"`
	Status     string              `json:"status"`
	CreatedAt  time.Time           `json:"created_at"`
}

// RetrievalCorrectionRun records one persisted ACA retrieval correction report.
type RetrievalCorrectionRun struct {
	ID              string                          `json:"id"`
	Suite           string                          `json:"suite"`
	ControlSuite    string                          `json:"control_suite,omitempty"`
	ArtifactDigest  string                          `json:"artifact_digest"`
	Summary         RetrievalInspectionBatchSummary `json:"summary"`
	PolicyCandidate bool                            `json:"policy_candidate"`
	PolicyApplied   bool                            `json:"policy_applied"`
	PolicyAccepted  bool                            `json:"policy_accepted"`
	PolicyReverted  bool                            `json:"policy_reverted"`
	DraftCount      int                             `json:"draft_count"`
	CreatedAt       time.Time                       `json:"created_at"`
}

// GraphCorrectionRun records one persisted repoindex search or DAG correction report.
type GraphCorrectionRun struct {
	ID             string    `json:"id"`
	Method         string    `json:"method"`
	Suite          string    `json:"suite"`
	ArtifactDigest string    `json:"artifact_digest"`
	Queries        int       `json:"queries"`
	Matched        int       `json:"matched"`
	Misses         int       `json:"misses"`
	Classification string    `json:"classification,omitempty"`
	RecommendedFix string    `json:"recommended_fix,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// MemoryProposal records one typed, deduped suggestion for evolving ACA memory state.
type MemoryProposal struct {
	ID               string         `json:"id"`
	DedupeKey        string         `json:"dedupe_key,omitempty"`
	Kind             string         `json:"kind"`
	Classification   string         `json:"classification,omitempty"`
	Status           string         `json:"status"`
	ReviewRequired   bool           `json:"review_required"`
	Confidence       float64        `json:"confidence"`
	BlastRadius      string         `json:"blast_radius,omitempty"`
	Summary          string         `json:"summary"`
	SourceRefs       []string       `json:"source_refs,omitempty"`
	ProposedChange   map[string]any `json:"proposed_change,omitempty"`
	EvaluationStatus string         `json:"evaluation_status,omitempty"`
	ApplyStatus      string         `json:"apply_status,omitempty"`
	Count            int            `json:"count"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
}

// EvidenceImportRun records one external-evidence intake into the ACA inbox.
type EvidenceImportRun struct {
	ID             string    `json:"id"`
	SourceKind     string    `json:"source_kind"`
	SourceRef      string    `json:"source_ref"`
	Title          string    `json:"title"`
	DraftPath      string    `json:"draft_path"`
	ArtifactDigest string    `json:"artifact_digest,omitempty"`
	ProcessorKind  string    `json:"processor_kind,omitempty"`
	ProcessorModel string    `json:"processor_model,omitempty"`
	Summary        string    `json:"summary"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
}

// ProposalWorkPacket is a stable machine-readable next-action packet for agents/hooks.
type ProposalWorkPacket struct {
	ProposalID        string `json:"proposal_id"`
	ProposalKind      string `json:"proposal_kind"`
	Action            string `json:"action"`
	Status            string `json:"status"`
	ReviewRequired    bool   `json:"review_required"`
	DraftPath         string `json:"draft_path,omitempty"`
	TargetPath        string `json:"target_path,omitempty"`
	Heading           string `json:"heading,omitempty"`
	PolicyPath        string `json:"policy_path,omitempty"`
	PromotionJobID    string `json:"promotion_job_id,omitempty"`
	RequiresVaultPath bool   `json:"requires_vault_path,omitempty"`
	VaultPath         string `json:"vault_path,omitempty"`
	NextCommand       string `json:"next_command,omitempty"`
}

// RetrievalHit is one ranked durable-knowledge candidate.
type RetrievalHit struct {
	Path              string              `json:"path"`
	Title             string              `json:"title"`
	Type              string              `json:"type,omitempty"`
	Trust             string              `json:"trust,omitempty"`
	Score             int                 `json:"score"`
	Snippet           string              `json:"snippet,omitempty"`
	PrimaryAnchorPath string              `json:"primary_anchor_path,omitempty"`
	RepoPaths         []string            `json:"repo_paths,omitempty"`
	AnchorPaths       []string            `json:"anchor_paths,omitempty"`
	AnchorRoles       map[string][]string `json:"anchor_roles,omitempty"`
	Symbols           []string            `json:"symbols,omitempty"`
}

// RetrievalWeights documents the scoring inputs for blended retrieval.
type RetrievalWeights struct {
	BaseIndexScore int `json:"base_index_score"`
	ADR            int `json:"adr"`
	Pattern        int `json:"pattern"`
	Incident       int `json:"incident"`
	Investigation  int `json:"investigation"`
	Map            int `json:"map"`
	Canonical      int `json:"canonical"`
	Reviewed       int `json:"reviewed"`
	Raw            int `json:"raw"`
	RelevantRef    int `json:"relevant_ref"`
	HandoffRef     int `json:"handoff_ref"`
	CodePath       int `json:"code_path"`
	CodeSymbol     int `json:"code_symbol"`
	RepoMotif      int `json:"repo_motif"`
	CoChange       int `json:"co_change"`
	SemanticMatch  int `json:"semantic_match"`
}

// RetrievalOptions controls which ACA retrieval components participate in one query.
// The default path should remain behavior-compatible with existing Retrieve().
type RetrievalOptions struct {
	IncludeTopOfMindResult    bool     `json:"include_top_of_mind_result"`
	IncludeLatestHandoff      bool     `json:"include_latest_handoff"`
	IncludeVaultHits          bool     `json:"include_vault_hits"`
	UseRelevantRefBoost       bool     `json:"use_relevant_ref_boost"`
	UseHandoffRefBoost        bool     `json:"use_handoff_ref_boost"`
	UseCodeHints              bool     `json:"use_code_hints"`
	UseSemanticVaultSearch    bool     `json:"use_semantic_vault_search"`
	UsePackageNoteFallback    bool     `json:"use_package_note_fallback"`
	UseRepoMotifPrior         bool     `json:"use_repo_motif_prior"`
	UseCoChangePrior          bool     `json:"use_co_change_prior"`
	CoChangeCommitLimit       int      `json:"co_change_commit_limit,omitempty"`
	CoChangeMaxFilesPerCommit int      `json:"co_change_max_files_per_commit,omitempty"`
	CoChangeHalfLifeDays      int      `json:"co_change_half_life_days,omitempty"`
	UseContinuityBundles      bool     `json:"use_continuity_bundles,omitempty"`
	UseQueryTypeBias          bool     `json:"use_query_type_bias"`
	AllowedTrusts             []string `json:"allowed_trusts,omitempty"`
	IncludeControlPlaneRefs   bool     `json:"include_control_plane_refs"`
}

// RetrievalResult blends control-plane state with ranked vault hits.
type RetrievalResult struct {
	WorkspaceID   string               `json:"workspace_id"`
	Query         string               `json:"query"`
	TopOfMind     *TopOfMind           `json:"top_of_mind,omitempty"`
	LatestHandoff *HandoffRecord       `json:"latest_handoff,omitempty"`
	Observations  []Observation        `json:"observations,omitempty"`
	Tensions      []Tension            `json:"tensions,omitempty"`
	VaultHits     []RetrievalHit       `json:"vault_hits,omitempty"`
	RepoMotifHits []RepoMotifSearchHit `json:"repo_motif_hits,omitempty"`
	Weights       RetrievalWeights     `json:"weights"`
	SemanticModel string               `json:"semantic_model,omitempty"`
	SemanticUsed  bool                 `json:"semantic_used"`
	GeneratedAt   time.Time            `json:"generated_at"`
}

// ContradictionFinding links an open tension to potentially conflicting or relevant durable notes.
type ContradictionFinding struct {
	Tension          Tension        `json:"tension"`
	Query            string         `json:"query"`
	SupportingNotes  []RetrievalHit `json:"supporting_notes,omitempty"`
	BlockedPromotion bool           `json:"blocked_promotion"`
}

// TaskCandidate is a selected task from the workspace task store.
type TaskCandidate struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Status      string   `json:"status"`
	ScopePath   string   `json:"scope_path,omitempty"`
	DependsOn   []string `json:"depends_on,omitempty"`
	PlanFile    string   `json:"plan_file,omitempty"`
	PlanSection string   `json:"plan_section,omitempty"`
	SessionID   string   `json:"session_id,omitempty"`
}

// TaskPacket is the bounded dispatch payload for a worker phase.
type TaskPacket struct {
	WorkspaceID     string           `json:"workspace_id"`
	Task            TaskCandidate    `json:"task"`
	Objective       string           `json:"objective"`
	Phase           string           `json:"phase"`
	HardConstraints []string         `json:"hard_constraints,omitempty"`
	Blockers        []string         `json:"blockers,omitempty"`
	RecentDecisions []RecentDecision `json:"recent_decisions,omitempty"`
	NextActions     []string         `json:"next_actions,omitempty"`
	RelevantRefs    []string         `json:"relevant_refs,omitempty"`
	LatestHandoff   *HandoffRecord   `json:"latest_handoff,omitempty"`
	GeneratedAt     time.Time        `json:"generated_at"`
}

// Layout describes the workspace-local ACA runtime scaffold.
type Layout struct {
	WorkspacePath          string
	RootDir                string
	RuntimeDir             string
	QueueDir               string
	HandoffsDir            string
	SessionsDir            string
	PolicyDir              string
	ExportsDir             string
	TemplatesDir           string
	TopOfMindPath          string
	CurrentRunPath         string
	TasksQueuePath         string
	BlockedQueuePath       string
	ObservationsPath       string
	TensionsPath           string
	PromotionJobsPath      string
	MaintenanceQueuePath   string
	EventsPath             string
	RetrievalPolicyPath    string
	PromotionPolicyPath    string
	TaskTypesPolicyPath    string
	OrientationExportPath  string
	ObsidianHomeIndexPath  string
	ObsidianFrontierPath   string
	ObsidianAtlasPath      string
	ObsidianProjectMOCPath string
	ObsidianInboxDraftsDir string
}

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/tooling/evals/retrievaleval/policy.go
```go
package retrievaleval

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type ModeThreshold struct {
	MinHitRateAt5         float64 `yaml:"min_hit_rate_at_5,omitempty" json:"min_hit_rate_at_5,omitempty"`
	MinHitRateAt10        float64 `yaml:"min_hit_rate_at_10,omitempty" json:"min_hit_rate_at_10,omitempty"`
	MinMeanReciprocalRank float64 `yaml:"min_mrr,omitempty" json:"min_mrr,omitempty"`
}

type Policy struct {
	Suite        string                   `yaml:"suite,omitempty" json:"suite,omitempty"`
	Limit        int                      `yaml:"limit,omitempty" json:"limit,omitempty"`
	Format       string                   `yaml:"format,omitempty" json:"format,omitempty"`
	Modes        []string                 `yaml:"modes,omitempty" json:"modes,omitempty"`
	FailOnAlerts bool                     `yaml:"fail_on_alerts,omitempty" json:"fail_on_alerts,omitempty"`
	Thresholds   map[string]ModeThreshold `yaml:"thresholds,omitempty" json:"thresholds,omitempty"`
}

type Alert struct {
	Mode    string  `json:"mode"`
	Metric  string  `json:"metric"`
	Actual  float64 `json:"actual"`
	Minimum float64 `json:"minimum"`
	Message string  `json:"message"`
}

func LoadPolicy(path string) (Policy, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return Policy{}, err
	}
	var out Policy
	if err := yaml.Unmarshal(body, &out); err != nil {
		return Policy{}, fmt.Errorf("decode retrieval policy yaml: %w", err)
	}
	return out, nil
}

func BuildAlerts(summaries []Summary, policy Policy) []Alert {
	if len(policy.Thresholds) == 0 {
		return nil
	}
	thresholdsByMode := make(map[string]ModeThreshold, len(policy.Thresholds))
	for mode, threshold := range policy.Thresholds {
		key := strings.ToLower(strings.TrimSpace(mode))
		if key == "" {
			continue
		}
		thresholdsByMode[key] = threshold
	}
	if len(thresholdsByMode) == 0 {
		return nil
	}
	alerts := make([]Alert, 0, len(thresholdsByMode)*3)
	for _, summary := range summaries {
		threshold, ok := thresholdsByMode[strings.ToLower(strings.TrimSpace(summary.Mode))]
		if !ok {
			continue
		}
		if threshold.MinHitRateAt5 > 0 && summary.HitRateAt5 < threshold.MinHitRateAt5 {
			alerts = append(alerts, Alert{
				Mode:    summary.Mode,
				Metric:  "hit_rate_at_5",
				Actual:  summary.HitRateAt5,
				Minimum: threshold.MinHitRateAt5,
				Message: fmt.Sprintf("%s hit@5 %.2f below %.2f", summary.Mode, summary.HitRateAt5, threshold.MinHitRateAt5),
			})
		}
		if threshold.MinHitRateAt10 > 0 && summary.HitRateAt10 < threshold.MinHitRateAt10 {
			alerts = append(alerts, Alert{
				Mode:    summary.Mode,
				Metric:  "hit_rate_at_10",
				Actual:  summary.HitRateAt10,
				Minimum: threshold.MinHitRateAt10,
				Message: fmt.Sprintf("%s hit@10 %.2f below %.2f", summary.Mode, summary.HitRateAt10, threshold.MinHitRateAt10),
			})
		}
		if threshold.MinMeanReciprocalRank > 0 && summary.MeanReciprocalRank < threshold.MinMeanReciprocalRank {
			alerts = append(alerts, Alert{
				Mode:    summary.Mode,
				Metric:  "mean_reciprocal_rank",
				Actual:  summary.MeanReciprocalRank,
				Minimum: threshold.MinMeanReciprocalRank,
				Message: fmt.Sprintf("%s MRR %.2f below %.2f", summary.Mode, summary.MeanReciprocalRank, threshold.MinMeanReciprocalRank),
			})
		}
	}
	sort.SliceStable(alerts, func(i, j int) bool {
		if alerts[i].Mode == alerts[j].Mode {
			return alerts[i].Metric < alerts[j].Metric
		}
		return alerts[i].Mode < alerts[j].Mode
	})
	return alerts
}

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/storage/interfaces.go
```go
package storage

import (
	"context"
	"io"
	"time"

	jobtypes "github.com/joshka0/foxctl/internal/storage/jobs/types"
)

// Store is the minimal interface implemented by all storage providers.
type Store interface {
	Close() error
}

// CacheEntry represents the persisted data for a cache item.
type CacheEntry struct {
	CacheKey     string
	SkillName    string
	SkillVersion string
	Workspace    string
	Result       []byte
	Digests      []string
	CreatedAt    time.Time
	ExpiresAt    time.Time
	LastAccessed time.Time
	HitCount     int
}

// CacheStats captures high-level cache metadata.
type CacheStats struct {
	Entries int64
	Path    string
	TTL     time.Duration
}

// CacheStore manages cached run results.
type CacheStore interface {
	Store
	Get(ctx context.Context, key string) (CacheEntry, bool, error)
	Put(ctx context.Context, entry CacheEntry) error
	Recent(ctx context.Context, workspace string, limit int) ([]CacheEntry, error)
	Delete(ctx context.Context, key string) error
	Stats(ctx context.Context) (CacheStats, error)
}

// Job is an alias to the canonical job metadata type.
type Job = jobtypes.Job

// JobState aliases the canonical job state enumeration.
type JobState = jobtypes.State

const (
	// JobStateQueued mirrors jobtypes.StateQueued.
	JobStateQueued = jobtypes.StateQueued
	// JobStateRunning mirrors jobtypes.StateRunning.
	JobStateRunning = jobtypes.StateRunning
	// JobStateOK mirrors jobtypes.StateOK.
	JobStateOK = jobtypes.StateOK
	// JobStateError mirrors jobtypes.StateError.
	JobStateError = jobtypes.StateError
	// JobStateCanceled mirrors jobtypes.StateCanceled.
	JobStateCanceled = jobtypes.StateCanceled
)

// JobStore exposes the public job management surface.
type JobStore interface {
	Store
	SubmitEcho(ctx context.Context, message string) (Job, error)
	List(ctx context.Context, limit int) ([]Job, error)
	Get(ctx context.Context, id string) (Job, error)
	Result(ctx context.Context, id string) ([]byte, error)
	Cancel(ctx context.Context, id string) error
	Delete(ctx context.Context, id string) error
	FindOrPrepareSkillJob(ctx context.Context, name string, input []byte, dedupe bool) (Job, bool, error)
	SetWorkspace(ctx context.Context, jobID, workspacePath string) error
	WaitForCompletion(ctx context.Context, jobID string, pollInterval time.Duration) (Job, error)
	TailProgress(ctx context.Context, jobID string, follow bool, w io.Writer) error
	ExecutePreparedSkill(ctx context.Context, jobID, manifestPath, artifactPath string) ([]byte, error)
}

// CASMetadata describes a stored CAS object.
type CASMetadata struct {
	Digest    string    `json:"digest"`
	Size      int64     `json:"size_bytes"`
	Kind      string    `json:"kind,omitempty"`
	Tags      []string  `json:"tags,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Metadata provides a compatibility alias for embedded object metadata.
type Metadata = CASMetadata

// CASObject augments metadata with pin state.
type CASObject struct {
	Metadata
	Pinned bool `json:"pinned"`
}

// CASGCOptions controls garbage collection behavior.
type CASGCOptions struct {
	DryRun     bool
	OlderThan  time.Duration
	KeepPinned bool
	MaxDelete  int
}

// CASGCResult summarizes the outcome of a GC run.
type CASGCResult struct {
	ObjectsDeleted int   `json:"objects_deleted"`
	ObjectsSkipped int   `json:"objects_skipped"`
	BytesFreed     int64 `json:"bytes_freed"`
	Errors         int   `json:"errors"`
}

// CASStore defines the CAS operations used by the CLI.
type CASStore interface {
	Store
	Put(ctx context.Context, r io.Reader, kind string, tags []string) (CASObject, error)
	Get(ctx context.Context, digest string) (io.ReadCloser, CASMetadata, error)
	Head(ctx context.Context, digest string) (CASObject, error)
	List(ctx context.Context) ([]CASObject, error)
	Remove(ctx context.Context, digest string) error
	Pin(ctx context.Context, digest string) error
	Unpin(ctx context.Context, digest string) error
	AddTags(ctx context.Context, digest string, tags []string) error
	GC(ctx context.Context, opts CASGCOptions) (CASGCResult, error)
}

// NamedEntry captures a stored named memory entry.
type NamedEntry struct {
	ID          string
	Name        string
	Type        string
	Workspace   string
	Summary     string
	Result      []byte
	Digests     []string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	LastAccess  time.Time
	AccessCount int
	SessionID   string // AI coding tool session ID (Claude Code, OpenCode, Cursor, etc.)

	// Atomic processing fields (SimpleMem-style semantic lossless compression)
	// See: https://github.com/aiming-lab/SimpleMem
	AtomicText string   // Self-contained, disambiguated rewrite of summary
	Entities   []string // Extracted entities (files, functions, people, concepts)
	Keywords   []string // BM25-optimized search terms
}

// ScoredEntry couples a named entry with a relevance score.
type ScoredEntry struct {
	Entry NamedEntry
	Score float64
}

// MemoryStats summarizes named memory metadata.
type MemoryStats struct {
	Named int64
	Path  string
}

// MemorySaveOptions provides structured options for saving memory entries.
type MemorySaveOptions struct {
	Name      string
	Type      string
	Workspace string
	Summary   string
	Result    []byte
	SessionID string // AI coding tool session ID (optional)
}

// MemoryListFilter provides filter options for listing memories.
type MemoryListFilter struct {
	Types     []string
	SessionID string
}

// EmbeddingMetadata captures embedding configuration for a workspace.
type EmbeddingMetadata struct {
	Workspace  string    `json:"workspace"`
	Provider   string    `json:"provider"`
	Model      string    `json:"model"`
	Dimensions int       `json:"dimensions"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// MemoryStore persists named memories and auto-cache entries.
type MemoryStore interface {
	Store
	Save(ctx context.Context, entry NamedEntry) (NamedEntry, error)
	SaveFromResult(ctx context.Context, name, typ, workspace, summary string, result []byte) (NamedEntry, error)
	Get(ctx context.Context, name, workspace string) (NamedEntry, error)
	List(ctx context.Context, workspace string, limit int) ([]NamedEntry, error)
	Delete(ctx context.Context, name, workspace string) error
	DeleteByNamePrefix(ctx context.Context, workspace, namePrefix string) (int, error)
	Search(ctx context.Context, workspace, query string, limit int) ([]ScoredEntry, error)
	Update(ctx context.Context, name, workspace string, summary, typ *string) (NamedEntry, error)
	Relevant(ctx context.Context, workspace string, limit int) ([]ScoredEntry, error)
	Stats(ctx context.Context) (MemoryStats, error)
	// UpdateEmbedding stores an embedding vector for a named memory entry.
	UpdateEmbedding(ctx context.Context, name, workspace string, embedding []float32) error
	// SearchSimilar finds entries similar to the given embedding using vector similarity.
	SearchSimilar(ctx context.Context, workspace string, embedding []float32, limit int) ([]ScoredEntry, error)
	// SearchSimilarByType finds entries of a specific type using vector similarity.
	SearchSimilarByType(ctx context.Context, workspace, entryType string, embedding []float32, limit int) ([]ScoredEntry, error)
	// SaveResult stores a result envelope using structured options.
	SaveResult(ctx context.Context, opts MemorySaveOptions) (NamedEntry, error)
	// ListFiltered returns named memories for a workspace with optional filters and pagination.
	ListFiltered(ctx context.Context, workspace string, filter MemoryListFilter, limit, offset int) ([]NamedEntry, int, error)
	// ListWithoutEmbedding returns memories that don't have embeddings yet.
	ListWithoutEmbedding(ctx context.Context, workspace string, limit int) ([]NamedEntry, error)
	// ValidateEmbeddingDimensions checks if new embedding dimensions are compatible with existing ones.
	ValidateEmbeddingDimensions(ctx context.Context, workspace string, dimensions int) error
	// SetEmbeddingMetadata stores embedding configuration for a workspace.
	SetEmbeddingMetadata(ctx context.Context, meta EmbeddingMetadata) error
	// ExistsByNameSuffix checks if any entry exists with a name ending in the given suffix.
	// Used for content-hash deduplication across sessions (e.g., suffix ":<type>:<digest>").
	ExistsByNameSuffix(ctx context.Context, workspace, suffix string) (bool, error)
	// UpdateAtomic stores atomic processing results for a named memory entry.
	// atomicText is the self-contained rewrite, entities are extracted identifiers,
	// keywords are BM25-optimized search terms.
	UpdateAtomic(ctx context.Context, name, workspace, atomicText string, entities, keywords []string) error
}

// Session represents a captured Claude Code conversation session.
type Session struct {
	ID              string    `json:"id"`
	WorkspaceID     string    `json:"workspace_id,omitempty"`
	WorkspacePath   string    `json:"workspace_path"`
	ProjectName     string    `json:"project_name"`
	GitBranch       string    `json:"git_branch"`
	ClaudeVersion   string    `json:"claude_version"`
	StartedAt       time.Time `json:"started_at"`
	EndedAt         time.Time `json:"ended_at"`
	Summary         string    `json:"summary"`
	Accomplished    []string  `json:"accomplished"`
	Decisions       []string  `json:"decisions"`
	Gotchas         []string  `json:"gotchas"`
	UserInsights    []string  `json:"user_insights,omitempty"`
	KeyQuestions    []string  `json:"key_questions,omitempty"` // Semantic search queries for context restoration
	Tags            []string  `json:"tags"`
	KeyFiles        []string  `json:"key_files"`
	ToolsPattern    string    `json:"tools_pattern"`
	MessageCount    int       `json:"message_count"`
	UserTurns       int       `json:"user_turns"`
	ToolInvocations int       `json:"tool_invocations"`
	TotalTokens     int       `json:"total_tokens"`
	RawJSONLPath    string    `json:"raw_jsonl_path"`
	ContentHash     string    `json:"content_hash,omitempty"` // Hash of filtered conversation content for deduplication
	Embedding       []byte    `json:"embedding,omitempty"`
	EmbeddingModel  string    `json:"embedding_model,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	// Lineage fields for session tracking
	ParentSessionID string `json:"parent_session_id,omitempty"`
	AgentID         string `json:"agent_id,omitempty"`      // AI agent identifier (default: "foxctl")
	AgentType       string `json:"agent_type,omitempty"`    // Source system (claude, codex, opencode)
	Status          string `json:"status,omitempty"`        // ok, error, canceled
	ErrorMessage    string `json:"error_message,omitempty"` // Error details when status=error
	// Agent execution context
	Prompt      string `json:"prompt,omitempty"`       // Original prompt/task for agent sessions
	PromptHash  string `json:"prompt_hash,omitempty"`  // SHA256 hash for correlation with wide events
	LLMProvider string `json:"llm_provider,omitempty"` // LLM provider (cerebras, openrouter, etc.)
	LLMModel    string `json:"llm_model,omitempty"`    // Model name
	// Pending restore flag for post-compact context injection
	PendingRestoreAt *time.Time `json:"pending_restore_at,omitempty"`
}

// Session status constants
const (
	SessionStatusRunning  = "running"  // Session is active
	SessionStatusOK       = "ok"       // Session completed successfully
	SessionStatusError    = "error"    // Session ended with error
	SessionStatusCanceled = "canceled" // Session was canceled
)

// IsTerminalStatus returns true if the status indicates the session has ended.
func IsTerminalStatus(status string) bool {
	return status == SessionStatusOK || status == SessionStatusError || status == SessionStatusCanceled
}

// SessionEdge represents a relationship between two sessions.
type SessionEdge struct {
	ID          string         `json:"id"`
	Workspace   string         `json:"workspace"`
	FromSession string         `json:"from_session"`
	ToSession   string         `json:"to_session"`
	EdgeType    string         `json:"edge_type"` // continues, forked_from, relates_to
	CreatedAt   time.Time      `json:"created_at"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// Session edge type constants
const (
	SessionEdgeContinues  = "continues"
	SessionEdgeForkedFrom = "forked_from"
	SessionEdgeRelatesTo  = "relates_to"
)

// SessionStats summarizes session store metadata.
type SessionStats struct {
	Count int64
	Path  string
}

// SessionListOptions configures session listing.
type SessionListOptions struct {
	WorkspaceID   string
	WorkspacePath string
	ProjectName   string
	Tags          []string
	Statuses      []string // Filter by status (e.g., "running", "ok", "error")
	Limit         int
	Offset        int
}

// SimilarSession represents a session with similarity score.
type SimilarSession struct {
	Session    Session `json:"session"`
	Similarity float64 `json:"similarity"`
}

// SessionTurn represents a single turn in a conversation session.
type SessionTurn struct {
	ID               string     `json:"id"`
	SessionID        string     `json:"session_id"`
	TurnIndex        int        `json:"turn_index"`
	Role             string     `json:"role"` // 'user', 'assistant', 'system'
	ContentPreview   string     `json:"content_preview,omitempty"`
	ContentCASDigest string     `json:"content_cas_digest,omitempty"` // CAS digest for full content
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	FilesTouched     []string   `json:"files_touched,omitempty"`
	HasError         bool       `json:"has_error"`
	ErrorType        string     `json:"error_type,omitempty"`
	ErrorMessage     string     `json:"error_message,omitempty"`
	Resolution       string     `json:"resolution,omitempty"`
	TokensUsed       int        `json:"tokens_used"`
	Timestamp        time.Time  `json:"timestamp"`
	CreatedAt        time.Time  `json:"created_at"`
}

// ToolCall represents a tool invocation within a turn.
type ToolCall struct {
	Name    string `json:"name"`
	Success bool   `json:"success"`
}

// SessionTurnListOptions configures turn listing.
type SessionTurnListOptions struct {
	SessionID  string
	ErrorsOnly bool
	Role       string
	Limit      int
	Offset     int
}

// SessionStore persists Claude Code conversation sessions.
type SessionStore interface {
	Store
	Save(ctx context.Context, session Session) (Session, error)
	Get(ctx context.Context, id string) (Session, error)
	List(ctx context.Context, opts SessionListOptions) ([]Session, error)
	Delete(ctx context.Context, id string) error
	Search(ctx context.Context, query string, limit int) ([]Session, error)
	SearchSimilar(ctx context.Context, workspace string, embedding []float32, limit int) ([]SimilarSession, error)
	UpdateSummary(ctx context.Context, id string, summary string, accomplished, decisions, gotchas, userInsights, tags, keyFiles []string, toolsPattern string) error
	SetEmbedding(ctx context.Context, id string, embedding []byte, model string) error
	Stats(ctx context.Context) (SessionStats, error)

	// Content hash deduplication
	FindByContentHash(ctx context.Context, contentHash string) (*Session, error)
	SetContentHash(ctx context.Context, id, contentHash string) error

	// Lineage operations
	GetActive(ctx context.Context, workspace, agentID string) (*Session, error)
	SetStatus(ctx context.Context, id, status string) error
	SetStatusWithError(ctx context.Context, id, status, errorMessage string) error
	FindLastSession(ctx context.Context, workspace, agentID string, statuses []string) (*Session, error)

	// Pending restore operations (for post-compact context injection)
	SetPendingRestore(ctx context.Context, id string) error
	ClearPendingRestore(ctx context.Context, id string) error
	GetPendingRestore(ctx context.Context, workspace string) (*Session, error)
	SaveEdge(ctx context.Context, edge SessionEdge) error
	GetAncestorChain(ctx context.Context, sessionID string, maxDepth int) ([]Session, error)
	GetEdges(ctx context.Context, sessionID string) ([]SessionEdge, error)

	// Turn operations
	SaveTurn(ctx context.Context, turn SessionTurn) (SessionTurn, error)
	SaveTurns(ctx context.Context, turns []SessionTurn) error
	GetTurns(ctx context.Context, sessionID string, opts SessionTurnListOptions) ([]SessionTurn, error)
	GetTurnsWithErrors(ctx context.Context, sessionID string) ([]SessionTurn, error)
	SearchTurns(ctx context.Context, query string, limit int) ([]SessionTurn, error)
	DeleteTurns(ctx context.Context, sessionID string) error

	// Chunk operations (for JSONL archive deep retrieval)
	SaveChunk(ctx context.Context, chunk SessionChunk) (SessionChunk, error)
	SaveChunks(ctx context.Context, chunks []SessionChunk) error
	GetChunks(ctx context.Context, sessionID string, limit int) ([]SessionChunk, error)
	GetChunk(ctx context.Context, sessionID string, chunkIndex int) (SessionChunk, error)
	SearchChunks(ctx context.Context, embedding []float32, limit int) ([]ScoredChunk, error)
	SearchChunksWithOptions(ctx context.Context, embedding []float32, limit int, opts ChunkSearchOptions) ([]ScoredChunk, error)
	DeleteChunks(ctx context.Context, sessionID string) error
	SetArchivePath(ctx context.Context, sessionID, archivePath string) error
	GetArchivePath(ctx context.Context, sessionID string) (string, error)

	// Chunk summary operations (for persisted chunk-level summaries)
	SaveChunkSummary(ctx context.Context, summary SessionChunkSummary) (SessionChunkSummary, error)
	SaveChunkSummaries(ctx context.Context, summaries []SessionChunkSummary) error
	GetChunkSummaries(ctx context.Context, sessionID string, windowIndex int) ([]SessionChunkSummary, error)
	GetChunkSummary(ctx context.Context, summaryID string) (SessionChunkSummary, error)
	DeleteChunkSummaries(ctx context.Context, sessionID string) error

	// Context window operations (for granular sub-session retrieval)
	SaveContextWindow(ctx context.Context, window ContextWindow) (ContextWindow, error)
	SaveContextWindows(ctx context.Context, windows []ContextWindow) error
	GetContextWindows(ctx context.Context, sessionID string) ([]ContextWindow, error)
	GetContextWindow(ctx context.Context, sessionID string, windowIndex int) (ContextWindow, error)
	// Deprecated: Use UpdateContextWindowSummary or SetContextWindowEmbedding instead
	UpdateWindowSummary(ctx context.Context, windowID string, summary string, embedding []byte, model string) error
	// UpdateContextWindowSummary updates only the summary text for a context window.
	UpdateContextWindowSummary(ctx context.Context, windowID string, summary string) error
	// SetContextWindowEmbedding sets the embedding and model for a context window.
	SetContextWindowEmbedding(ctx context.Context, windowID string, embedding []byte, model string) error
	SearchContextWindows(ctx context.Context, embedding []float32, limit int) ([]ScoredContextWindow, error)
	DeleteContextWindows(ctx context.Context, sessionID string) error
}

// SessionChunk represents a chunk of a session for deep retrieval.
type SessionChunk struct {
	ID                 string    `json:"id"`
	SessionID          string    `json:"session_id"`
	ChunkIndex         int       `json:"chunk_index"`
	ChunkType          string    `json:"chunk_type"` // 'user_request', 'assistant_response', 'tool_output', 'error', 'compact_boundary'
	ContentHash        string    `json:"content_hash"`
	ContentPreview     string    `json:"content_preview,omitempty"`
	ByteOffset         int64     `json:"byte_offset"`
	ByteLength         int64     `json:"byte_length"`
	ToolsUsed          []string  `json:"tools_used,omitempty"`
	FilesTouched       []string  `json:"files_touched,omitempty"`
	HasError           bool      `json:"has_error"`
	ErrorType          string    `json:"error_type,omitempty"`
	ContextWindowIndex int       `json:"context_window_index"` // Which context window this chunk belongs to
	Embedding          []byte    `json:"embedding,omitempty"`
	EmbeddingModel     string    `json:"embedding_model,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
}

// TurnAnnotation represents a per-turn annotation with slice anchors.
// Stored in a separate annotations database with native vector search.
type TurnAnnotation struct {
	ID                 string `json:"id"` // ULID
	SessionID          string `json:"session_id"`
	TurnIndex          int    `json:"turn_index"`           // 0-based in session
	ContextWindowIndex int    `json:"context_window_index"` // compaction window

	// Source location in JSONL
	ByteOffset int64     `json:"byte_offset"`
	ByteLength int64     `json:"byte_length"`
	LineNum    int       `json:"line_num"`
	Timestamp  time.Time `json:"timestamp,omitempty"`

	// Classification
	ChunkType string `json:"chunk_type"` // user_request, assistant_response, etc.
	Role      string `json:"role"`       // user, assistant, system

	// Deterministic slice anchors (JSON arrays)
	CodeBlocks     []any    `json:"code_blocks,omitempty"` // [{language, content_hash, preview}]
	Commands       []any    `json:"commands,omitempty"`    // [{tool, command, exit_code}]
	Errors         []any    `json:"errors,omitempty"`      // [{type, message, tool}]
	FilePaths      []string `json:"file_paths,omitempty"`
	Symbols        []any    `json:"symbols,omitempty"` // [{name, kind, file}]
	ToolsUsed      []string `json:"tools_used,omitempty"`
	ContentPreview string   `json:"content_preview,omitempty"`
	ContentHash    string   `json:"content_hash"` // SHA256 of raw JSONL line

	// LLM annotations (filled by tiny model)
	TOCLabel          string `json:"toc_label,omitempty"`
	TOCCategory       string `json:"toc_category,omitempty"`
	Intent            string `json:"intent,omitempty"`
	AnnotationModel   string `json:"annotation_model,omitempty"`
	AnnotationVersion string `json:"annotation_version,omitempty"`

	// Embedding for semantic search
	Embedding      []byte `json:"embedding,omitempty"`
	EmbeddingModel string `json:"embedding_model,omitempty"`
	EmbeddingText  string `json:"embedding_text,omitempty"`

	// Metadata
	HasError          bool `json:"has_error"`
	IsCompactBoundary bool `json:"is_compact_boundary"`
	PreCompactTokens  int  `json:"pre_compact_tokens,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SessionChunkSummary captures a persisted summary over a chunk window.
type SessionChunkSummary struct {
	ID            string    `json:"id"`
	SessionID     string    `json:"session_id"`
	WindowIndex   int       `json:"window_index"`
	Trigger       string    `json:"trigger,omitempty"`
	ChunkIndices  []int     `json:"chunk_indices"`
	ChunkIndexMin int       `json:"chunk_index_min"`
	ChunkIndexMax int       `json:"chunk_index_max"`
	Tools         []string  `json:"tools,omitempty"`
	Files         []string  `json:"files,omitempty"`
	Errors        []string  `json:"errors,omitempty"`
	Summary       string    `json:"summary"`
	SummaryModel  string    `json:"summary_model,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// ScoredChunk couples a chunk with a relevance score.
type ScoredChunk struct {
	Chunk      SessionChunk `json:"chunk"`
	Similarity float64      `json:"similarity"`
}

// ChunkSearchOptions controls filtering for chunk-level vector search.
type ChunkSearchOptions struct {
	Workspace string // workspace path or stable ID
	SessionID string // restrict to a single session
}

// ChunkListOptions configures chunk listing.
type ChunkListOptions struct {
	SessionID  string
	ChunkType  string
	ErrorsOnly bool
	Limit      int
	Offset     int
}

// ContextWindow represents a compaction-bounded work span within a session.
// Each window captures the state between compaction events, enabling granular
// progressive memory retrieval at a finer level than whole sessions.
type ContextWindow struct {
	ID               string    `json:"id"`
	SessionID        string    `json:"session_id"`
	WindowIndex      int       `json:"window_index"`       // 0, 1, 2... per session
	StartedAt        time.Time `json:"started_at"`         // First message timestamp in window
	EndedAt          time.Time `json:"ended_at"`           // compact_boundary timestamp (or session end)
	PreCompactTokens int       `json:"pre_compact_tokens"` // From compactMetadata.preTokens
	Trigger          string    `json:"trigger"`            // 'auto' or 'manual'
	ChunkStart       int       `json:"chunk_start"`        // First chunk_index in window
	ChunkEnd         int       `json:"chunk_end"`          // Last chunk_index in window
	MessageCount     int       `json:"message_count"`      // Messages in this window
	Summary          string    `json:"summary,omitempty"`  // Per-window summary (optional)
	Embedding        []byte    `json:"embedding,omitempty"`
	EmbeddingModel   string    `json:"embedding_model,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

// ScoredContextWindow couples a context window with a relevance score.
type ScoredContextWindow struct {
	Window     ContextWindow `json:"window"`
	Similarity float64       `json:"similarity"`
}

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/context/companion/hybrid_pipeline.go
```go
package companion

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// InsertEvent inserts a new event into companion_events.
// For tool_call/tool_result events, payloadJSON MUST be non-nil (defensive invariant).
// For user_message/assistant_message events, payloadJSON and payloadRef MUST be nil.
func (m *ConversationMemory) InsertEvent(ctx context.Context, event *ConversationEvent) error {
	if event == nil {
		return fmt.Errorf("event is required")
	}
	if strings.TrimSpace(event.ConversationID) == "" {
		return fmt.Errorf("conversation_id is required")
	}
	eventType := strings.TrimSpace(strings.ToLower(event.EventType))
	if eventType == "" {
		return fmt.Errorf("event_type is required")
	}

	eventType = strings.ToLower(eventType)
	event.EventType = eventType

	isToolEvent := eventType == "tool_call" || eventType == "tool_result"
	isMessageEvent := eventType == "user_message" || eventType == "assistant_message"

	if isToolEvent && len(event.PayloadJSON) == 0 {
		return fmt.Errorf("tool events must include payload_json")
	}
	if isMessageEvent {
		if len(event.PayloadJSON) != 0 {
			return fmt.Errorf("message events must not include payload_json")
		}
		if strings.TrimSpace(event.PayloadRef) != "" {
			return fmt.Errorf("message events must not include payload_ref")
		}
	}
	if !isToolEvent && !isMessageEvent {
		return fmt.Errorf("invalid event_type %q", eventType)
	}

	var parentToolCallID any
	if event.ParentToolCallID != 0 {
		parentToolCallID = event.ParentToolCallID
	}

	// Convert empty strings to nil so SQL sees NULL (required by CHECK constraints).
	var turnID, toolName, toolRunID, payloadJSON, payloadRef, contentHash any
	if s := strings.TrimSpace(event.TurnID); s != "" {
		turnID = s
	}
	if s := strings.TrimSpace(event.ToolName); s != "" {
		toolName = s
	}
	if s := strings.TrimSpace(event.ToolRunID); s != "" {
		toolRunID = s
	}
	if s := strings.TrimSpace(event.PayloadJSON); s != "" {
		payloadJSON = s
	}
	if s := strings.TrimSpace(event.PayloadRef); s != "" {
		payloadRef = s
	}
	if s := strings.TrimSpace(event.ContentHash); s != "" {
		contentHash = s
	}

	var insertedID int64
	if err := m.db.QueryRowContext(ctx, `
		INSERT INTO companion_events (
			conversation_id,
			event_type,
			turn_id,
			tool_name,
			tool_run_id,
			parent_tool_call_id,
			payload_json,
			payload_ref,
			token_count,
			content_hash,
			created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, CURRENT_TIMESTAMP)
		RETURNING id
	`,
		event.ConversationID,
		eventType,
		turnID,
		toolName,
		toolRunID,
		parentToolCallID,
		payloadJSON,
		payloadRef,
		event.TokenCount,
		contentHash,
	).Scan(&insertedID); err != nil {
		return fmt.Errorf("insert companion event: %w", err)
	}
	event.ID = insertedID

	return nil
}

// claimWork claims a contiguous event range for single-writer updates.
// Returns (oldCursor, latestID] when successfully claimed.
func (m *ConversationMemory) claimWork(ctx context.Context, tx *sql.Tx, convID string) (fromEvent, toEvent int64, claimed bool, err error) {
	if strings.TrimSpace(convID) == "" {
		return 0, 0, false, fmt.Errorf("conversation_id is required")
	}

	if err = tx.QueryRowContext(ctx, `
		SELECT last_processed_event
		FROM companion_memory_mode_state
		WHERE conversation_id = $1
	`, convID).Scan(&fromEvent); err != nil {
		if err != sql.ErrNoRows {
			return 0, 0, false, fmt.Errorf("get cursor: %w", err)
		}

		if _, initErr := tx.ExecContext(ctx, `
			INSERT INTO companion_memory_mode_state
				(conversation_id, mode, schema_version, last_processed_event, last_soft_event, last_evidence_event, updated_at)
			VALUES ($1, 'hybrid', 1, 0, 0, 0, CURRENT_TIMESTAMP)
			ON CONFLICT (conversation_id) DO NOTHING
		`, convID); initErr != nil {
			return 0, 0, false, fmt.Errorf("init cursor: %w", initErr)
		}

		if scanErr := tx.QueryRowContext(ctx, `
			SELECT last_processed_event
			FROM companion_memory_mode_state
			WHERE conversation_id = $1
		`, convID).Scan(&fromEvent); scanErr != nil {
			return 0, 0, false, fmt.Errorf("get cursor after init: %w", scanErr)
		}
	}

	if err = tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(id), 0)
		FROM companion_events
		WHERE conversation_id = $1 AND id > $2
	`, convID, fromEvent).Scan(&toEvent); err != nil {
		return 0, 0, false, fmt.Errorf("get latest event: %w", err)
	}
	if toEvent == 0 {
		return fromEvent, 0, false, nil
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE companion_memory_mode_state
		SET last_processed_event = $1, updated_at = CURRENT_TIMESTAMP
		WHERE conversation_id = $2 AND last_processed_event = $3
	`, toEvent, convID, fromEvent)
	if err != nil {
		return 0, 0, false, fmt.Errorf("claim cursor: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return 0, 0, false, fmt.Errorf("cursor claim rows affected: %w", err)
	}
	if rows == 0 {
		return 0, 0, false, nil
	}

	return fromEvent, toEvent, true, nil
}

func containsPreference(text string) bool {
	for _, signal := range []string{"i prefer", "i always", "i never", "i don't like", "don't like", "i like", "i enjoy"} {
		if strings.Contains(text, signal) {
			return true
		}
	}
	return false
}

// hasMemoryWorthySignals checks if a message should pass Tier 1 extraction.
// tool_result events ALWAYS return true.
func hasMemoryWorthySignals(event ConversationEvent) bool {
	eventType := strings.ToLower(strings.TrimSpace(event.EventType))
	if eventType == "tool_result" {
		return true
	}
	if eventType != "user_message" && eventType != "assistant_message" {
		return false
	}

	text := strings.ToLower(event.Content)
	if containsPreference(text) {
		return true
	}
	if containsDecision(text) {
		return true
	}
	if containsQuestion(text) {
		return true
	}
	if containsDefinition(text) {
		return true
	}
	if containsGoalChange(text) {
		return true
	}
	return containsRetraction(text)
}

func containsDecision(text string) bool {
	for _, signal := range []string{"let's go with", "decided to", "the plan is", "we will", "we're going to", "i've decided", "i decided", "let us", "let's do"} {
		if strings.Contains(text, signal) {
			return true
		}
	}
	return false
}

func containsQuestion(text string) bool {
	if strings.HasSuffix(text, "?") {
		return true
	}
	for _, signal := range []string{"what about", "should we", "could we", "can we", "what if", "how about", "when do", "where should"} {
		if strings.Contains(text, signal) {
			return true
		}
	}
	return false
}

func containsDefinition(text string) bool {
	for _, signal := range []string{" means", " by", " i mean", " is when", "defined as", "is called"} {
		if strings.Contains(text, signal) {
			return true
		}
	}
	return false
}

func containsGoalChange(text string) bool {
	for _, signal := range []string{"the goal is", "we need to", "let's focus", "from now on", "going forward", "new objective", "change the goal", "priority is"} {
		if strings.Contains(text, signal) {
			return true
		}
	}
	return false
}

func containsRetraction(text string) bool {
	for _, signal := range []string{"actually no", "that was wrong", "forget that", "disregard", "scratch that", "nevermind", "i retract", "i take it back"} {
		if strings.Contains(text, signal) {
			return true
		}
	}
	return false
}

// BuildHybridContextLayers is the main hybrid pipeline entry point.
// It claims a range of events, processes Tier 0 and Tier 1 work, and returns.
func (m *ConversationMemory) BuildHybridContextLayers(ctx context.Context, conversationID string) error {
	if strings.TrimSpace(conversationID) == "" {
		return fmt.Errorf("conversation_id is required")
	}

	if err := m.EnsureHybridMode(ctx, conversationID); err != nil {
		return fmt.Errorf("ensure hybrid mode: %w", err)
	}

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin pipeline tx: %w", err)
	}
	defer tx.Rollback()

	fromEvent, toEvent, claimed, err := m.claimWork(ctx, tx, conversationID)
	if err != nil {
		return fmt.Errorf("claim work: %w", err)
	}
	if !claimed {
		return nil
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT
			e.id,
			e.conversation_id,
			e.event_type,
			e.turn_id,
			e.tool_name,
			e.tool_run_id,
			e.parent_tool_call_id,
			e.payload_json,
			e.payload_ref,
			e.token_count,
			e.content_hash,
			t.content
		FROM companion_events e
		LEFT JOIN companion_turns t
			on t.id = e.turn_id
		WHERE e.conversation_id = $1 AND e.id > $2 AND e.id <= $3
		ORDER BY e.id
	`, conversationID, fromEvent, toEvent)
	if err != nil {
		return fmt.Errorf("query event batch: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var event ConversationEvent
		var (
			turnID         sql.NullString
			toolName       sql.NullString
			toolRunID      sql.NullString
			parentToolCall sql.NullInt64
			payloadJSON    sql.NullString
			payloadRef     sql.NullString
			contentHash    sql.NullString
			turnContent    sql.NullString
		)

		if err := rows.Scan(
			&event.ID,
			&event.ConversationID,
			&event.EventType,
			&turnID,
			&toolName,
			&toolRunID,
			&parentToolCall,
			&payloadJSON,
			&payloadRef,
			&event.TokenCount,
			&contentHash,
			&turnContent,
		); err != nil {
			return fmt.Errorf("scan event: %w", err)
		}

		if turnID.Valid {
			event.TurnID = turnID.String
		}
		if toolName.Valid {
			event.ToolName = toolName.String
		}
		if toolRunID.Valid {
			event.ToolRunID = toolRunID.String
		}
		if parentToolCall.Valid {
			event.ParentToolCallID = parentToolCall.Int64
		}
		if payloadJSON.Valid {
			event.PayloadJSON = payloadJSON.String
		}
		if payloadRef.Valid {
			event.PayloadRef = payloadRef.String
		}
		if contentHash.Valid {
			event.ContentHash = contentHash.String
		}
		if turnContent.Valid {
			event.Content = turnContent.String
		}

		if err := m.processEventTier0(ctx, tx, conversationID, event); err != nil {
			return fmt.Errorf("process tier0: %w", err)
		}

		if hasMemoryWorthySignals(event) {
			if err := m.processEventTier1(ctx, tx, conversationID, event); err != nil {
				return fmt.Errorf("process tier1: %w", err)
			}
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("scan events: %w", err)
	}

	return tx.Commit()
}

// processEventTier0 handles Tier 0 bookkeeping: open episode state, tool runs, boundary checks.
func (m *ConversationMemory) processEventTier0(ctx context.Context, tx *sql.Tx, convID string, event ConversationEvent) error {
	if err := m.updateOpenEpisodeState(ctx, tx, convID, event); err != nil {
		return fmt.Errorf("update open episode state: %w", err)
	}

	reason, err := m.checkEpisodeBoundary(ctx, tx, convID, event)
	if err != nil {
		return fmt.Errorf("check episode boundary: %w", err)
	}
	if reason != "" {
		if err := m.handleEpisodeSeal(ctx, tx, convID, reason); err != nil {
			return fmt.Errorf("handle episode seal: %w", err)
		}
	}

	return nil
}

// processEventTier1 handles Tier 1 extraction: hard state, evidence, assumptions.
func (m *ConversationMemory) processEventTier1(ctx context.Context, tx *sql.Tx, convID string, event ConversationEvent) error {
	text := strings.TrimSpace(event.Content)
	if text == "" {
		return nil
	}

	extractions := extractProfileClaims(text)
	extractions = append(extractions, extractExplicitFacts(text)...)
	extractions = append(extractions, extractDecisions(text)...)
	extractions = append(extractions, extractOpenQuestions(text)...)
	if goal := extractGoalChange(text); goal != nil {
		extractions = append(extractions, *goal)
	}
	extractions = append(extractions, extractRetractions(text)...)

	for _, entry := range extractions {
		if err := m.persistDeterministicExtraction(ctx, tx, convID, event.ID, entry); err != nil {
			return fmt.Errorf("persist extraction: %w", err)
		}
	}

	return nil
}

// GetMemoryMode returns the current memory mode for a conversation.
// Companion memory is hybrid-only; missing mode state is treated as hybrid.
func (m *ConversationMemory) GetMemoryMode(ctx context.Context, conversationID string) (string, error) {
	if strings.TrimSpace(conversationID) == "" {
		return "", fmt.Errorf("conversation_id is required")
	}

	var mode string
	err := m.db.QueryRowContext(ctx, `
		SELECT mode
		FROM companion_memory_mode_state
		WHERE conversation_id = $1
	`, conversationID).Scan(&mode)
	if err == nil {
		return MemoryModeHybrid, nil
	}
	if err != nil && err != sql.ErrNoRows {
		return "", fmt.Errorf("get memory mode state: %w", err)
	}

	return MemoryModeHybrid, nil
}

// EnsureHybridMode initializes hybrid mode for a conversation if not already set.
func (m *ConversationMemory) EnsureHybridMode(ctx context.Context, conversationID string) error {
	if strings.TrimSpace(conversationID) == "" {
		return fmt.Errorf("conversation_id is required")
	}

	_, err := m.db.ExecContext(ctx, `
		INSERT INTO companion_memory_mode_state
			(conversation_id, mode, schema_version, last_processed_event, last_soft_event, last_evidence_event, updated_at)
		VALUES ($1, 'hybrid', 1, 0, 0, 0, CURRENT_TIMESTAMP)
		ON CONFLICT (conversation_id) DO UPDATE SET
			mode = 'hybrid',
			updated_at = CURRENT_TIMESTAMP
	`, conversationID)
	if err != nil {
		return fmt.Errorf("ensure hybrid mode: %w", err)
	}

	return nil
}

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/rlm/runtime/node.go
```go
package runtime

import (
	"errors"
	"fmt"
	"time"
)

var (
	// ErrInvalidNodeStatus marks unknown or unsupported node statuses.
	ErrInvalidNodeStatus = errors.New("rlm runtime: invalid node status")
	// ErrInvalidNodeStatusTransition marks disallowed node lifecycle transitions.
	ErrInvalidNodeStatusTransition = errors.New("rlm runtime: invalid node status transition")
)

// NodeStatus is one lifecycle status for an RLM node.
type NodeStatus string

const (
	NodeStatusQueued    NodeStatus = "queued"
	NodeStatusRunning   NodeStatus = "running"
	NodeStatusWaiting   NodeStatus = "waiting"
	NodeStatusCompleted NodeStatus = "completed"
	NodeStatusFailed    NodeStatus = "failed"
	NodeStatusCanceled  NodeStatus = "canceled"
)

// IsValid reports whether the status is a known lifecycle value.
func (status NodeStatus) IsValid() bool {
	switch status {
	case NodeStatusQueued, NodeStatusRunning, NodeStatusWaiting, NodeStatusCompleted, NodeStatusFailed, NodeStatusCanceled:
		return true
	default:
		return false
	}
}

// IsTerminal reports whether the status cannot transition further.
func (status NodeStatus) IsTerminal() bool {
	switch status {
	case NodeStatusCompleted, NodeStatusFailed, NodeStatusCanceled:
		return true
	default:
		return false
	}
}

// NodeStatusTransitionError wraps invalid transition details.
type NodeStatusTransitionError struct {
	From NodeStatus `json:"from"`
	To   NodeStatus `json:"to"`
}

func (err NodeStatusTransitionError) Error() string {
	return fmt.Sprintf("rlm runtime: invalid node status transition (%s -> %s)", err.From, err.To)
}

// Unwrap allows errors.Is(err, ErrInvalidNodeStatusTransition).
func (err NodeStatusTransitionError) Unwrap() error {
	return ErrInvalidNodeStatusTransition
}

// CanTransitionNodeStatus checks whether one status can move to the next status.
func CanTransitionNodeStatus(from, to NodeStatus) bool {
	if !from.IsValid() || !to.IsValid() {
		return false
	}
	if from == to {
		return true
	}
	switch from {
	case NodeStatusQueued:
		return to == NodeStatusRunning || to == NodeStatusWaiting || to == NodeStatusCanceled
	case NodeStatusRunning:
		return to == NodeStatusWaiting || to == NodeStatusCompleted || to == NodeStatusFailed || to == NodeStatusCanceled
	case NodeStatusWaiting:
		return to == NodeStatusRunning || to == NodeStatusCompleted || to == NodeStatusFailed || to == NodeStatusCanceled
	default:
		return false
	}
}

// ValidateNodeStatusTransition validates one status transition pair.
func ValidateNodeStatusTransition(from, to NodeStatus) error {
	if !from.IsValid() {
		return fmt.Errorf("%w: from=%q", ErrInvalidNodeStatus, from)
	}
	if !to.IsValid() {
		return fmt.Errorf("%w: to=%q", ErrInvalidNodeStatus, to)
	}
	if CanTransitionNodeStatus(from, to) {
		return nil
	}
	return NodeStatusTransitionError{From: from, To: to}
}

// ApplyNodeStatusTransition applies one validated status transition.
func ApplyNodeStatusTransition(node Node, to NodeStatus, at time.Time) (Node, error) {
	if err := ValidateNodeStatusTransition(node.Status, to); err != nil {
		return Node{}, err
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}

	next := node
	if next.Status == to {
		return next, nil
	}

	next.Status = to
	next.UpdatedAt = at
	if to == NodeStatusRunning && next.StartedAt.IsZero() {
		next.StartedAt = at
	}
	if to.IsTerminal() && next.FinishedAt.IsZero() {
		next.FinishedAt = at
	}
	return next, nil
}

// Run is the canonical root record for one recursive RLM execution.
type Run struct {
	ID         string         `json:"id"`
	RootNodeID string         `json:"root_node_id,omitempty"`
	Status     NodeStatus     `json:"status"`
	CreatedAt  time.Time      `json:"created_at,omitempty"`
	UpdatedAt  time.Time      `json:"updated_at,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// Node is one unit of recursive RLM execution.
type Node struct {
	RunID        string         `json:"run_id"`
	ID           string         `json:"id"`
	ParentNodeID string         `json:"parent_node_id,omitempty"`
	Depth        int            `json:"depth,omitempty"`
	Status       NodeStatus     `json:"status"`
	Prompt       string         `json:"prompt,omitempty"`
	CreatedAt    time.Time      `json:"created_at,omitempty"`
	UpdatedAt    time.Time      `json:"updated_at,omitempty"`
	StartedAt    time.Time      `json:"started_at,omitempty"`
	FinishedAt   time.Time      `json:"finished_at,omitempty"`
	Result       *NodeResult    `json:"result,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

// CanTransition reports whether this node can move to the requested status.
func (node Node) CanTransition(to NodeStatus) bool {
	return CanTransitionNodeStatus(node.Status, to)
}

// NodeResult is the structured output for one node execution.
type NodeResult struct {
	Status       NodeStatus     `json:"status"`
	Summary      string         `json:"summary,omitempty"`
	Answer       string         `json:"answer,omitempty"`
	Findings     []Finding      `json:"findings,omitempty"`
	EvidenceRefs []EvidenceRef  `json:"evidence_refs,omitempty"`
	ArtifactRefs []ArtifactRef  `json:"artifact_refs,omitempty"`
	ErrorCode    string         `json:"error_code,omitempty"`
	ErrorMessage string         `json:"error_message,omitempty"`
	StartedAt    time.Time      `json:"started_at,omitempty"`
	CompletedAt  time.Time      `json:"completed_at,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

// Finding is one distilled child output with supporting references.
type Finding struct {
	ID           string         `json:"id,omitempty"`
	Summary      string         `json:"summary,omitempty"`
	EvidenceRefs []EvidenceRef  `json:"evidence_refs,omitempty"`
	ArtifactRefs []ArtifactRef  `json:"artifact_refs,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

// EvidenceRef points at source material used by a node finding.
type EvidenceRef struct {
	Kind    string `json:"kind,omitempty"`
	Ref     string `json:"ref,omitempty"`
	Title   string `json:"title,omitempty"`
	Excerpt string `json:"excerpt,omitempty"`
}

// ArtifactRef points at persisted output generated by a node run.
type ArtifactRef struct {
	ID        string `json:"id,omitempty"`
	URI       string `json:"uri,omitempty"`
	MediaType string `json:"media_type,omitempty"`
	Summary   string `json:"summary,omitempty"`
}

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/tooling/evals/retrievaleval/eval.go
```go
package retrievaleval

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Query struct {
	ID            string   `yaml:"id" json:"id"`
	Query         string   `yaml:"query" json:"query"`
	ExpectedAnyOf []string `yaml:"expected_any_of" json:"expected_any_of"`
	Notes         string   `yaml:"notes,omitempty" json:"notes,omitempty"`
}

type Suite struct {
	Name    string  `yaml:"name" json:"name"`
	Queries []Query `yaml:"queries" json:"queries"`
}

type ModeResult struct {
	Mode             string   `json:"mode"`
	Available        bool     `json:"available"`
	Error            string   `json:"error,omitempty"`
	TotalHits        int      `json:"total_hits"`
	TopPaths         []string `json:"top_paths,omitempty"`
	FirstCorrectRank int      `json:"first_correct_rank,omitempty"`
	HitAt5           bool     `json:"hit_at_5"`
	HitAt10          bool     `json:"hit_at_10"`
}

type QueryResult struct {
	ID    string                `json:"id"`
	Query string                `json:"query"`
	Notes string                `json:"notes,omitempty"`
	Modes map[string]ModeResult `json:"modes"`
}

type Summary struct {
	Mode               string  `json:"mode"`
	Queries            int     `json:"queries"`
	Available          int     `json:"available"`
	HitRateAt5         float64 `json:"hit_rate_at_5"`
	HitRateAt10        float64 `json:"hit_rate_at_10"`
	MeanReciprocalRank float64 `json:"mean_reciprocal_rank"`
}

type RunResult struct {
	Suite       string        `json:"suite"`
	Workspace   string        `json:"workspace"`
	VaultPath   string        `json:"vault_path"`
	Limit       int           `json:"limit"`
	Queries     []QueryResult `json:"queries"`
	Summaries   []Summary     `json:"summaries"`
	GeneratedAt time.Time     `json:"generated_at"`
}

func LoadSuite(path string) (Suite, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return Suite{}, err
	}
	var suite Suite
	if err := yaml.Unmarshal(body, &suite); err != nil {
		return Suite{}, fmt.Errorf("decode suite yaml: %w", err)
	}
	if strings.TrimSpace(suite.Name) == "" {
		suite.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	return suite, nil
}

func EvaluateMode(mode string, hitPaths, expected []string, totalHits int, err error) ModeResult {
	result := ModeResult{
		Mode:      mode,
		Available: err == nil,
		TotalHits: totalHits,
		TopPaths:  append([]string(nil), hitPaths...),
	}
	if err != nil {
		result.Error = err.Error()
		return result
	}
	normalizedExpected := normalizeExpected(expected)
	for i, hit := range hitPaths {
		if containsPath(normalizedExpected, hit) {
			result.FirstCorrectRank = i + 1
			result.HitAt5 = i < 5
			result.HitAt10 = i < 10
			break
		}
	}
	return result
}

func Summarize(results []QueryResult, modes []string) []Summary {
	out := make([]Summary, 0, len(modes))
	for _, mode := range modes {
		s := Summary{Mode: mode, Queries: len(results)}
		var rrSum float64
		for _, query := range results {
			modeResult, ok := query.Modes[mode]
			if !ok || !modeResult.Available {
				continue
			}
			s.Available++
			if modeResult.HitAt5 {
				s.HitRateAt5++
			}
			if modeResult.HitAt10 {
				s.HitRateAt10++
			}
			if modeResult.FirstCorrectRank > 0 {
				rrSum += 1.0 / float64(modeResult.FirstCorrectRank)
			}
		}
		if s.Available > 0 {
			s.HitRateAt5 /= float64(s.Available)
			s.HitRateAt10 /= float64(s.Available)
			s.MeanReciprocalRank = rrSum / float64(s.Available)
		}
		out = append(out, s)
	}
	return out
}

func RenderMarkdown(result RunResult) string {
	var b strings.Builder
	b.WriteString("# Retrieval Eval\n\n")
	fmt.Fprintf(&b, "- Suite: `%s`\n", result.Suite)
	fmt.Fprintf(&b, "- Workspace: `%s`\n", result.Workspace)
	fmt.Fprintf(&b, "- Vault: `%s`\n", result.VaultPath)
	fmt.Fprintf(&b, "- Limit: `%d`\n\n", result.Limit)
	b.WriteString("## Summary\n\n")
	b.WriteString("| Mode | Available | hit@5 | hit@10 | MRR |\n")
	b.WriteString("| --- | ---: | ---: | ---: | ---: |\n")
	for _, s := range result.Summaries {
		b.WriteString(fmt.Sprintf("| %s | %d/%d | %.2f | %.2f | %.2f |\n", s.Mode, s.Available, s.Queries, s.HitRateAt5, s.HitRateAt10, s.MeanReciprocalRank))
	}
	b.WriteString("\n## Queries\n\n")
	for _, q := range result.Queries {
		b.WriteString("### " + q.ID + "\n\n")
		b.WriteString("- Query: `" + q.Query + "`\n")
		if q.Notes != "" {
			b.WriteString("- Notes: " + q.Notes + "\n")
		}
		for _, mode := range sortedModeKeys(q.Modes) {
			m := q.Modes[mode]
			line := fmt.Sprintf("- %s: ", mode)
			if !m.Available {
				b.WriteString(line + "unavailable")
				if m.Error != "" {
					b.WriteString(" (" + m.Error + ")")
				}
				b.WriteString("\n")
				continue
			}
			b.WriteString(line + fmt.Sprintf("rank=%d hit@5=%t hit@10=%t\n", m.FirstCorrectRank, m.HitAt5, m.HitAt10))
			for _, path := range m.TopPaths[:minInt(len(m.TopPaths), 3)] {
				b.WriteString("  - `" + path + "`\n")
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

func normalizeExpected(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(strings.TrimPrefix(item, "path:"))
		item = filepath.ToSlash(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func containsPath(expected []string, hit string) bool {
	hit = filepath.ToSlash(strings.TrimSpace(hit))
	for _, item := range expected {
		if hit == item {
			return true
		}
	}
	return false
}

func sortedModeKeys(m map[string]ModeResult) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	// fixed stable order if present
	order := map[string]int{
		"baseline":                   0,
		"lexical":                    1,
		"semantic":                   2,
		"blended":                    3,
		"skill_default":              4,
		"skill_context":              5,
		"skill_default_plus_context": 6,
		"aca_control_only":           7,
		"aca_vault_only":             8,
		"aca_repo_hints":             9,
		"aca_canonical_only":         10,
		"aca_package_fallback":       11,
		"aca_query_typed":            12,
		"aca_default":                13,
		"aca_cochange":               14,
		"aca_cochange_continuity":    15,
		"cochange_artifacts":         16,
		"repoindex_search":           17,
		"repoindex_dag":              18,
		"rlm_llm":                    19,
		"rlm_llm_codeintel":          20,
		"rlm_llm_code_staged":        21,
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			ri, iok := order[keys[i]]
			rj, jok := order[keys[j]]
			if !iok {
				ri = 99
			}
			if !jok {
				rj = 99
			}
			if rj < ri || (rj == ri && keys[j] < keys[i]) {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/context/contextplane/retrieval_inspect.go
```go
package contextplane

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/storage/cas"
	"github.com/joshka0/foxctl/internal/storage/obsidianindex"
	"gopkg.in/yaml.v3"
)

type RetrievalInspection struct {
	Query           string                    `json:"query"`
	ExpectedPaths   []string                  `json:"expected_paths,omitempty"`
	RetrievedPaths  []string                  `json:"retrieved_paths,omitempty"`
	Matched         bool                      `json:"matched"`
	Classification  string                    `json:"classification"`
	CandidateNote   string                    `json:"candidate_note,omitempty"`
	CandidateExists bool                      `json:"candidate_exists"`
	Observation     Observation               `json:"observation"`
	Proposal        RetrievalCorrectionAction `json:"proposal"`
	GeneratedAt     time.Time                 `json:"generated_at"`
}

type RetrievalCorrectionAction struct {
	Kind              string   `json:"kind"`
	Summary           string   `json:"summary"`
	PolicyPath        string   `json:"policy_path,omitempty"`
	PolicyPatch       string   `json:"policy_patch,omitempty"`
	TargetNotePath    string   `json:"target_note_path,omitempty"`
	NoteType          string   `json:"note_type,omitempty"`
	NoteTitle         string   `json:"note_title,omitempty"`
	MetadataPatch     string   `json:"metadata_patch,omitempty"`
	SupportingNote    string   `json:"supporting_note,omitempty"`
	ExpectedRepoPaths []string `json:"expected_repo_paths,omitempty"`
}

type RetrievalInspectionBatchSummary struct {
	Queries               int            `json:"queries"`
	Matched               int            `json:"matched"`
	Misses                int            `json:"misses"`
	Classifications       map[string]int `json:"classifications,omitempty"`
	RecommendedActions    []string       `json:"recommended_actions,omitempty"`
	PolicyPatchCandidate  bool           `json:"policy_patch_candidate"`
	PackageNoteCandidates []string       `json:"package_note_candidates,omitempty"`
}

func (s *WorkspaceStore) CurrentRetrievalOptions() RetrievalOptions {
	return s.loadRetrievalOptions()
}

func (s *WorkspaceStore) SetRetrievalPackageNoteFallback(enabled bool) (string, error) {
	layout, err := s.EnsureLayout()
	if err != nil {
		return "", err
	}
	body, err := s.ReadRetrievalPolicy()
	if err != nil {
		return "", err
	}
	doc := map[string]any{}
	if len(strings.TrimSpace(string(body))) > 0 {
		if err := yaml.Unmarshal(body, &doc); err != nil {
			return "", fmt.Errorf("decode retrieval policy: %w", err)
		}
	}
	aca, _ := doc["aca"].(map[string]any)
	if aca == nil {
		aca = map[string]any{}
	}
	aca["package_note_fallback"] = enabled
	doc["aca"] = aca
	updated, err := yaml.Marshal(doc)
	if err != nil {
		return "", fmt.Errorf("encode retrieval policy: %w", err)
	}
	if err := atomicWriteFile(layout.RetrievalPolicyPath, updated, 0o644); err != nil {
		return "", err
	}
	return layout.RetrievalPolicyPath, nil
}

func (s *WorkspaceStore) ReadRetrievalPolicy() ([]byte, error) {
	layout, err := s.EnsureLayout()
	if err != nil {
		return nil, err
	}
	return os.ReadFile(layout.RetrievalPolicyPath)
}

func (s *WorkspaceStore) WriteRetrievalPolicy(body []byte) (string, error) {
	layout, err := s.EnsureLayout()
	if err != nil {
		return "", err
	}
	if err := atomicWriteFile(layout.RetrievalPolicyPath, body, 0o644); err != nil {
		return "", err
	}
	return layout.RetrievalPolicyPath, nil
}

func (s *WorkspaceStore) InspectRetrieval(
	ctx context.Context,
	index obsidianindex.Store,
	vaultRoot string,
	query string,
	expectedPaths []string,
	result RetrievalResult,
	opts RetrievalOptions,
	limit int,
) (RetrievalInspection, error) {
	query = strings.TrimSpace(query)
	expectedPaths = normalizeExpectedPaths(expectedPaths)
	retrievedPaths := extractRetrievalPaths(result.VaultHits)
	retrievedMatches := retrievedPathsMatchExpected(result.VaultHits, expectedPaths)
	if retrievedMatches {
		return RetrievalInspection{
			Query:          query,
			ExpectedPaths:  expectedPaths,
			RetrievedPaths: retrievedPaths,
			Matched:        true,
			Classification: "matched",
			Observation:    buildRetrievalObservation(filepath.Base(s.layout.WorkspacePath), "matched", query, expectedPaths, "", result.VaultHits),
			Proposal: RetrievalCorrectionAction{
				Kind:    "none",
				Summary: "ACA retrieval already matched the expected path set.",
			},
			GeneratedAt: time.Now().UTC(),
		}, nil
	}

	expectedRepoPaths := expectedCodePaths(expectedPaths)
	candidateNote := ""
	candidateExists := false
	var candidateHit obsidianindex.SearchHit
	if len(expectedRepoPaths) > 0 && index != nil {
		candidateNote = firstPackageNoteCandidate(filepath.Base(s.layout.WorkspacePath), expectedRepoPaths)
		if candidateNote != "" {
			if vaultNoteExists(vaultRoot, candidateNote) {
				candidateExists = true
			}
			hit, ok := findExactCandidateNote(ctx, index, candidateNote)
			if ok {
				candidateExists = true
				candidateHit = hit
			}
		}
	}

	var classification string
	var proposal RetrievalCorrectionAction

	switch {
	case candidateExists && !opts.UsePackageNoteFallback && queryLooksCodeCentric(query):
		classification = "package_note_fallback_disabled"
		proposal = RetrievalCorrectionAction{
			Kind:        "policy_patch",
			Summary:     "Enable deterministic ACA package-note fallback for this workspace.",
			PolicyPath:  s.layout.RetrievalPolicyPath,
			PolicyPatch: "aca:\n  package_note_fallback: true\n",
		}
	case candidateExists && len(expectedRepoPaths) > 0 && !searchHitMatchesExpected(candidateHit, expectedPaths):
		classification = "bridge_metadata_gap"
		targetPath := candidateHit.Path
		if strings.TrimSpace(targetPath) == "" {
			targetPath = candidateNote
		}
		proposal = RetrievalCorrectionAction{
			Kind:              "metadata_patch",
			Summary:           fmt.Sprintf("Add repo path metadata to %s so ACA can connect it to the expected files.", targetPath),
			TargetNotePath:    targetPath,
			SupportingNote:    targetPath,
			ExpectedRepoPaths: expectedRepoPaths,
			MetadataPatch:     renderRepoPathMetadataPatch(expectedRepoPaths),
		}
	case !candidateExists && len(expectedRepoPaths) > 0:
		classification = "missing_package_note"
		proposal = RetrievalCorrectionAction{
			Kind:              "draft_package_note",
			Summary:           fmt.Sprintf("Create a canonical package note for %s.", candidateNote),
			TargetNotePath:    candidateNote,
			NoteType:          "map",
			NoteTitle:         suggestedPackageNoteTitle(candidateNote),
			ExpectedRepoPaths: expectedRepoPaths,
		}
	case len(result.VaultHits) > 0:
		classification = "ranking_mismatch"
		proposal = RetrievalCorrectionAction{
			Kind:           "manual_review",
			Summary:        "ACA retrieved notes, but ranking did not surface the expected path set.",
			SupportingNote: firstRetrievedPath(result.VaultHits),
		}
	default:
		classification = "missing_vault_coverage"
		proposal = RetrievalCorrectionAction{
			Kind:    "manual_review",
			Summary: "ACA found no useful durable note coverage for this query.",
		}
	}

	return RetrievalInspection{
		Query:           query,
		ExpectedPaths:   expectedPaths,
		RetrievedPaths:  retrievedPaths,
		Matched:         false,
		Classification:  classification,
		CandidateNote:   candidateNote,
		CandidateExists: candidateExists,
		Observation:     buildRetrievalObservation(filepath.Base(s.layout.WorkspacePath), classification, query, expectedPaths, candidateNote, result.VaultHits),
		Proposal:        proposal,
		GeneratedAt:     time.Now().UTC(),
	}, nil
}

func SummarizeRetrievalInspections(items []RetrievalInspection) RetrievalInspectionBatchSummary {
	summary := RetrievalInspectionBatchSummary{
		Queries:         len(items),
		Classifications: map[string]int{},
	}
	packageCandidates := []string{}
	packageSeen := map[string]struct{}{}
	for _, item := range items {
		summary.Classifications[item.Classification]++
		if item.Matched {
			summary.Matched++
		} else {
			summary.Misses++
		}
		if item.Proposal.Kind == "policy_patch" {
			summary.PolicyPatchCandidate = true
		}
		if item.Proposal.Kind == "draft_package_note" && strings.TrimSpace(item.Proposal.TargetNotePath) != "" {
			if _, ok := packageSeen[item.Proposal.TargetNotePath]; !ok {
				packageSeen[item.Proposal.TargetNotePath] = struct{}{}
				packageCandidates = append(packageCandidates, item.Proposal.TargetNotePath)
			}
		}
	}
	sort.Strings(packageCandidates)
	summary.PackageNoteCandidates = packageCandidates
	if summary.PolicyPatchCandidate {
		summary.RecommendedActions = append(summary.RecommendedActions, "Enable aca.package_note_fallback and rerun ACA retrieval.")
	}
	if len(summary.PackageNoteCandidates) > 0 {
		summary.RecommendedActions = append(summary.RecommendedActions, "Draft canonical package notes for the missing package-note targets.")
	}
	if summary.Classifications["bridge_metadata_gap"] > 0 {
		summary.RecommendedActions = append(summary.RecommendedActions, "Patch note frontmatter paths for canonical notes that exist but do not map to repo files.")
	}
	if len(summary.RecommendedActions) == 0 {
		summary.RecommendedActions = append(summary.RecommendedActions, "No deterministic corrective action identified; inspect ranking and note coverage manually.")
	}
	return summary
}

func PersistRetrievalInspectionReport(ctx context.Context, casRoot string, report any) (string, error) {
	casRoot = strings.TrimSpace(casRoot)
	if casRoot == "" {
		return "", nil
	}
	store, err := cas.NewStore(casRoot)
	if err != nil {
		return "", err
	}
	defer store.Close()
	body, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", err
	}
	obj, err := store.Put(ctx, bytes.NewReader(append(body, '\n')), "application/json", []string{"aca-retrieval-inspection"})
	if err != nil {
		return "", err
	}
	return obj.Digest, nil
}

func ReadInspectionArtifact(ctx context.Context, casRoot, digest string) ([]byte, error) {
	store, err := cas.NewStore(casRoot)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	rc, _, err := store.Get(ctx, digest)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

func normalizeExpectedPaths(items []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		normalized := filepath.ToSlash(strings.TrimSpace(item))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	sort.Strings(out)
	return out
}

func extractRetrievalPaths(hits []RetrievalHit) []string {
	out := make([]string, 0, len(hits))
	for _, hit := range hits {
		path := filepath.ToSlash(strings.TrimSpace(hit.Path))
		if path == "" {
			continue
		}
		out = append(out, path)
	}
	return uniqueStrings(out)
}

func retrievedPathsMatchExpected(hits []RetrievalHit, expected []string) bool {
	for _, hit := range hits {
		if retrievalHitMatchesExpected(hit, expected) {
			return true
		}
	}
	return false
}

func retrievalHitMatchesExpected(hit RetrievalHit, expected []string) bool {
	path := filepath.ToSlash(strings.TrimSpace(hit.Path))
	for _, item := range expected {
		if item == path {
			return true
		}
	}
	for _, repoPath := range hit.RepoPaths {
		repoPath = filepath.ToSlash(strings.TrimSpace(repoPath))
		for _, item := range expected {
			if item == repoPath {
				return true
			}
		}
	}
	return false
}

func searchHitMatchesExpected(hit obsidianindex.SearchHit, expected []string) bool {
	if containsString(expected, filepath.ToSlash(strings.TrimSpace(hit.Path))) {
		return true
	}
	for _, repoPath := range hit.RepoPaths {
		if containsString(expected, filepath.ToSlash(strings.TrimSpace(repoPath))) {
			return true
		}
	}
	return false
}

func expectedCodePaths(expected []string) []string {
	out := make([]string, 0, len(expected))
	for _, item := range expected {
		item = filepath.ToSlash(strings.TrimSpace(item))
		if item == "" {
			continue
		}
		if strings.HasSuffix(strings.ToLower(item), ".md") {
			continue
		}
		out = append(out, item)
	}
	return uniqueStrings(out)
}

func firstPackageNoteCandidate(repoName string, expectedRepoPaths []string) string {
	candidates := packageNotePathCandidates(repoName, expectedRepoPaths)
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0]
}

func findExactCandidateNote(ctx context.Context, index obsidianindex.Store, candidate string) (obsidianindex.SearchHit, bool) {
	for _, query := range []string{candidate, filepath.Base(candidate), strings.TrimSuffix(filepath.Base(candidate), filepath.Ext(candidate))} {
		if strings.TrimSpace(query) == "" {
			continue
		}
		hits, err := index.SearchNotes(ctx, query, 10)
		if err != nil {
			continue
		}
		for _, hit := range hits {
			if filepath.ToSlash(strings.TrimSpace(hit.Path)) == filepath.ToSlash(strings.TrimSpace(candidate)) {
				return hit, true
			}
		}
	}
	return obsidianindex.SearchHit{}, false
}

func vaultNoteExists(vaultRoot, notePath string) bool {
	vaultRoot = strings.TrimSpace(vaultRoot)
	notePath = filepath.ToSlash(strings.TrimSpace(notePath))
	if vaultRoot == "" || notePath == "" {
		return false
	}
	fullPath := filepath.Join(vaultRoot, filepath.FromSlash(notePath))
	info, err := os.Stat(fullPath)
	return err == nil && !info.IsDir()
}

func suggestedPackageNoteTitle(path string) string {
	base := strings.TrimSuffix(filepath.Base(strings.TrimSpace(path)), filepath.Ext(path))
	if base == "" {
		return "Package Note"
	}
	parts := strings.Fields(strings.ReplaceAll(base, "-", " "))
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func renderRepoPathMetadataPatch(paths []string) string {
	var b strings.Builder
	b.WriteString("paths:\n")
	for _, path := range uniqueStrings(paths) {
		b.WriteString("  - ")
		b.WriteString(filepath.ToSlash(strings.TrimSpace(path)))
		b.WriteString("\n")
	}
	return b.String()
}

func buildRetrievalObservation(project, classification, query string, expectedPaths []string, candidateNote string, hits []RetrievalHit) Observation {
	statement := retrievalObservationStatement(classification, query, candidateNote, expectedPaths)
	evidence := []string{"query:" + strings.TrimSpace(query)}
	for _, path := range expectedPaths {
		if strings.HasSuffix(strings.ToLower(path), ".md") {
			evidence = append(evidence, "note:"+path)
		} else {
			evidence = append(evidence, "path:"+path)
		}
	}
	if candidate := strings.TrimSpace(candidateNote); candidate != "" {
		evidence = append(evidence, "note:"+candidate)
	}
	limit := len(hits)
	if limit > 3 {
		limit = 3
	}
	for _, hit := range hits[:limit] {
		if path := strings.TrimSpace(hit.Path); path != "" {
			evidence = append(evidence, "note:"+path)
		}
	}
	now := time.Now().UTC()
	return Observation{
		Statement:    statement,
		Confidence:   retrievalObservationConfidence(classification),
		Count:        1,
		Project:      strings.TrimSpace(project),
		Area:         "aca-retrieval",
		EvidenceRefs: uniqueStrings(evidence),
		FirstSeen:    now,
		LastSeen:     now,
	}
}

func retrievalObservationStatement(classification, query, candidateNote string, expectedPaths []string) string {
	target := candidateNote
	if target == "" && len(expectedPaths) > 0 {
		target = expectedPaths[0]
	}
	switch classification {
	case "package_note_fallback_disabled":
		return fmt.Sprintf("ACA retrieval missed deterministic package-note fallback for %s on query %q.", target, strings.TrimSpace(query))
	case "missing_package_note":
		return fmt.Sprintf("ACA retrieval lacks a canonical package note for %s.", target)
	case "bridge_metadata_gap":
		return fmt.Sprintf("ACA retrieval note metadata is missing repo path coverage for %s.", target)
	case "ranking_mismatch":
		return fmt.Sprintf("ACA retrieval ranked the wrong notes for query %q against %s.", strings.TrimSpace(query), target)
	case "matched":
		return fmt.Sprintf("ACA retrieval matched the expected path set for query %q.", strings.TrimSpace(query))
	default:
		return fmt.Sprintf("ACA retrieval found no durable note coverage for query %q.", strings.TrimSpace(query))
	}
}

func retrievalObservationConfidence(classification string) float64 {
	switch classification {
	case "package_note_fallback_disabled", "missing_package_note", "bridge_metadata_gap":
		return 0.82
	case "ranking_mismatch":
		return 0.68
	default:
		return 0.61
	}
}

func firstRetrievedPath(hits []RetrievalHit) string {
	if len(hits) == 0 {
		return ""
	}
	return strings.TrimSpace(hits[0].Path)
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/context/companion/hybrid_evidence.go
```go
package companion

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/rs/zerolog"
)

const evidenceTimeFormat = "2006-01-02 15:04:05.000000"

// InsertEvidenceSnippet stores an extractive evidence snippet with dedup.
//
// Computes content_hash = hash(sourceEventID || normalizedText) to prevent
// cross-event collapse. If the redact step drops the snippet as too noisy, it returns 0.
func (m *ConversationMemory) InsertEvidenceSnippet(ctx context.Context, tx *sql.Tx, snippet *EvidenceSnippet) (int64, error) {
	if snippet == nil {
		return 0, fmt.Errorf("snippet is required")
	}
	if strings.TrimSpace(snippet.ConversationID) == "" {
		return 0, fmt.Errorf("conversation_id is required")
	}
	if snippet.SourceEventID <= 0 {
		return 0, fmt.Errorf("source_event_id must be positive")
	}

	redactedText, keep := redactEvidence(snippet.FactText)
	if !keep {
		zerolog.Ctx(ctx).Warn().
			Str("conversation_id", snippet.ConversationID).
			Int64("source_event_id", snippet.SourceEventID).
			Msg("dropping evidence snippet after redaction gate")
		return 0, nil
	}
	snippet.FactText = redactedText

	text := strings.TrimSpace(snippet.FactText)
	if text == "" {
		return 0, fmt.Errorf("fact_text is required")
	}
	snippet.FactText = text
	snippet.ContentHash = computeEvidenceContentHash(snippet.SourceEventID, snippet.FactText)
	if snippet.Confidence <= 0 {
		snippet.Confidence = 0.5
	}
	if snippet.Bucket == "" {
		snippet.Bucket = "default"
	}

	now := m.nowUTC()
	createdAt := now.Format(evidenceTimeFormat)

	var ttl any
	var expiresAt any
	if snippet.TTLDays != nil && *snippet.TTLDays > 0 {
		ttl = *snippet.TTLDays
		expiresAt = now.AddDate(0, 0, *snippet.TTLDays).Format(evidenceTimeFormat)
	}

	row := m.queryRowWithTx(
		ctx,
		tx,
		`
		INSERT OR IGNORE INTO companion_evidence_snippets (
			conversation_id, source_event_id, event_type, fact_text,
			content_hash, confidence, bucket, ttl_days, created_at, expires_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10
		)
		RETURNING id
		`,
		snippet.ConversationID,
		snippet.SourceEventID,
		snippet.EventType,
		snippet.FactText,
		snippet.ContentHash,
		snippet.Confidence,
		snippet.Bucket,
		ttl,
		createdAt,
		expiresAt,
	)
	var id int64
	if err := row.Scan(&id); err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, fmt.Errorf("insert evidence snippet: %w", err)
	}

	snippet.ID = id
	return id, nil
}

// redactEvidence strips sensitive content from evidence text before storage.
// Returns (redacted text, keep) where keep=false when redaction exceeds 50%.
func redactEvidence(text string) (string, bool) {
	if text == "" {
		return "", false
	}

	redacted := redactPatterns(text)
	redacted = redactHighEntropy(redacted)

	if redactionRatio(text, redacted) > 0.5 {
		return "", false
	}

	return redacted, true
}

// redactPatterns applies regex-based redactors for known sensitive patterns.
func redactPatterns(text string) string {
	redacted := reAPIKeySk.ReplaceAllString(text, "[REDACTED:api_key]")
	redacted = reAPIKeyAKIA.ReplaceAllString(redacted, "[REDACTED:api_key]")
	redacted = reAPIKeyXox.ReplaceAllString(redacted, "[REDACTED:api_key]")
	redacted = reBearerToken.ReplaceAllString(redacted, "[REDACTED:bearer_token]")
	redacted = reEmail.ReplaceAllString(redacted, "[REDACTED:email]")
	redacted = reURLCredentials.ReplaceAllString(redacted, "[REDACTED:url_with_credentials]")
	return redacted
}

// redactHighEntropy redacts base64/hex token-like values longer than 32 chars.
func redactHighEntropy(text string) string {
	redactFn := func(token string) string {
		clean := strings.Trim(token, ".,;:!?()[]{}<>\"'")
		if len(clean) <= 32 {
			return token
		}
		if isHexString(clean) && shannonEntropy(clean) > 3.8 {
			return "[REDACTED:high_entropy_token]"
		}
		if isBase64String(clean) && shannonEntropy(clean) > 3.8 {
			return "[REDACTED:high_entropy_token]"
		}
		return token
	}

	redacted := reHexLong.ReplaceAllStringFunc(text, redactFn)
	return reBase64Long.ReplaceAllStringFunc(redacted, redactFn)
}

// redactionRatio computes what fraction of the original text was replaced.
func redactionRatio(original, redacted string) float64 {
	orig := []rune(original)
	red := []rune(redacted)
	if len(orig) == 0 {
		return 0
	}

	lcs := lcsLen(orig, red)
	if lcs >= len(orig) {
		return 0
	}

	ratio := 1 - float64(lcs)/float64(len(orig))
	if ratio < 0 {
		return 0
	}
	if ratio > 1 {
		return 1
	}
	return ratio
}

// SearchEvidenceFTS searches evidence snippets using FTS5 bm25() ranking.
func (m *ConversationMemory) SearchEvidenceFTS(ctx context.Context, convID, query string, limit int) ([]EvidenceSnippet, error) {
	if strings.TrimSpace(convID) == "" {
		return nil, fmt.Errorf("conversation_id is required")
	}
	if strings.TrimSpace(query) == "" || limit <= 0 {
		return []EvidenceSnippet{}, nil
	}

	rows, err := m.db.QueryContext(ctx, `
		SELECT es.id, es.conversation_id, es.source_event_id, es.event_type, es.fact_text,
		       es.confidence, es.bucket, es.created_at,
		       bm25(companion_evidence_fts) AS rank
		FROM companion_evidence_fts fts
		JOIN companion_evidence_snippets es ON es.id = fts.rowid
		WHERE companion_evidence_fts MATCH $1 AND fts.conversation_id = $2
		ORDER BY rank
		LIMIT $3
	`, query, convID, limit)
	if err != nil {
		return nil, fmt.Errorf("search evidence fts: %w", err)
	}
	defer rows.Close()

	var snippets []EvidenceSnippet
	for rows.Next() {
		var snippet EvidenceSnippet
		var rank float64
		if err := rows.Scan(
			&snippet.ID,
			&snippet.ConversationID,
			&snippet.SourceEventID,
			&snippet.EventType,
			&snippet.FactText,
			&snippet.Confidence,
			&snippet.Bucket,
			&snippet.CreatedAt,
			&rank,
		); err != nil {
			continue
		}
		snippets = append(snippets, snippet)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan evidence snippets: %w", err)
	}

	return snippets, nil
}

// InsertAssumption adds a new assumption to the ledger.
func (m *ConversationMemory) InsertAssumption(ctx context.Context, tx *sql.Tx, assumption *Assumption) (int64, error) {
	if assumption == nil {
		return 0, fmt.Errorf("assumption is required")
	}
	if strings.TrimSpace(assumption.ConversationID) == "" {
		return 0, fmt.Errorf("conversation_id is required")
	}
	if strings.TrimSpace(assumption.Assumption) == "" {
		return 0, fmt.Errorf("assumption text is required")
	}
	if assumption.SourceEventID <= 0 {
		return 0, fmt.Errorf("source_event_id must be positive")
	}
	if assumption.Status == "" {
		assumption.Status = "active"
	}
	if assumption.Confidence <= 0 {
		assumption.Confidence = 0.5
	}

	row := m.queryRowWithTx(ctx, tx, `
		INSERT INTO companion_assumptions_ledger (
			conversation_id, assumption, status, reason, source_event_id, confidence, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, CURRENT_TIMESTAMP
		)
		RETURNING id
	`, assumption.ConversationID, assumption.Assumption, assumption.Status, assumption.Reason, assumption.SourceEventID, assumption.Confidence)
	var id int64
	if err := row.Scan(&id); err != nil {
		return 0, fmt.Errorf("insert assumption: %w", err)
	}

	assumption.ID = id
	return id, nil
}

// RetractAssumption retracts an active assumption with a reason.
func (m *ConversationMemory) RetractAssumption(ctx context.Context, tx *sql.Tx, id int64, retractedByEventID int64, reason string) error {
	if id <= 0 {
		return fmt.Errorf("assumption id must be positive")
	}

	result, err := m.execWithTx(ctx, tx, `
		UPDATE companion_assumptions_ledger
		SET status = 'retracted',
			retracted_at = CURRENT_TIMESTAMP,
			retracted_by_event_id = $2,
			retraction_reason = $3
		WHERE id = $1
	`, id, retractedByEventID, reason)
	if err != nil {
		return fmt.Errorf("retract assumption: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("retract assumption rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("assumption not found: %d", id)
	}

	return nil
}

// GetActiveAssumptions returns all active assumptions for a conversation.
func (m *ConversationMemory) GetActiveAssumptions(ctx context.Context, convID string) ([]Assumption, error) {
	if strings.TrimSpace(convID) == "" {
		return nil, fmt.Errorf("conversation_id is required")
	}

	rows, err := m.db.QueryContext(ctx, `
		SELECT id, conversation_id, assumption, status, reason, source_event_id, confidence
		FROM companion_assumptions_ledger
		WHERE conversation_id = $1 AND status = 'active'
		ORDER BY id DESC
	`, convID)
	if err != nil {
		return nil, fmt.Errorf("get active assumptions: %w", err)
	}
	defer rows.Close()

	var assumptions []Assumption
	for rows.Next() {
		var a Assumption
		if err := rows.Scan(&a.ID, &a.ConversationID, &a.Assumption, &a.Status, &a.Reason, &a.SourceEventID, &a.Confidence); err != nil {
			continue
		}
		assumptions = append(assumptions, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan assumptions: %w", err)
	}
	return assumptions, nil
}

// PromoteAssumption promotes an assumption to hard state with audit metadata.
func (m *ConversationMemory) PromoteAssumption(ctx context.Context, tx *sql.Tx, assumptionID int64, promotedBy string, sourceEventID int64) error {
	if assumptionID <= 0 {
		return fmt.Errorf("assumption id must be positive")
	}
	if sourceEventID <= 0 {
		return fmt.Errorf("source_event_id must be positive")
	}

	var convID, assumptionText string
	var confidence float64
	if err := m.queryRowWithTx(ctx, tx, `
		SELECT conversation_id, assumption, confidence
		FROM companion_assumptions_ledger
		WHERE id = $1 AND status = 'active'
	`, assumptionID).Scan(&convID, &assumptionText, &confidence); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("assumption not found or not active: %d", assumptionID)
		}
		return fmt.Errorf("load assumption: %w", err)
	}

	result, err := m.execWithTx(ctx, tx, `
		UPDATE companion_assumptions_ledger
		SET status = 'promoted'
		WHERE id = $1 AND status = 'active'
	`, assumptionID)
	if err != nil {
		return fmt.Errorf("promote assumption: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("promote assumption rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("assumption not found or already promoted: %d", assumptionID)
	}

	metadata, err := json.Marshal(map[string]any{
		"promoted_by":            promotedBy,
		"original_assumption_id": assumptionID,
		"original_confidence":    confidence,
		"promotion_event_id":     sourceEventID,
	})
	if err != nil {
		return fmt.Errorf("marshal promotion metadata: %w", err)
	}

	valueJSON, err := json.Marshal(map[string]any{
		"assumption": assumptionText,
	})
	if err != nil {
		return fmt.Errorf("marshal assumption value: %w", err)
	}

	_, err = m.AppendHardStateEntry(ctx, tx, &HardStateEntry{
		ConversationID: convID,
		EntryType:      "assumption",
		Key:            fmt.Sprintf("assumption:%d", assumptionID),
		ValueJSON:      string(valueJSON),
		Status:         "active",
		SourceEventID:  sourceEventID,
		Confidence:     confidence,
		MetadataJSON:   strPtr(string(metadata)),
	})
	if err != nil {
		return fmt.Errorf("insert promoted hard state entry: %w", err)
	}

	return nil
}

// AppendHardStateEntry inserts a new immutable hard state entry.
func (m *ConversationMemory) AppendHardStateEntry(ctx context.Context, tx *sql.Tx, entry *HardStateEntry) (int64, error) {
	if entry == nil {
		return 0, fmt.Errorf("hard state entry is required")
	}
	if strings.TrimSpace(entry.ConversationID) == "" {
		return 0, fmt.Errorf("conversation_id is required")
	}
	if strings.TrimSpace(entry.EntryType) == "" {
		return 0, fmt.Errorf("entry_type is required")
	}
	if strings.TrimSpace(entry.Key) == "" {
		return 0, fmt.Errorf("key is required")
	}
	if strings.TrimSpace(entry.ValueJSON) == "" {
		return 0, fmt.Errorf("value_json is required")
	}
	if entry.SourceEventID <= 0 {
		return 0, fmt.Errorf("source_event_id must be positive")
	}
	if entry.Confidence <= 0 {
		entry.Confidence = 0.8
	}
	if entry.Status == "" {
		entry.Status = "active"
	}

	var supersedes any
	if entry.Supersedes != nil && *entry.Supersedes > 0 {
		supersedes = *entry.Supersedes
	}

	row := m.queryRowWithTx(ctx, tx, `
		INSERT INTO companion_hard_state_entries (
			conversation_id, entry_type, key, value_json, status, source_event_id,
			confidence, metadata_json, supersedes, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, CURRENT_TIMESTAMP
		)
		RETURNING id
	`, entry.ConversationID, entry.EntryType, entry.Key, entry.ValueJSON, entry.Status,
		entry.SourceEventID, entry.Confidence, entry.MetadataJSON, supersedes)

	var id int64
	if err := row.Scan(&id); err != nil {
		return 0, fmt.Errorf("insert hard state entry: %w", err)
	}

	entry.ID = id
	return id, nil
}

// SupersedeHardStateEntry creates a new entry that supersedes an existing one.
func (m *ConversationMemory) SupersedeHardStateEntry(ctx context.Context, tx *sql.Tx, oldEntryID int64, newEntry *HardStateEntry) (int64, error) {
	if oldEntryID <= 0 {
		return 0, fmt.Errorf("old entry id must be positive")
	}
	if newEntry == nil {
		return 0, fmt.Errorf("hard state entry is required")
	}
	newEntry.Supersedes = int64Ptr(oldEntryID)
	return m.AppendHardStateEntry(ctx, tx, newEntry)
}

// RetractHardStateEntry creates a retraction entry for an existing key.
func (m *ConversationMemory) RetractHardStateEntry(ctx context.Context, tx *sql.Tx, oldEntryID int64, sourceEventID int64, reason string) (int64, error) {
	if oldEntryID <= 0 {
		return 0, fmt.Errorf("old entry id must be positive")
	}
	if sourceEventID <= 0 {
		return 0, fmt.Errorf("source_event_id must be positive")
	}

	var convID, entryType, key string
	var confidence float64
	var oldMetadata sql.NullString
	if err := m.queryRowWithTx(ctx, tx, `
		SELECT conversation_id, entry_type, key, confidence, metadata_json
		FROM companion_hard_state_entries
		WHERE id = $1
	`, oldEntryID).Scan(&convID, &entryType, &key, &confidence, &oldMetadata); err != nil {
		if err == sql.ErrNoRows {
			return 0, fmt.Errorf("hard state entry not found: %d", oldEntryID)
		}
		return 0, fmt.Errorf("load hard state entry: %w", err)
	}

	metadata := map[string]any{}
	if oldMetadata.Valid && oldMetadata.String != "" {
		_ = json.Unmarshal([]byte(oldMetadata.String), &metadata)
	}
	metadata["retracted"] = true
	metadata["retracted_by_event_id"] = sourceEventID
	metadata["retraction_reason"] = reason
	metadata["superseded_entry_id"] = oldEntryID

	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return 0, fmt.Errorf("marshal retraction metadata: %w", err)
	}

	retractedEntry := &HardStateEntry{
		ConversationID: convID,
		EntryType:      entryType,
		Key:            key,
		ValueJSON:      "null",
		Status:         "retracted",
		SourceEventID:  sourceEventID,
		Confidence:     confidence,
		MetadataJSON:   strPtr(string(metadataJSON)),
		Supersedes:     int64Ptr(oldEntryID),
	}

	return m.AppendHardStateEntry(ctx, tx, retractedEntry)
}

// DeleteExpiredEvidence deletes evidence snippets past their TTL.
func (m *ConversationMemory) DeleteExpiredEvidence(ctx context.Context) (int64, error) {
	result, err := m.db.ExecContext(ctx, `
		DELETE FROM companion_evidence_snippets
		WHERE expires_at IS NOT NULL AND expires_at < CURRENT_TIMESTAMP
	`)
	if err != nil {
		return 0, fmt.Errorf("delete expired evidence: %w", err)
	}
	removed, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected: %w", err)
	}
	return removed, nil
}

func computeEvidenceContentHash(sourceEventID int64, normalizedText string) string {
	payload := fmt.Sprintf("%d|%s", sourceEventID, normalizeEvidenceText(normalizedText))
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func normalizeEvidenceText(text string) string {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(text)))
	return strings.Join(fields, " ")
}

func lcsLen(a, b []rune) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}

	dp := make([]int, len(b)+1)
	for i := 0; i < len(a); i++ {
		prevDiag := 0
		for j := 0; j < len(b); j++ {
			tmp := dp[j+1]
			if a[i] == b[j] {
				dp[j+1] = prevDiag + 1
			} else if dp[j] > dp[j+1] {
				dp[j+1] = dp[j]
			}
			prevDiag = tmp
		}
	}
	return dp[len(b)]
}

func isHexString(token string) bool {
	return reHexLong.MatchString(token)
}

func isBase64String(token string) bool {
	return reBase64Long.MatchString(token)
}

func shannonEntropy(value string) float64 {
	if value == "" {
		return 0
	}
	freq := make(map[rune]int, len(value))
	for _, r := range value {
		freq[unicode.ToLower(r)]++
	}

	total := float64(len([]rune(value)))
	if total == 0 {
		return 0
	}

	var entropy float64
	for _, count := range freq {
		prob := float64(count) / total
		if prob <= 0 {
			continue
		}
		entropy -= prob * (math.Log(prob) / math.Log(2))
	}
	return entropy
}

func (m *ConversationMemory) nowUTC() time.Time {
	return m.clock.Now().UTC()
}

func (m *ConversationMemory) queryRowWithTx(ctx context.Context, tx *sql.Tx, query string, args ...any) *sql.Row {
	if tx != nil {
		return tx.QueryRowContext(ctx, query, args...)
	}
	return m.db.QueryRowContext(ctx, query, args...)
}

func (m *ConversationMemory) execWithTx(ctx context.Context, tx *sql.Tx, query string, args ...any) (sql.Result, error) {
	if tx != nil {
		return tx.ExecContext(ctx, query, args...)
	}
	return m.db.ExecContext(ctx, query, args...)
}

var (
	reAPIKeySk       = regexp.MustCompile(`(?i)\bsk-[a-z0-9]{20,}\b`)
	reAPIKeyAKIA     = regexp.MustCompile(`\bAKIA[0-9A-Z]{16,}\b`)
	reAPIKeyXox      = regexp.MustCompile(`\bxox-[a-z0-9_-]{10,}\b`)
	reBearerToken    = regexp.MustCompile(`(?i)\bbearer\s+[a-z0-9\-._~+/=]{16,}\b`)
	reEmail          = regexp.MustCompile(`(?i)\b[0-9a-z._%+\-]+@[0-9a-z.\-]+\.[a-z]{2,}\b`)
	reURLCredentials = regexp.MustCompile(`(?i)\b(?:[a-z][a-z0-9+\-.]*://[^/\s:@]+:[^/\s@]+@[^/\s]+)`)
	reHexLong        = regexp.MustCompile(`(?i)\b[0-9a-f]{33,}\b`)
	reBase64Long     = regexp.MustCompile(`(?i)\b[a-z0-9+\-_]{33,}={0,2}\b`)
)

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/storage/trajectory/types.go
```go
package trajectory

import (
	"time"
)

// Status represents the outcome of a trajectory.
type Status string

// Status constants per dspy_trajectory_capture.md §3.1.
const (
	StatusOK      Status = "ok"
	StatusError   Status = "error"
	StatusAborted Status = "aborted"
	StatusPartial Status = "partial"
)

// Outcome captures the final result of a trajectory for optimization.
// This data is used by the optimization system to learn from past executions.
type Outcome struct {
	// Success indicates whether the trajectory achieved its goal.
	Success bool `json:"success"`

	// TasksCompleted is the number of tasks completed during the trajectory.
	TasksCompleted int `json:"tasks_completed"`

	// ToolCallCount is the total number of tool calls made.
	ToolCallCount int `json:"tool_call_count"`

	// ErrorCount is the number of errors encountered.
	ErrorCount int `json:"error_count"`

	// DurationMS is the total execution time in milliseconds.
	DurationMS int64 `json:"duration_ms"`

	// HumanRating is an optional human-provided rating (1-5 scale).
	HumanRating *int `json:"human_rating,omitempty"`

	// Feedback is optional human-provided feedback text.
	Feedback string `json:"feedback,omitempty"`

	// Metrics holds arbitrary optimization metrics.
	Metrics map[string]float64 `json:"metrics,omitempty"`

	// RecordedAt is when the outcome was recorded.
	RecordedAt time.Time `json:"recorded_at,omitempty"`
}

// EventKind represents the type of trajectory event.
type EventKind string

// EventKind constants per dspy_trajectory_capture.md §3.3.
const (
	EventKindUserRequest    EventKind = "user_request"
	EventKindAgentThought   EventKind = "agent_thought"
	EventKindToolCall       EventKind = "tool_call"
	EventKindToolResult     EventKind = "tool_result"
	EventKindReviewRequest  EventKind = "review_request"
	EventKindReviewResult   EventKind = "review_result"
	EventKindTaskTransition EventKind = "task_transition"
	EventKindGraphSearch    EventKind = "graph_search"
	EventKindSWEGrep        EventKind = "swe_grep"
	EventKindHookCall       EventKind = "hook_call"
	EventKindHookResult     EventKind = "hook_result"
)

// Source indicates where a user request originated.
type Source string

// Source constants per dspy_trajectory_capture.md §3.2.
const (
	SourceCLI     Source = "cli"
	SourceMailbox Source = "mailbox"
	SourceAPI     Source = "api"
	SourceViewer  Source = "viewer"
)

// Trajectory is a logical record representing a coherent run or episode,
// usually anchored to a single user request and task.
// Per dspy_trajectory_capture.md §3.1.
type Trajectory struct {
	// ID is a ULID uniquely identifying the trajectory.
	ID string `json:"id"`

	// WorkspaceID scopes the trajectory to a workspace.
	WorkspaceID string `json:"workspace_id"`

	// RootRequestID links to the UserRequestCapture that started the trajectory.
	RootRequestID string `json:"root_request_id,omitempty"`

	// TaskIDs lists tasks touched by this trajectory.
	TaskIDs []string `json:"task_ids,omitempty"`

	// EpicID optionally links to an epic-level plan.
	EpicID string `json:"epic_id,omitempty"`

	// AgentRole identifies the agent type (coder, planner, reviewer, overseer).
	AgentRole string `json:"agent_role,omitempty"`

	// JobID links to the jobs entry for the main agent run.
	JobID string `json:"job_id,omitempty"`

	// TraceID is the Protocol v1 meta.trace_id linking all envelopes.
	TraceID string `json:"trace_id,omitempty"`

	// Status indicates the outcome: ok, error, aborted, partial.
	Status Status `json:"status"`

	// Summary is a human-readable description.
	Summary string `json:"summary,omitempty"`

	// ArtifactDigest is a CAS digest for the full trajectory payload if large.
	ArtifactDigest string `json:"artifact_digest,omitempty"`

	// Outcome captures the final result for optimization purposes.
	Outcome *Outcome `json:"outcome,omitempty"`

	// CreatedAt records when the trajectory was created.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt records the last update time.
	UpdatedAt time.Time `json:"updated_at"`

	// SessionID links to the AI coding tool session that created this trajectory.
	// Supports Claude Code, OpenCode, Cursor, and other tools via FOXCTL_SESSION_ID.
	SessionID string `json:"session_id,omitempty"`
}

// CommandContext provides context about the command that triggered a user request.
// Per dspy_trajectory_capture.md §3.2.
type CommandContext struct {
	// CLICommand is the full CLI command string (e.g., "foxctl agent spawn ...").
	CLICommand string `json:"cli_command,omitempty"`

	// ProtocolCommand is the Protocol v1 command (e.g., "agent/spawn").
	ProtocolCommand string `json:"protocol_command,omitempty"`

	// JobID links to a job triggered by the command.
	JobID string `json:"job_id,omitempty"`

	// TraceID is the Protocol v1 meta.trace_id.
	TraceID string `json:"trace_id,omitempty"`
}

// TaskHints provides pre-linking hints to tasks/epics.
// Per dspy_trajectory_capture.md §3.2.
type TaskHints struct {
	// TaskID is the task related to the request.
	TaskID string `json:"task_id,omitempty"`

	// EpicID is the epic related to the request.
	EpicID string `json:"epic_id,omitempty"`

	// ScopePaths limits the request scope to specific paths.
	ScopePaths []string `json:"scope_paths,omitempty"`
}

// UserRequestCapture normalizes a user-initiated intent.
// Per dspy_trajectory_capture.md §3.2.
type UserRequestCapture struct {
	// ID uniquely identifies the user request.
	ID string `json:"id"`

	// WorkspaceID scopes the request to a workspace.
	WorkspaceID string `json:"workspace_id"`

	// Actor identifies who made the request (e.g., actor:human:<id>).
	Actor string `json:"actor"`

	// Source indicates where the request came from (cli, mailbox, api, viewer).
	Source Source `json:"source"`

	// TS is the timestamp of the request.
	TS time.Time `json:"ts"`

	// Text is the raw user request or prompt.
	Text string `json:"text"`

	// CommandContext provides context about the command that triggered the request.
	CommandContext *CommandContext `json:"command_context,omitempty"`

	// TaskHints provides pre-linking hints to tasks/epics.
	TaskHints *TaskHints `json:"task_hints,omitempty"`
}

// EventMeta holds correlation metadata for trajectory events.
// Per dspy_trajectory_capture.md §3.3.1.
type EventMeta struct {
	// TraceID is the primary correlation id; required for trajectory-related events.
	TraceID string `json:"trace_id,omitempty"`

	// JobID links to a job when events originate from jobs/* commands.
	JobID string `json:"job_id,omitempty"`

	// TaskID links to a task for task-scoped events.
	TaskID string `json:"task_id,omitempty"`

	// EpicID links to an epic-level plan.
	EpicID string `json:"epic_id,omitempty"`

	// ReviewID links to a review artifact.
	ReviewID string `json:"review_id,omitempty"`

	// ReviewRequestID links to a review request message or job.
	ReviewRequestID string `json:"review_request_id,omitempty"`

	// ReviewResultID links to a review result message or job.
	ReviewResultID string `json:"review_result_id,omitempty"`

	// ActorID or AgentID identifies the agent/actor (e.g., actor:agent:dspy:<slug>).
	ActorID string `json:"actor_id,omitempty"`

	// TaskRunID uniquely identifies an execution attempt of a task.
	TaskRunID string `json:"task_run_id,omitempty"`

	// TraceParent links to a parent trace for spawned trajectories.
	TraceParent string `json:"trace_parent,omitempty"`

	// JobAttempt is the retry attempt number for a job.
	JobAttempt int `json:"job_attempt,omitempty"`

	// CreatedBy identifies the service/user that created the event.
	CreatedBy string `json:"created_by,omitempty"`

	// CASDigest is the sha256:<hex> digest matching any referenced CAS artifact.
	CASDigest string `json:"cas_digest,omitempty"`
}

// Event is a normalized view over envelopes, mailbox messages,
// and review artifacts.
// Per dspy_trajectory_capture.md §3.3.
type Event struct {
	// ID uniquely identifies the event (ULID).
	ID string `json:"id"`

	// TrajectoryID links to the parent trajectory.
	TrajectoryID string `json:"trajectory_id"`

	// TS is the event timestamp.
	TS time.Time `json:"ts"`

	// Kind indicates the event type (user_request, tool_call, etc.).
	Kind EventKind `json:"kind"`

	// Actor identifies who or what produced the event.
	Actor string `json:"actor,omitempty"`

	// Command is the Protocol v1 command when applicable.
	Command string `json:"command,omitempty"`

	// Status is derived from envelope status or review status.
	Status string `json:"status,omitempty"`

	// DataInline is a small preview (e.g., truncated message).
	DataInline map[string]any `json:"data_inline,omitempty"`

	// DataArtifact is a CAS digest for full details.
	DataArtifact string `json:"data_artifact,omitempty"`

	// Meta holds correlation metadata.
	Meta *EventMeta `json:"meta,omitempty"`
}

// ListFilter specifies filtering options for listing trajectories.
type ListFilter struct {
	// WorkspaceID filters by workspace (required).
	WorkspaceID string

	// TaskID filters by task.
	TaskID string

	// EpicID filters by epic.
	EpicID string

	// AgentRole filters by agent role (coder, planner, reviewer, overseer).
	AgentRole string

	// Status filters by trajectory status.
	Status Status

	// TraceID filters by trace id.
	TraceID string

	// SessionID filters by session id.
	SessionID string

	// Since filters to trajectories created after this time.
	Since time.Time

	// Until filters to trajectories created before this time.
	Until time.Time

	// Limit caps the number of results (default 100).
	Limit int
}

// OutcomeFilter specifies filtering options for listing trajectories by outcome.
type OutcomeFilter struct {
	// WorkspaceID scopes the query (required).
	WorkspaceID string

	// AgentRole filters by agent role.
	AgentRole string

	// Success filters by success status (nil = any).
	Success *bool

	// MinRating filters to trajectories with human rating >= this value.
	MinRating *int

	// MaxRating filters to trajectories with human rating <= this value.
	MaxRating *int

	// HasFeedback filters to trajectories that have human feedback.
	HasFeedback *bool

	// Since filters to trajectories with outcomes recorded after this time.
	Since time.Time

	// Until filters to trajectories with outcomes recorded before this time.
	Until time.Time

	// Limit caps the number of results (default 100).
	Limit int
}

// EventFilter specifies filtering options for listing events.
type EventFilter struct {
	// TrajectoryID filters by trajectory (required).
	TrajectoryID string

	// Kind filters by event kind.
	Kind EventKind

	// Since filters to events after this time.
	Since time.Time

	// Until filters to events before this time.
	Until time.Time

	// Limit caps the number of results (default 1000).
	Limit int
}

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/intelligence/retrieval/v2/query_plan.go
```go
package retrievalv2

import "github.com/joshka0/foxctl/internal/intelligence/searchquery"

type IdentifierKind string

const (
	IdentifierKindSymbol    IdentifierKind = "symbol"
	IdentifierKindQualified IdentifierKind = "qualified"
	IdentifierKindSnake     IdentifierKind = "snake"
	IdentifierKindCamel     IdentifierKind = "camel"
	IdentifierKindPath      IdentifierKind = "path"
)

type Identifier struct {
	Value string
	Kind  IdentifierKind
}

type Phrase struct {
	Text string
}

type PathHint struct {
	Path string
}

// QueryPlan captures normalized retrieval-query structure without exposing the
// underlying searchquery package as part of the retrieval-v2 surface.
type QueryPlan struct {
	Raw         string
	Terms       []string
	Identifiers []Identifier
	Phrases     []Phrase
	PathHints   []PathHint
}

func ParseQuery(raw string) QueryPlan {
	return fromSearchQueryPlan(searchquery.ParseQuery(raw))
}

func ComposeLexicalQuery(plan QueryPlan) string {
	return searchquery.ComposeLexicalQuery(toSearchQueryPlan(plan))
}

func fromSearchQueryPlan(plan searchquery.QueryPlan) QueryPlan {
	out := QueryPlan{
		Raw:         plan.Raw,
		Terms:       append([]string(nil), plan.Terms...),
		Identifiers: make([]Identifier, 0, len(plan.Identifiers)),
		Phrases:     make([]Phrase, 0, len(plan.Phrases)),
		PathHints:   make([]PathHint, 0, len(plan.PathHints)),
	}
	for _, item := range plan.Identifiers {
		out.Identifiers = append(out.Identifiers, Identifier{
			Value: item.Value,
			Kind:  IdentifierKind(item.Kind),
		})
	}
	for _, item := range plan.Phrases {
		out.Phrases = append(out.Phrases, Phrase{Text: item.Text})
	}
	for _, item := range plan.PathHints {
		out.PathHints = append(out.PathHints, PathHint{Path: item.Path})
	}
	return out
}

func toSearchQueryPlan(plan QueryPlan) searchquery.QueryPlan {
	out := searchquery.QueryPlan{
		Raw:         plan.Raw,
		Terms:       append([]string(nil), plan.Terms...),
		Identifiers: make([]searchquery.Identifier, 0, len(plan.Identifiers)),
		Phrases:     make([]searchquery.Phrase, 0, len(plan.Phrases)),
		PathHints:   make([]searchquery.PathHint, 0, len(plan.PathHints)),
	}
	for _, item := range plan.Identifiers {
		out.Identifiers = append(out.Identifiers, searchquery.Identifier{
			Value: item.Value,
			Kind:  searchquery.IdentifierKind(item.Kind),
		})
	}
	for _, item := range plan.Phrases {
		out.Phrases = append(out.Phrases, searchquery.Phrase{Text: item.Text})
	}
	for _, item := range plan.PathHints {
		out.PathHints = append(out.PathHints, searchquery.PathHint{Path: item.Path})
	}
	return out
}

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/tooling/evals/longcoteval/types.go
```go
package longcoteval

import (
	"encoding/json"
	"time"
)

// ConditionID identifies one LongCoT evaluation arm. Keep these values stable;
// reports and experiment logs use them as durable join keys.
type ConditionID string

const (
	ConditionBaselineNoToolsOfficial ConditionID = "baseline_no_tools_official_prompt"
	ConditionRLMNoToolsSingle        ConditionID = "rlm_no_tools_single"
	ConditionRLMReplNoSubcalls       ConditionID = "rlm_repl_no_subcalls"
	ConditionRLMReplRecursive        ConditionID = "rlm_repl_recursive"
	ConditionRLMBraidSingle          ConditionID = "rlm_braid_single"
	ConditionRLMNoToolsStaged        ConditionID = "rlm_no_tools_staged"
	ConditionRLMNoModelToolsSingle   ConditionID = "rlm_no_model_tools_single"
	ConditionRLMNoModelToolsStaged   ConditionID = "rlm_no_model_tools_staged"
)

// ConditionKind groups conditions by execution family.
type ConditionKind string

const (
	ConditionKindBaseline ConditionKind = "baseline"
	ConditionKindRLM      ConditionKind = "rlm"
)

// AttemptStatus records the execution outcome before/around verifier import.
type AttemptStatus string

const (
	AttemptStatusOK         AttemptStatus = "ok"
	AttemptStatusError      AttemptStatus = "error"
	AttemptStatusCanceled   AttemptStatus = "canceled"
	AttemptStatusLeaked     AttemptStatus = "leaked"
	AttemptStatusUnverified AttemptStatus = "unverified"
)

const (
	VerifierStatusCorrect         = "correct"
	VerifierStatusIncorrect       = "incorrect"
	VerifierStatusFailed          = "failed"
	VerifierStatusWrongFormatting = "wrong_formatting"
)

// Question is the normalized metadata foxctl needs from the official LongCoT
// dataset shape and verifier bridge.
type Question struct {
	ID                    string                `json:"question_id"`
	Domain                string                `json:"domain,omitempty"`
	Split                 string                `json:"split,omitempty"` // legacy fixture compatibility
	Difficulty            string                `json:"difficulty,omitempty"`
	Template              string                `json:"template,omitempty"`
	PromptText            string                `json:"prompt,omitempty"`
	Answer                string                `json:"answer,omitempty"`
	Canary                string                `json:"canary,omitempty"`
	QuestionHash          string                `json:"question_hash,omitempty"`
	AllowOptionalSubcalls bool                  `json:"allow_optional_subcalls,omitempty"`
	RLMReview             bool                  `json:"rlm_review,omitempty"`
	RLMReviewRecursive    bool                  `json:"rlm_review_recursive,omitempty"`
	RequiredSubcallRules  []RequiredSubcallRule `json:"required_subcall_rules,omitempty"`
}

// RequiredSubcallRule declares a runtime-owned recursive-shape requirement.
type RequiredSubcallRule struct {
	Child            int `json:"child"`
	RequiredSubcalls int `json:"required_subcalls"`
}

// Condition describes one configured benchmark arm.
type Condition struct {
	ID                    ConditionID   `json:"condition_id"`
	Kind                  ConditionKind `json:"condition_kind"`
	PromptTemplateVersion string        `json:"prompt_template_version,omitempty"`
	RLMRouteProfile       string        `json:"rlm_route_profile,omitempty"`
	RLMPlanMode           string        `json:"rlm_plan_mode,omitempty"`
	RLMToolProfile        string        `json:"rlm_tool_profile,omitempty"`
	AllowedTools          []string      `json:"allowed_tools,omitempty"`
	MaxTokens             int           `json:"max_tokens,omitempty"`
	MaxDepth              int           `json:"max_depth,omitempty"`
	MaxIterations         int           `json:"max_iterations,omitempty"`
	MaxSubcalls           int           `json:"max_subcalls,omitempty"`
	TimeoutMS             int64         `json:"timeout_ms,omitempty"`
	Temperature           float64       `json:"temperature,omitempty"`
	Seed                  int64         `json:"seed,omitempty"`
}

// Usage records model accounting as reported or estimated for one attempt.
type Usage struct {
	InputTokens       int             `json:"input_tokens,omitempty"`
	OutputTokens      int             `json:"output_tokens,omitempty"`
	TotalTokens       int             `json:"total_tokens,omitempty"`
	ReasoningTokens   int             `json:"reasoning_tokens,omitempty"`
	CachedInputTokens int             `json:"cached_input_tokens,omitempty"`
	InputCostUSD      float64         `json:"input_cost_usd,omitempty"`
	OutputCostUSD     float64         `json:"output_cost_usd,omitempty"`
	TotalCostUSD      float64         `json:"total_cost_usd,omitempty"`
	RawProviderUsage  json.RawMessage `json:"raw_provider_usage,omitempty"`
}

// ToolEvent records one exposed tool call. The first slice only uses this shape
// for typed reporting; real runner integration can fill it later.
type ToolEvent struct {
	CallID                     string  `json:"call_id,omitempty"`
	Name                       string  `json:"name"`
	Status                     string  `json:"status,omitempty"`
	DurationMS                 int64   `json:"duration_ms,omitempty"`
	InputBytes                 int     `json:"input_bytes,omitempty"`
	OutputBytes                int     `json:"output_bytes,omitempty"`
	InputTokenEstimate         int     `json:"input_token_estimate,omitempty"`
	OutputTokenEstimate        int     `json:"output_token_estimate,omitempty"`
	RawOutputBytes             int     `json:"raw_output_bytes,omitempty"`
	ReducedOutputBytes         int     `json:"reduced_output_bytes,omitempty"`
	RawOutputTokenEstimate     int     `json:"raw_output_token_estimate,omitempty"`
	ReducedOutputTokenEstimate int     `json:"reduced_output_token_estimate,omitempty"`
	ReductionRatio             float64 `json:"reduction_ratio,omitempty"`
	CASDigest                  string  `json:"cas_digest,omitempty"`
	Error                      string  `json:"error,omitempty"`
}

// LeakageFlags make benchmark-contamination state explicit. These fields must
// be driven by exact config/tool data, not prompt keyword heuristics.
type LeakageFlags struct {
	FilesystemEnabled             bool     `json:"filesystem_enabled"`
	NetworkEnabled                bool     `json:"network_enabled"`
	RepoSearchEnabled             bool     `json:"repo_search_enabled"`
	MemoryEnabled                 bool     `json:"memory_enabled"`
	VaultEnabled                  bool     `json:"vault_enabled"`
	ArtifactEnabled               bool     `json:"artifact_enabled"`
	ShellEnabled                  bool     `json:"shell_enabled"`
	SubcallEnabled                bool     `json:"subcall_enabled"`
	SubcallAllowed                bool     `json:"subcall_allowed,omitempty"`
	VerifierAccessibleDuringSolve bool     `json:"verifier_accessible_during_solve"`
	DatasetAccessibleDuringSolve  bool     `json:"dataset_accessible_during_solve"`
	AnswerAccessibleDuringSolve   bool     `json:"answer_accessible_during_solve"`
	ForbiddenToolNames            []string `json:"forbidden_tool_names,omitempty"`
}

// Leaked reports whether any leakage flag invalidates primary benchmark use.
func (f LeakageFlags) Leaked() bool {
	return f.FilesystemEnabled || f.NetworkEnabled || f.RepoSearchEnabled || f.MemoryEnabled || f.VaultEnabled || f.ArtifactEnabled || f.ShellEnabled || (f.SubcallEnabled && !f.SubcallAllowed) || f.VerifierAccessibleDuringSolve || f.DatasetAccessibleDuringSolve || f.AnswerAccessibleDuringSolve || len(f.ForbiddenToolNames) > 0
}

// RLMAttemptMeta stores RLM-specific telemetry without forcing baseline arms to
// carry irrelevant fields.
type RLMAttemptMeta struct {
	RouteProfile         string         `json:"route_profile,omitempty"`
	PlanMode             string         `json:"plan_mode,omitempty"`
	ToolProfile          string         `json:"tool_profile,omitempty"`
	MaxDepth             int            `json:"max_depth,omitempty"`
	MaxIterations        int            `json:"max_iterations,omitempty"`
	MaxSubcalls          int            `json:"max_subcalls,omitempty"`
	Iterations           int            `json:"iterations,omitempty"`
	Subcalls             int            `json:"subcalls,omitempty"`
	EvidenceRefs         []string       `json:"evidence_refs,omitempty"`
	RetrievedPaths       []string       `json:"retrieved_paths,omitempty"`
	Metadata             map[string]any `json:"metadata,omitempty"`
	Phases               []RLMPhaseMeta `json:"phases,omitempty"`
	ParentInputTokens    int            `json:"parent_input_tokens,omitempty"`
	ParentOutputTokens   int            `json:"parent_output_tokens,omitempty"`
	ParentTotalTokens    int            `json:"parent_total_tokens,omitempty"`
	ParentIterationCount int            `json:"parent_iteration_count,omitempty"`
	ParentToolUsage      map[string]any `json:"parent_tool_usage,omitempty"`
}

type RLMPhaseMeta struct {
	Name               string   `json:"name"`
	AllowedTools       []string `json:"allowed_tools,omitempty"`
	RequiredTools      []string `json:"required_tools,omitempty"`
	ToolNames          []string `json:"tool_names,omitempty"`
	ParentInputTokens  int      `json:"parent_input_tokens,omitempty"`
	ParentOutputTokens int      `json:"parent_output_tokens,omitempty"`
	ParentTotalTokens  int      `json:"parent_total_tokens,omitempty"`
	AnswerExcerpt      string   `json:"answer_excerpt,omitempty"`
}

// Attempt is one condition's answer for one question.
type Attempt struct {
	RunID         string        `json:"run_id"`
	PairID        string        `json:"pair_id"`
	AttemptID     string        `json:"attempt_id"`
	QuestionID    string        `json:"question_id"`
	ConditionID   ConditionID   `json:"condition_id"`
	ConditionKind ConditionKind `json:"condition_kind"`
	Status        AttemptStatus `json:"status"`

	ResponseText  string `json:"response_text,omitempty"`
	ReasoningText string `json:"reasoning_text,omitempty"`

	Successful        bool   `json:"successful,omitempty"`
	Correct           bool   `json:"correct,omitempty"`
	VerifierStatus    string `json:"verifier_status,omitempty"`
	WrongFormatting   bool   `json:"wrong_formatting,omitempty"`
	VerificationError string `json:"verification_error,omitempty"`
	NormalizedAnswer  string `json:"normalized_answer,omitempty"`

	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	Runner   string `json:"runner,omitempty"`

	Usage        Usage           `json:"usage"`
	ToolEvents   []ToolEvent     `json:"tool_events,omitempty"`
	LeakageFlags LeakageFlags    `json:"leakage_flags"`
	RLM          *RLMAttemptMeta `json:"rlm,omitempty"`

	TrajectoryID string `json:"trajectory_id,omitempty"`
	SessionID    string `json:"session_id,omitempty"`
	DurationMS   int64  `json:"duration_ms,omitempty"`
	Error        string `json:"error,omitempty"`
}

func (a Attempt) IsTerminal() bool {
	switch a.Status {
	case AttemptStatusOK, AttemptStatusError, AttemptStatusCanceled, AttemptStatusLeaked, AttemptStatusUnverified:
		return true
	default:
		return false
	}
}

func (a Attempt) IsCorrect() bool {
	return a.Correct || a.VerifierStatus == VerifierStatusCorrect
}

// SavedArtifact points to files emitted by an eval run.
type SavedArtifact struct {
	Kind string `json:"kind"`
	Path string `json:"path"`
}

// RunResult is the benchmark-native output for one LongCoT foxctl run.
type RunResult struct {
	RunID       string          `json:"run_id"`
	GeneratedAt time.Time       `json:"generated_at"`
	Config      map[string]any  `json:"config,omitempty"`
	Questions   []Question      `json:"questions,omitempty"`
	Attempts    []Attempt       `json:"attempts,omitempty"`
	Summary     Summary         `json:"summary"`
	Artifacts   []SavedArtifact `json:"artifacts,omitempty"`
}

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/context/companion/hybrid_types.go
```go
package companion

// ConversationEvent represents a row in companion_events.
type ConversationEvent struct {
	ID               int64  `json:"id"`
	ConversationID   string `json:"conversation_id"`
	EventType        string `json:"event_type"`
	TurnID           string `json:"turn_id,omitempty"`
	ToolName         string `json:"tool_name,omitempty"`
	ToolRunID        string `json:"tool_run_id,omitempty"`
	ParentToolCallID int64  `json:"parent_tool_call_id,omitempty"`
	PayloadJSON      string `json:"payload_json,omitempty"`
	PayloadRef       string `json:"payload_ref,omitempty"`
	TokenCount       int    `json:"token_count,omitempty"`
	ContentHash      string `json:"content_hash,omitempty"`
	Content          string `json:"content,omitempty"`
	CreatedAt        string `json:"created_at"`
}

// HardStateEntry represents a row in companion_hard_state_entries.
type HardStateEntry struct {
	ID             int64   `json:"id"`
	ConversationID string  `json:"conversation_id"`
	EntryType      string  `json:"entry_type"`
	Key            string  `json:"key"`
	ValueJSON      string  `json:"value_json"`
	Status         string  `json:"status"`
	SourceEventID  int64   `json:"source_event_id"`
	Confidence     float64 `json:"confidence"`
	MetadataJSON   *string `json:"metadata_json,omitempty"`
	Supersedes     *int64  `json:"supersedes,omitempty"`
	CreatedAt      string  `json:"created_at"`
}

// SoftEpisode represents a row in companion_soft_episodes.
type SoftEpisode struct {
	ID             int64   `json:"id"`
	ConversationID string  `json:"conversation_id"`
	EpisodeType    string  `json:"episode_type"`
	StartEventID   int64   `json:"start_event_id"`
	EndEventID     int64   `json:"end_event_id"`
	Summary        string  `json:"summary"`
	NeedsSummary   int     `json:"needs_summary"`
	AssumptionIDs  string  `json:"assumption_ids"`
	TokenCount     int     `json:"token_count"`
	BoundaryHash   string  `json:"boundary_hash"`
	CreatedAt      string  `json:"created_at"`
	DeletedAt      *string `json:"deleted_at,omitempty"`
}

// EvidenceSnippet represents a row in companion_evidence_snippets.
type EvidenceSnippet struct {
	ID             int64   `json:"id"`
	ConversationID string  `json:"conversation_id"`
	SourceEventID  int64   `json:"source_event_id"`
	EventType      string  `json:"event_type"`
	FactText       string  `json:"fact_text"`
	ContentHash    string  `json:"content_hash"`
	Confidence     float64 `json:"confidence"`
	Bucket         string  `json:"bucket"`
	TTLDays        *int    `json:"ttl_days,omitempty"`
	CreatedAt      string  `json:"created_at"`
	ExpiresAt      *string `json:"expires_at,omitempty"`
	CanVerify      bool    `json:"can_verify,omitempty"`
}

// Assumption represents a row in companion_assumptions_ledger.
type Assumption struct {
	ID                 int64   `json:"id"`
	ConversationID     string  `json:"conversation_id"`
	Assumption         string  `json:"assumption"`
	Status             string  `json:"status"`
	Reason             *string `json:"reason,omitempty"`
	SourceEventID      int64   `json:"source_event_id"`
	Confidence         float64 `json:"confidence"`
	CreatedAt          string  `json:"created_at"`
	RetractedAt        *string `json:"retracted_at,omitempty"`
	RetractedByEventID *int64  `json:"retracted_by_event_id,omitempty"`
	RetractionReason   *string `json:"retraction_reason,omitempty"`
}

// MemoryModeState represents a row in companion_memory_mode_state.
type MemoryModeState struct {
	ConversationID     string `json:"conversation_id"`
	Mode               string `json:"mode"`
	SchemaVersion      int    `json:"schema_version"`
	LastProcessedEvent int64  `json:"last_processed_event"`
	LastSoftEvent      int64  `json:"last_soft_event"`
	LastEvidenceEvent  int64  `json:"last_evidence_event"`
	UpdatedAt          string `json:"updated_at"`
}

// OpenEpisodeState represents a row in companion_open_episode.
type OpenEpisodeState struct {
	ConversationID    string  `json:"conversation_id"`
	StartEventID      int64   `json:"start_event_id"`
	EpisodeType       string  `json:"episode_type"`
	EventCount        int     `json:"event_count"`
	TopicSig          *string `json:"topic_sig,omitempty"`
	PendingSealReason *string `json:"pending_seal_reason,omitempty"`
	UpdatedAt         string  `json:"updated_at"`
}

// OpenToolRun represents a row in companion_open_tool_runs.
type OpenToolRun struct {
	ConversationID    string `json:"conversation_id"`
	ToolRunID         string `json:"tool_run_id"`
	StartEventID      int64  `json:"start_event_id"`
	ParentCallEventID *int64 `json:"parent_call_event_id,omitempty"`
	CreatedAt         string `json:"created_at"`
}

// ExtractionStagingEntry represents a row in companion_extraction_staging.
type ExtractionStagingEntry struct {
	ID                int64   `json:"id"`
	ConversationID    string  `json:"conversation_id"`
	SourceEventID     int64   `json:"source_event_id"`
	ProposedEntryType string  `json:"proposed_entry_type"`
	RawText           string  `json:"raw_text"`
	Reason            string  `json:"reason"`
	AttemptCount      int     `json:"attempt_count"`
	CreatedAt         string  `json:"created_at"`
	ResolvedAt        *string `json:"resolved_at,omitempty"`
	DiscardedAt       *string `json:"discarded_at,omitempty"`
	DiscardReason     *string `json:"discard_reason,omitempty"`
}

// HardStateCache represents a row in companion_hard_state_cache.
type HardStateCache struct {
	ConversationID string `json:"conversation_id"`
	CompactJSON    string `json:"compact_json"`
	LastEntryID    int64  `json:"last_entry_id"`
	UpdatedAt      string `json:"updated_at"`
}

// Conversation event types stored in companion_events.
const (
	EventTypeUserMessage      = "user_message"
	EventTypeAssistantMessage = "assistant_message"
	EventTypeToolCall         = "tool_call"
	EventTypeToolResult       = "tool_result"
)

// Hard state entry types for companion_hard_state_entries.
const (
	EntryTypePreference       = "preference"
	EntryTypeDecision         = "decision"
	EntryTypeGlossary         = "glossary"
	EntryTypeOpenQuestion     = "open_question"
	EntryTypeGoal             = "goal"
	EntryTypePolicy           = "policy"
	EntryTypeIdentity         = "identity"
	EntryTypeRelationship     = "relationship_dynamic"
	EntryTypeTechnicalContext = "technical_context"
)

// Hard state entry lifecycle statuses.
const (
	EntryStatusActive     = "active"
	EntryStatusSuperseded = "superseded"
	EntryStatusRetracted  = "retracted"
)

// Assumption ledger statuses.
const (
	AssumptionStatusActive    = "active"
	AssumptionStatusRetracted = "retracted"
	AssumptionStatusPromoted  = "promoted"
)

// ExtractedEntry represents a deterministic extraction result from event content.
type ExtractedEntry struct {
	EntryType  string  `json:"entry_type"`
	Key        string  `json:"key,omitempty"`
	RawText    string  `json:"raw_text"`
	Value      string  `json:"value,omitempty"`
	Confidence float64 `json:"confidence"`
}

func strPtr(s string) *string { return &s }
func int64Ptr(i int64) *int64 { return &i }

// Conversation memory pipeline modes.
const (
	MemoryModeHybrid = "hybrid"
)

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/runtime/observability/wide_event.go
```go
package observability

import (
	"time"
)

// WideEvent represents a comprehensive observability event for a single operation.
// Unlike narrow events (e.g., SweGrepEvent), wide events capture the full context
// of an operation including identity, timing, outcome, and domain-specific data.
//
// Design principles (from loggingsucks.com):
//   - Log what happened to the request, not what your code does
//   - One event per operation with all potentially useful debugging info
//   - Include business context alongside technical details
type WideEvent struct {
	// Identity - who/what/when
	Ts       time.Time `json:"ts"`                  // Event timestamp (UTC)
	TraceID  string    `json:"trace_id"`            // ULID correlating all events in an operation
	SpanID   string    `json:"span_id"`             // ULID for this specific event
	ParentID string    `json:"parent_id,omitempty"` // Parent span for nested operations

	// Service metadata
	Service   string `json:"service"`   // "foxctl"
	Version   string `json:"version"`   // Build version
	Component string `json:"component"` // "cli", "web", "hook", "skill"

	// Operation context
	Operation string `json:"operation"`         // "skill.run", "hook.execute", etc.
	Command   string `json:"command,omitempty"` // Skill/hook/command name
	Subtype   string `json:"subtype,omitempty"` // Additional classification

	// Business context
	SessionID   string `json:"session_id,omitempty"`   // FOXCTL_SESSION_ID
	AgentID     string `json:"agent_id,omitempty"`     // FOXCTL_AGENT_ID
	WorkspaceID string `json:"workspace_id,omitempty"` // Logical workspace
	JobID       string `json:"job_id,omitempty"`       // Background job ID

	// Outcome
	Status     Status `json:"status"`      // ok, error, canceled
	DurationMS int64  `json:"duration_ms"` // Wall-clock duration

	// Error details (populated when Status == StatusError)
	ErrorType    string `json:"error_type,omitempty"`    // Error category (e.g., "validation", "timeout")
	ErrorCode    string `json:"error_code,omitempty"`    // Machine-readable error code
	ErrorMessage string `json:"error_message,omitempty"` // Human-readable message (redacted)
	Retriable    *bool  `json:"retriable,omitempty"`     // Whether the operation can be retried

	// Domain-specific data (extensible)
	// Use for skill-specific metrics, counts, booleans, hashes - never raw content
	Data map[string]any `json:"data,omitempty"`
}

// Status represents the outcome of an operation.
type Status string

const (
	// StatusOK indicates the operation completed successfully.
	StatusOK Status = "ok"
	// StatusError indicates the operation failed.
	StatusError Status = "error"
	// StatusCanceled indicates the operation was canceled (e.g., context canceled).
	StatusCanceled Status = "canceled"
)

// Component constants for consistent tagging.
const (
	ComponentCLI   = "cli"
	ComponentWeb   = "web"
	ComponentHook  = "hook"
	ComponentSkill = "skill"
	ComponentJob   = "job"
	ComponentAgent = "agent"
	// ComponentContextBuilder identifies layered context assembly and retrieval operations.
	ComponentContextBuilder = "contextbuilder"
)

// Operation constants for common operations.
const (
	OpSkillRun     = "skill.run"
	OpSkillCache   = "skill.cache"
	OpHookExecute  = "hook.execute"
	OpJobSubmit    = "job.submit"
	OpJobComplete  = "job.complete"
	OpHTTPRequest  = "http.request"
	OpSessionStart = "session.start"
	OpSessionEnd   = "session.end"

	// OpAgentSpawn is the operation name for agent spawn.
	OpAgentSpawn     = "agent.spawn"
	OpAgentWait      = "agent.wait"
	OpAgentComplete  = "agent.complete"
	OpAgentKill      = "agent.kill"
	OpAgentIteration = "agent.iteration"
	OpAgentTool      = "agent.tool"

	// OpContextSemanticArtifactSearch is emitted for optional semantic artifact retrieval.
	OpContextSemanticArtifactSearch = "context.semantic_artifact_search"
	// OpContextLayeredBundle is emitted when layered context assembly completes with stable refs.
	OpContextLayeredBundle = "context.layered_bundle"
)

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/intelligence/retrieval/v2/types.go
```go
package retrievalv2

import (
	"context"
	"errors"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex"
	"github.com/joshka0/foxctl/internal/intelligence/searchindex"
)

// SourceID identifies a recall source in the retrieval-v2 pipeline.
type SourceID string

const (
	// SourceExact performs exact symbol/title/path recall using the SQL search index.
	SourceExact SourceID = "exact"
	// SourceLexical performs BM25/keyword-style recall using the SQL search index.
	SourceLexical SourceID = "lexical"
	// SourceVector performs embedding recall using the SQL search index.
	SourceVector SourceID = "vector"
	// SourceRepoIndex performs structural recall using the repo graph index.
	SourceRepoIndex SourceID = "repo_index"
)

// QueryEmbeddingMode controls which embedder method is used for query embeddings.
type QueryEmbeddingMode string

const (
	// QueryEmbeddingModeAuto prefers query-optimized embedding when available.
	QueryEmbeddingModeAuto QueryEmbeddingMode = "auto"
	// QueryEmbeddingModeEmbed forces provider.Embed.
	QueryEmbeddingModeEmbed QueryEmbeddingMode = "embed"
	// QueryEmbeddingModeEmbedQuery uses provider.EmbedQuery when available.
	QueryEmbeddingModeEmbedQuery QueryEmbeddingMode = "embed_query"
)

// SearchIndex is the minimal read interface used by Engine.
type SearchIndex interface {
	ExactRecall(ctx context.Context, workspaceID, query string, opts searchindex.ExactRecallOptions) ([]searchindex.SearchHit, error)
	LexicalRecall(ctx context.Context, workspaceID, query string, opts searchindex.RecallOptions) ([]searchindex.SearchHit, error)
	VectorRecall(ctx context.Context, workspaceID string, embedding []float32, opts searchindex.VectorRecallOptions) ([]searchindex.SearchHit, error)
}

// Embedder is the minimal embedding contract used by retrieval-v2.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
	Model() string
	Dimensions() int
}

// QueryEmbedder extends Embedder with query-specific embedding support.
type QueryEmbedder interface {
	Embedder
	EmbedQuery(ctx context.Context, query string) ([]float32, error)
}

// SearchRequest configures one retrieval search.
type SearchRequest struct {
	WorkspaceID string
	Query       string

	// MaxResults caps total post-fusion output.
	MaxResults int

	Sources SearchSourcesConfig
	Fuse    FuseOptions
	Group   GroupOptions

	// QueryEmbeddingMode controls how query embeddings are produced.
	QueryEmbeddingMode QueryEmbeddingMode
}

// SearchSourcesConfig controls recall source behavior.
type SearchSourcesConfig struct {
	EnableExact     bool
	EnableLexical   bool
	EnableVector    bool
	EnableRepoIndex bool

	ExactLimit     int
	LexicalLimit   int
	VectorLimit    int
	RepoIndexLimit int

	LexicalMinScore float64
	VectorMinScore  float64
	RepoIndexMode   string

	VectorModel string
}

const (
	// FuseModeRRF uses reciprocal-rank fusion.
	FuseModeRRF FuseMode = "rrf"
	// FuseModeWeighted uses source-weighted score summation.
	FuseModeWeighted FuseMode = "weighted"
)

// FuseOptions controls cross-source fusion.
type FuseMode string

type FuseOptions struct {
	Mode            FuseMode
	TopK            int
	RRFK            float64
	SourceWeights   map[SourceID]float64
	MaxContributors int
}

// GroupOptions controls final file/group-level grouping.
type GroupOptions struct {
	Enabled    bool
	MaxGroups  int
	MaxMembers int
}

type SourceHit struct {
	Source   SourceID             `json:"source"`
	ID       string               `json:"id"`
	Document searchindex.Document `json:"-"`
	Score    float64              `json:"score"`
	Rank     int                  `json:"rank"`
}

type SourceContribution struct {
	Source SourceID `json:"source"`
	Score  float64  `json:"score"`
	Rank   int      `json:"rank"`
}

type FusedHit struct {
	ID            string               `json:"-"`
	Document      searchindex.Document `json:"document"`
	Score         float64              `json:"score"`
	Sources       []SourceID           `json:"sources"`
	SourceScores  map[SourceID]float64 `json:"source_scores"`
	Contributions []SourceContribution `json:"contributions"`
}

// AnchorHit preserves anchor-level detail when grouping file-oriented results.
type AnchorHit struct {
	Anchor     searchindex.Anchor `json:"anchor"`
	Score      float64            `json:"score"`
	Source     SourceID           `json:"source"`
	SymbolID   string             `json:"symbol_id,omitempty"`
	SymbolName string             `json:"symbol_name,omitempty"`
}

// Group is a file/group-level aggregate result.
type Group struct {
	Key      string      `json:"key"`
	Path     string      `json:"path"`
	Kind     string      `json:"kind"`
	Summary  string      `json:"summary,omitempty"`
	Score    float64     `json:"score"`
	Sources  []SourceID  `json:"sources"`
	Hits     []FusedHit  `json:"hits"`
	Anchors  []AnchorHit `json:"anchors,omitempty"`
	HitCount int         `json:"hit_count"`
}

// ParsedQuery captures normalized plan output for downstream use.
type ParsedQuery struct {
	Raw          string
	LexicalQuery string
	Plan         QueryPlan
}

// SourceStats tracks recall request/return/error counts for one source.
type SourceStats struct {
	Requested int
	Returned  int
	Err       error
}

// SearchStats tracks end-to-end retrieval execution stats.
type SearchStats struct {
	Sources      map[SourceID]SourceStats
	TotalRaw     int
	TotalFused   int
	TotalGrouped int
}

// SearchResponse is the engine output.
type SearchResponse struct {
	Query    string      `json:"query"`
	Plan     ParsedQuery `json:"plan"`
	Hits     []FusedHit  `json:"hits"`
	Groups   []Group     `json:"groups"`
	Stats    SearchStats `json:"stats"`
	Embedded bool        `json:"embedded"`
}

type RepoSearchRequest struct {
	Query string
	Limit int
}

type RepoDAGGrepRequest struct {
	Query      string
	Limit      int
	EdgeSets   []string
	Direction  string
	Depth      int
	Budget     int
	PerNodeCap int
	Render     string
}

// RepoQueryService is the minimal graph-query interface used by Engine.
type RepoQueryService interface {
	Search(ctx context.Context, req RepoSearchRequest) ([]repoindex.Node, error)
	DAGGrep(ctx context.Context, req RepoDAGGrepRequest) (repoindex.DAGGrepResult, error)
}

// DefaultSearchRequest returns a conservative starting configuration.
func DefaultSearchRequest(workspaceID, query string) SearchRequest {
	return SearchRequest{
		WorkspaceID:        workspaceID,
		Query:              query,
		MaxResults:         25,
		Sources:            DefaultSearchSourcesConfig(),
		Fuse:               DefaultFuseOptions(),
		Group:              DefaultGroupOptions(),
		QueryEmbeddingMode: QueryEmbeddingModeAuto,
	}
}

// DefaultSearchSourcesConfig returns source defaults for broad recall.
func DefaultSearchSourcesConfig() SearchSourcesConfig {
	return SearchSourcesConfig{
		EnableLexical:   true,
		EnableExact:     true,
		EnableVector:    true,
		EnableRepoIndex: false,
		ExactLimit:      10,
		LexicalLimit:    80,
		VectorLimit:     40,
		RepoIndexLimit:  20,
		RepoIndexMode:   "search",
	}
}

// DefaultFuseOptions returns a safe default cross-source fusion policy.
func DefaultFuseOptions() FuseOptions {
	return FuseOptions{
		Mode:            FuseModeRRF,
		TopK:            60,
		RRFK:            60,
		MaxContributors: 4,
		SourceWeights: map[SourceID]float64{
			SourceExact:     2.5,
			SourceLexical:   1.0,
			SourceVector:    0.85,
			SourceRepoIndex: 1.2,
		},
	}
}

// DefaultGroupOptions returns defaults that preserve source-level detail.
func DefaultGroupOptions() GroupOptions {
	return GroupOptions{
		Enabled:    true,
		MaxGroups:  25,
		MaxMembers: 5,
	}
}

var (
	errNoWorkspace = errors.New("search request missing workspace_id")
	errNoQuery     = errors.New("search request missing query")
	errNoIndex     = errors.New("search request has no index")
)

// Err... are exported for callers that want to branch on validation failures.
var (
	ErrNoWorkspace = errNoWorkspace
	ErrNoQuery     = errNoQuery
	ErrNoIndex     = errNoIndex
)

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/storage/tasks/store.go
(lines 99-191: Task model fields plus task/review status constants, including stale review state used for invalidation semantics.)
```go
type Task struct {
	ID              string
	WorkspaceID     string
	Title           string
	Description     string
	ScopePath       string
	ParentID        string
	Children        []string
	DependsOn       []string
	Status          string
	CreatedAt       time.Time
	CompletedAt     *time.Time
	AssignedActorID string
	AssignedAt      *time.Time
	OwnerActorID    string
	ClaimedAt       *time.Time
	HeartbeatAt     *time.Time
	BlockedReason   string
	BlockedAt       *time.Time
	Notes           string
	Gotchas         string

	// Review gate fields (review_gate.md)
	LastReviewStatus string     // "ok", "failed", "pending", or empty
	LastReviewAt     *time.Time // timestamp of last review
	LastReviewID     string     // ID of most recent review artifact

	// Plan integration fields (links task to ~/.claude/plans/)
	PlanFile    string // Path to the Claude Code plan file this task is linked to
	PlanSection string // Section path within the plan (e.g., "Phase 1 > Step 1.1")

	// Session tracking - links task to AI coding tool session
	SessionID string // AI coding tool session ID (Claude Code, OpenCode, Cursor, etc.)

	// Graph metrics - computed by tasksgraph analyzer
	PageRank float64 // PageRank score for priority ordering (higher = more important)

	// Epic linkage - groups tasks under a higher-level goal
	EpicID string // ID of the epic this task belongs to (if any)
	// Milestone linkage - binds a task to one explicit agile lane or milestone.
	MilestoneID string // ID of the milestone this task belongs to (if any)

	// Atomic processing fields (SimpleMem-style semantic lossless compression)
	// See: https://github.com/aiming-lab/SimpleMem
	AtomicDescription string   // Self-contained, disambiguated rewrite of description
	Entities          []string // Extracted entities (files, functions, people, concepts)
	Keywords          []string // BM25-optimized search terms
}

// ListOptions configures task list queries.
type ListOptions struct {
	SessionID string   // Filter by session ID (empty = all sessions)
	Statuses  []string // Filter by status (empty = all statuses)
	Limit     int      // Max results (0 = no limit)
}

// Task status constants per review_gate.md
const (
	// StatusPending is the default status for new tasks.
	StatusPending = "pending"
	// StatusInProgress indicates work is actively happening.
	StatusInProgress = "in_progress"
	// StatusReadyForReview indicates implementation is believed complete, awaiting review.
	StatusReadyForReview = "ready_for_review"
	// StatusCompleted marks a task as done (has passed review).
	StatusCompleted = "completed"
	// StatusBlocked indicates the task is blocked on dependencies or other issues.
	StatusBlocked = "blocked"
	// StatusCanceled indicates the task has been abandoned or superseded.
	StatusCanceled = "canceled"
)

const (
	taskBusyRetryWindow = 2 * time.Second
	taskBusyRetryStep   = 50 * time.Millisecond
)

// Review status constants
const (
	// ReviewStatusOK indicates the last review passed.
	ReviewStatusOK = "ok"
	// ReviewStatusFailed indicates the last review failed.
	ReviewStatusFailed = "failed"
	// ReviewStatusPending indicates a review is in progress.
	ReviewStatusPending = "pending"
	// ReviewStatusStale indicates a review was invalidated by new writes.
	ReviewStatusStale = "stale"
)

// Epic represents a higher-level goal that groups related tasks.
// Epics persist across sessions and provide continuity for multi-session work.
type Epic struct {
	ID          string

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/context/contextplane/taskhistory/taskhistory.go
(lines 64-166: Taskhistory Options and Pack/TranscriptHistory structures showing bounded context packet composition across task, sessions, git, anchors, and ACA notes.)
```go
type Options struct {
	WorkspacePath          string
	WorkspaceID            string
	TaskID                 string
	SessionLimit           int
	HandoffLimit           int
	FileLimit              int
	GitCommitLimit         int
	AnchorLimit            int
	NoteLimit              int
	DAGDepth               int
	DAGBudget              int
	PerNodeCap             int
	TranscriptHistoryScope TranscriptHistoryScope
}

type Pack struct {
	WorkspacePath string                       `json:"workspace_path"`
	WorkspaceID   string                       `json:"workspace_id"`
	GeneratedAt   time.Time                    `json:"generated_at"`
	Task          contextplane.TaskCandidate   `json:"task"`
	TaskPacket    contextplane.TaskPacket      `json:"task_packet"`
	Handoffs      []contextplane.HandoffRecord `json:"handoffs,omitempty"`
	FilesTouched  []string                     `json:"files_touched,omitempty"`
	ExternalRefs  []string                     `json:"external_refs,omitempty"`
	Sessions      []SessionSummary             `json:"sessions,omitempty"`
	GitHistory    []GitFileHistory             `json:"git_history,omitempty"`
	RepoAnchors   []repoquery.Anchor           `json:"repo_anchors,omitempty"`
	DAGAnchors    []repoquery.Anchor           `json:"dag_anchors,omitempty"`
	ACANotes      []contextplane.RetrievalHit  `json:"aca_notes,omitempty"`
	Transcript    *TranscriptHistory           `json:"transcript,omitempty"`
	Summary       string                       `json:"summary,omitempty"`
}

type TranscriptHistory struct {
	Overview            string                 `json:"overview,omitempty"`
	ObjectiveLabel      string                 `json:"objective_label,omitempty"`
	RequestedScope      TranscriptHistoryScope `json:"requested_scope,omitempty"`
	AppliedScope        TranscriptHistoryScope `json:"applied_scope,omitempty"`
	WorkspacePath       string                 `json:"workspace_path,omitempty"`
	FamilyPath          string                 `json:"family_path,omitempty"`
	AgentBrief          string                 `json:"agent_brief,omitempty"`
	HumanBrief          []string               `json:"human_brief,omitempty"`
	ContinueWith        []string               `json:"continue_with,omitempty"`
	WatchOutFor         []string               `json:"watch_out_for,omitempty"`
	Regressions         []string               `json:"regressions,omitempty"`
	RecurringMistakes   []string               `json:"recurring_mistakes,omitempty"`
	RecentLearnings     []string               `json:"recent_learnings,omitempty"`
	RecentSurprises     []string               `json:"recent_surprises,omitempty"`
	RetrievedBrief      string                 `json:"retrieved_brief,omitempty"`
	RetrievedHighlights []string               `json:"retrieved_highlights,omitempty"`
	EvidenceRefs        []string               `json:"evidence_refs,omitempty"`
	SourceNames         []string               `json:"source_names,omitempty"`
}

type SessionSummary struct {
	ID                 string    `json:"id"`
	Reason             string    `json:"reason,omitempty"`
	ProjectName        string    `json:"project_name,omitempty"`
	Summary            string    `json:"summary,omitempty"`
	Accomplished       []string  `json:"accomplished,omitempty"`
	Decisions          []string  `json:"decisions,omitempty"`
	Gotchas            []string  `json:"gotchas,omitempty"`
	KeyFiles           []string  `json:"key_files,omitempty"`
	TimelineSummaries  []string  `json:"timeline_summaries,omitempty"`
	TimelineTools      []string  `json:"timeline_tools,omitempty"`
	TimelineFiles      []string  `json:"timeline_files,omitempty"`
	RecentFilesTouched []string  `json:"recent_files_touched,omitempty"`
	StartedAt          time.Time `json:"started_at,omitempty"`
	EndedAt            time.Time `json:"ended_at,omitempty"`
}

type GitCommit struct {
	Hash    string `json:"hash"`
	Date    string `json:"date"`
	Subject string `json:"subject"`
}

type GitFileHistory struct {
	Path    string      `json:"path"`
	Commits []GitCommit `json:"commits,omitempty"`
}

type Collector struct {
	WorkspaceStore   *contextplane.WorkspaceStore
	TaskStore        taskstore.Store
	SessionStore     SessionSource
	MemoryStore      storage.MemoryStore
	RepoStore        *repoindex.Store
	VaultIndex       obsidianindex.Store
	SemanticProvider semantic.EmbeddingProvider
	TranscriptWorker *TranscriptSummaryWorker
	TranscriptRun    TranscriptSummaryRunFunc
	GitRunner        GitRunner
}

func (c Collector) Collect(ctx context.Context, opts Options) (Pack, error) {
	if c.WorkspaceStore == nil {
		return Pack{}, fmt.Errorf("workspace store is required")
	}
	if c.TaskStore == nil {
		return Pack{}, fmt.Errorf("task store is required")
	}

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/context/contextplane/store.go
(lines 24-128: Workspace layout paths and initialization, including RetrievalPolicyPath/PromotionPolicyPath/TaskTypesPolicyPath and default policy file seeding in EnsureLayout.)
```go
// NewWorkspaceStore returns a file-backed control-plane store for a workspace.
func NewWorkspaceStore(workspacePath string) *WorkspaceStore {
	clean := ws.Normalize(workspacePath)
	rootDir := filepath.Join(clean, ".foxctl")
	runtimeDir := filepath.Join(rootDir, "runtime")
	queueDir := filepath.Join(runtimeDir, "queue")
	handoffsDir := filepath.Join(runtimeDir, "handoffs")
	sessionsDir := filepath.Join(runtimeDir, "sessions")
	policyDir := filepath.Join(rootDir, "policy")
	exportsDir := filepath.Join(rootDir, "exports")
	templatesDir := filepath.Join(rootDir, "templates", "obsidian-vault")

	return &WorkspaceStore{
		layout: Layout{
			WorkspacePath:          clean,
			RootDir:                rootDir,
			RuntimeDir:             runtimeDir,
			QueueDir:               queueDir,
			HandoffsDir:            handoffsDir,
			SessionsDir:            sessionsDir,
			PolicyDir:              policyDir,
			ExportsDir:             exportsDir,
			TemplatesDir:           templatesDir,
			TopOfMindPath:          filepath.Join(runtimeDir, "top_of_mind.json"),
			CurrentRunPath:         filepath.Join(runtimeDir, "current_run.json"),
			TasksQueuePath:         filepath.Join(queueDir, "tasks.ndjson"),
			BlockedQueuePath:       filepath.Join(queueDir, "blocked.ndjson"),
			ObservationsPath:       filepath.Join(runtimeDir, "observations.ndjson"),
			TensionsPath:           filepath.Join(runtimeDir, "tensions.ndjson"),
			PromotionJobsPath:      filepath.Join(runtimeDir, "promotion_jobs.ndjson"),
			MaintenanceQueuePath:   filepath.Join(runtimeDir, "maintenance_queue.ndjson"),
			EventsPath:             filepath.Join(runtimeDir, "events.ndjson"),
			RetrievalPolicyPath:    filepath.Join(policyDir, "retrieval.yaml"),
			PromotionPolicyPath:    filepath.Join(policyDir, "promotion.yaml"),
			TaskTypesPolicyPath:    filepath.Join(policyDir, "task_types.yaml"),
			OrientationExportPath:  filepath.Join(exportsDir, "latest-orientation.md"),
			ObsidianHomeIndexPath:  filepath.Join(templatesDir, "00-home", "index.md"),
			ObsidianFrontierPath:   filepath.Join(templatesDir, "00-home", "active-frontier.md"),
			ObsidianAtlasPath:      filepath.Join(templatesDir, "atlas", "projects.md"),
			ObsidianProjectMOCPath: filepath.Join(templatesDir, "notes", "moc", "project-index.md"),
			ObsidianInboxDraftsDir: filepath.Join(templatesDir, "inbox", "drafted-from-foxctl"),
		},
	}
}

// Layout returns the resolved workspace-local scaffold paths.
func (s *WorkspaceStore) Layout() Layout {
	return s.layout
}

// EnsureLayout creates the ACA workspace scaffold and seeds default policy/template files.
func (s *WorkspaceStore) EnsureLayout() (Layout, error) {
	for _, dir := range []string{
		s.layout.RootDir,
		s.layout.RuntimeDir,
		s.layout.QueueDir,
		s.layout.HandoffsDir,
		s.layout.SessionsDir,
		s.layout.PolicyDir,
		s.layout.ExportsDir,
		filepath.Dir(s.layout.ObsidianHomeIndexPath),
		filepath.Dir(s.layout.ObsidianAtlasPath),
		filepath.Dir(s.layout.ObsidianProjectMOCPath),
		s.layout.ObsidianInboxDraftsDir,
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return Layout{}, fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}

	if err := ensureFileWithDefault(s.layout.CurrentRunPath, "{\n  \"status\": \"idle\"\n}\n"); err != nil {
		return Layout{}, err
	}
	for _, path := range []string{
		s.layout.TasksQueuePath,
		s.layout.BlockedQueuePath,
		s.layout.ObservationsPath,
		s.layout.TensionsPath,
		s.layout.PromotionJobsPath,
		s.layout.MaintenanceQueuePath,
		s.layout.EventsPath,
	} {
		if err := ensureFileWithDefault(path, ""); err != nil {
			return Layout{}, err
		}
	}
	if err := ensureFileWithDefault(s.layout.RetrievalPolicyPath, defaultRetrievalPolicy); err != nil {
		return Layout{}, err
	}
	if err := ensureFileWithDefault(s.layout.PromotionPolicyPath, defaultPromotionPolicy); err != nil {
		return Layout{}, err
	}
	if err := ensureFileWithDefault(s.layout.TaskTypesPolicyPath, defaultTaskTypesPolicy); err != nil {
		return Layout{}, err
	}
	if err := ensureFileWithDefault(s.layout.ObsidianHomeIndexPath, defaultObsidianHomeIndex); err != nil {
		return Layout{}, err
	}
	if err := ensureFileWithDefault(s.layout.ObsidianFrontierPath, defaultObsidianFrontier); err != nil {
		return Layout{}, err
	}
	if err := ensureFileWithDefault(s.layout.ObsidianAtlasPath, defaultObsidianAtlasProjects); err != nil {
		return Layout{}, err
	}
	if err := ensureFileWithDefault(s.layout.ObsidianProjectMOCPath, defaultObsidianProjectMOC); err != nil {

```

(lines 1082-1165: Default retrieval/promotion/task-type policy documents that encode promotion and maintenance trigger rules.)
```go

const defaultRetrievalPolicy = `retrieval_order:
  - top_of_mind
  - active_task_packet
  - latest_handoff
  - procedural_memory
  - vault_retrieval
  - external_sources

budgets:
  top_of_mind_tokens: 1000
  task_packet_tokens: 500
  handoff_tokens: 400
  evidence_snippets_min: 3
  evidence_snippets_max: 7

ranking_weights:
  project_match: 4
  trust_level: 3
  note_type_weight: 3
  link_proximity: 2
  lexical_match: 2
  co_change: 3
  semantic_match: 2
  recency: 1
  reuse_frequency: 1

aca:
  package_note_fallback: false
  co_change_prior: false
  co_change_commit_limit: 40
  co_change_max_files_per_commit: 20
  co_change_half_life_days: 90
  continuity_bundles: true
`

const defaultPromotionPolicy = `trust_levels:
  - raw
  - reviewed
  - canonical

allowed_write_paths:
  - runtime_state
  - session_logs
  - inbox_drafts
  - exported_ops_snapshots

append_only_sections:
  - index_notes
  - review_sections
  - recent_findings

protected_sections:
  - adr_conclusions
  - methodology_rules
  - reviewed_incident_outcomes
`

const defaultTaskTypesPolicy = `task_types:
  - design
  - implement
  - verify
  - curate
  - promote
  - maintenance

maintenance_triggers:
  repeated_tension: true
  stale_note_review: true
  broken_link_review: true
  contradiction_review: true
`

const defaultObsidianHomeIndex = `---
title: AgentCTL Knowledge Home
type: map
status: draft
trust: reviewed
---

# AgentCTL Knowledge Home

- [[active-frontier]]
- [[projects]]

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/context/contextplane/proposals.go
(lines 11-48: RecordMemoryProposal default lifecycle state initialization: status, evaluation_status, apply_status, timestamps, and dedupe persistence.)
```go

func (s *WorkspaceStore) RecordMemoryProposal(ctx context.Context, proposal MemoryProposal) (MemoryProposal, error) {
	db, closeFn, err := s.openMutableDB(ctx)
	if err != nil {
		return MemoryProposal{}, err
	}
	defer func() { _ = closeFn() }()
	if strings.TrimSpace(proposal.Status) == "" {
		proposal.Status = "open"
	}
	if strings.TrimSpace(proposal.EvaluationStatus) == "" {
		proposal.EvaluationStatus = "not_evaluated"
	}
	if strings.TrimSpace(proposal.ApplyStatus) == "" {
		proposal.ApplyStatus = "pending"
	}
	now := timeutil.NowUTC()
	if proposal.CreatedAt.IsZero() {
		proposal.CreatedAt = now
	}
	if proposal.UpdatedAt.IsZero() {
		proposal.UpdatedAt = proposal.CreatedAt
	}
	if proposal.Count <= 0 {
		proposal.Count = 1
	}
	if err := upsertMemoryProposalRow(ctx, db, proposal); err != nil {
		return MemoryProposal{}, fmt.Errorf("record memory proposal: %w", err)
	}
	stored, err := findMemoryProposalRowByKey(ctx, db, effectiveMemoryProposalKey(proposal))
	if err != nil {
		return MemoryProposal{}, err
	}
	if stored == nil {
		return MemoryProposal{}, fmt.Errorf("proposal persisted but could not be reloaded")
	}
	return *stored, nil
}

```

(lines 335-420: Conversion from retrieval/evidence inspections into MemoryProposal records, including proposed change payload and source refs.)
```go
func (s *WorkspaceStore) RecordRetrievalProposal(ctx context.Context, inspection RetrievalInspection) (MemoryProposal, error) {
	return s.RecordMemoryProposal(ctx, memoryProposalFromRetrievalInspection(inspection))
}

func (s *WorkspaceStore) RecordEvidenceImportProposal(ctx context.Context, run EvidenceImportRun, extraction EvidenceExtraction, target EvidenceTargetSuggestion) (MemoryProposal, error) {
	return s.RecordMemoryProposal(ctx, memoryProposalFromEvidenceImport(run, extraction, target))
}

func memoryProposalFromRetrievalInspection(inspection RetrievalInspection) MemoryProposal {
	kind, blastRadius, reviewRequired := proposalShapeFromRetrievalAction(inspection.Proposal.Kind)
	change := map[string]any{
		"proposal_kind": inspection.Proposal.Kind,
		"summary":       inspection.Proposal.Summary,
	}
	if inspection.Proposal.PolicyPath != "" {
		change["policy_path"] = inspection.Proposal.PolicyPath
	}
	if inspection.Proposal.PolicyPatch != "" {
		change["policy_patch"] = inspection.Proposal.PolicyPatch
		change["package_note_fallback"] = strings.Contains(inspection.Proposal.PolicyPatch, "package_note_fallback: true")
	}
	if inspection.Proposal.TargetNotePath != "" {
		change["target_note_path"] = inspection.Proposal.TargetNotePath
	}
	if inspection.Proposal.NoteType != "" {
		change["note_type"] = inspection.Proposal.NoteType
	}
	if inspection.Proposal.NoteTitle != "" {
		change["note_title"] = inspection.Proposal.NoteTitle
	}
	if inspection.Proposal.MetadataPatch != "" {
		change["metadata_patch"] = inspection.Proposal.MetadataPatch
	}
	if inspection.Proposal.SupportingNote != "" {
		change["supporting_note"] = inspection.Proposal.SupportingNote
	}
	if len(inspection.Proposal.ExpectedRepoPaths) > 0 {
		change["expected_repo_paths"] = append([]string(nil), inspection.Proposal.ExpectedRepoPaths...)
	}
	if inspection.Query != "" {
		change["query"] = inspection.Query
	}
	if len(inspection.ExpectedPaths) > 0 {
		change["expected_paths"] = append([]string(nil), inspection.ExpectedPaths...)
	}
	if len(inspection.RetrievedPaths) > 0 {
		change["retrieved_paths"] = append([]string(nil), inspection.RetrievedPaths...)
	}
	return MemoryProposal{
		Kind:             kind,
		Classification:   inspection.Classification,
		Status:           "open",
		ReviewRequired:   reviewRequired,
		Confidence:       inspection.Observation.Confidence,
		BlastRadius:      blastRadius,
		Summary:          firstNonEmpty(strings.TrimSpace(inspection.Proposal.Summary), strings.TrimSpace(inspection.Observation.Statement), fmt.Sprintf("ACA proposal for %s", inspection.Classification)),
		SourceRefs:       memoryProposalSourceRefs(inspection),
		ProposedChange:   change,
		EvaluationStatus: "not_evaluated",
		ApplyStatus:      "pending",
		Count:            1,
		CreatedAt:        inspection.GeneratedAt,
		UpdatedAt:        inspection.GeneratedAt,
	}
}

func proposalShapeFromRetrievalAction(kind string) (proposalKind, blastRadius string, reviewRequired bool) {
	switch strings.TrimSpace(kind) {
	case "policy_patch":
		return "retrieval_policy_patch", "low", false
	case "metadata_patch":
		return "bridge_metadata_patch", "medium", true
	case "draft_package_note":
		return "missing_note_draft", "medium", true
	default:
		return "manual_review", "high", true
	}
}

func memoryProposalFromEvidenceImport(run EvidenceImportRun, extraction EvidenceExtraction, target EvidenceTargetSuggestion) MemoryProposal {
	proposalKind := "external_evidence_import"
	blastRadius := "medium"
	reviewRequired := true
	reviewAction := "review_and_merge"
	topicKey := normalizeEvidenceTopic(firstNonEmpty(strings.TrimSpace(run.Title), strings.TrimSpace(run.SourceRef), strings.TrimSpace(extraction.Summary)))
	if looksMethodologyEvidence(run, extraction) {

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
<taskname="Primitive Gap Map"/>
<task>Map the 15 proposed unified-context primitives to existing code primitives. For each primitive, identify closest analog(s), classify exists/partial/missing, and note field-level gaps. Focus on types, structs, interfaces, and status/lifecycle enums; avoid call-site analysis.</task>

<architecture>
- ACA contextplane defines task/context/proposal/policy projections.
- Companion hybrid memory defines append-only events plus hard-state/evidence/assumption lifecycles.
- Storage interfaces define shared memory/session/task contracts.
- Retrieval v2 and eval packages define retrieval result and episode-style telemetry shapes.
- Trajectory/observability define provenance event envelopes.
</architecture>

<selected_context>
internal/context/contextplane/types.go: TopOfMind, Handoff, Observation, Tension, MaintenanceTask, MemoryProposal, RetrievalResult, TaskPacket, ProposalWorkPacket.
internal/context/contextplane/retrieval_inspect.go: RetrievalInspection, RetrievalCorrectionAction, RetrievalInspectionBatchSummary.
internal/context/contextplane/store.go (slices): Layout policy paths and default retrieval/promotion/task-type policies.
internal/context/contextplane/proposals.go (slices): MemoryProposal default lifecycle values and retrieval/evidence -> proposal conversion.
internal/context/contextplane/taskhistory/taskhistory.go (slice): Options + Pack + TranscriptHistory bounded context packet shape.
internal/context/companion/hybrid_types.go: ConversationEvent, HardStateEntry, SoftEpisode, EvidenceSnippet, Assumption, ExtractionStagingEntry, lifecycle constants.
internal/context/companion/hybrid_pipeline.go: append-only InsertEvent invariants and event-type constraints.
internal/context/companion/hybrid_evidence.go: AppendHardStateEntry, SupersedeHardStateEntry, RetractHardStateEntry.
internal/storage/interfaces.go: NamedEntry, ScoredEntry, MemorySaveOptions, MemoryListFilter, MemoryStore, Session.
internal/storage/tasks/store.go (slice): Task fields + task/review status constants including stale review state.
internal/storage/trajectory/types.go: Trajectory, EventMeta, Event, Outcome, filter structs.
internal/intelligence/retrieval/v2/types.go: SearchRequest, SourceHit, FusedHit, Group, SearchResponse, SourceStats.
internal/intelligence/retrieval/v2/query_plan.go: QueryPlan/Identifier/Phrase/PathHint.
internal/runtime/observability/wide_event.go: WideEvent envelope + status/component/operation constants.
internal/tooling/evals/retrievaleval/eval.go: QueryResult/ModeResult/Summary/RunResult.
internal/tooling/evals/retrievaleval/policy.go: Policy, ModeThreshold, Alert.
internal/tooling/evals/longcoteval/types.go: Attempt, ToolEvent, LeakageFlags, RLMAttemptMeta, RunResult.
internal/rlm/runtime/node.go: EvidenceRef and node-level evidence/artifact refs.
</selected_context>

<relationships>
- `contextplane.MemoryProposal` is produced from retrieval/evidence inspections and constrained by policy docs.
- `companion.ConversationEvent` feeds derived `HardStateEntry`/`EvidenceSnippet`/`Assumption` records with supersede/retract transitions.
- `taskhistory.Pack` composes task, sessions, git, anchors, and ACA notes into a bounded packet.
- `retrievalv2.SearchResponse` plus eval `RunResult`/`Attempt` provide retrieval-episode and feedback-like analogs.
- `trajectory.Event` and `observability.WideEvent` are parallel provenance event models.
- `rlm.runtime.EvidenceRef` is the closest existing universal reference type, but narrow vs proposed multi-scheme refs.
</relationships>

<ambiguities>
- No single canonical schema unifies contextplane, companion, retrievalv2, and trajectory models.
- Some policy/lifecycle behavior is represented as status strings and default YAML rather than typed policy engines.
- `taskhistory.Pack` is packet-like but task-history oriented, not a declared universal context contract.
</ambiguities>
</user_instructions>
