<file_map>
/Users/joshka/repos/worktrees/feat-longcot-eval-dry-run
├── cmd
│   ├── foxctl
│   │   └── cmd
│   │       ├── godot_plugin
│   │       ├── memorycmd
│   │       ├── sessionscmd
│   │       ├── gateway.go * +
│   │       └── web.go * +
│   ├── foxctl_web
│   │   └── main.go * +
│   ├── atcp-live
│   ├── atcp-room-smoke
│   ├── atcp-smoke
│   ├── atcpctl
│   ├── atcpd
│   ├── companion_mailbox_test
│   ├── companion_memory_test
│   ├── companion_test
│   ├── foxctl-mail
│   ├── foxctl_tui
│   └── synctest
├── deploy
│   ├── kubernetes
│   │   ├── base
│   │   │   ├── deployment.yaml *
│   │   │   └── service.yaml *
│   │   └── overlays
│   │       ├── public-gui
│   │       │   ├── foxctl-deployment-patch.yaml *
│   │       │   ├── gateway-configmap.yaml *
│   │       │   ├── gateway-deployment.yaml *
│   │       │   ├── gateway-network-policy.yaml *
│   │       │   ├── gateway-service.yaml *
│   │       │   └── kustomization.yaml *
│   │       ├── local
│   │       ├── postgres
│   │       └── public-gui-local
│   └── libsql
├── internal
│   ├── interfaces
│   │   ├── chatadapter
│   │   │   ├── teams
│   │   │   │   ├── auth.go * +
│   │   │   │   ├── botclient.go * +
│   │   │   │   ├── driver.go * +
│   │   │   │   ├── interactions.go * +
│   │   │   │   └── jwt.go * +
│   │   │   ├── telegram
│   │   │   │   └── driver.go * +
│   │   │   ├── discord
│   │   │   └── bridge.go * +
│   │   ├── gateway
│   │   │   ├── sshterm
│   │   │   ├── static
│   │   │   ├── webterm
│   │   │   └── server.go * +
│   │   ├── web
│   │   │   ├── api
│   │   │   │   └── agents.go * +
│   │   │   ├── consolews
│   │   │   ├── sse
│   │   │   └── server.go * +
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
│   │   └── tui
│   │       ├── components
│   │       │   ├── cmd
│   │       │   │   ├── detailpane
│   │       │   │   ├── drawer
│   │       │   │   ├── streamviewer
│   │       │   │   └── tabs
│   │       │   └── testdata
│   │       │       ├── snapshots
│   │       │       └── tuistory
│   │       ├── entities
│   │       ├── runtime
│   │       ├── testfixture
│   │       └── theme
│   ├── runtime
│   │   ├── daemon
│   │   │   └── service.go * +
│   │   ├── actor
│   │   │   ├── handlers
│   │   │   └── memory
│   │   ├── agentpolicy
│   │   ├── agentprompt
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
│   ├── rlm
│   │   ├── env
│   │   ├── optdata
│   │   ├── repl
│   │   └── runtime
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

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/interfaces/chatadapter/teams/types.go
Imports:
  - import "encoding/json"
---
Classes:
  - Activity
    Properties:
      - Type
      - ID
      - ChannelID
      - ServiceURL
      - Text
      - ReplyToID
      - From
      - Recipient
      - Conversation
      - ChannelData
      - Entities
      - Attachments
      - Value
  - Attachment
    Properties:
      - ContentType
      - Content
  - ChannelAccount
    Properties:
      - ID
      - Name
  - ConversationAccount
    Properties:
      - ID
      - TenantID
      - IsGroup
  - Entity
    Properties:
      - Type
      - Text
      - Mentioned
  - ResourceResponse
    Properties:
      - ID
---


File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/interfaces/chatadapter/adapter.go
Imports:
  - import (
	"context"
	"sync/atomic"

	"github.com/joshka0/foxctl/internal/domain/identity"
)
---
Classes:
  - MessageEvent
    Methods:
      - L61: func (e MessageEvent) Respond(content string, embeds []Embed) (MessageRef, error)
      - L73: func (e MessageEvent) Edit(ref MessageRef, content string, embeds []Embed) error
      - L81: func (e MessageEvent) Responded() bool
    Properties:
      - Content
      - Principal
      - User
      - ChannelID
      - GuildID
      - MessageID
      - respond
      - edit
      - responded
  - CommandDef
    Properties:
      - Name
      - Description
      - Options
  - CommandOption
    Properties:
      - Name
      - Description
      - Type
      - Required
      - Choices
  - Choice
    Properties:
      - Name
      - Value
  - CommandEvent
    Methods:
      - L148: func (e CommandEvent) Respond(content string, embeds []Embed) error
      - L160: func (e CommandEvent) Responded() bool
    Properties:
      - Command
      - Options
      - Principal
      - User
      - ChannelID
      - GuildID
      - respond
      - responded
  - InteractionEvent
    Methods:
      - L183: func (e InteractionEvent) Respond(content string, embeds []Embed) error
      - L195: func (e InteractionEvent) Update(content string, embeds []Embed, components []Component) error
      - L203: func (e InteractionEvent) Responded() bool
    Properties:
      - Type
      - CustomID
      - Principal
      - User
      - ChannelID
      - GuildID
      - MessageRef
      - respond
      - update
      - responded
  - MessageRef
    Properties:
      - ChannelID
      - MessageID
  - Component
    Properties:
      - Type
      - CustomID
      - Label
      - Style
      - Disabled
      - Children
  - UserRef
    Properties:
      - ID
      - Username
  - Embed
    Properties:
      - Title
      - Description
      - Color
      - Fields
      - Footer
  - Field
    Properties:
      - Name
      - Value
      - Inline

Functions:
  - L89: func NewMessageEvent(content string, user UserRef, channelID, guildID, messageID string, respond func(string, []Embed) (MessageRef, error), edit func(MessageRef, string, []Embed) error) MessageEvent
  - L211: func NewInteractionEvent(typ, customID string, user UserRef, channelID, guildID string, msgRef MessageRef, respond func(string, []Embed) error, update func(string, []Embed, []Component) error) InteractionEvent
  - L283: func NewCommandEvent(command string, options map[string]any, user UserRef, channelID, guildID string, respond func(string, []Embed) error) CommandEvent

Global vars:
  - OptionTypeString
  - OptionTypeInt
  - OptionTypeBool
  - ComponentActionRow
  - ComponentButton
  - ButtonPrimary
  - ButtonSecondary
  - _
---


File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/platform/lsp/jsonrpc/client.go
Imports:
  - import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)
---
Classes:
  - Client
    Methods:
      - L70: func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error)
      - L100: func (c *Client) CallInto(ctx context.Context, method string, params any, result any) error
      - L116: func (c *Client) Notify(method string, params any) error
      - L130: func (c *Client) writeMessage(msg any) error
      - L149: func (c *Client) readResponse(ctx context.Context, expectedID int64) (*Response, error)
      - L201: func (c *Client) readHeaders() (int, error)
      - L230: func (c *Client) ReadMessage(ctx context.Context) (*Response, error)
      - L265: func (c *Client) Close() error
    Properties:
      - stdin
      - stdout
      - nextID
      - mu
  - Request
    Properties:
      - JSONRPC
      - ID
      - Method
      - Params
  - Notification
    Properties:
      - JSONRPC
      - Method
      - Params
  - Response
    Properties:
      - JSONRPC
      - ID
      - Result
      - Error
  - Error
    Methods:
      - L64: func (e *Error) Error() string
    Properties:
      - Code
      - Message
      - Data

Functions:
  - L27: func NewClient(stdin io.WriteCloser, stdout io.Reader) *Client
---


File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/atcp/broker/broker.go
Imports:
  - import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/joshka0/foxctl/internal/atcp/adapter/generictty"
	"github.com/joshka0/foxctl/internal/atcp/adapter/profiles"
	"github.com/joshka0/foxctl/internal/atcp/broker/lease"
	"github.com/joshka0/foxctl/internal/atcp/broker/room"
	"github.com/joshka0/foxctl/internal/atcp/broker/router"
	"github.com/joshka0/foxctl/internal/atcp/broker/session"
	"github.com/joshka0/foxctl/internal/atcp/broker/storage"
	"github.com/joshka0/foxctl/internal/atcp/intents"
)
---
Classes:
  - Options
    Properties:
      - Sessions
      - AdapterFactory
      - AllowUnleasedInputForTests
      - Storage
      - HydrateContext
  - Broker
    Methods:
      - L191: func (b *Broker) hydrate(ctx context.Context) error
      - L225: func (b *Broker) Sessions() *session.Manager
      - L228: func (b *Broker) Leases() *lease.Manager
      - L233: func (b *Broker) Rooms() *room.Manager
      - L238: func (b *Broker) Router() *router.Router
      - L248: func (b *Broker) CreateRoom(ctx context.Context, req room.CreateRoomRequest) (room.Room, error)
      - L262: func (b *Broker) JoinRoom(ctx context.Context, req room.JoinRequest) (room.Member, error)
      - L278: func (b *Broker) LeaveRoom(ctx context.Context, roomID, agentID string) (room.Member, error)
      - L293: func (b *Broker) SendMessage(ctx context.Context, msg router.Message) (router.Result, error)
      - L314: func (b *Broker) ListMessages(roomID string, limit int) ([]storage.MessageRecord, error)
      - L331: func (b *Broker) appendMessageRecord(rec storage.MessageRecord)
      - L346: func (b *Broker) Stop()
      - L364: func (b *Broker) CreateSession(spec session.Spec, logOpts session.OutputLogOptions) (session.Snapshot, error)
      - L424: func (b *Broker) detachSessionFromRooms(sessionID string)
      - L446: func (b *Broker) DeleteSession(id string) error
      - L462: func (b *Broker) GetSession(id string) (session.Snapshot, error)
      - L471: func (b *Broker) ListSessions() []session.Snapshot
      - L478: func (b *Broker) SessionReady(sessionID string) (bool, string, error)
      - L497: func (b *Broker) Adapter(sessionID string) (Adapter, error)
      - L508: func (b *Broker) SubmitText(sessionID string, intent intents.TerminalText) (int, error)
      - L515: func (b *Broker) SubmitKey(sessionID string, intent intents.TerminalKey) (int, error)
      - L522: func (b *Broker) Submit(sessionID string, intent intents.TerminalSubmit) (int, error)
      - L560: func (b *Broker) Paste(sessionID string, intent intents.TerminalPaste) (int, error)
      - L567: func (b *Broker) WriteBytes(sessionID string, intent intents.TerminalWriteBytes) (int, error)
      - L575: func (b *Broker) AcquireLease(req lease.AcquireRequest) (*lease.Lease, error)
      - L583: func (b *Broker) ReleaseLease(id string) error
      - L590: func (b *Broker) runTerminal(sessionID, leaseID string, compile func(Adapter) ([]byte, error)) (int, error)
      - L616: func (b *Broker) checkLease(sessionID, leaseID string) error
    Properties:
      - sessions
      - leases
      - roomMgr
      - msgRouter
      - store
      - messagesMu
      - messages
      - adaptersMu
      - adapters
      - factory
      - allowUnleased
      - detachMu

Functions:
  - L47: func DefaultAdapterFactory(_ string) Adapter
  - L49: func DefaultReadinessProfile(profile string) session.ReadinessProfile
  - L53: func mergeReadinessProfile(base, override session.ReadinessProfile) session.ReadinessProfile
  - L137: func New(opts Options) (*Broker, error)
  - L182: func MustNew(opts Options) *Broker
  - L337: func cloneMessageRecord(rec storage.MessageRecord) storage.MessageRecord
---


File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/runtime/actor/base_actor.go
Imports:
  - import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)
---
Classes:
  - BaseActor
    Methods:
      - L110: func (a *BaseActor) ID() string
      - L115: func (a *BaseActor) Namespace() string
      - L120: func (a *BaseActor) Config() Config
      - L125: func (a *BaseActor) State() State
      - L130: func (a *BaseActor) SetState(s State)
      - L135: func (a *BaseActor) Start(ctx context.Context) error
      - L150: func (a *BaseActor) Stop(ctx context.Context) error
      - L171: func (a *BaseActor) RegisterHandler(subject string, handler MessageHandler)
      - L178: func (a *BaseActor) UnregisterHandler(subject string)
      - L186: func (a *BaseActor) OnMailReceived(ctx context.Context, msg *Message) error
      - L212: func (a *BaseActor) OnTimeout(ctx context.Context, event TimerEvent) error
      - L220: func (a *BaseActor) OnError(ctx context.Context, err error) Directive
      - L229: func (a *BaseActor) Reply(ctx context.Context, original *Message, reply *Message) error
      - L257: func (a *BaseActor) SetTimer(name string, duration time.Duration, data any)
      - L296: func (a *BaseActor) CancelTimer(name string) bool
    Properties:
      - mu
      - config
      - state
      - handlers
      - timers
      - onStart
      - onStop
      - onError
      - onTimeout
      - replySender
  - Context
    Methods:
      - L59: func WithOnStart(fn func(ctx context.Context) error) BaseActorOption
      - L66: func WithOnStop(fn func(ctx context.Context) error) BaseActorOption
  - error
    Methods:
      - L73: func WithOnError(fn func(ctx context.Context, err error) Directive) BaseActorOption
  - TimerEvent
    Methods:
      - L80: func WithOnTimeout(fn func(ctx context.Context, event TimerEvent) error) BaseActorOption
  - Message
    Methods:
      - L87: func WithReplySender(fn func(ctx context.Context, msg *Message) error) BaseActorOption

Functions:
  - L94: func NewBaseActor(cfg Config, opts ...BaseActorOption) *BaseActor

Global vars:
  - _
---

</file_map>
<file_contents>
File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/interfaces/chatadapter/bridge.go
(lines 1-190: Bridge command routing and skill dispatch, including agent command interception and daemon URL usage from chat platforms.)
```go
package chatadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/interfaces/web/api"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/joshka0/foxctl/internal/runtime/observability"
)

// Bridge connects chat adapter commands to foxctl skills and APIs.
type Bridge struct {
	runner     *api.SkillRunner
	cfg        config.Config
	daemonURL  string
	httpClient *http.Client
}

// NewBridge creates a Bridge with an explicit SkillRunner for dependency injection.
// daemonURL is the base URL for the agent daemon API (e.g. "http://localhost:8090").
func NewBridge(runner *api.SkillRunner, cfg config.Config, daemonURL string) *Bridge {
	if daemonURL == "" {
		daemonURL = "http://localhost:8090"
	}
	return &Bridge{
		runner:     runner,
		cfg:        cfg,
		daemonURL:  daemonURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// NewDefaultBridge creates a Bridge with a default SkillRunner.
func NewDefaultBridge(cfg config.Config, daemonURL string) *Bridge {
	return NewBridge(api.NewSkillRunner(cfg), cfg, daemonURL)
}

// skillRoute maps a command name to its skill name and input builder.
type skillRoute struct {
	skill      string
	buildInput func(opts map[string]any) (map[string]any, error)
}

// routes maps slash command names to skill invocations.
var routes = map[string]skillRoute{
	"search": {
		skill: "code/semantic_search",
		buildInput: func(opts map[string]any) (map[string]any, error) {
			return map[string]any{
				"query":  opts["query"],
				"limit":  10,
				"format": "tree",
			}, nil
		},
	},
	"todo": {
		skill: "todo/manage",
		buildInput: func(opts map[string]any) (map[string]any, error) {
			action, _ := opts["action"].(string)
			action = strings.TrimSpace(action)
			switch action {
			case "", "list":
				return map[string]any{
					"operation": "list",
				}, nil
			case "add":
				title, _ := opts["title"].(string)
				title = strings.TrimSpace(title)
				if title == "" {
					return nil, fmt.Errorf("missing required option `title` for `/todo action:add`; hint: set the `title` option")
				}
				return map[string]any{
					"operation": "add",
					"add": map[string]any{
						"title": title,
					},
				}, nil
			case "complete":
				id, _ := opts["id"].(string)
				id = strings.TrimSpace(id)
				if id == "" {
					return nil, fmt.Errorf("missing required option `id` for `/todo action:complete`; hint: set the `id` option to the task ID")
				}
				return map[string]any{
					"operation": "complete",
					"complete": map[string]any{
						"id": id,
					},
				}, nil
			default:
				return nil, fmt.Errorf("unknown `/todo` action: %q (expected list|add|complete)", action)
			}
		},
	},
	"memory": {
		skill: "memory/query",
		buildInput: func(opts map[string]any) (map[string]any, error) {
			return map[string]any{
				"query": opts["query"],
			}, nil
		},
	},
	"logs": {
		skill: "obs/logs",
		buildInput: func(opts map[string]any) (map[string]any, error) {
			input := map[string]any{}
			if errOnly, ok := opts["errors_only"]; ok {
				input["errors_only"] = errOnly
			}
			return input, nil
		},
	},
}

// HandleCommand dispatches a CommandEvent to the appropriate skill or API.
func (b *Bridge) HandleCommand(ctx context.Context, evt CommandEvent) error {
	observability.Emit(ctx, observability.NewEvent("discord.command_received").
		WithComponent("discord").
		WithCommand(evt.Command).
		WithData("user", evt.User.Username).
		Success(0))

	// Check for agent commands first (use daemon HTTP API, not skills)
	switch evt.Command {
	case "agent-spawn":
		return b.handleAgentSpawn(ctx, evt)
	case "agent-list":
		return b.handleAgentList(ctx, evt)
	}

	// Look up skill route
	route, ok := routes[evt.Command]
	if !ok {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return evt.Respond(fmt.Sprintf("Unknown command: `/%s`. Available: /search, /todo, /memory, /logs, /agent-spawn, /agent-list", evt.Command), nil)
	}

	input, err := route.buildInput(evt.Options)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return evt.Respond(err.Error(), nil)
	}
	result, err := b.runner.Run(ctx, route.skill, input)
	if err != nil {
		observability.Emit(ctx, observability.NewEvent("discord.skill_failed").
			WithComponent("discord").
			WithCommand(route.skill).
			Error(err, 0))
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return evt.Respond(fmt.Sprintf("Error running `%s`: %s\nHint: verify the skill is installed and the daemon is running.", route.skill, err.Error()), nil)
	}

	if ctx.Err() != nil {
		return ctx.Err()
	}

	if !result.Success {
		return evt.Respond(fmt.Sprintf("Skill `%s` failed: %s\nHint: check skill logs with `/logs` or verify input.", route.skill, result.Error), nil)
	}

	// Format output as a message
	content := formatSkillOutput(route.skill, result.Output)
	return evt.Respond(content, nil)
}

// formatSkillOutput converts skill JSON output into a Discord-friendly message.
func formatSkillOutput(skillName string, output json.RawMessage) string {
	if len(output) == 0 {
		return fmt.Sprintf("`%s` completed (no output)", skillName)
	}

	// Skills emit canonical envelopes. Prefer formatting the envelope data payload
	// instead of dumping the full envelope.
	if env, err := protocol.DecodeEnvelope(output); err == nil && env.Version == envelope.Version && env.Command != "" {
		name := skillName
		if env.Command != "" {

```

(lines 330-397: Bridge agent management HTTP calls (/api/agents/spawn and /api/agents) used by chat command path for spawn/list behavior.)
```go
		"exec_mode":      "autonomous",
		"max_iterations": 10,
	})
	if err != nil {
		return evt.Respond(fmt.Sprintf("Failed to build request: %s", err), nil)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.daemonURL+"/api/agents/spawn", strings.NewReader(string(payload)))
	if err != nil {
		return evt.Respond(fmt.Sprintf("Failed to create request: %s\nHint: check daemon URL format.", err), nil)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return evt.Respond(fmt.Sprintf("Daemon unreachable: %s\nHint: ensure the daemon is running (`foxctl web serve`).", err), nil)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return evt.Respond(fmt.Sprintf("Agent spawn failed (HTTP %d)\nHint: check daemon logs with `/logs`.", resp.StatusCode), nil)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return evt.Respond("Agent spawned, but couldn't parse response.\nHint: check daemon API version.", nil)
	}

	sessionID, _ := result["session_id"].(string)
	return evt.Respond(fmt.Sprintf("Agent spawned (role: `%s`, session: `%s`)", role, sessionID), nil)
}

// handleAgentList lists running agents via the daemon HTTP API.
func (b *Bridge) handleAgentList(ctx context.Context, evt CommandEvent) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.daemonURL+"/api/agents", nil)
	if err != nil {
		return evt.Respond(fmt.Sprintf("Failed to create request: %s\nHint: check daemon URL format.", err), nil)
	}

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return evt.Respond(fmt.Sprintf("Daemon unreachable: %s\nHint: ensure the daemon is running (`foxctl web serve`).", err), nil)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return evt.Respond(fmt.Sprintf("Agent list failed (HTTP %d): %s\nHint: check daemon logs.", resp.StatusCode, string(body)), nil)
	}

	var agents []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&agents); err != nil {
		return evt.Respond("Failed to parse agent list.\nHint: check daemon API and network.", nil)
	}

	if len(agents) == 0 {
		return evt.Respond("No agents running. Use `/agent-spawn` to start one.", nil)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**Agents** (%d)\n", len(agents)))
	for _, a := range agents {
		name, _ := a["name"].(string)
		role, _ := a["role"].(string)
		status, _ := a["status"].(string)
		sb.WriteString(fmt.Sprintf("- **%s** (%s) — %s\n", name, role, status))
	}
	return evt.Respond(truncate(sb.String(), 2000), nil)
}

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/interfaces/web/server.go
(lines 1-430: Web server composition, chat adapter startup wiring, orchestration host wiring, and top-level HTTP/API registration including Teams webhook and agents routes.)
```go
//nolint:forbidigo // The web server still owns a zerolog seam until the wider observability migration is completed.
package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	consolepkg "github.com/joshka0/foxctl/internal/console"
	"github.com/joshka0/foxctl/internal/console/app"
	"github.com/joshka0/foxctl/internal/context/companion"
	"github.com/joshka0/foxctl/internal/interfaces/chatadapter"
	"github.com/joshka0/foxctl/internal/interfaces/chatadapter/discord"
	"github.com/joshka0/foxctl/internal/interfaces/chatadapter/teams"
	"github.com/joshka0/foxctl/internal/interfaces/chatadapter/telegram"
	"github.com/joshka0/foxctl/internal/interfaces/web/api"
	"github.com/joshka0/foxctl/internal/interfaces/web/consolews"
	"github.com/joshka0/foxctl/internal/interfaces/web/sse"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/runtime/observability"
	"github.com/joshka0/foxctl/internal/storage/convref"
)

// Server is the foxctl web server.
type Server struct {
	opts             Options
	cfg              config.Config
	log              zerolog.Logger
	sseHub           *sse.Hub
	consoleTransport *consolews.Hub
	consoleSessions  consolepkg.SessionManager
	chatAdapter      chatadapter.ChatAdapter
	turnLock         companion.Locker
	convRefStore     convref.Store
	orchRuntime      api.OrchestrationRuntimeHost
}

// NewServer creates and returns a configured Server for the web layer.
// It initializes the SSE hub and console websocket transport, configures
// console session persistence and the console runner factory, and wires
// observability events to the SSE publisher.
// The provided ctx is used for console transport persistence goroutines and
// should be tied to the application's lifecycle.
//
// Index:
// - Purpose: Initialize the web server and real-time hubs
// - Flow: create hubs → wire persistence/runner factory → connect SSE publisher → return server
// - SideEffects: sets global SSE publisher; starts hub dependencies
// - FailureModes: NewServer does not fail on consoleapp.NewDefaultRunnerFactory; runner errors surface when the console transport invokes the per-session factory
// - Related: consoleapp.NewDefaultRunnerFactory, sse.NewHub, consolews.NewHub
// - Keywords: web_server, sse_hub, consolews, console_runner_factory
func NewServer(ctx context.Context, opts Options, cfg config.Config, log zerolog.Logger) (*Server, error) {
	sseHub := sse.NewHub()
	consoleTransport := consolews.NewHub(ctx)

	// Wire up observability events to SSE for real-time activity streaming
	observability.SetSSEPublisher(sseHub)

	// Set up persistence adapter for console sessions
	persistence := consolepkg.NewSessionPersistence(cfg.Storage.Root)
	consoleTransport.SetPersistence(persistence)

	// Set up runner factory for console sessions (LLM integration)
	runnerFactory := consoleapp.NewDefaultRunnerFactory(ctx)
	consoleTransport.SetRunnerFactory(runnerFactory)

	s := &Server{
		opts:             opts,
		cfg:              cfg,
		log:              log,
		sseHub:           sseHub,
		consoleTransport: consoleTransport,
		consoleSessions:  consoleTransport,
		turnLock:         buildCompanionLocker(ctx, cfg),
	}

	orchRuntime, err := api.NewOrchestrationRuntimeHost(ctx, cfg, log)
	if err != nil {
		return nil, fmt.Errorf("configure orchestration runtime host: %w", err)
	}
	s.orchRuntime = orchRuntime

	// Start chat adapter if configured
	switch opts.ChatAdapter {
	case "":
		// no-op
	case "discord":
		if cfg.Discord.BotToken == "" {
			log.Warn().Msg("--chat discord requires DISCORD_BOT_TOKEN; skipping adapter")
			break
		}
		if err := s.startChatAdapter(ctx); err != nil {
			log.Warn().Err(err).Msg("failed to start chat adapter (continuing without it)")
		}
	case "telegram":
		if cfg.Telegram.BotToken == "" {
			log.Warn().Msg("--chat telegram requires TELEGRAM_BOT_TOKEN; skipping adapter")
			break
		}
		if err := s.startChatAdapter(ctx); err != nil {
			log.Warn().Err(err).Msg("failed to start chat adapter (continuing without it)")
		}
	case "teams":
		if strings.TrimSpace(cfg.Teams.TenantID) == "" || strings.TrimSpace(cfg.Teams.ClientID) == "" || strings.TrimSpace(cfg.Teams.ClientSecret) == "" {
			log.Warn().Msg("--chat teams requires TEAMS_TENANT_ID, TEAMS_CLIENT_ID, TEAMS_CLIENT_SECRET; skipping adapter")
			break
		}
		if cfg.Teams.SkipJWTVerify && !opts.DevCORS {
			log.Warn().Msg("--chat teams with TEAMS_SKIP_JWT_VERIFY=true requires --dev-cors; skipping adapter")
			break
		}
		if err := s.startChatAdapter(ctx); err != nil {
			log.Warn().Err(err).Msg("failed to start chat adapter (continuing without it)")
		}
	default:
		log.Warn().Str("chat_adapter", opts.ChatAdapter).Msg("unknown chat adapter; skipping")
	}

	return s, nil
}

// WaitConsoleTransport drains background console transport goroutines such as
// session persistence workers during shutdown.
func (s *Server) WaitConsoleTransport() {
	if s == nil || s.consoleTransport == nil {
		return
	}
	s.consoleTransport.Wait()
}

func buildCompanionLocker(ctx context.Context, cfg config.Config) companion.Locker {
	if ctx == nil {
		ctx = context.Background()
	}

	if strings.EqualFold(cfg.Database.Driver, "postgres") && strings.TrimSpace(cfg.Database.Postgres.DSN) != "" {
		poolCfg, err := pgxpool.ParseConfig(cfg.Database.Postgres.DSN)
		if err != nil {
			observability.Emit(ctx, observability.NewEvent("web.companion_turnlock_init").
				WithComponent("web").
				WithData("mode", "in-memory").
				WithData("reason", "invalid_postgres_dsn").
				Error(err, 0))
			return companion.NewTurnLock()
		}
		if cfg.Database.Postgres.MaxOpenConns > 0 {
			poolCfg.MaxConns = int32(cfg.Database.Postgres.MaxOpenConns)
		}

		pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
		if err != nil {
			observability.Emit(ctx, observability.NewEvent("web.companion_turnlock_init").
				WithComponent("web").
				WithData("mode", "in-memory").
				WithData("reason", "postgres_pool_init_failed").
				Error(err, 0))
			return companion.NewTurnLock()
		}

		observability.Emit(ctx, observability.NewEvent("web.companion_turnlock_init").
			WithComponent("web").
			WithData("mode", "postgres").
			Success(0))
		return companion.NewPgTurnLock(pool)
	}

	observability.Emit(ctx, observability.NewEvent("web.companion_turnlock_init").
		WithComponent("web").
		WithData("mode", "in-memory").
		Success(0))
	return companion.NewTurnLock()
}

func (s *Server) buildConvRefStore(ctx context.Context) convref.Store {
	if strings.EqualFold(s.cfg.Database.Driver, "postgres") && strings.TrimSpace(s.cfg.Database.Postgres.DSN) != "" {
		store, err := convref.OpenPostgres(ctx, s.cfg.Database.Postgres.DSN)
		if err != nil {
			s.log.Warn().Err(err).Msg("failed to open postgres convref store; conversation refs will not be persisted")
			return nil
		}
		return store
	}

	// SQLite fallback for local/dev setups.
	dbPath := filepath.Join(s.cfg.Storage.Root, "storage", "convref.db")
	store, err := convref.OpenSQLite(ctx, dbPath)
	if err != nil {
		s.log.Warn().Err(err).Msg("failed to open sqlite convref store; conversation refs will not be persisted")
		return nil
	}
	return store
}

// Run starts the SSE hub event loop. Call in a goroutine.
func (s *Server) Run(ctx context.Context) {
	if s.orchRuntime != nil {
		go func() {
			if err := s.orchRuntime.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				s.log.Error().Err(err).Msg("orchestration runtime host failed")
			}
		}()
	}
	s.sseHub.Run(ctx)
	if s.orchRuntime != nil {
		if err := s.orchRuntime.Close(); err != nil {
			s.log.Warn().Err(err).Msg("failed to close orchestration runtime host")
		}
	}
}

// SSEHub returns the SSE hub for publishing events.
func (s *Server) SSEHub() *sse.Hub {
	return s.sseHub
}

// startChatAdapter initializes and connects the chat adapter.
func (s *Server) startChatAdapter(ctx context.Context) error {
	// Derive daemon URL from the server's listen address
	daemonURL := ""
	if strings.TrimSpace(s.opts.Addr) != "" {
		addr := strings.TrimSpace(s.opts.Addr)
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			// Handle bare port formats like ":8090" and "8090".
			if strings.HasPrefix(addr, ":") {
				host = "localhost"
				port = strings.TrimPrefix(addr, ":")
			} else {
				digitsOnly := true
				for _, r := range addr {
					if r < '0' || r > '9' {
						digitsOnly = false
						break
					}
				}
				if digitsOnly {
					host = "localhost"
					port = addr
				}
			}
		} else if host == "" || host == "0.0.0.0" {
			host = "localhost"
		}
		if port != "" {
			daemonURL = "http://" + net.JoinHostPort(host, port)
		}
	}

	bridge := chatadapter.NewDefaultBridge(s.cfg, daemonURL)

	switch s.opts.ChatAdapter {
	case "discord":
		adapter := discord.New(s.cfg.Discord, daemonURL)
		adapter.SetSSEHub(s.sseHub)

		adapter.OnCommand(bridge.HandleCommand)
		adapter.OnInteraction(adapter.HandleInteraction)

		// Phase 3: Wire console sessions for natural language messaging
		sessionBridge := discord.NewSessionBridge(s.consoleSessions, adapter, s.cfg.Discord, s.turnLock)
		adapter.OnMessage(sessionBridge.HandleMessage)

		if err := adapter.Connect(ctx); err != nil {
			return err
		}
		if err := adapter.RegisterCommands(ctx, discord.MVPCommands()); err != nil {
			_ = adapter.Disconnect(ctx)
			return err
		}

		s.chatAdapter = adapter

	case "telegram":
		adapter := telegram.New(s.cfg.Telegram, daemonURL, nil)
		adapter.SetSSEHub(s.sseHub)

		adapter.OnCommand(bridge.HandleCommand)
		adapter.OnInteraction(adapter.HandleInteraction)

		sessionBridge := telegram.NewSessionBridge(s.consoleSessions, adapter, s.cfg.Telegram, s.turnLock)
		adapter.OnMessage(sessionBridge.HandleMessage)

		if err := adapter.Connect(ctx); err != nil {
			return err
		}
		if err := adapter.RegisterCommands(ctx, telegram.MVPCommands()); err != nil {
			_ = adapter.Disconnect(ctx)
			return err
		}

		s.chatAdapter = adapter

	case "teams":
		if s.cfg.Teams.SkipJWTVerify && !s.opts.DevCORS {
			observability.Emit(ctx, observability.NewEvent("teams.skip_jwt_verify_blocked").
				WithComponent("web").
				WithData("warning", "TEAMS_SKIP_JWT_VERIFY requires --dev-cors; refusing to start Teams adapter").
				Error(nil, 0))
			return fmt.Errorf("teams adapter refused: TEAMS_SKIP_JWT_VERIFY requires --dev-cors")
		}

		adapter := teams.New(s.cfg.Teams, daemonURL, nil)
		adapter.SetSSEHub(s.sseHub)
		if convRefStore := s.buildConvRefStore(ctx); convRefStore != nil {
			adapter.SetConvRefStore(convRefStore)
			s.convRefStore = convRefStore
		}

		adapter.OnCommand(bridge.HandleCommand)
		adapter.OnInteraction(adapter.HandleInteraction)

		sessionBridge := chatadapter.NewSessionBridge(s.consoleSessions, adapter, chatadapter.SessionBridgeConfig{
			PlatformName:     "teams",
			MaxMessageLen:    4000,
			EditIntervalMS:   s.cfg.Teams.EditIntervalMS,
			ChatProfile:      s.cfg.Teams.ChatProfile,
			ChatSystemPrompt: s.cfg.Teams.ChatSystemPrompt,
		}, s.turnLock)
		adapter.OnMessage(sessionBridge.HandleMessage)

		if err := adapter.Connect(ctx); err != nil {
			if s.convRefStore != nil {
				_ = s.convRefStore.Close()
				s.convRefStore = nil
			}
			return err
		}
		if err := adapter.RegisterCommands(ctx, nil); err != nil {
			_ = adapter.Disconnect(ctx)
			if s.convRefStore != nil {
				_ = s.convRefStore.Close()
				s.convRefStore = nil
			}
			return err
		}

		s.chatAdapter = adapter

	default:
		return nil
	}

	if s.chatAdapter != nil {
		s.log.Info().Str("adapter", s.chatAdapter.Name()).Msg("chat adapter started")
	}
	return nil
}

// StopChatAdapter gracefully disconnects the chat adapter.
func (s *Server) StopChatAdapter(ctx context.Context) {
	if s.chatAdapter != nil {
		if err := s.chatAdapter.Disconnect(ctx); err != nil {
			s.log.Warn().Err(err).Msg("chat adapter disconnect error")
		}
	}
	if s.convRefStore != nil {
		if err := s.convRefStore.Close(); err != nil {
			s.log.Warn().Err(err).Msg("convref store close error")
		}
		s.convRefStore = nil
	}
}

// Handler returns the HTTP handler for the server.
//
// Index:
// - Purpose: Build the HTTP mux and wire API routes
// - Flow: register routes → return mux
// - SideEffects: registers handlers
// - FailureModes: none (handler construction)
// - Related: api.* handlers, sse.Handler
// - Keywords: api_routes, http_mux, handlers
func (s *Server) Handler() http.Handler {
	apiMux := http.NewServeMux()

	// --- Health ---
	apiMux.HandleFunc("/api/health", api.StatusHandler(s.cfg, s.log))

	// --- SSE Events ---
	apiMux.HandleFunc("/api/events", sse.Handler(s.sseHub))
	apiMux.HandleFunc("/api/logs/cleanup", api.LogCleanupHandler(s.cfg, s.log))

	// --- OAuth AuthBroker Callback ---
	apiMux.HandleFunc("/api/oauth/callback", api.OAuthCallbackHandler(s.cfg, s.log))

	// --- Microsoft Teams Webhook (Chat Adapter) ---
	apiMux.HandleFunc("/api/teams/messages", func(w http.ResponseWriter, r *http.Request) {
		ta, ok := s.chatAdapter.(*teams.Adapter)
		if !ok || ta == nil {
			http.Error(w, "teams adapter inactive", http.StatusServiceUnavailable)
			return
		}
		ta.HTTPHandler()(w, r)
	})

	// --- Jobs (Phase 2) ---
	apiMux.HandleFunc("/api/jobs", api.JobsListHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/jobs/", api.JobDetailHandler(s.cfg, s.log))

	// --- CAS (Phase 2) ---
	apiMux.HandleFunc("/api/cas/", api.CASHandler(s.cfg, s.log))

	// --- Workspaces ---
	apiMux.HandleFunc("/api/workspaces", api.WorkspacesHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/workspaces/switch", api.WorkspaceSwitchHandler(s.cfg, s.log))

	// --- OpenAPI / Swagger ---
	apiMux.HandleFunc("/api/openapi.json", api.OpenAPIHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/swagger", api.SwaggerUIHandler())
	apiMux.HandleFunc("/api/swagger/", api.SwaggerUIHandler())

	// --- Skills (Phase 4) ---
	apiMux.HandleFunc("/api/skills", api.SkillsListHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/skills/schema", api.SkillsSchemaHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/skills/run", api.SkillsRunHandler(s.cfg, s.log))
	// Skill CRUD routes - supports all HTTP methods:
	//   GET    /api/skills/todo/manage        → list
	//   GET    /api/skills/todo/manage/{id}   → get
	//   POST   /api/skills/todo/manage        → add
	//   PUT    /api/skills/todo/manage/{id}   → update
	//   DELETE /api/skills/todo/manage/{id}   → delete
	apiMux.HandleFunc("/api/skills/", api.SkillsCRUDHandler(s.cfg, s.log))

```

(lines 510-559: Continuation of API route registration for orchestration and companion endpoints demonstrating breadth of API surface.)
```go
	// lock instance providing zero mutual exclusion.
	skillRunner := api.NewSkillRunner(s.cfg)
	apiMux.HandleFunc("/api/companion/providers", api.CompanionProvidersHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/companion/cochange", api.CompanionCoChangeHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/companion/chat", api.CompanionChatHandler(s.cfg, s.log, s.turnLock, skillRunner))
	apiMux.HandleFunc("/api/companion/conversations", api.CompanionConversationsHandler(s.cfg, s.log))
	apiMux.HandleFunc("/api/companion/conversations/", func(w http.ResponseWriter, r *http.Request) {
		// Route based on sub-path: /api/companion/conversations/:id/(messages|personality)
		// or DELETE /api/companion/conversations/:id for soft delete
		path := strings.TrimPrefix(r.URL.Path, "/api/companion/conversations/")
		parts := strings.Split(path, "/")
		if len(parts) >= 2 && parts[1] != "" {
			switch parts[1] {
			case "messages":
				if r.Method == http.MethodDelete {
					api.CompanionMessageDeleteHandler(s.cfg, s.log).ServeHTTP(w, r)
				} else {
					api.CompanionConversationMessagesHandler(s.cfg, s.log).ServeHTTP(w, r)
				}
			case "compress":
				api.CompanionConversationCompressHandler(s.cfg, s.log).ServeHTTP(w, r)
			case "personality":
				// Check for sub-path: /api/companion/conversations/:id/personality/dimension
				if len(parts) >= 3 && parts[2] == "dimension" {
					api.CompanionPersonalityDimensionPatchHandler(s.cfg, s.log).ServeHTTP(w, r)
				} else {
					api.CompanionPersonalityHandler(s.cfg, s.log).ServeHTTP(w, r)
				}
			case "settings":
				api.CompanionConversationSettingsHandler(s.cfg, s.log).ServeHTTP(w, r)
			default:
				http.Error(w, "unknown conversation endpoint", http.StatusNotFound)
			}
		} else if len(parts) >= 1 && parts[0] != "" {
			// /api/companion/conversations/:id - DELETE for soft delete, PATCH for rename
			switch r.Method {
			case http.MethodDelete:
				api.CompanionConversationDeleteHandler(s.cfg, s.log).ServeHTTP(w, r)
			case http.MethodPatch:
				api.CompanionConversationRenameHandler(s.cfg, s.log).ServeHTTP(w, r)
			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
		} else {
			http.Error(w, "invalid conversation path", http.StatusBadRequest)
		}
	})
	apiMux.HandleFunc("/api/companion/context", api.CompanionContextSetHandler(s.cfg, s.log))
	// Context routes with path params (GET, DELETE for specific conversation/key)
	apiMux.HandleFunc("/api/companion/context/", func(w http.ResponseWriter, r *http.Request) {

```

(lines 705-742: CORS/request-logging middleware seam relevant to standalone API extraction.)
```go
func withCORS(dev bool, next http.Handler) http.Handler {
	if !dev {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			origin = "http://localhost:5173"
		}

		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// withRequestLogging is a middleware placeholder for HTTP request logging.
//
// Currently a pass-through (no logging) for two reasons:
//  1. Debug-level request logging adds overhead and noise for local API server
//  2. Wide events are better suited for operation-level telemetry (errors, latency)
//     rather than per-request debug traces
//
// TODO: Consider adding observability events for:
//   - Slow requests (latency > threshold)
//   - Error responses (4xx, 5xx)
//   - Specific endpoints that benefit from audit trails
func withRequestLogging(_ zerolog.Logger, next http.Handler) http.Handler {
	return next
}

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/runtime/daemon/service.go
(lines 391-722: Daemon run loop, socket/RPC server lifecycle, and method dispatch including agent.spawn/list/ask/kill handlers.)
```go
func (s *Service) Run(ctx context.Context) error {
	// Remove stale socket
	socketPath := s.socketPath
	_ = os.Remove(socketPath)

	// Ensure socket directory exists
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
		return fmt.Errorf("create socket directory: %w", err)
	}

	// Create listener
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", socketPath, err)
	}
	s.setListener(listener)
	fmt.Fprintf(os.Stderr, "daemon: listener bound on %s\n", socketPath)

	// Set socket permissions (user-only)
	if err := os.Chmod(socketPath, 0o600); err != nil {
		listener.Close()
		return fmt.Errorf("chmod socket: %w", err)
	}

	// Write PID file
	if err := s.writePIDFile(); err != nil {
		listener.Close()
		return fmt.Errorf("write pid file: %w", err)
	}
	defer s.removePIDFile()

	// Start leader lease coordination (only enabled when COORDINATION is configured for remote sync).
	// This gates background workers so only one machine mutates shared state at a time.
	if err := s.startLeaderLease(ctx); err != nil {
		// Log but don't fail daemon startup; safest behavior is to run as follower (no leader workers).
		fmt.Fprintf(os.Stderr, "warning: leader lease init failed (running without leader workers): %v\n", err)
	}

	// Accept connections
	go s.acceptLoop(ctx, listener)

	// Wait for shutdown signal
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.shutdownCh:
		return nil
	}
}

// Shutdown gracefully shuts down the daemon.
//
// Index:
// - Purpose: Stop daemon workers and release resources
// - Flow: signal shutdown → wait for requests → stop workers → close stores → remove socket
// - SideEffects: stops goroutines; closes databases; removes socket/PID
// - FailureModes: context timeout while waiting for in-flight requests
// - Related: Service.Run, Service.acceptLoop
// - Keywords: daemon_shutdown, stop_workers, close_stores, socket_path
func (s *Service) Shutdown(ctx context.Context) error {
	s.shutdownOnce.Do(func() {
		s.shutdownMu.Lock()
		s.isShutdown = true
		s.shutdownMu.Unlock()
		close(s.shutdownCh)
	})

	if listener := s.clearListener(); listener != nil {
		listener.Close()
	}

	// Wait for in-flight requests with timeout
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}

	// Stop leader lease loop first so it can't restart workers during shutdown.
	s.stopLeaderLease(ctx)
	s.leaderWorkersMu.Lock()
	s.stopLeaderWorkers()
	s.leaderWorkersMu.Unlock()

	// Close shared resources
	s.cacheMu.Lock()
	if s.cacheStore != nil {
		s.cacheStore.Close()
		s.cacheStore = nil
	}
	s.cacheMu.Unlock()

	// Close connection pool
	if s.dbPool != nil {
		s.dbPool.Close()
		sqliteutil.SetGlobalPool(nil)
	}

	// Remove socket
	_ = os.Remove(s.socketPath)

	return nil
}

// acceptLoop accepts connections and handles them.
//
// Index:
// - Purpose: Accept daemon connections and dispatch handlers
// - Flow: accept conn → guard shutdown → spawn handler goroutine
// - SideEffects: spawns goroutines; reads socket
// - FailureModes: accept errors ignored unless shutting down
// - Related: Service.handleConnection
// - Keywords: accept_loop, unix_socket, goroutine, shutdown
func (s *Service) acceptLoop(ctx context.Context, listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-s.shutdownCh:
				return
			default:
				// Transient error, continue
				fmt.Fprintf(os.Stderr, "daemon: accept error: %v\n", err)
				continue
			}
		}

		// Atomically check shutdown and add to wait group to avoid race with Shutdown()
		s.shutdownMu.Lock()
		if s.isShutdown {
			s.shutdownMu.Unlock()
			conn.Close()
			return
		}
		s.wg.Add(1)
		s.shutdownMu.Unlock()

		go func() {
			defer s.wg.Done()
			s.handleConnection(ctx, conn)
		}()
	}
}

func (s *Service) setListener(listener net.Listener) {
	s.listenerMu.Lock()
	defer s.listenerMu.Unlock()
	s.listener = listener
}

func (s *Service) clearListener() net.Listener {
	s.listenerMu.Lock()
	defer s.listenerMu.Unlock()
	listener := s.listener
	s.listener = nil
	return listener
}

// Request is the JSON-RPC-like request format.
type Request struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
	ID     string          `json:"id,omitempty"`
}

// Response is the JSON-RPC-like response format.
type Response struct {
	ID     string `json:"id,omitempty"`
	Result any    `json:"result,omitempty"`
	Error  *Error `json:"error,omitempty"`
}

// Error represents an error response.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// handleConnection handles a single client connection.
//
// Index:
// - Purpose: Decode daemon request and route to handler
// - Flow: decode request → switch method → build response → write response
// - SideEffects: reads/writes socket; updates request count
// - FailureModes: parse errors, handler errors
// - Related: Service.handleRun, Service.handleStatus
// - Keywords: daemon_request, json_rpc, handle_run, handle_status
func (s *Service) handleConnection(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	// Set read deadline
	if err := conn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return // Connection already closed or broken
	}

	s.requestCount.Add(1)

	// Read request
	decoder := json.NewDecoder(conn)
	var req Request
	if err := decoder.Decode(&req); err != nil {
		s.writeError(conn, req.ID, "EPARSE", fmt.Sprintf("parse request: %v", err))
		return
	}

	// Route request
	var resp Response
	resp.ID = req.ID

	switch req.Method {
	case "status":
		resp.Result = s.handleStatus()
	case "run":
		result, err := s.handleRun(ctx, req.Params)
		if err != nil {
			resp.Error = &Error{Code: "ERUN", Message: err.Error()}
		} else {
			resp.Result = result
		}
	case "warm":
		result, err := s.handleWarm(req.Params)
		if err != nil {
			resp.Error = &Error{Code: "EWARM", Message: err.Error()}
		} else {
			resp.Result = result
		}
	case "shutdown":
		resp.Result = map[string]any{"status": "shutting_down"}
		s.writeResponse(conn, resp)
		go func() {
			time.Sleep(100 * time.Millisecond)
			s.shutdownOnce.Do(func() {
				close(s.shutdownCh)
			})
		}()
		return
	case "agent.spawn":
		result, err := s.dispatchAgentSpawn(ctx, req.Params)
		if err != nil {
			resp.Error = &Error{Code: "ESPAWN", Message: err.Error()}
		} else {
			resp.Result = result
		}
	case "agent.list":
		result, err := s.dispatchAgentList(ctx)
		if err != nil {
			resp.Error = &Error{Code: "ELIST", Message: err.Error()}
		} else {
			resp.Result = result
		}
	case "agent.status":
		result, err := s.handleAgentStatus(req.Params)
		if err != nil {
			resp.Error = &Error{Code: "ESTATUS", Message: err.Error()}
		} else {
			resp.Result = result
		}
	case "agent.kill":
		result, err := s.dispatchAgentKill(ctx, req.Params)
		if err != nil {
			resp.Error = &Error{Code: "EKILL", Message: err.Error()}
		} else {
			resp.Result = result
		}
	case "agent.ask":
		result, err := s.handleAgentAskRPC(ctx, req.Params)
		if err != nil {
			resp.Error = &Error{Code: "EASK", Message: err.Error()}
		} else {
			resp.Result = result
		}
	case "agent.resume":
		result, err := s.handleAgentResume(ctx, req.Params)
		if err != nil {
			resp.Error = &Error{Code: "ERESUME", Message: err.Error()}
		} else {
			resp.Result = result
		}
	case "agent.hierarchy":
		result, err := s.handleAgentHierarchy(req.Params)
		if err != nil {
			resp.Error = &Error{Code: "EHIERARCHY", Message: err.Error()}
		} else {
			resp.Result = result
		}
	default:
		resp.Error = &Error{Code: "EMETHOD", Message: fmt.Sprintf("unknown method: %s", req.Method)}
	}

	s.writeResponse(conn, resp)
}

// StatusResult contains daemon status information.
type StatusResult struct {
	PID            int      `json:"pid"`
	StartedAt      string   `json:"started_at"`
	UptimeSeconds  float64  `json:"uptime_seconds"`
	RequestCount   int64    `json:"request_count"`
	WarmWorkspaces []string `json:"warm_workspaces"`
	LeaderEnabled  bool     `json:"leader_enabled,omitempty"`
	IsLeader       bool     `json:"is_leader,omitempty"`
	DeviceID       string   `json:"device_id,omitempty"`
	LeaderOwnerID  string   `json:"leader_owner_id,omitempty"`
}

func (s *Service) handleStatus() StatusResult {
	s.warmMu.RLock()
	workspaces := make([]string, 0, len(s.warmWorkspaces))
	for ws := range s.warmWorkspaces {
		workspaces = append(workspaces, ws)
	}
	s.warmMu.RUnlock()

	s.leaderMu.RLock()
	leaderEnabled := s.leaderEnabled
	isLeader := s.isLeader
	deviceID := s.deviceID
	ownerID := s.leaderOwnerID
	s.leaderMu.RUnlock()

	return StatusResult{
		PID:            os.Getpid(),
		StartedAt:      s.started.Format(time.RFC3339),
		UptimeSeconds:  time.Since(s.started).Seconds(),
		RequestCount:   s.requestCount.Load(),
		WarmWorkspaces: workspaces,

```

(lines 1401-1560: Agent orchestration initialization path and runtime dependencies required before spawning is possible.)
```go
func (s *Service) startAgentOrchestration(ctx context.Context) error {
	s.agentMu.Lock()
	defer s.agentMu.Unlock()

	// Idempotency: allow leader transitions to call start repeatedly.
	if s.agentRuntime != nil {
		return nil
	}

	// Determine LLM configuration from centralized config
	// Priority: configured provider > OPENROUTER > CEREBRAS > GROQ > GEMINI
	llmProvider, llmAPIKey, llmModel := s.resolveLLMConfig()

	if llmAPIKey == "" && !strings.EqualFold(s.cfg.LLM.ResolveAuthMode(llmProvider), "none") {
		fmt.Fprintf(os.Stderr, "agent orchestration: skipped (no LLM API key configured)\n")
		return nil
	}

	// Create cancellable context
	s.agentCtx, s.agentCancel = context.WithCancel(ctx)

	// Open sessions store for persistence (store in Service for cleanup on shutdown)
	sessionStore, err := sessions.OpenFromConfig(ctx, s.cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent orchestration: session store open failed: %v\n", err)
		// Continue without persistence - agents will be in-memory only
		sessionStore = nil
	}
	s.agentSessionStore = sessionStore

	// Open mailbox store for inter-agent messaging (store in Service for cleanup on shutdown)
	var mailboxStore runtime.MailboxStore
	if mb, err := mailbox.Open(ctx, s.cfg.Storage.Root); err != nil {
		fmt.Fprintf(os.Stderr, "agent orchestration: mailbox store open failed: %v\n", err)
	} else {
		s.agentMailboxStore = mb
		mailboxStore = mb
	}

	// Open blackboard store for workspace coordination (store in Service for cleanup on shutdown)
	var boardStore runtime.BoardStore
	if bb, err := blackboard.OpenBoardStore(ctx, s.cfg.Storage.Root); err != nil {
		fmt.Fprintf(os.Stderr, "agent orchestration: board store open failed: %v\n", err)
	} else {
		s.agentBoardStore = bb
		boardStore = bb
	}

	// Open CAS store for full turn content persistence
	var casStore storage.CASStore
	if cs, err := cas.OpenDefault(ctx, s.cfg.Storage.Root); err != nil {
		fmt.Fprintf(os.Stderr, "agent orchestration: CAS store open failed: %v\n", err)
	} else {
		casStore = cs
	}

	// Create memory store opener for agent tools
	openMemoryStore := func(ctx context.Context) (storage.MemoryStore, error) {
		return memory.OpenWithConfig(ctx, s.cfg)
	}

	var promptVariantStore optimization.PromptVariantStore
	if pvs, err := optimization.OpenPromptVariantStore(ctx, s.cfg.Storage.Root); err != nil {
		fmt.Fprintf(os.Stderr, "agent orchestration: prompt variant store open failed: %v\n", err)
	} else {
		s.agentPromptVariants = pvs
		promptVariantStore = pvs
	}

	// Load hook configuration and create dispatcher for agent tools
	var hookDispatcher hooks.Dispatcher
	hookCfg, err := hooks.LoadConfigWithDefaults(s.opts.Workspace)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent orchestration: hook config load failed: %v\n", err)
		hookCfg = hooks.EmptyConfig()
	}
	hookDispatcher = hooks.NewDispatcherWithRegistry(hookCfg, s.cfg.Paths.Skills)

	// Create action executor for processing hook output actions
	// Use concrete store fields (not runtime interface variables) for full interface compliance
	actionExecutor := hooks.NewExecutor(hooks.ExecutorConfig{
		MailboxStore: s.agentMailboxStore,
		BoardStore:   s.agentBoardStore,
		FailOpen:     true, // Don't block on action errors
	})

	// Create runtime config
	runtimeCfg := runtime.Config{
		DefaultMaxIterations: 50,
		DefaultTimeout:       30 * time.Minute,
		LLMProvider:          llmProvider,
		LLMModel:             llmModel,
		LLMAPIKey:            llmAPIKey,
		LLMBaseURL:           s.cfg.LLM.ResolveBaseURL(llmProvider),
		LLMAuthMode:          s.cfg.LLM.ResolveAuthMode(llmProvider),
		LLMAuthHeader:        s.cfg.LLM.ResolveAuthHeader(llmProvider),
		LLMAuthPrefix:        s.cfg.LLM.ResolveAuthPrefix(llmProvider),
		WorkspaceRoot:        s.opts.Workspace,
		DefaultMaxDepth:      3,
		PromptVariantStore:   promptVariantStore,
		OpenMemoryStore:      openMemoryStore,
		SessionStore:         sessionStore,
		MailboxStore:         mailboxStore,
		BoardStore:           boardStore,
		HookDispatcher:       hookDispatcher,
		ActionExecutor:       actionExecutor,
		CASStore:             casStore,
	}

	// Create runtime
	s.agentRuntime = runtime.NewRuntime(runtimeCfg)

	// Recover stale sessions from previous daemon run
	if sessionStore != nil {
		if err := s.agentRuntime.RecoverSessions(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "agent orchestration: session recovery failed: %v\n", err)
		}
	}

	// Create overseer config
	overseerCfg := runtime.OverseerConfig{
		MaxDepth:            3,
		MaxConcurrentAgents: 10,
	}

	// Create overseer
	s.agentOverseer = runtime.NewOverseer(s.agentRuntime, overseerCfg)

	fmt.Fprintf(os.Stderr, "agent orchestration: started (provider=%s, model=%s)\n", llmProvider, llmModel)
	return nil
}

func (s *Service) stopAgentOrchestration() {
	s.agentMu.Lock()
	defer s.agentMu.Unlock()

	if s.agentCancel != nil {
		s.agentCancel()
		s.agentCancel = nil
	}

	// Kill all running agent sessions (best-effort).
	if s.agentRuntime != nil {
		for _, session := range s.agentRuntime.List() {
			_ = s.agentRuntime.Kill(session.ID)
		}
	}

	// Mark all tracked agents as stopped in agents.db (best-effort)
	if len(s.agentSessionMap) > 0 {
		ctx := context.Background()
		store, err := agentstore.Open(ctx, s.cfg.Storage.Root)
		if err != nil {
			slog.Warn("failed to open agents store for shutdown cleanup", "error", err)
		} else {
			for agentID := range s.agentSessionMap {
				if err := store.UpdateState(ctx, agentID, agent.StateStopped); err != nil {
					slog.Warn("failed to update agent state on shutdown", "agent_id", agentID, "error", err)
				}
			}

```

(lines 1670-1900: Daemon agent spawn implementation path (validation, runtime spawn, persistence, and agent/session mapping).)
```go
	Reply     string         `json:"reply"`
	ReplyData map[string]any `json:"reply_data,omitempty"`
	Status    string         `json:"status"`
}

func (s *Service) handleAgentSpawnV2(ctx context.Context, params json.RawMessage) (*AgentSpawnResult, error) {
	return s.handleAgentSpawnWithRoute(ctx, params)
}

func (s *Service) handleAgentSpawnWithRoute(ctx context.Context, params json.RawMessage) (*AgentSpawnResult, error) {
	if s.agentRuntime == nil {
		return nil, errors.New("agent orchestration not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var p AgentSpawnParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}

	if p.Role == "" {
		return nil, errors.New("role is required")
	}

	// Normalize skills allowlist for runtime tool names
	if len(p.SkillsAllow) > 0 {
		normalized, unknown := toolnames.ValidateAllowlist(toolnames.ToolModeRuntime, p.SkillsAllow)
		if len(unknown) > 0 {
			slog.Warn("unknown skills_allow entries in spawn request", "unknown", unknown)
		}
		p.SkillsAllow = normalized
	}

	// Generate actor ID if not provided
	actorID := p.AgentID
	if actorID == "" {
		actorID = "actor:" + p.Role + ":" + ulid.Make().String()
	}

	// Create agent config
	cfg := types.AgentConfig{
		Role:            types.AgentRole(p.Role),
		ActorID:         actorID,
		WorkspaceID:     p.WorkspaceID,
		WorkspaceRoot:   strings.TrimSpace(p.WorkspaceRoot),
		EpicID:          p.EpicID,
		TaskID:          p.TaskID,
		Prompt:          p.Prompt,
		SkillsAllow:     p.SkillsAllow,
		TerminalBinding: agent.NormalizeTerminalBinding(p.TerminalBinding),
	}
	if cfg.WorkspaceID == "" && cfg.WorkspaceRoot != "" {
		cfg.WorkspaceID = ws.ID(cfg.WorkspaceRoot)
	}

	// Apply execution config if provided
	if p.MaxIterations > 0 {
		cfg.MaxIterations = p.MaxIterations
	}
	if p.MaxContextTokens > 0 {
		cfg.MaxContextTokens = p.MaxContextTokens
	}
	if p.ExecMode != "" {
		switch p.ExecMode {
		case string(agent.ModeReactive), string(agent.ModeAutonomous), string(agent.ModeAutonomousReactive), string(agent.ModeProactive), string(agent.ModeTick), string(agent.ModeStory):
			cfg.ExecMode = agent.ExecutionMode(p.ExecMode)
		default:
			return nil, fmt.Errorf("invalid exec_mode: %s", p.ExecMode)
		}
	}
	if p.MaxAutoTurns > 0 {
		cfg.MaxAutoTurns = p.MaxAutoTurns
	}
	if p.ThinkInterval > 0 {
		cfg.ThinkInterval = p.ThinkInterval
	}
	if p.Timeout != "" {
		if strings.TrimSpace(p.Timeout) == "0" {
			cfg.Timeout = -1
		} else {
			d, err := time.ParseDuration(p.Timeout)
			if err != nil {
				return nil, fmt.Errorf("invalid timeout %q: %w", p.Timeout, err)
			}
			if d <= 0 {
				return nil, fmt.Errorf("invalid timeout %q: must be positive", p.Timeout)
			}
			cfg.Timeout = d
		}
	}

	if cfg.ExecMode == agent.ModeTick {
		cfg.LLMProvider = "lmstudio"
		if strings.TrimSpace(cfg.LLMModel) == "" {
			cfg.LLMModel = llmproviders.DefaultModelForProvider("lmstudio")
		}
	}

	// Apply LLM override if provided
	if p.LLMProvider != "" {
		cfg.LLMProvider = p.LLMProvider
		cfg.LLMAPIKey = s.cfg.LLM.ResolveAPIKey(cfg.LLMProvider)
		cfg.LLMBaseURL = s.cfg.LLM.ResolveBaseURL(cfg.LLMProvider)
		cfg.LLMAuthMode = s.cfg.LLM.ResolveAuthMode(cfg.LLMProvider)
		cfg.LLMAuthHeader = s.cfg.LLM.ResolveAuthHeader(cfg.LLMProvider)
		cfg.LLMAuthPrefix = s.cfg.LLM.ResolveAuthPrefix(cfg.LLMProvider)
		if strings.TrimSpace(cfg.LLMModel) == "" {
			cfg.LLMModel = llmproviders.DefaultModelForProvider(cfg.LLMProvider)
		}
	}
	if p.LLMModel != "" {
		cfg.LLMModel = p.LLMModel
	}
	if p.LLMAPIKey != "" {
		cfg.LLMAPIKey = p.LLMAPIKey
	}
	if p.LLMBaseURL != "" {
		cfg.LLMBaseURL = p.LLMBaseURL
	}
	if p.LLMAuthMode != "" {
		cfg.LLMAuthMode = p.LLMAuthMode
	}
	if p.LLMAuthHeader != "" {
		cfg.LLMAuthHeader = p.LLMAuthHeader
	}
	if p.LLMAuthPrefix != "" {
		cfg.LLMAuthPrefix = p.LLMAuthPrefix
	}

	// Spawn the agent
	session, err := s.agentRuntime.Spawn(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("spawn agent: %w", err)
	}

	// Persist agent record to agents.db
	agentID := ulid.Make().String()
	agentName := p.Name
	if agentName == "" {
		agentName = agent.GenerateAgentName(rand.New(rand.NewSource(time.Now().UnixNano())))
	}

	execMode := agent.ExecutionMode(p.ExecMode)
	if execMode == "" {
		execMode = agent.ModeReactive
	}

	agentRecord := agent.Agent{
		ID:              agentID,
		Namespace:       actorID,
		Name:            agentName,
		Slug:            p.Slug,
		Role:            p.Role,
		Prompt:          p.Prompt,
		SkillsAllow:     p.SkillsAllow,
		Policy:          agent.Policy{},
		ShareBB:         "scoped",
		State:           agent.StateRunning,
		CreatedAt:       time.Now().UTC(),
		LLMProvider:     cfg.LLMProvider,
		LLMModel:        cfg.LLMModel,
		LLMAPIKey:       cfg.LLMAPIKey,
		LLMBaseURL:      cfg.LLMBaseURL,
		LLMAuthMode:     cfg.LLMAuthMode,
		LLMAuthHeader:   cfg.LLMAuthHeader,
		LLMAuthPrefix:   cfg.LLMAuthPrefix,
		ExecMode:        execMode,
		ExecutionLayer:  agent.ExecutionLayerClassic,
		MaxIterations:   p.MaxIterations,
		MaxAutoTurns:    p.MaxAutoTurns,
		ThinkInterval:   p.ThinkInterval,
		MemoryScope:     agent.NormalizeMemoryScope(agent.MemoryScope(strings.TrimSpace(p.MemoryScope))),
		TerminalBinding: cfg.TerminalBinding,
		MemoryRetention: func() agent.MemoryRetention {
			if strings.TrimSpace(p.MemoryRetention) == "" {
				return agent.DefaultMemoryRetentionForScope(agent.NormalizeMemoryScope(agent.MemoryScope(strings.TrimSpace(p.MemoryScope))))
			}
			return agent.NormalizeMemoryRetention(agent.MemoryRetention(strings.TrimSpace(p.MemoryRetention)))
		}(),
	}
	if execMode == agent.ModeTick {
		agentRecord.LLMProvider = "lmstudio"
		if strings.TrimSpace(agentRecord.LLMModel) == "" {
			agentRecord.LLMModel = llmproviders.DefaultModelForProvider("lmstudio")
		}
	}
	if agentRecord.SkillsAllow == nil {
		agentRecord.SkillsAllow = []string{}
	}

	store, err := agentstore.Open(ctx, s.cfg.Storage.Root)
	if err != nil {
		slog.Warn("failed to open agents store for persistence", "error", err)
	} else {
		defer store.Close()
		if err := store.Create(ctx, agentRecord); err != nil {
			slog.Warn("failed to persist agent record", "agent_id", agentID, "error", err)
		} else {
			// Track mapping for kill/shutdown cleanup
			s.agentMu.Lock()
			if s.agentSessionMap == nil {
				s.agentSessionMap = make(map[string]string)
			}
			s.agentSessionMap[agentID] = session.ID
			s.agentMu.Unlock()

			go func(agentID string, session *runtime.Session) {
				<-session.Done()

				agentState := agent.StateStopped
				sess := session.GetSession()
				if sess.Status == types.StatusError {
					agentState = agent.StateError
				}

				updateCtx := context.Background()
				updateStore, err := agentstore.Open(updateCtx, s.cfg.Storage.Root)
				if err != nil {
					slog.Warn("failed to open agent store for state update", "agent_id", agentID, "error", err)
					return
				}
				defer updateStore.Close()

				if err := updateStore.UpdateState(updateCtx, agentID, agentState); err != nil {
					slog.Warn("failed to update agent state on completion", "agent_id", agentID, "error", err)
				}

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/interfaces/chatadapter/telegram/driver.go
(lines 1-220: Telegram adapter lifecycle, long-poll startup, registration, and update loop wiring.)
```go
// Package telegram implements the ChatAdapter interface for Telegram using the Bot API.
package telegram

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/joshka0/foxctl/internal/domain/identity"
	"github.com/joshka0/foxctl/internal/interfaces/chatadapter"
	"github.com/joshka0/foxctl/internal/interfaces/web/sse"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/runtime/observability"
)

// Adapter implements chatadapter.ChatAdapter for Telegram.
//
// Notes:
// - Telegram doesn't have typed slash-command schemas like Discord; we parse "/cmd args" manually.
// - Telegram has no true ephemeral messages; CommandEvent.Respond sends a normal reply.
type Adapter struct {
	token     string
	daemonURL string

	httpClient *http.Client
	clock      chatadapter.Clock
	cfg        config.TelegramSettings

	bot         *tgbotapi.BotAPI
	botUsername string
	botID       int64

	cmdHandler chatadapter.CommandHandler
	intHandler chatadapter.InteractionHandler
	msgHandler chatadapter.MessageHandler

	sseHub    *sse.Hub
	sseClient *sse.Client

	agentThreads sync.Map // agentID -> agentThread (root msg + throttling state)
	agentRootIdx sync.Map // "<chatID>:<messageID>" -> agentRootIndexEntry; for reply-based agent chat

	msgSem chan struct{} // limits concurrent command/message handlers

	mu sync.Mutex

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// defaultClock implements chatadapter.Clock using real time.
type defaultClock struct{}

func (defaultClock) Now() time.Time { return time.Now() }

const (
	agentIndexTTL             = 24 * time.Hour
	agentIndexJanitorInterval = 30 * time.Minute
)

type agentRootIndexEntry struct {
	AgentID  string
	LastSeen time.Time
}

// New creates a Telegram adapter with the given config, daemon URL, and optional clock.
// If clk is nil, a real-time clock is used.
func New(cfg config.TelegramSettings, daemonURL string, clk chatadapter.Clock) *Adapter {
	limit := cfg.MaxConcurrentMessages
	if limit <= 0 {
		limit = 10
	}

	if clk == nil {
		clk = defaultClock{}
	}

	return &Adapter{
		token:      cfg.BotToken,
		daemonURL:  daemonURL,
		httpClient: &http.Client{Timeout: 15 * time.Second},
		clock:      clk,
		cfg:        cfg,
		msgSem:     make(chan struct{}, limit),
	}
}

func (a *Adapter) Name() string { return "telegram" }

// SetSSEHub configures the SSE hub for activity event routing (Phase 2).
func (a *Adapter) SetSSEHub(hub *sse.Hub) {
	a.sseHub = hub
}

// OnCommand sets the command handler for "/cmd args" messages.
func (a *Adapter) OnCommand(handler chatadapter.CommandHandler) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cmdHandler = handler
}

// OnInteraction sets the inline keyboard (callback query) handler.
func (a *Adapter) OnInteraction(handler chatadapter.InteractionHandler) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.intHandler = handler
}

// OnMessage sets the natural language message handler (Phase 3).
func (a *Adapter) OnMessage(handler chatadapter.MessageHandler) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.msgHandler = handler
}

// Connect initializes the Telegram bot and starts the long polling update loop.
func (a *Adapter) Connect(ctx context.Context) error {
	// Ensure we always have a non-nil parent context. Handlers derive per-request
	// contexts from a.ctx, and context.WithCancel/WithTimeout will panic if given nil.
	if ctx == nil {
		ctx = context.Background()
	}
	a.ctx, a.cancel = context.WithCancel(ctx)

	token := strings.TrimSpace(a.token)
	if token == "" {
		return fmt.Errorf("telegram: missing TELEGRAM_BOT_TOKEN")
	}

	bot, err := tgbotapi.NewBotAPIWithClient(token, tgbotapi.APIEndpoint, a.httpClient)
	if err != nil {
		if a.cancel != nil {
			a.cancel()
		}
		return fmt.Errorf("telegram: create bot: %w", err)
	}

	a.mu.Lock()
	a.bot = bot
	a.botUsername = bot.Self.UserName
	a.botID = bot.Self.ID
	a.mu.Unlock()

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60 // long polling timeout (seconds)
	updates := bot.GetUpdatesChan(u)

	a.wg.Add(1)
	go a.updateLoop(updates)

	// Start SSE event listener if hub is configured.
	if a.sseHub != nil {
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			a.startEventListener(a.ctx)
		}()
	}

	// Periodically evict old agent thread/index entries (prevents unbounded growth).
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		a.agentIndexJanitor(a.ctx)
	}()

	observability.Emit(ctx, observability.NewEvent("telegram.connected").
		WithComponent("telegram").
		WithData("username", a.botUsername).
		Success(0))

	return nil
}

// Disconnect stops the polling loop and cancels any in-flight handlers.
func (a *Adapter) Disconnect(ctx context.Context) error {
	a.mu.Lock()
	bot := a.bot
	a.bot = nil
	if a.sseHub != nil && a.sseClient != nil {
		a.sseHub.Unregister(a.sseClient)
		a.sseClient = nil
	}
	a.mu.Unlock()

	if a.cancel != nil {
		a.cancel()
	}
	if bot != nil {
		bot.StopReceivingUpdates()
	}
	a.wg.Wait()

	observability.Emit(ctx, observability.NewEvent("telegram.disconnected").
		WithComponent("telegram").
		Success(0))

	return nil
}

// RegisterCommands registers bot commands (names + descriptions). Telegram does not support typed params.
func (a *Adapter) RegisterCommands(ctx context.Context, cmds []chatadapter.CommandDef) error {
	a.mu.Lock()
	bot := a.bot
	a.mu.Unlock()
	if bot == nil {
		return fmt.Errorf("telegram: not connected")
	}

	tgCmds := make([]tgbotapi.BotCommand, 0, len(cmds))
	for _, c := range cmds {
		name := strings.TrimSpace(c.Name)

```

(lines 260-700: Telegram message/command/callback routing, agent reply-thread dispatch, and chat gating logic for first-class channel behavior.)
```go
	default:
		// ignore unsupported updates for now
	}
}

func (a *Adapter) handleMessage(m *tgbotapi.Message) {
	if m == nil || m.Chat == nil {
		return
	}
	if m.From == nil || m.From.IsBot {
		return
	}

	text := strings.TrimSpace(m.Text)
	if text == "" {
		// ignore non-text messages for MVP (photos, stickers, etc.)
		return
	}

	// tgbotapi/v5 doesn't expose Telegram forum topic fields (message_thread_id).
	// For now we key sessions by chat only and rely on reply_to_message_id to keep
	// responses in the same topic when applicable.
	chatKey := channelKey(m.Chat.ID, 0)

	// Commands take precedence over natural language chat.
	if m.IsCommand() {
		a.dispatchCommand(m, chatKey)
		return
	}

	// If this is a reply to one of our agent "thread" messages, route it to the
	// agent chat endpoint instead of the console session bridge.
	if m.ReplyToMessage != nil {
		rootKey := agentRootKey(m.Chat.ID, m.ReplyToMessage.MessageID)
		if v, ok := a.agentRootIdx.Load(rootKey); ok {
			switch ent := v.(type) {
			case agentRootIndexEntry:
				agentID := strings.TrimSpace(ent.AgentID)
				if agentID != "" {
					ent.LastSeen = a.clock.Now()
					a.agentRootIdx.Store(rootKey, ent)
					a.dispatchAgentAsk(m, agentID, text)
					return
				}
			case string:
				// Backward compatible (in-memory only) with older entry types.
				agentID := strings.TrimSpace(ent)
				if agentID != "" {
					a.agentRootIdx.Store(rootKey, agentRootIndexEntry{AgentID: agentID, LastSeen: a.clock.Now()})
					a.dispatchAgentAsk(m, agentID, text)
					return
				}
			}
		}
	}

	a.dispatchMessage(m, chatKey, text)
}

func (a *Adapter) dispatchCommand(m *tgbotapi.Message, chatKey string) {
	a.mu.Lock()
	handler := a.cmdHandler
	a.mu.Unlock()

	cmd := strings.TrimSpace(m.Command())
	if cmd == "" {
		return
	}
	// Telegram recommends underscores in command names; normalize for our router.
	cmd = strings.ReplaceAll(cmd, "_", "-")
	args := strings.TrimSpace(m.CommandArguments())

	// Adapter-local helper commands (not routed to foxctl).
	switch cmd {
	case "chat-id":
		content := fmt.Sprintf("chat_id: %d\nchannel_key: %s", m.Chat.ID, chatKey)
		_, _ = a.sendMessage(m.Chat.ID, m.MessageID, content, nil)
		return
	}

	if handler == nil {
		return
	}

	opts := parseCommandOptions(cmd, args)

	user := chatadapter.UserRef{
		ID:       strconv.FormatInt(m.From.ID, 10),
		Username: displayName(m.From),
	}

	respond := func(content string, _ []chatadapter.Embed) error {
		_, err := a.sendMessage(m.Chat.ID, m.MessageID, content, nil)
		return err
	}

	evt := chatadapter.NewCommandEvent(cmd, opts, user, chatKey, "", respond)

	a.dispatchWithLimit("telegram.command", m.Chat.ID, func(ctx context.Context) error {
		return handler(ctx, evt)
	})
}

func (a *Adapter) dispatchMessage(m *tgbotapi.Message, chatKey string, content string) {
	a.mu.Lock()
	handler := a.msgHandler
	botUsername := a.botUsername
	botID := a.botID
	a.mu.Unlock()
	if handler == nil {
		return
	}

	// Determine if we should respond:
	// - Private chats: respond to all messages
	// - Allowlisted chats: respond to all messages
	// - Otherwise: respond only if mentioned or replying to the bot
	mentioned := botUsername != "" && strings.Contains(content, "@"+botUsername)
	replyingToBot := m.ReplyToMessage != nil &&
		m.ReplyToMessage.From != nil &&
		(m.ReplyToMessage.From.ID == botID ||
			(botUsername != "" && m.ReplyToMessage.From.UserName == botUsername))
	inChat := a.isChatID(m.Chat.ID)
	isPrivate := m.Chat.IsPrivate()

	if !isPrivate && !inChat && !mentioned && !replyingToBot {
		return
	}

	if mentioned && !inChat && !isPrivate {
		// If we're only responding due to a mention, strip the mention from the content.
		content = cleanMention(content, botUsername)
	}

	if strings.TrimSpace(content) == "" {
		return
	}

	user := chatadapter.UserRef{
		ID:       strconv.FormatInt(m.From.ID, 10),
		Username: displayName(m.From),
	}

	principal := identity.Principal{
		UserID:   strconv.FormatInt(m.From.ID, 10),
		Username: displayName(m.From),
		Platform: "telegram",
	}

	respond := func(content string, _ []chatadapter.Embed) (chatadapter.MessageRef, error) {
		sentID, err := a.sendMessage(m.Chat.ID, m.MessageID, content, nil)
		if err != nil {
			return chatadapter.MessageRef{}, err
		}
		return chatadapter.MessageRef{
			ChannelID: chatKey,
			MessageID: strconv.Itoa(sentID),
		}, nil
	}

	edit := func(ref chatadapter.MessageRef, content string, _ []chatadapter.Embed) error {
		chatID, _, ok := parseChannelKey(ref.ChannelID)
		if !ok {
			return fmt.Errorf("telegram: invalid channel ref %q", ref.ChannelID)
		}
		msgID, err := strconv.Atoi(ref.MessageID)
		if err != nil {
			return fmt.Errorf("telegram: invalid message ref %q: %w", ref.MessageID, err)
		}
		return a.editMessage(chatID, msgID, content, nil)
	}

	evt := chatadapter.NewMessageEvent(content, user, chatKey, "", strconv.Itoa(m.MessageID), respond, edit)
	evt.Principal = principal

	a.dispatchWithLimit("telegram.message", m.Chat.ID, func(ctx context.Context) error {
		return handler(ctx, evt)
	})
}

func (a *Adapter) dispatchAgentAsk(m *tgbotapi.Message, agentID string, content string) {
	agentID = strings.TrimSpace(agentID)
	content = strings.TrimSpace(content)
	if agentID == "" || content == "" || m == nil || m.Chat == nil {
		return
	}

	a.dispatchWithLimit("telegram.agent_ask", m.Chat.ID, func(ctx context.Context) error {
		// Best-effort typing indicator while we call the agent.
		a.ShowTyping(ctx, channelKey(m.Chat.ID, 0))

		reply, err := a.askAgent(ctx, agentID, content)
		if err != nil {
			_, sendErr := a.sendMessage(m.Chat.ID, m.MessageID, "Ask failed: "+err.Error(), nil)
			if sendErr != nil {
				return sendErr
			}
			return err
		}

		sentID, err := a.sendMessage(m.Chat.ID, m.MessageID, reply, telegramKeyboardDetails(agentID))
		if err == nil && sentID > 0 {
			a.agentRootIdx.Store(agentRootKey(m.Chat.ID, sentID), agentRootIndexEntry{AgentID: agentID, LastSeen: a.clock.Now()})
		}
		return err
	})
}

func (a *Adapter) handleCallbackQuery(q *tgbotapi.CallbackQuery) {
	if q == nil || q.From == nil || q.Message == nil || q.Message.Chat == nil {
		return
	}
	if q.From.IsBot {
		return
	}

	a.mu.Lock()
	handler := a.intHandler
	a.mu.Unlock()
	if handler == nil {
		return
	}

	chatKey := channelKey(q.Message.Chat.ID, 0)

	user := chatadapter.UserRef{
		ID:       strconv.FormatInt(q.From.ID, 10),
		Username: displayName(q.From),
	}

	msgRef := chatadapter.MessageRef{
		ChannelID: chatKey,
		MessageID: strconv.Itoa(q.Message.MessageID),
	}

	respond := func(content string, _ []chatadapter.Embed) error {
		// Send a normal message (Telegram has no ephemeral). Best-effort ack the callback query.
		_ = a.answerCallbackQuery(q.ID, "")
		_, err := a.sendMessage(q.Message.Chat.ID, q.Message.MessageID, content, nil)
		return err
	}

	update := func(content string, _ []chatadapter.Embed, _ []chatadapter.Component) error {
		_ = a.answerCallbackQuery(q.ID, "")
		return a.editMessage(q.Message.Chat.ID, q.Message.MessageID, content, nil)
	}

	evt := chatadapter.NewInteractionEvent("button", q.Data, user, chatKey, "", msgRef, respond, update)

	a.dispatchWithLimit("telegram.interaction", q.Message.Chat.ID, func(ctx context.Context) error {
		return handler(ctx, evt)
	})
}

func (a *Adapter) dispatchWithLimit(op string, chatID int64, fn func(ctx context.Context) error) {
	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}

	select {
	case a.msgSem <- struct{}{}:
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			defer func() { <-a.msgSem }()

			handlerCtx, cancel := context.WithTimeout(parent, 5*time.Minute)
			defer cancel()

			if err := fn(handlerCtx); err != nil {
				observability.Emit(handlerCtx, observability.NewEvent(op+"_error").
					WithComponent("telegram").
					WithData("chat_id", chatID).
					Error(err, 0))
			}
		}()
	default:
		observability.Emit(parent, observability.NewEvent(op+"_dropped").
			WithComponent("telegram").
			WithData("chat_id", chatID).
			WithData("reason", "concurrency limit reached").
			Success(0))
	}
}

func (a *Adapter) isChatID(chatID int64) bool {
	for _, id := range a.cfg.ChatIDs {
		if id == chatID {
			return true
		}
	}
	return false
}

func displayName(u *tgbotapi.User) string {
	if u == nil {
		return ""
	}
	if strings.TrimSpace(u.UserName) != "" {
		return u.UserName
	}
	name := strings.TrimSpace(strings.TrimSpace(u.FirstName) + " " + strings.TrimSpace(u.LastName))
	if name != "" {
		return name
	}
	return strconv.FormatInt(u.ID, 10)
}

func cleanMention(content string, botUsername string) string {
	botUsername = strings.TrimSpace(botUsername)
	if botUsername == "" {
		return strings.TrimSpace(content)
	}
	return strings.TrimSpace(strings.ReplaceAll(content, "@"+botUsername, ""))
}

func channelKey(chatID int64, threadID int) string {
	if threadID > 0 {
		return fmt.Sprintf("%d:%d", chatID, threadID)
	}
	return fmt.Sprintf("%d", chatID)
}

func agentRootKey(chatID int64, messageID int) string {
	return fmt.Sprintf("%d:%d", chatID, messageID)
}

func parseChannelKey(key string) (chatID int64, threadID int, ok bool) {
	key = strings.TrimSpace(key)
	if key == "" {
		return 0, 0, false
	}
	parts := strings.SplitN(key, ":", 2)
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	if len(parts) == 1 {
		return id, 0, true
	}
	tid, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, false
	}
	return id, tid, true
}

func parseCommandOptions(cmd string, args string) map[string]any {
	// Best-effort parsing for MVP commands.
	opts := make(map[string]any)
	switch cmd {
	case "search", "memory":
		if strings.TrimSpace(args) != "" {
			opts["query"] = strings.TrimSpace(args)
		}
	case "logs":
		if strings.Contains(strings.ToLower(args), "error") {
			opts["errors_only"] = true
		}
	case "todo":
		action := "list"
		if strings.TrimSpace(args) != "" {
			fields := strings.Fields(args)
			if len(fields) > 0 {
				action = fields[0]
			}
		}
		opts["action"] = action
		switch action {
		case "add":
			title := strings.TrimSpace(strings.TrimPrefix(args, "add"))
			title = strings.TrimSpace(title)
			if title != "" {
				opts["title"] = title
			}
		case "complete":
			fields := strings.Fields(args)
			if len(fields) >= 2 {
				opts["id"] = fields[1]
			}
		}
	case "agent-spawn":
		fields := strings.Fields(args)
		if len(fields) > 0 {
			opts["role"] = fields[0]
			opts["prompt"] = strings.TrimSpace(strings.TrimPrefix(args, fields[0]))
		}
	case "agent-list":
		// no args
	default:
		// pass raw args through for debugging
		if strings.TrimSpace(args) != "" {
			opts["args"] = strings.TrimSpace(args)
		}
	}
	return opts
}

func (a *Adapter) sendMessage(chatID int64, replyToMsgID int, content string, keyboard *tgbotapi.InlineKeyboardMarkup) (int, error) {
	a.mu.Lock()
	bot := a.bot
	a.mu.Unlock()
	if bot == nil {
		return 0, fmt.Errorf("telegram: not connected")
	}

	content = truncateForTelegram(content)

	msg := tgbotapi.NewMessage(chatID, content)
	msg.DisableWebPagePreview = true
	if replyToMsgID > 0 {
		msg.ReplyToMessageID = replyToMsgID
		msg.AllowSendingWithoutReply = true
	}
	if keyboard != nil {
		msg.ReplyMarkup = keyboard
	}

	sent, err := bot.Send(msg)
	if err != nil {
		if d, ok := retryAfterDelay(err); ok {
			if !sleepWithContext(a.ctx, d) {
				return 0, fmt.Errorf("telegram: send message: %w", err)
			}
			sent, err = bot.Send(msg)
		}
		if err != nil {
			return 0, fmt.Errorf("telegram: send message: %w", err)
		}
	}
	return sent.MessageID, nil
}

func (a *Adapter) editMessage(chatID int64, messageID int, content string, keyboard *tgbotapi.InlineKeyboardMarkup) error {
	a.mu.Lock()
	bot := a.bot
	a.mu.Unlock()
	if bot == nil {
		return fmt.Errorf("telegram: not connected")
	}

```

(lines 740-834: Typing indicator and janitor/index maintenance behavior used for operational robustness.)
```go

func sleepWithContext(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	if ctx == nil {
		ctx = context.Background()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (a *Adapter) answerCallbackQuery(callbackID string, text string) error {
	a.mu.Lock()
	bot := a.bot
	a.mu.Unlock()
	if bot == nil {
		return fmt.Errorf("telegram: not connected")
	}
	cfg := tgbotapi.NewCallback(callbackID, text)
	_, err := bot.Request(cfg)
	return err
}

// ShowTyping sends a typing indicator to the chat for best-effort UX, honoring context cancellation.
func (a *Adapter) ShowTyping(ctx context.Context, channelID string) {
	chatID, _, ok := parseChannelKey(channelID)
	if !ok {
		return
	}

	select {
	case <-ctx.Done():
		return
	default:
	}

	a.mu.Lock()
	bot := a.bot
	a.mu.Unlock()
	if bot == nil {
		return
	}

	_, _ = bot.Request(tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping))
}

func (a *Adapter) agentIndexJanitor(ctx context.Context) {
	ticker := time.NewTicker(agentIndexJanitorInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.evictAgentIndexes(a.clock.Now())
		}
	}
}

func (a *Adapter) evictAgentIndexes(now time.Time) {
	cutoff := now.Add(-agentIndexTTL)

	a.agentThreads.Range(func(key any, value any) bool {
		st, ok := value.(agentThread)
		if !ok {
			a.agentThreads.Delete(key)
			return true
		}
		if st.LastSeen.IsZero() || st.LastSeen.Before(cutoff) {
			a.agentThreads.Delete(key)
		}
		return true
	})

	a.agentRootIdx.Range(func(key any, value any) bool {
		ent, ok := value.(agentRootIndexEntry)
		if !ok {
			a.agentRootIdx.Delete(key)
			return true
		}
		if strings.TrimSpace(ent.AgentID) == "" || ent.LastSeen.IsZero() || ent.LastSeen.Before(cutoff) {
			a.agentRootIdx.Delete(key)
		}
		return true
	})
}

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/interfaces/chatadapter/teams/driver.go
(lines 1-420: Teams adapter core lifecycle, Connect/Disconnect, webhook entrypoint, tenant-scoped conversation mapping, gating rules, and dispatch fanout.)
```go
// Package teams implements the ChatAdapter interface for Microsoft Teams via the Bot Framework.
package teams

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/joshka0/foxctl/internal/domain/identity"
	"github.com/joshka0/foxctl/internal/interfaces/chatadapter"
	"github.com/joshka0/foxctl/internal/interfaces/web/sse"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/runtime/observability"
	"github.com/joshka0/foxctl/internal/storage/convref"
)

// serviceURLEntry holds the normalized service URL together with the raw
// (platform-native) conversation ID so that outbound Bot Framework API calls
// can use the raw ID while the map key uses a tenant-scoped ConversationKey.
type serviceURLEntry struct {
	url       string // normalized service URL
	rawConvID string // platform-native conversation ID (not tenant-scoped)
}

// Adapter implements chatadapter.ChatAdapter for Microsoft Teams (Bot Framework webhook).
type Adapter struct {
	daemonURL string
	cfg       config.TeamsSettings

	httpClient *http.Client
	tokenMgr   *tokenManager
	botClient  *BotClient
	verifier   JWTVerifier
	clock      chatadapter.Clock

	cmdHandler chatadapter.CommandHandler
	intHandler chatadapter.InteractionHandler
	msgHandler chatadapter.MessageHandler

	sseHub    *sse.Hub
	sseClient *sse.Client

	// persistent conversation refs (optional)
	convRefStore convref.Store

	// convKey (tenant-scoped) -> serviceURLEntry
	serviceURLs sync.Map

	agentThreads sync.Map // agentID -> agentThread (root card + throttling state)
	agentRootIdx sync.Map // "convKey:activityID" -> agentRootIndexEntry; for reply-based agent chat

	msgSem chan struct{}

	mu sync.Mutex

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// defaultClock implements chatadapter.Clock using real time.
type defaultClock struct{}

func (defaultClock) Now() time.Time { return time.Now() }

// New creates a Teams adapter with the given config, daemon URL, and optional clock.
// If clk is nil, a real-time clock is used.
func New(cfg config.TeamsSettings, daemonURL string, clk chatadapter.Clock) *Adapter {
	limit := cfg.MaxConcurrentMessages
	if limit <= 0 {
		limit = 10
	}

	if clk == nil {
		clk = defaultClock{}
	}

	return &Adapter{
		daemonURL:  daemonURL,
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 15 * time.Second},
		clock:      clk,
		msgSem:     make(chan struct{}, limit),
	}
}

// Name returns the adapter identifier ("teams").
func (a *Adapter) Name() string { return "teams" }

// SetSSEHub configures the SSE hub for proactive messaging.
func (a *Adapter) SetSSEHub(hub *sse.Hub) { a.sseHub = hub }

// SetConvRefStore configures the persistent conversation reference store.
func (a *Adapter) SetConvRefStore(store convref.Store) { a.convRefStore = store }

// OnCommand sets the handler for incoming text commands ("/cmd args").
func (a *Adapter) OnCommand(handler chatadapter.CommandHandler) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cmdHandler = handler
}

// OnInteraction sets the handler for interactive components (not used in MVP).
func (a *Adapter) OnInteraction(handler chatadapter.InteractionHandler) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.intHandler = handler
}

// OnMessage sets the handler for natural language messages (Phase 3).
func (a *Adapter) OnMessage(handler chatadapter.MessageHandler) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.msgHandler = handler
}

// Connect validates config and initializes dependencies. There is no long-lived connection.
func (a *Adapter) Connect(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	a.ctx, a.cancel = context.WithCancel(ctx)

	if strings.TrimSpace(a.cfg.TenantID) == "" {
		return fmt.Errorf("teams: missing TenantID (TEAMS_TENANT_ID)")
	}
	if strings.TrimSpace(a.cfg.ClientID) == "" {
		return fmt.Errorf("teams: missing ClientID (TEAMS_CLIENT_ID)")
	}
	if strings.TrimSpace(a.cfg.ClientSecret) == "" {
		return fmt.Errorf("teams: missing ClientSecret (TEAMS_CLIENT_SECRET)")
	}

	a.tokenMgr = newTokenManager(a.cfg.ClientID, a.cfg.ClientSecret, a.httpClient)
	a.botClient = newBotClient(a.tokenMgr, a.httpClient)

	if a.cfg.SkipJWTVerify {
		a.verifier = nopJWTVerifier{}
		observability.Emit(ctx, observability.NewEvent("teams.jwt_verify_skipped").
			WithComponent("teams").
			WithData("warning", "TEAMS_SKIP_JWT_VERIFY is enabled (dev-only); inbound webhook auth is disabled").
			Success(0))
	} else {
		a.verifier = newJWTVerifier(a.cfg.ClientID, a.cfg.TenantID, a.httpClient)
	}

	// Start SSE event listener if hub is configured.
	if a.sseHub != nil {
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			a.startEventListener(a.ctx)
		}()
	}

	// Periodically evict old agent thread/index entries.
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		a.agentIndexJanitor(a.ctx)
	}()

	observability.Emit(ctx, observability.NewEvent("teams.connected").
		WithComponent("teams").
		Success(0))
	return nil
}

// Disconnect cancels in-flight handlers and waits for background goroutines.
func (a *Adapter) Disconnect(ctx context.Context) error {
	if a.cancel != nil {
		a.cancel()
	}
	a.wg.Wait()

	a.serviceURLs.Range(func(key, _ any) bool {
		a.serviceURLs.Delete(key)
		return true
	})

	observability.Emit(ctx, observability.NewEvent("teams.disconnected").
		WithComponent("teams").
		Success(0))
	return nil
}

// RegisterCommands is a no-op for Teams (commands are parsed from message text).
func (a *Adapter) RegisterCommands(_ context.Context, _ []chatadapter.CommandDef) error { return nil }

// ShowTyping sends a typing indicator to a conversation (best-effort).
// channelID is the tenant-scoped conversation key (as stored in serviceURLs).
func (a *Adapter) ShowTyping(ctx context.Context, channelID string) {
	if a.botClient == nil {
		return
	}
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		return
	}

	var entry serviceURLEntry
	if v, ok := a.serviceURLs.Load(channelID); ok {
		cachedEntry, cachedOK := v.(serviceURLEntry)
		if !cachedOK {
			return
		}
		entry = cachedEntry
	} else if a.convRefStore != nil {
		ref, err := a.convRefStore.Get(ctx, channelID)
		if err == nil && ref != nil {
			entry = serviceURLEntry{
				url:       ref.ServiceURL,
				rawConvID: ref.RawConversationID,
			}
			// Populate cache for subsequent lookups.
			a.serviceURLs.Store(channelID, entry)
		}
	}
	if strings.TrimSpace(entry.url) == "" {
		return
	}

	typingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	// SendTyping requires the raw (platform-native) conversation ID for the
	// Bot Framework API URL, not the tenant-scoped key.
	_ = a.botClient.SendTyping(typingCtx, entry.url, entry.rawConvID)
}

// HTTPHandler returns the webhook handler for incoming Bot Framework activities.
func (a *Adapter) HTTPHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		raw, err := io.ReadAll(io.LimitReader(r.Body, 2*1024*1024))
		if err != nil {
			http.Error(w, "read body failed", http.StatusBadRequest)
			return
		}

		var activity Activity
		if err := json.Unmarshal(raw, &activity); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}

		// Verify inbound auth before ack.
		if a.verifier != nil {
			if err := a.verifier.Verify(r.Context(), r.Header.Get("Authorization")); err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				observability.Emit(r.Context(), observability.NewEvent("teams.webhook_unauthorized").
					WithComponent("teams").
					Error(err, 0))
				return
			}
		}

		convID := strings.TrimSpace(activity.Conversation.ID)
		if convID == "" {
			http.Error(w, "missing conversation id", http.StatusBadRequest)
			return
		}

		serviceURL, err := normalizeServiceURL(activity.ServiceURL)
		if err != nil {
			http.Error(w, "untrusted serviceUrl", http.StatusBadRequest)
			observability.Emit(r.Context(), observability.NewEvent("teams.untrusted_service_url").
				WithComponent("teams").
				Error(err, 0))
			return
		}

		// Construct Principal early so the tenant-scoped conversation key
		// is available before any map stores or gating checks. This prevents
		// two tenants with colliding raw conversation IDs from overwriting
		// each other's service URL.
		tenantID := extractTenantID(activity, a.cfg.TenantID)
		if tenantID == "" {
			// Not fatal: single-tenant deployments may not have tenant IDs. This can cause
			// cross-tenant collisions if multiple tenants share conversation IDs.
			observability.Emit(r.Context(), observability.NewEvent("teams.missing_tenant_id").
				WithComponent("teams").
				WithData("conversation_id", convID).
				WithData("from_id", strings.TrimSpace(activity.From.ID)).
				WithData("warning", "missing tenant id; conversation key will be unscoped").
				Success(0))
		}
		principal := identity.Principal{
			TenantID: tenantID,
			UserID:   strings.TrimSpace(activity.From.ID),
			Username: strings.TrimSpace(activity.From.Name),
			Platform: "teams",
		}
		convKey := principal.ConversationKey(convID)

		a.serviceURLs.Store(convKey, serviceURLEntry{
			url:       serviceURL,
			rawConvID: convID,
		})
		if a.convRefStore != nil {
			_ = a.convRefStore.Upsert(r.Context(), convref.Ref{
				ConversationKey:   convKey,
				Platform:          "teams",
				TenantID:          tenantID,
				RawConversationID: convID,
				ServiceURL:        serviceURL,
				LastActivityID:    strings.TrimSpace(activity.ID),
				BotID:             strings.TrimSpace(activity.Recipient.ID),
			})
		}

		// Route invoke activities (Adaptive Card Action.Submit).
		if strings.TrimSpace(activity.Type) == "invoke" {
			w.WriteHeader(http.StatusOK)
			a.dispatchWithLimit(a.ctx, "teams.invoke", convKey, func(ctx context.Context) error {
				return a.handleInvoke(ctx, serviceURL, activity, convKey)
			})
			return
		}

		// Ignore non-message activities.
		if strings.TrimSpace(activity.Type) != "message" {
			w.WriteHeader(http.StatusOK)
			return
		}

		// Ignore self-messages.
		if strings.TrimSpace(activity.From.ID) != "" && strings.TrimSpace(activity.From.ID) == strings.TrimSpace(activity.Recipient.ID) {
			w.WriteHeader(http.StatusOK)
			return
		}

		text := strings.TrimSpace(activity.Text)
		if text == "" {
			w.WriteHeader(http.StatusOK)
			return
		}

		// The allowlist in config uses raw (platform-native) conversation IDs,
		// so we check against the raw convID here.
		allowlisted := a.isChatConversation(convID)
		isOneToOne := !activity.Conversation.IsGroup
		mentioned := isBotMentioned(activity)

		if !allowlisted && !isOneToOne && !mentioned {
			// Enterprise default: do not respond in channels unless explicitly allowlisted or mentioned.
			w.WriteHeader(http.StatusOK)
			return
		}

		// Clean bot mention markup from the message text.
		text = stripBotMentions(activity, text)
		if strings.TrimSpace(text) == "" {
			w.WriteHeader(http.StatusOK)
			return
		}

		// Fast ack: Teams expects a quick 200 OK.
		w.WriteHeader(http.StatusOK)

		// Guard: adapter must be fully initialized via Connect() before dispatching.
		if a.botClient == nil {
			observability.Emit(r.Context(), observability.NewEvent("teams.webhook_uninitialized").
				WithComponent("teams").
				WithData("conversation_id", convKey).
				Error(fmt.Errorf("botClient is nil; Connect() not called"), 0))
			return
		}

		// Check if this is a reply to a known agent root card.
		if replyToID := strings.TrimSpace(activity.ReplyToID); replyToID != "" {
			key := agentRootKey(convKey, replyToID)
			if entry, ok := a.agentRootIdx.Load(key); ok {
				if idx, ok := entry.(agentRootIndexEntry); ok && idx.AgentID != "" {
					a.dispatchAgentAsk(a.ctx, serviceURL, activity, convKey, idx.AgentID, text)
					return
				}
			}
		}

		a.dispatchWithLimit(a.ctx, "teams.message", convKey, func(ctx context.Context) error {
			// Text commands: "/cmd args"
			if cmd, args, ok := parseTextCommand(text); ok {
				return a.handleCommand(ctx, serviceURL, activity, principal, convKey, cmd, args)
			}
			return a.handleMessage(ctx, serviceURL, activity, principal, convKey, text)
		})
	}
}

func extractTenantID(activity Activity, cfgTenantID string) string {
	if tid := strings.TrimSpace(activity.Conversation.TenantID); tid != "" {
		return tid
	}

	// Teams sends channelData.tenant.id in all contexts (conversation.tenantId may be absent).
	if len(activity.ChannelData) > 0 {
		var cd struct {
			Tenant struct {
				ID string `json:"id"`
			} `json:"tenant"`
		}
		if err := json.Unmarshal(activity.ChannelData, &cd); err == nil {
			if tid := strings.TrimSpace(cd.Tenant.ID); tid != "" {
				return tid
			}
		}
	}

	return strings.TrimSpace(cfgTenantID)
}

```

(lines 457-671: Teams command/message handlers and command option parsing that bridge inbound chat payloads to chatadapter events and daemon-facing actions.)
```go
func (a *Adapter) handleCommand(ctx context.Context, serviceURL string, activity Activity, principal identity.Principal, convKey string, cmd string, args string) error {
	a.mu.Lock()
	handler := a.cmdHandler
	a.mu.Unlock()
	if handler == nil {
		return nil
	}

	// Raw conversation ID for Bot Framework API calls.
	rawConvID := strings.TrimSpace(activity.Conversation.ID)
	if rawConvID == "" {
		return fmt.Errorf("teams: missing conversation id")
	}

	user := chatadapter.UserRef{
		ID:       principal.UserID,
		Username: principal.Username,
	}
	if user.Username == "" {
		user.Username = user.ID
	}

	respond := func(content string, _ []chatadapter.Embed) error {
		if strings.TrimSpace(content) == "" {
			return nil
		}
		out := Activity{Type: "message", Text: content}
		replyTo := strings.TrimSpace(activity.ID)
		if replyTo != "" {
			_, err := a.botClient.ReplyToActivity(ctx, serviceURL, rawConvID, replyTo, out)
			return err
		}
		_, err := a.botClient.SendActivity(ctx, serviceURL, rawConvID, out)
		return err
	}

	opts := parseCommandOptions(cmd, args)
	// Use convKey (tenant-scoped) as the event's ChannelID for consistent
	// session bridging and serviceURLs lookups.
	evt := chatadapter.NewCommandEvent(cmd, opts, user, convKey, "", respond)
	evt.Principal = principal
	return handler(ctx, evt)
}

func (a *Adapter) handleMessage(ctx context.Context, serviceURL string, activity Activity, principal identity.Principal, convKey string, content string) error {
	a.mu.Lock()
	handler := a.msgHandler
	a.mu.Unlock()
	if handler == nil {
		return nil
	}

	// Raw conversation ID for Bot Framework API calls.
	rawConvID := strings.TrimSpace(activity.Conversation.ID)
	if rawConvID == "" {
		return fmt.Errorf("teams: missing conversation id")
	}

	user := chatadapter.UserRef{
		ID:       principal.UserID,
		Username: principal.Username,
	}
	if user.Username == "" {
		user.Username = user.ID
	}

	respond := func(content string, _ []chatadapter.Embed) (chatadapter.MessageRef, error) {
		out := Activity{Type: "message", Text: content}
		replyTo := strings.TrimSpace(activity.ID)
		var rr ResourceResponse
		var err error
		if replyTo != "" {
			rr, err = a.botClient.ReplyToActivity(ctx, serviceURL, rawConvID, replyTo, out)
		} else {
			rr, err = a.botClient.SendActivity(ctx, serviceURL, rawConvID, out)
		}
		if err != nil {
			return chatadapter.MessageRef{}, err
		}
		// MessageRef uses convKey for consistent lookups.
		return chatadapter.MessageRef{ChannelID: convKey, MessageID: rr.ID}, nil
	}

	edit := func(ref chatadapter.MessageRef, content string, _ []chatadapter.Embed) error {
		return a.botClient.UpdateActivity(ctx, serviceURL, rawConvID, ref.MessageID, Activity{Type: "message", Text: content})
	}

	// Use convKey (tenant-scoped) as the event's ChannelID for consistent
	// session bridging and serviceURLs lookups.
	evt := chatadapter.NewMessageEvent(content, user, convKey, "", strings.TrimSpace(activity.ID), respond, edit)
	evt.Principal = principal
	return handler(ctx, evt)
}

func (a *Adapter) isChatConversation(conversationID string) bool {
	for _, id := range a.cfg.ChatConversationIDs {
		if strings.TrimSpace(id) == conversationID {
			return true
		}
	}
	return false
}

func isBotMentioned(a Activity) bool {
	botID := strings.TrimSpace(a.Recipient.ID)
	if botID == "" {
		return false
	}
	for _, e := range a.Entities {
		if strings.EqualFold(strings.TrimSpace(e.Type), "mention") && strings.TrimSpace(e.Mentioned.ID) == botID {
			return true
		}
	}
	return false
}

func stripBotMentions(a Activity, text string) string {
	botID := strings.TrimSpace(a.Recipient.ID)
	if botID == "" {
		return strings.TrimSpace(text)
	}

	out := text
	for _, e := range a.Entities {
		if !strings.EqualFold(strings.TrimSpace(e.Type), "mention") {
			continue
		}
		if strings.TrimSpace(e.Mentioned.ID) != botID {
			continue
		}
		if strings.TrimSpace(e.Text) != "" {
			out = strings.ReplaceAll(out, e.Text, "")
		}
	}

	// Best-effort: strip Teams <at> markup.
	out = strings.ReplaceAll(out, "<at>", "")
	out = strings.ReplaceAll(out, "</at>", "")

	return strings.TrimSpace(out)
}

func parseTextCommand(text string) (cmd string, args string, ok bool) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return "", "", false
	}

	fields := strings.Fields(strings.TrimPrefix(text, "/"))
	if len(fields) == 0 {
		return "", "", false
	}
	cmd = strings.TrimSpace(fields[0])
	if cmd == "" {
		return "", "", false
	}
	// Mirror Telegram normalization.
	cmd = strings.ReplaceAll(cmd, "_", "-")

	if len(fields) > 1 {
		args = strings.TrimSpace(strings.TrimPrefix(text, "/"+fields[0]))
		args = strings.TrimSpace(args)
	}
	return cmd, args, true
}

func parseCommandOptions(cmd string, args string) map[string]any {
	// Best-effort parsing for MVP commands (mirrors Telegram).
	opts := make(map[string]any)
	switch cmd {
	case "search", "memory":
		if strings.TrimSpace(args) != "" {
			opts["query"] = strings.TrimSpace(args)
		}
	case "logs":
		if strings.Contains(strings.ToLower(args), "error") {
			opts["errors_only"] = true
		}
	case "todo":
		action := "list"
		if strings.TrimSpace(args) != "" {
			fields := strings.Fields(args)
			if len(fields) > 0 {
				action = fields[0]
			}
		}
		opts["action"] = action
		switch action {
		case "add":
			title := strings.TrimSpace(strings.TrimPrefix(args, "add"))
			title = strings.TrimSpace(title)
			if title != "" {
				opts["title"] = title
			}
		case "complete":
			fields := strings.Fields(args)
			if len(fields) >= 2 {
				opts["id"] = fields[1]
			}
		}
	case "agent-spawn":
		fields := strings.Fields(args)
		if len(fields) > 0 {
			opts["role"] = fields[0]
			opts["prompt"] = strings.TrimSpace(strings.TrimPrefix(args, fields[0]))
		}
	case "agent-list":
		// no args
	default:
		if strings.TrimSpace(args) != "" {
			opts["args"] = strings.TrimSpace(args)
		}
	}
	return opts
}

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/deploy/kubernetes/overlays/public-gui/gateway-deployment.yaml
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: gui-auth-gateway
  namespace: foxctl
  labels:
    app.kubernetes.io/name: foxctl
    app.kubernetes.io/component: gui-auth-gateway
spec:
  replicas: 2
  selector:
    matchLabels:
      app.kubernetes.io/name: foxctl
      app.kubernetes.io/component: gui-auth-gateway
  template:
    metadata:
      labels:
        app.kubernetes.io/name: foxctl
        app.kubernetes.io/component: gui-auth-gateway
    spec:
      terminationGracePeriodSeconds: 30
      containers:
      - name: gui-auth-gateway
        image: foxctl-gui-auth-gateway:latest
        imagePullPolicy: IfNotPresent
        ports:
        - name: http
          containerPort: 8080
          protocol: TCP
        envFrom:
        - configMapRef:
            name: gui-auth-gateway-config
        env:
        - name: BETTER_AUTH_SECRET
          valueFrom:
            secretKeyRef:
              name: gui-auth-gateway-secrets
              key: better-auth-secret
        - name: GUI_AUTH_SMTP_USER
          valueFrom:
            secretKeyRef:
              name: gui-auth-gateway-secrets
              key: smtp-user
              optional: true
        - name: GUI_AUTH_SMTP_PASS
          valueFrom:
            secretKeyRef:
              name: gui-auth-gateway-secrets
              key: smtp-pass
              optional: true
        - name: FOXCTL_POSTGRES_DSN
          valueFrom:
            secretKeyRef:
              name: postgres-secrets
              key: dsn
              optional: true
        resources:
          requests:
            memory: "128Mi"
            cpu: "50m"
          limits:
            memory: "512Mi"
            cpu: "500m"
        livenessProbe:
          httpGet:
            path: /healthz
            port: http
          initialDelaySeconds: 10
          periodSeconds: 30
        readinessProbe:
          httpGet:
            path: /readyz
            port: http
          initialDelaySeconds: 5
          periodSeconds: 10
        securityContext:
          allowPrivilegeEscalation: false
          readOnlyRootFilesystem: true
          capabilities:
            drop:
            - ALL
        volumeMounts:
        - name: tmp
          mountPath: /tmp
      volumes:
      - name: tmp
        emptyDir:
          sizeLimit: 64Mi

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/deploy/kubernetes/overlays/public-gui/gateway-service.yaml
```yaml
apiVersion: v1
kind: Service
metadata:
  name: gui-auth-gateway
  namespace: foxctl
  labels:
    app.kubernetes.io/name: foxctl
    app.kubernetes.io/component: gui-auth-gateway
spec:
  type: ClusterIP
  ports:
  - name: http
    port: 8080
    targetPort: http
    protocol: TCP
  selector:
    app.kubernetes.io/name: foxctl
    app.kubernetes.io/component: gui-auth-gateway

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/deploy/kubernetes/base/service.yaml
```yaml
apiVersion: v1
kind: Service
metadata:
  name: foxctl
  namespace: foxctl
  labels:
    app.kubernetes.io/name: foxctl
    app.kubernetes.io/component: core
spec:
  type: ClusterIP
  ports:
  - name: http
    port: 8080
    targetPort: health
    protocol: TCP
  selector:
    app.kubernetes.io/name: foxctl
    app.kubernetes.io/component: core

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/deploy/kubernetes/overlays/public-gui/foxctl-deployment-patch.yaml
```yaml
- op: replace
  path: /spec/replicas
  value: 1

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/deploy/kubernetes/overlays/public-gui/kustomization.yaml
```yaml
# Public gui-agent overlay with Better Auth gateway in front of foxctl.
# Usage: kubectl apply -k deploy/kubernetes/overlays/public-gui/

apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

resources:
- ../postgres
- gateway-configmap.yaml
- gateway-secrets.yaml
- gateway-deployment.yaml
- gateway-service.yaml
- gateway-network-policy.yaml

patches:
- path: ingress-patch.yaml
  target:
    kind: Ingress
    name: foxctl
- path: foxctl-deployment-patch.yaml
  target:
    kind: Deployment
    name: foxctl
- path: foxctl-hpa-patch.yaml
  target:
    kind: HorizontalPodAutoscaler
    name: foxctl

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/deploy/kubernetes/overlays/public-gui/gateway-configmap.yaml
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: gui-auth-gateway-config
  namespace: foxctl
  labels:
    app.kubernetes.io/name: foxctl
    app.kubernetes.io/component: gui-auth-gateway
data:
  PORT: "8080"
  BETTER_AUTH_URL: "https://gui.example.com"
  GUI_AUTH_UPSTREAM_URL: "http://foxctl:8080"
  GUI_AUTH_DIST_DIR: "/app/packages/gui-agent/dist"
  GUI_AUTH_MAGIC_LINK_LOG_ONLY: "false"
  GUI_AUTH_ALLOWED_EMAILS: "operator@example.com"
  GUI_AUTH_SMTP_HOST: "smtp.example.com"
  GUI_AUTH_SMTP_PORT: "587"
  GUI_AUTH_SMTP_SECURE: "false"
  GUI_AUTH_SMTP_FROM: "gui-agent@example.com"

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/cmd/foxctl/cmd/gateway.go
```go
package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/interfaces/gateway"
	"github.com/joshka0/foxctl/internal/platform/logging"
	"github.com/joshka0/foxctl/internal/protocol"
)

var gatewayCmd = &cobra.Command{
	Use:   "gateway",
	Short: "Start the Tailscale gateway for room sandbox terminal access",
	Long: `Start the foxctl gateway daemon.

The gateway provides HTTPS (via Tailscale tsnet) or HTTP (dev mode) access
to room sandbox terminals. It supports both web terminal (xterm.js) and SSH access.

In production mode, the gateway connects to your Tailscale tailnet using tsnet,
providing auto-TLS and MagicDNS access. In dev mode, it listens on localhost
without Tailscale.

Examples:
  # Start with Tailscale auth key
  foxctl gateway --ts-authkey tskey-auth-xxx

  # Start using TS_AUTHKEY env var
  TS_AUTHKEY=tskey-auth-xxx foxctl gateway

  # Start in dev mode (no Tailscale, localhost only)
  foxctl gateway --dev

  # Custom port for dev mode
  foxctl gateway --dev --port 9000

  # Custom state directory
  foxctl gateway --state-dir /var/lib/foxctl/gateway`,
	RunE: runGateway,
}

var (
	gatewayDev      bool
	gatewayPort     int
	gatewayStateDir string
	gatewayAuthKey  string
	gatewayHostname string
)

func init() {
	rootCmd.AddCommand(gatewayCmd)

	gatewayCmd.Flags().BoolVar(&gatewayDev, "dev", false, "Development mode: localhost HTTP without Tailscale")
	gatewayCmd.Flags().IntVar(&gatewayPort, "port", gateway.DefaultPort, "HTTP port for dev mode")
	gatewayCmd.Flags().StringVar(&gatewayStateDir, "state-dir", gateway.DefaultStateDir, "Directory for tsnet state persistence")
	gatewayCmd.Flags().StringVar(&gatewayAuthKey, "ts-authkey", "", "Tailscale auth key (or set TS_AUTHKEY env var)")
	gatewayCmd.Flags().StringVar(&gatewayHostname, "hostname", gateway.HostnamePrefix, "tsnet hostname")
}

func runGateway(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()

	// Get logger from context (set by PersistentPreRunE in root.go)
	log := logging.FromContext(ctx).With().
		Str("component", "gateway").
		Bool("dev", gatewayDev).
		Logger()

	// If no logger in context, create a default one writing to stderr
	if log.GetLevel() == zerolog.Disabled {
		log = logging.New(logging.Config{
			Level:  logging.ParseLevel("info"),
			Format: logging.ParseFormat("json"),
		}).With().
			Str("component", "gateway").
			Bool("dev", gatewayDev).
			Logger()
	}

	opts := gateway.Options{
		Dev:      gatewayDev,
		Port:     gatewayPort,
		StateDir: gatewayStateDir,
		AuthKey:  gatewayAuthKey,
		Hostname: gatewayHostname,
	}

	err := gateway.Run(ctx, opts, log)
	if err != nil {
		// Check if it's an auth key error for structured envelope output
		var authErr *gateway.AuthKeyError
		if errors.As(err, &authErr) {
			env := envelope.Error(
				"gateway",
				string(protocol.ErrorCodeEARG),
				authErr.Error(),
				authErr.Envelope()["data"],
			)
			return writeEnvelope(env)
		}
		return fmt.Errorf("gateway: %w", err)
	}
	return nil
}

// writeEnvelope writes a JSON envelope to stdout and returns a
// WrittenEnvelopeError so the caller exits non-zero.
func writeEnvelope(env envelope.Envelope) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(env); err != nil {
		return err
	}
	return &protocol.WrittenEnvelopeError{
		Command: env.Command,
		Code:    protocol.ErrorCode(env.Error.Code),
		Message: env.Error.Message,
	}
}

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/interfaces/chatadapter/teams/interactions.go
```go
package teams

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/joshka0/foxctl/internal/interfaces/chatadapter"
	"github.com/joshka0/foxctl/internal/runtime/observability"
)

// HandleInteraction processes Adaptive Card Action.Submit callbacks from Teams.
// CustomID format: "action:agentID" (e.g., "stop:01J...").
func (a *Adapter) HandleInteraction(ctx context.Context, evt chatadapter.InteractionEvent) error {
	parts := strings.SplitN(evt.CustomID, ":", 2)
	if len(parts) != 2 {
		return evt.Respond("Unknown interaction", nil)
	}

	action, agentID := parts[0], strings.TrimSpace(parts[1])
	if agentID == "" {
		return evt.Respond("Unknown interaction (missing agent id)", nil)
	}

	switch action {
	case "stop":
		return a.handleStop(ctx, evt, agentID)
	case "retry":
		return a.handleRetry(ctx, evt, agentID)
	case "details":
		return a.handleDetails(ctx, evt, agentID)
	default:
		return evt.Respond(fmt.Sprintf("Unknown action: %s", action), nil)
	}
}

func (a *Adapter) handleStop(ctx context.Context, evt chatadapter.InteractionEvent, agentID string) error {
	if a.daemonURL == "" {
		return evt.Respond("Daemon URL not configured", nil)
	}

	url := fmt.Sprintf("%s/api/agents/%s/daemon/kill", strings.TrimRight(a.daemonURL, "/"), agentID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return evt.Respond(fmt.Sprintf("Failed to create request: %s", err), nil)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return evt.Respond(fmt.Sprintf("Failed to kill agent: %s", err), nil)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return evt.Respond(fmt.Sprintf("Kill failed (%d): %s", resp.StatusCode, truncateForTeams(string(body))), nil)
	}

	return evt.Respond(fmt.Sprintf("Stop requested for agent %s.\nagent_id: %s", chatadapter.TruncateRunes(agentID, 8), agentID), nil)
}

func (a *Adapter) handleRetry(ctx context.Context, evt chatadapter.InteractionEvent, agentID string) error {
	if a.daemonURL == "" {
		return evt.Respond("Daemon URL not configured", nil)
	}

	url := fmt.Sprintf("%s/api/agents/%s/daemon/start", strings.TrimRight(a.daemonURL, "/"), agentID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return evt.Respond(fmt.Sprintf("Failed to create request: %s", err), nil)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return evt.Respond(fmt.Sprintf("Failed to restart agent: %s", err), nil)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return evt.Respond(fmt.Sprintf("Retry failed (%d): %s", resp.StatusCode, truncateForTeams(string(body))), nil)
	}

	return evt.Respond(fmt.Sprintf("Retry requested for agent %s.\nagent_id: %s", chatadapter.TruncateRunes(agentID, 8), agentID), nil)
}

func (a *Adapter) handleDetails(ctx context.Context, evt chatadapter.InteractionEvent, agentID string) error {
	if a.daemonURL == "" {
		return evt.Respond("Daemon URL not configured", nil)
	}

	// Best-effort: if we have a session ID, show the structured summary.
	if v, ok := a.agentThreads.Load(agentID); ok {
		if st, ok := v.(agentThread); ok {
			if sid := strings.TrimSpace(st.SessionID); sid != "" {
				if sess, err := a.fetchSessionSummary(ctx, sid); err == nil {
					return evt.Respond(formatSessionSummaryText(agentID, sess), nil)
				}
			}
		}
	}

	url := fmt.Sprintf("%s/api/agents/%s", strings.TrimRight(a.daemonURL, "/"), agentID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return evt.Respond(fmt.Sprintf("Failed to create request: %s", err), nil)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return evt.Respond(fmt.Sprintf("Failed to fetch agent: %s", err), nil)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return evt.Respond(fmt.Sprintf("Failed to read response: %s", err), nil)
	}

	if resp.StatusCode >= 400 {
		return evt.Respond(fmt.Sprintf("Agent not found (%d): %s", resp.StatusCode, truncateForTeams(string(body))), nil)
	}

	var agent any
	if err := json.Unmarshal(body, &agent); err != nil {
		return evt.Respond(truncateForTeams(string(body)), nil)
	}
	pretty, _ := json.MarshalIndent(agent, "", "  ")
	return evt.Respond(truncateForTeams(string(pretty)), nil)
}

// handleInvoke processes an invoke activity from Teams (Adaptive Card Action.Submit).
func (a *Adapter) handleInvoke(ctx context.Context, serviceURL string, activity Activity, convKey string) error {
	a.mu.Lock()
	handler := a.intHandler
	a.mu.Unlock()
	if handler == nil {
		return nil
	}

	if a.botClient == nil {
		observability.Emit(ctx, observability.NewEvent("teams.invoke_uninitialized").
			WithComponent("teams").
			WithData("conversation_id", convKey).
			Error(fmt.Errorf("botClient is nil; Connect() not called"), 0))
		return fmt.Errorf("teams: botClient not initialized")
	}

	// Parse the invoke value: {action, agentID}.
	var payload struct {
		Action  string `json:"action"`
		AgentID string `json:"agentID"`
	}
	if len(activity.Value) > 0 {
		if err := json.Unmarshal(activity.Value, &payload); err != nil {
			observability.Emit(ctx, observability.NewEvent("teams.invoke_parse_failed").
				WithComponent("teams").
				WithData("conversation_id", convKey).
				Error(err, 0))
			return nil
		}
	}

	action := strings.TrimSpace(payload.Action)
	agentID := strings.TrimSpace(payload.AgentID)
	if action == "" || agentID == "" {
		return nil
	}

	customID := action + ":" + agentID
	rawConvID := strings.TrimSpace(activity.Conversation.ID)
	if rawConvID == "" {
		return nil
	}

	user := chatadapter.UserRef{
		ID:       strings.TrimSpace(activity.From.ID),
		Username: strings.TrimSpace(activity.From.Name),
	}
	if user.Username == "" {
		user.Username = user.ID
	}

	msgRef := chatadapter.MessageRef{
		ChannelID: convKey,
		MessageID: strings.TrimSpace(activity.ID),
	}

	respond := func(content string, _ []chatadapter.Embed) error {
		if strings.TrimSpace(content) == "" {
			return nil
		}
		out := Activity{Type: "message", Text: content}
		replyTo := strings.TrimSpace(activity.ID)
		if replyTo != "" {
			_, err := a.botClient.ReplyToActivity(ctx, serviceURL, rawConvID, replyTo, out)
			return err
		}
		_, err := a.botClient.SendActivity(ctx, serviceURL, rawConvID, out)
		return err
	}

	update := func(content string, _ []chatadapter.Embed, _ []chatadapter.Component) error {
		// Update the card that triggered the invoke.
		activityID := strings.TrimSpace(activity.ID)
		if activityID == "" {
			return nil
		}
		return a.botClient.UpdateActivity(ctx, serviceURL, rawConvID, activityID, Activity{Type: "message", Text: content})
	}

	evt := chatadapter.NewInteractionEvent("button", customID, user, convKey, "", msgRef, respond, update)
	return handler(ctx, evt)
}

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/cmd/foxctl/cmd/web.go
```go
package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/joshka0/foxctl/internal/interfaces/web"
	"github.com/joshka0/foxctl/internal/platform/logging"
	"github.com/joshka0/foxctl/internal/runtime/observability"
)

var webCmd = &cobra.Command{
	Use:   "web",
	Short: "Web server commands",
}

var webServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the web API server",
	Long: `Start the foxctl web API server.

The server provides REST API endpoints for the GUI and other clients.
By default it listens on port 8090.

Examples:
  # Start server on default port 8090
  foxctl web serve

  # Start server on custom port
  foxctl web serve --port 3000

  # Start server with CORS enabled for development
  foxctl web serve --dev-cors

  # Start server with static UI files
  foxctl web serve --ui-dir ./packages/gui-agent/dist`,
	RunE: runWebServe,
}

var (
	webPort     int
	webDevCORS  bool
	webUIDir    string
	webChat     string
	webDBDriver string
	webDBDSN    string
)

func init() {
	rootCmd.AddCommand(webCmd)
	webCmd.AddCommand(webServeCmd)

	webServeCmd.Flags().IntVarP(&webPort, "port", "p", 8090, "Port to listen on")
	webServeCmd.Flags().BoolVar(&webDevCORS, "dev-cors", false, "Enable CORS for development (allows localhost:5174)")
	webServeCmd.Flags().StringVar(&webUIDir, "ui-dir", "", "Directory containing static UI files to serve")
	webServeCmd.Flags().StringVar(&webChat, "chat", "", "Chat adapter to enable (discord|telegram|teams)")
	webServeCmd.Flags().StringVar(&webDBDriver, "db-driver", "", "Database driver (sqlite|libsql|turso|postgres)")
	webServeCmd.Flags().StringVar(&webDBDSN, "db-dsn", "", "PostgreSQL DSN (overrides FOXCTL_POSTGRES_DSN)")
}

func runWebServe(cmd *cobra.Command, _ []string) error {
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	// Load config
	cfg, err := loadConfig(ctx)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Apply CLI flag overrides
	if webDBDriver != "" {
		cfg.Database.Driver = webDBDriver
	}
	if webDBDSN != "" {
		cfg.Database.Postgres.DSN = webDBDSN
	}

	// Setup logger for web server internals
	// TODO: Migrate web server to use observability instead of zerolog
	logPath := cfg.Logging.Output
	if logPath == "" {
		logPath = filepath.Join("/tmp", "foxctl-gui.log")
	}
	logWriter, closeWriter, err := logging.OpenWriter(logPath)
	if err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}
	if closeWriter != nil {
		defer func() {
			if err := closeWriter(); err != nil {
				fmt.Fprintf(os.Stderr, "failed to close log writer: %v\n", err)
			}
		}()
	}

	log := logging.New(logging.Config{
		Level:  logging.ParseLevel(cfg.Logging.Level),
		Format: logging.ParseFormat(cfg.Logging.Format),
		Writer: logWriter,
	}).With().
		Timestamp().
		Str("component", "web").
		Logger()

	// Create web server
	addr := fmt.Sprintf(":%d", webPort)
	opts := web.Options{
		Addr:        addr,
		DevCORS:     webDevCORS,
		UIDir:       webUIDir,
		ChatAdapter: webChat,
	}

	server, err := web.NewServer(ctx, opts, cfg, log)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	// Start SSE hub in background
	go server.Run(ctx)

	// Create HTTP server with appropriate timeouts
	// Note: WriteTimeout is 0 to support SSE/streaming connections
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Set up graceful shutdown
	shutdownCh := make(chan os.Signal, 1)
	signal.Notify(shutdownCh, syscall.SIGTERM, syscall.SIGINT)

	// Start server in goroutine
	errCh := make(chan error, 1)
	go func() {
		log.Info().Str("addr", addr).Msg("Starting foxctl web server")
		log.Info().Str("health", fmt.Sprintf("http://localhost%s/api/health", addr)).Msg("Health endpoint")
		if webDevCORS {
			log.Info().Msg("CORS enabled for development (localhost:5174)")
		}
		if webUIDir != "" {
			log.Info().Str("ui_dir", webUIDir).Msg("Serving UI directory")
		}
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	// Wait for shutdown signal or error
	select {
	case sig := <-shutdownCh:
		log.Info().Str("signal", sig.String()).Msg("Received signal, shutting down gracefully")
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	case <-ctx.Done():
		log.Info().Msg("Context cancelled, shutting down")
	}

	// Cancel context to stop persistence goroutines
	cancel()

	// Wait for console transport persistence goroutines
	server.WaitConsoleTransport()

	// Graceful shutdown with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	// Stop chat adapter if running (use shutdown context for bounded disconnect)
	server.StopChatAdapter(shutdownCtx)

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		observability.Emit(context.Background(), observability.NewEvent("web.shutdown_error").
			WithComponent(observability.ComponentCLI).
			Error(err, 0))
		return err
	}

	log.Info().Msg("Server stopped")
	return nil
}

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/deploy/kubernetes/base/deployment.yaml
```yaml
# Core foxctl deployment

apiVersion: apps/v1
kind: Deployment
metadata:
  name: foxctl
  namespace: foxctl
  labels:
    app.kubernetes.io/name: foxctl
    app.kubernetes.io/component: core
spec:
  replicas: 3
  selector:
    matchLabels:
      app.kubernetes.io/name: foxctl
      app.kubernetes.io/component: core
  template:
    metadata:
      labels:
        app.kubernetes.io/name: foxctl
        app.kubernetes.io/component: core
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port: "8080"
        prometheus.io/path: "/metrics"
    spec:
      serviceAccountName: foxctl
      terminationGracePeriodSeconds: 60

      securityContext:
        runAsNonRoot: true
        runAsUser: 1000
        runAsGroup: 1000
        fsGroup: 1000

      containers:
      - name: foxctl
        image: foxctl:latest
        imagePullPolicy: IfNotPresent

        ports:
        - name: health
          containerPort: 8080
          protocol: TCP

        # Configuration from ConfigMap
        envFrom:
        - configMapRef:
            name: foxctl-config

        env:
        # Secrets
        - name: TURSO_URL
          valueFrom:
            secretKeyRef:
              name: foxctl-secrets
              key: turso-url
        - name: TURSO_AUTH_TOKEN
          valueFrom:
            secretKeyRef:
              name: foxctl-secrets
              key: turso-token
        - name: VOYAGE_API_KEY
          valueFrom:
            secretKeyRef:
              name: foxctl-secrets
              key: voyage-key
        - name: FOXCTL_OAUTH_ENC_KEY
          valueFrom:
            secretKeyRef:
              name: foxctl-secrets
              key: oauth-enc-key
              optional: true

        # Pod identity for distributed coordination
        - name: POD_NAME
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        - name: POD_NAMESPACE
          valueFrom:
            fieldRef:
              fieldPath: metadata.namespace
        - name: POD_IP
          valueFrom:
            fieldRef:
              fieldPath: status.podIP

        # Observability directory
        - name: FOXCTL_OBS_DIR
          value: "/var/log/foxctl"

        resources:
          requests:
            memory: "256Mi"
            cpu: "100m"
          limits:
            memory: "1Gi"
            cpu: "500m"

        securityContext:
          allowPrivilegeEscalation: false
          readOnlyRootFilesystem: true
          capabilities:
            drop:
            - ALL

        livenessProbe:
          exec:
            command: ["foxctl", "health"]
          initialDelaySeconds: 10
          periodSeconds: 30
          timeoutSeconds: 5
          failureThreshold: 3

        readinessProbe:
          exec:
            command: ["foxctl", "health", "--ready"]
          initialDelaySeconds: 5
          periodSeconds: 10
          timeoutSeconds: 3
          failureThreshold: 3

        lifecycle:
          preStop:
            exec:
              command: ["foxctl", "drain"]

        volumeMounts:
        - name: cache
          mountPath: /var/cache/foxctl
        - name: logs
          mountPath: /var/log/foxctl
        - name: tmp
          mountPath: /tmp

      volumes:
      - name: cache
        emptyDir:
          sizeLimit: 1Gi
      - name: logs
        emptyDir:
          sizeLimit: 500Mi
      - name: tmp
        emptyDir:
          sizeLimit: 100Mi

      affinity:
        podAntiAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
          - weight: 100
            podAffinityTerm:
              labelSelector:
                matchLabels:
                  app.kubernetes.io/name: foxctl
              topologyKey: kubernetes.io/hostname

      topologySpreadConstraints:
      - maxSkew: 1
        topologyKey: topology.kubernetes.io/zone
        whenUnsatisfiable: ScheduleAnyway
        labelSelector:
          matchLabels:
            app.kubernetes.io/name: foxctl

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/deploy/kubernetes/overlays/public-gui/gateway-network-policy.yaml
```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: gui-auth-gateway
  namespace: foxctl
  labels:
    app.kubernetes.io/name: foxctl
    app.kubernetes.io/component: gui-auth-gateway
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/name: foxctl
      app.kubernetes.io/component: gui-auth-gateway
  policyTypes:
  - Ingress
  - Egress
  ingress:
  - ports:
    - protocol: TCP
      port: 8080
  egress:
  - to:
    - podSelector:
        matchLabels:
          app.kubernetes.io/name: foxctl
          app.kubernetes.io/component: core
    ports:
    - protocol: TCP
      port: 8080
  - to:
    - namespaceSelector: {}
      podSelector:
        matchLabels:
          k8s-app: kube-dns
    ports:
    - protocol: UDP
      port: 53
  - to:
    - ipBlock:
        cidr: 0.0.0.0/0
        except:
        - 10.0.0.0/8
        - 172.16.0.0/12
        - 192.168.0.0/16
    ports:
    - protocol: TCP
      port: 443
    - protocol: TCP
      port: 465
    - protocol: TCP
      port: 587

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/cmd/foxctl_web/main.go
```go
// Command foxctl_web runs the foxctl web server.
//
// This server provides:
//   - REST API for jobs, CAS, skills, and other foxctl data
//   - SSE endpoint for real-time UI invalidation
//   - Optional static file serving for the built GUI
//
// Usage:
//
//	foxctl_web [flags]
//
// Flags:
//
//	-addr string
//	      HTTP listen address (default "127.0.0.1:8090")
//	-ui-dir string
//	      Path to built UI (e.g., ./packages/gui-agent/dist)
//	-dev-cors
//	      Enable permissive CORS for local dev (default true)
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/joshka0/foxctl/internal/interfaces/web"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/platform/logging"
)

func main() {
	var (
		addr    string
		uiDir   string
		devCORS bool
	)
	flag.StringVar(&addr, "addr", "127.0.0.1:8090", "HTTP listen address")
	flag.StringVar(&uiDir, "ui-dir", "", "Path to built UI (e.g., ./packages/gui-agent/dist)")
	flag.BoolVar(&devCORS, "dev-cors", true, "Enable permissive CORS for local dev")
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Load env files
	config.LoadDotEnv()

	cfg, err := config.LoadCached(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	logPath := cfg.Logging.Output
	if logPath == "" {
		logPath = filepath.Join(os.TempDir(), "foxctl-companion.log")
	}
	writer, closeWriter, err := logging.OpenWriter(logPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	if closeWriter != nil {
		defer func() {
			if err := closeWriter(); err != nil {
				fmt.Fprintf(os.Stderr, "failed to close log writer: %v\n", err)
			}
		}()
	}

	// Set up logger
	logger := logging.New(logging.Config{
		Level:  logging.ParseLevel(cfg.Logging.Level),
		Format: logging.ParseFormat(cfg.Logging.Format),
		Writer: writer,
	}).With().
		Timestamp().
		Str("service", "foxctl_web").
		Logger()

	// Create server
	srv, err := web.NewServer(ctx, web.Options{
		Addr:    addr,
		UIDir:   uiDir,
		DevCORS: devCORS,
	}, cfg, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to create server")
	}

	// Start SSE hub
	go srv.Run(ctx)

	// Create HTTP server
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      0, // SSE needs no write timeout
		IdleTimeout:       120 * time.Second,
	}

	// Start HTTP server
	go func() {
		logger.Info().
			Str("addr", addr).
			Bool("dev_cors", devCORS).
			Str("ui_dir", uiDir).
			Msg("foxctl_web listening")

		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal().Err(err).Msg("http server failed")
		}
	}()

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh

	logger.Info().Str("signal", sig.String()).Msg("shutting down")

	// Cancel context to stop SSE hub and persistence goroutines
	cancel()

	// Wait for console transport persistence goroutines
	srv.WaitConsoleTransport()

	// Graceful HTTP shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Error().Err(err).Msg("http shutdown error")
	}

	logger.Info().Msg("shutdown complete")
}

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/interfaces/chatadapter/teams/botclient.go
```go
package teams

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// BotClient calls the Bot Framework Connector API for outbound messages/edits.
type BotClient struct {
	tokenMgr   *tokenManager
	httpClient *http.Client
}

func newBotClient(tokenMgr *tokenManager, httpClient *http.Client) *BotClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &BotClient{
		tokenMgr:   tokenMgr,
		httpClient: httpClient,
	}
}

func (c *BotClient) SendActivity(ctx context.Context, serviceURL, conversationID string, activity Activity) (ResourceResponse, error) {
	return c.postActivity(ctx, serviceURL, conversationID, "", activity)
}

func (c *BotClient) ReplyToActivity(ctx context.Context, serviceURL, conversationID, replyToActivityID string, activity Activity) (ResourceResponse, error) {
	return c.postActivity(ctx, serviceURL, conversationID, replyToActivityID, activity)
}

func (c *BotClient) postActivity(ctx context.Context, serviceURL, conversationID, replyToID string, activity Activity) (ResourceResponse, error) {
	baseURL, err := normalizeServiceURL(serviceURL)
	if err != nil {
		return ResourceResponse{}, err
	}
	conv := strings.TrimSpace(conversationID)
	if conv == "" {
		return ResourceResponse{}, fmt.Errorf("teams: missing conversation id")
	}

	path := baseURL + "/v3/conversations/" + url.PathEscape(conv) + "/activities"
	if strings.TrimSpace(replyToID) != "" {
		path += "/" + url.PathEscape(strings.TrimSpace(replyToID))
	}

	if strings.TrimSpace(activity.Type) == "" {
		activity.Type = "message"
	}
	if strings.TrimSpace(replyToID) != "" && strings.TrimSpace(activity.ReplyToID) == "" {
		activity.ReplyToID = strings.TrimSpace(replyToID)
	}

	var resp ResourceResponse
	if err := c.doJSON(ctx, http.MethodPost, path, activity, &resp); err != nil {
		return ResourceResponse{}, err
	}
	return resp, nil
}

func (c *BotClient) UpdateActivity(ctx context.Context, serviceURL, conversationID, activityID string, activity Activity) error {
	baseURL, err := normalizeServiceURL(serviceURL)
	if err != nil {
		return err
	}
	conv := strings.TrimSpace(conversationID)
	if conv == "" {
		return fmt.Errorf("teams: missing conversation id")
	}
	actID := strings.TrimSpace(activityID)
	if actID == "" {
		return fmt.Errorf("teams: missing activity id")
	}

	path := baseURL + "/v3/conversations/" + url.PathEscape(conv) + "/activities/" + url.PathEscape(actID)
	activity.ID = actID
	if strings.TrimSpace(activity.Type) == "" {
		activity.Type = "message"
	}
	return c.doJSON(ctx, http.MethodPut, path, activity, nil)
}

func (c *BotClient) SendTyping(ctx context.Context, serviceURL, conversationID string) error {
	baseURL, err := normalizeServiceURL(serviceURL)
	if err != nil {
		return err
	}
	conv := strings.TrimSpace(conversationID)
	if conv == "" {
		return fmt.Errorf("teams: missing conversation id")
	}

	path := baseURL + "/v3/conversations/" + url.PathEscape(conv) + "/activities"
	activity := Activity{Type: "typing"}
	return c.doJSON(ctx, http.MethodPost, path, activity, nil)
}

func (c *BotClient) doJSON(ctx context.Context, method, urlStr string, body any, out any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if c.tokenMgr == nil {
		return fmt.Errorf("teams: token manager not configured")
	}

	token, err := c.tokenMgr.Token(ctx)
	if err != nil {
		return err
	}

	var payload []byte
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("teams: marshal request: %w", err)
		}
	}

	const maxRetries = 3
	for attempt := 0; attempt < maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, urlStr, bytes.NewReader(payload))
		if err != nil {
			return fmt.Errorf("teams: create request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			// Context cancellation should exit quickly.
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("teams: request: %w", err)
		}

		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 32*1024))
		_ = resp.Body.Close()
		if readErr != nil {
			return fmt.Errorf("teams: read response: %w", readErr)
		}

		// Retry on 429 and transient 5xx.
		if resp.StatusCode == http.StatusTooManyRequests || (resp.StatusCode >= 500 && resp.StatusCode <= 599) {
			if attempt == maxRetries-1 {
				return fmt.Errorf("teams: connector API failed (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(raw)))
			}
			delay := backoffDelay(resp, attempt)
			if !sleepWithContext(ctx, delay) {
				return ctx.Err()
			}
			continue
		}

		// Refresh token on 401 and retry.
		if resp.StatusCode == http.StatusUnauthorized && attempt < maxRetries-1 {
			newToken, tokenErr := c.tokenMgr.Token(ctx)
			if tokenErr != nil {
				return fmt.Errorf("teams: refresh token after 401: %w", tokenErr)
			}
			token = newToken
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("teams: connector API failed (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(raw)))
		}

		if out != nil && len(raw) > 0 {
			if err := json.Unmarshal(raw, out); err != nil {
				return fmt.Errorf("teams: decode response: %w", err)
			}
		}
		return nil
	}

	return fmt.Errorf("teams: request failed after retries")
}

func backoffDelay(resp *http.Response, attempt int) time.Duration {
	if resp == nil {
		return time.Second
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		if v := strings.TrimSpace(resp.Header.Get("Retry-After")); v != "" {
			if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
				return time.Duration(secs) * time.Second
			}
		}
		return 2 * time.Second
	}

	// Simple exponential backoff for 5xx.
	switch attempt {
	case 0:
		return 500 * time.Millisecond
	case 1:
		return time.Second
	default:
		return 2 * time.Second
	}
}

func sleepWithContext(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	if ctx == nil {
		ctx = context.Background()
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func normalizeServiceURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("teams: missing serviceUrl")
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("teams: invalid serviceUrl: %w", err)
	}
	if u.Scheme != "https" {
		return "", fmt.Errorf("teams: untrusted serviceUrl scheme %q (expected https)", u.Scheme)
	}
	if u.User != nil {
		return "", fmt.Errorf("teams: untrusted serviceUrl (userinfo not allowed)")
	}

	host := strings.ToLower(strings.TrimSpace(u.Hostname()))
	if !isTrustedServiceHost(host) {
		return "", fmt.Errorf("teams: untrusted serviceUrl host %q", host)
	}

	u.Fragment = ""
	u.RawQuery = ""

	// Keep path (regional endpoint), but normalize trailing slash.
	u.Path = strings.TrimRight(u.Path, "/")
	return strings.TrimRight(u.String(), "/"), nil
}

func isTrustedServiceHost(host string) bool {
	if host == "" {
		return false
	}
	if host == "smba.trafficmanager.net" {
		return true
	}
	if strings.HasSuffix(host, ".botframework.com") {
		return true
	}
	if strings.HasSuffix(host, ".teams.microsoft.com") {
		return true
	}
	return false
}

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/interfaces/chatadapter/teams/jwt.go
```go
package teams

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const botFrameworkOpenIDConfigURL = "https://login.botframework.com/v1/.well-known/openidconfiguration"

type JWTVerifier interface {
	Verify(ctx context.Context, authHeader string) error
}

type nopJWTVerifier struct{}

func (nopJWTVerifier) Verify(_ context.Context, _ string) error { return nil }

type openIDConfig struct {
	Issuer  string `json:"issuer"`
	JWKSURI string `json:"jwks_uri"`
}

type jwks struct {
	Keys []jwkKey `json:"keys"`
}

type jwkKey struct {
	Kty string   `json:"kty"`
	Use string   `json:"use"`
	Kid string   `json:"kid"`
	X5c []string `json:"x5c"`
	N   string   `json:"n"`
	E   string   `json:"e"`
}

type botClaims struct {
	jwt.RegisteredClaims
	Version         string `json:"ver"`
	AppID           string `json:"appid"`
	AuthorizedParty string `json:"azp"`
	TenantID        string `json:"tid"`
}

type jwtVerifier struct {
	clientID string
	tenantID string

	openIDURL  string
	httpClient *http.Client
	now        func() time.Time

	mu            sync.RWMutex
	issuer        string
	jwksURI       string
	jwksFetchedAt time.Time
	keys          map[string]*rsa.PublicKey
}

func newJWTVerifier(clientID, tenantID string, httpClient *http.Client) *jwtVerifier {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &jwtVerifier{
		clientID:   strings.TrimSpace(clientID),
		tenantID:   strings.TrimSpace(tenantID),
		openIDURL:  botFrameworkOpenIDConfigURL,
		httpClient: httpClient,
		now:        time.Now,
		keys:       make(map[string]*rsa.PublicKey),
	}
}

func (v *jwtVerifier) Verify(parent context.Context, authHeader string) error {
	if strings.TrimSpace(v.clientID) == "" {
		return fmt.Errorf("teams: jwt verify: missing client id")
	}
	if strings.TrimSpace(v.tenantID) == "" {
		return fmt.Errorf("teams: jwt verify: missing tenant id")
	}

	tokenStr := extractBearerToken(authHeader)
	if tokenStr == "" {
		return fmt.Errorf("teams: missing bearer token")
	}

	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()

	if err := v.ensureOpenID(ctx); err != nil {
		return err
	}
	if err := v.ensureKeys(ctx); err != nil {
		return err
	}

	claims := &botClaims{}
	keyFunc := func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		kid = strings.TrimSpace(kid)
		if kid == "" {
			return nil, fmt.Errorf("teams: jwt: missing kid")
		}

		if key := v.lookupKey(kid); key != nil {
			return key, nil
		}

		// Key rotation: refresh once on miss.
		_ = v.refreshKeys(ctx)
		if key := v.lookupKey(kid); key != nil {
			return key, nil
		}
		return nil, fmt.Errorf("teams: jwt: unknown kid %q", kid)
	}

	token, err := jwt.ParseWithClaims(tokenStr, claims, keyFunc, jwt.WithValidMethods([]string{"RS256"}))
	if err != nil {
		return fmt.Errorf("teams: jwt parse: %w", err)
	}
	if token == nil || !token.Valid {
		return fmt.Errorf("teams: invalid jwt")
	}

	iss := strings.TrimSpace(claims.Issuer)
	expectedIss := v.getIssuer()
	if expectedIss != "" && iss != expectedIss {
		return fmt.Errorf("teams: jwt: invalid issuer %q", iss)
	}

	if !audContains(claims.Audience, v.clientID) {
		return fmt.Errorf("teams: jwt: invalid audience")
	}

	ver := strings.TrimSpace(claims.Version)
	switch ver {
	case "1.0":
		if strings.TrimSpace(claims.AppID) != v.clientID {
			return fmt.Errorf("teams: jwt: invalid appid for ver=1.0")
		}
	case "2.0":
		if strings.TrimSpace(claims.AuthorizedParty) != v.clientID && strings.TrimSpace(claims.AppID) != v.clientID {
			return fmt.Errorf("teams: jwt: invalid azp/appid for ver=2.0")
		}
	default:
		// Unknown version; be conservative.
		return fmt.Errorf("teams: jwt: unsupported ver %q", ver)
	}

	if strings.TrimSpace(claims.TenantID) != v.tenantID {
		return fmt.Errorf("teams: jwt: invalid tenant id")
	}

	return nil
}

func audContains(aud jwt.ClaimStrings, want string) bool {
	want = strings.TrimSpace(want)
	if want == "" {
		return false
	}
	for _, a := range aud {
		if strings.TrimSpace(a) == want {
			return true
		}
	}
	return false
}

func extractBearerToken(h string) string {
	h = strings.TrimSpace(h)
	if h == "" {
		return ""
	}
	parts := strings.SplitN(h, " ", 2)
	if len(parts) != 2 {
		return ""
	}
	if !strings.EqualFold(strings.TrimSpace(parts[0]), "bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func (v *jwtVerifier) getIssuer() string {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.issuer
}

func (v *jwtVerifier) ensureOpenID(ctx context.Context) error {
	issuer, jwksURI, ok := v.snapshotOpenID()
	if ok && issuer != "" && jwksURI != "" {
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.openIDURL, nil)
	if err != nil {
		return fmt.Errorf("teams: openid: create request: %w", err)
	}

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("teams: openid: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("teams: openid: unexpected HTTP %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return fmt.Errorf("teams: openid: read response: %w", err)
	}

	var cfg openIDConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("teams: openid: decode: %w", err)
	}
	if strings.TrimSpace(cfg.Issuer) == "" || strings.TrimSpace(cfg.JWKSURI) == "" {
		return fmt.Errorf("teams: openid: missing issuer/jwks_uri")
	}

	v.mu.Lock()
	v.issuer = strings.TrimSpace(cfg.Issuer)
	v.jwksURI = strings.TrimSpace(cfg.JWKSURI)
	v.mu.Unlock()

	return nil
}

func (v *jwtVerifier) snapshotOpenID() (issuer, jwksURI string, ok bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.issuer, v.jwksURI, v.issuer != "" && v.jwksURI != ""
}

func (v *jwtVerifier) ensureKeys(ctx context.Context) error {
	v.mu.RLock()
	hasKeys := len(v.keys) > 0
	fetchedAt := v.jwksFetchedAt
	jwksURI := v.jwksURI
	v.mu.RUnlock()

	if hasKeys && !fetchedAt.IsZero() && v.now().Sub(fetchedAt) < 6*time.Hour {
		return nil
	}
	if strings.TrimSpace(jwksURI) == "" {
		return fmt.Errorf("teams: jwks uri not configured")
	}
	return v.refreshKeys(ctx)
}

func (v *jwtVerifier) refreshKeys(ctx context.Context) error {
	v.mu.RLock()
	jwksURI := v.jwksURI
	v.mu.RUnlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURI, nil)
	if err != nil {
		return fmt.Errorf("teams: jwks: create request: %w", err)
	}

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("teams: jwks: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("teams: jwks: unexpected HTTP %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return fmt.Errorf("teams: jwks: read response: %w", err)
	}

	keys, err := parseJWKS(raw)
	if err != nil {
		return err
	}

	v.mu.Lock()
	v.keys = keys
	v.jwksFetchedAt = v.now()
	v.mu.Unlock()
	return nil
}

func (v *jwtVerifier) lookupKey(kid string) *rsa.PublicKey {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.keys[kid]
}

func parseJWKS(raw []byte) (map[string]*rsa.PublicKey, error) {
	var doc jwks
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("teams: jwks: decode: %w", err)
	}

	out := make(map[string]*rsa.PublicKey)
	for _, k := range doc.Keys {
		kid := strings.TrimSpace(k.Kid)
		if kid == "" {
			continue
		}

		pub, err := jwkToRSAPublicKey(k)
		if err != nil {
			continue
		}
		out[kid] = pub
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("teams: jwks: no usable keys")
	}
	return out, nil
}

func jwkToRSAPublicKey(k jwkKey) (*rsa.PublicKey, error) {
	// Prefer x5c chain if present.
	if len(k.X5c) > 0 && strings.TrimSpace(k.X5c[0]) != "" {
		der, err := base64.StdEncoding.DecodeString(strings.TrimSpace(k.X5c[0]))
		if err != nil {
			return nil, err
		}
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			return nil, err
		}
		if pub, ok := cert.PublicKey.(*rsa.PublicKey); ok {
			return pub, nil
		}
		return nil, errors.New("non-RSA x5c key")
	}

	// Fallback: n/e.
	nBytes, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(k.N))
	if err != nil {
		return nil, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(k.E))
	if err != nil {
		return nil, err
	}
	if len(nBytes) == 0 || len(eBytes) == 0 {
		return nil, errors.New("missing n/e")
	}

	n := new(big.Int).SetBytes(nBytes)
	e := new(big.Int).SetBytes(eBytes).Int64()
	if e <= 0 || e > int64(^uint(0)>>1) {
		return nil, errors.New("invalid exponent")
	}

	return &rsa.PublicKey{N: n, E: int(e)}, nil
}

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/interfaces/chatadapter/teams/auth.go
```go
package teams

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	botFrameworkTokenURL = "https://login.microsoftonline.com/botframework.com/oauth2/v2.0/token"
	botFrameworkScope    = "https://api.botframework.com/.default"
)

type tokenManager struct {
	clientID     string
	clientSecret string

	tokenURL string
	scope    string

	httpClient *http.Client
	now        func() time.Time

	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
	TokenType   string `json:"token_type"`
}

func newTokenManager(clientID, clientSecret string, httpClient *http.Client) *tokenManager {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &tokenManager{
		clientID:     strings.TrimSpace(clientID),
		clientSecret: strings.TrimSpace(clientSecret),
		tokenURL:     botFrameworkTokenURL,
		scope:        botFrameworkScope,
		httpClient:   httpClient,
		now:          time.Now,
	}
}

func (m *tokenManager) Token(ctx context.Context) (string, error) {
	if strings.TrimSpace(m.clientID) == "" {
		return "", fmt.Errorf("teams: missing client id")
	}
	if strings.TrimSpace(m.clientSecret) == "" {
		return "", fmt.Errorf("teams: missing client secret")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Refresh ~5 minutes before expiry.
	if m.token != "" && m.now().Before(m.expiresAt.Add(-5*time.Minute)) {
		return m.token, nil
	}

	tok, exp, err := m.exchange(ctx)
	if err != nil {
		return "", err
	}

	m.token = tok
	m.expiresAt = exp
	return m.token, nil
}

func (m *tokenManager) exchange(parent context.Context) (token string, expiresAt time.Time, _ error) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", m.clientID)
	form.Set("client_secret", m.clientSecret)
	form.Set("scope", m.scope)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.tokenURL, bytes.NewBufferString(form.Encode()))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("teams: create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("teams: token request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", time.Time{}, fmt.Errorf("teams: token endpoint returned HTTP %d", resp.StatusCode)
	}

	var tr tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return "", time.Time{}, fmt.Errorf("teams: decode token response: %w", err)
	}
	if strings.TrimSpace(tr.AccessToken) == "" {
		return "", time.Time{}, fmt.Errorf("teams: empty access_token in token response")
	}
	if tr.ExpiresIn <= 0 {
		// Default to 1 hour if not provided (conservative).
		tr.ExpiresIn = int64((1 * time.Hour).Seconds())
	}

	return tr.AccessToken, m.now().Add(time.Duration(tr.ExpiresIn) * time.Second), nil
}

```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/interfaces/gateway/server.go
(lines 48-142: Gateway Options/defaults and server construction showing tsnet-oriented design and exposed room/webterm surfaces.)
```go
	Tmux  string `json:"tmux"`
}

// Options configures the gateway server.
type Options struct {
	// Dev enables development mode (localhost HTTP, no Tailscale).
	Dev bool

	// Port is the HTTP port for dev mode. Defaults to 8765.
	Port int

	// StateDir is the directory for tsnet state persistence.
	// Defaults to ~/.foxctl/gateway.
	StateDir string

	// AuthKey is the Tailscale auth key. Can also be set via TS_AUTHKEY env var.
	AuthKey string

	// Hostname is the tsnet hostname. Defaults to "foxctl-gateway".
	Hostname string
}

// DefaultOptions returns options with sensible defaults.
func DefaultOptions() Options {
	return Options{
		Port:     DefaultPort,
		StateDir: DefaultStateDir,
		Hostname: HostnamePrefix,
	}
}

// ResolveStateDir expands ~ and returns the absolute state directory path.
func ResolveStateDir(dir string) (string, error) {
	if strings.HasPrefix(dir, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		dir = filepath.Join(home, dir[2:])
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path: %w", err)
	}
	return abs, nil
}

// ResolveAuthKey returns the auth key from options or the TS_AUTHKEY env var.
func ResolveAuthKey(optKey string) string {
	if optKey != "" {
		return optKey
	}
	return os.Getenv("TS_AUTHKEY")
}

// Server is the gateway server.
type Server struct {
	opts     Options
	log      zerolog.Logger
	tsnet    *tsnet.Server
	http     *http.Server
	mu       sync.RWMutex
	health   HealthStatus
	started  bool
	termHub  *webterm.Hub
	sshSrv   *sshterm.Server
	sshRooms *sshterm.RoomManager
	rooms    *agentpane.TerminalRoomService
}

// NewServer creates a new gateway server.
func NewServer(opts Options, log zerolog.Logger) *Server {
	if opts.Port == 0 {
		opts.Port = DefaultPort
	}
	if opts.Hostname == "" {
		opts.Hostname = HostnamePrefix
	}

	sshConfig := sshterm.DefaultSSHServerConfig()
	sshRooms := sshterm.NewRoomManager(sshConfig, log)
	termHub := webterm.NewHub(webterm.HubConfig{}, log)

	return &Server{
		opts: opts,
		log:  log.With().Str("component", "gateway").Logger(),
		health: HealthStatus{
			Tsnet: "starting",
			Store: "starting",
			Tmux:  "ok",
		},
		termHub:  termHub,
		sshRooms: sshRooms,
		rooms:    agentpane.NewTerminalRoomService(termHub, sshRooms),
	}

```

(lines 299-640: Gateway runtime lifecycle including dev mode vs tsnet mode, auth-key/state checks, ListenTLS and SSH on :22, and WhoIs identity enforcement over tailnet.)
```go
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return fmt.Errorf("gateway already started")
	}
	s.started = true
	s.mu.Unlock()

	if s.opts.Dev {
		return s.startDev(ctx)
	}
	return s.startTsnet(ctx)
}

// startDev starts the gateway in dev mode (localhost HTTP).
func (s *Server) startDev(ctx context.Context) error {
	addr := fmt.Sprintf("127.0.0.1:%d", s.opts.Port)

	s.log.Warn().
		Str("addr", addr).
		Msg("Starting in dev mode — no Tailscale, no TLS, localhost only")

	s.http = &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		BaseContext:       func(_ net.Listener) context.Context { return ctx },
	}

	s.setHealth(HealthStatus{
		Tsnet: "disabled",
		Store: "ok",
		Tmux:  "ok",
	})

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen http: %w", err)
	}

	errCh := make(chan error, 2) // may get errors from HTTP and SSH

	go func() {
		s.log.Info().Str("addr", addr).Msg("Gateway HTTP server listening")
		if err := s.http.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("http server: %w", err)
		}
	}()

	// Start SSH server in dev mode (on next port)
	sshAddr := fmt.Sprintf("127.0.0.1:%d", s.opts.Port+1)
	if err := s.startSSHDev(ctx, sshAddr, errCh); err != nil {
		errCh <- fmt.Errorf("ssh server: %w", err)
	}

	// Wait for context cancellation or server error
	select {
	case <-ctx.Done():
		return s.shutdown()
	case err := <-errCh:
		s.log.Error().Err(err).Msg("Gateway sub-server failed")
		// Best effort shutdown of other servers
		_ = s.shutdown()
		return err
	}
}

// startTsnet starts the gateway in Tailscale mode (HTTPS via tsnet).
func (s *Server) startTsnet(ctx context.Context) error {
	// Resolve auth key
	authKey := ResolveAuthKey(s.opts.AuthKey)

	// Resolve state directory
	stateDir, err := ResolveStateDir(s.opts.StateDir)
	if err != nil {
		return fmt.Errorf("resolve state directory: %w", err)
	}

	// Check if we have existing state (no auth key needed for restarts)
	hasState := false
	if info, statErr := os.Stat(stateDir); statErr == nil && info.IsDir() {
		// Check for tsnet identity file
		identityFile := filepath.Join(stateDir, "tailscaled.state")
		if _, idErr := os.Stat(identityFile); idErr == nil {
			hasState = true
		}
	}

	if !hasState && authKey == "" {
		return &AuthKeyError{
			StateDir: stateDir,
		}
	}

	// Create state directory if needed
	if mkdirErr := os.MkdirAll(stateDir, 0o700); mkdirErr != nil {
		return fmt.Errorf("create state directory: %w", mkdirErr)
	}

	// Create tsnet server
	ts := &tsnet.Server{
		Hostname: s.opts.Hostname,
		Dir:      stateDir,
		AuthKey:  authKey,
		Logf: func(format string, args ...any) {
			s.log.Debug().Msgf("tsnet: "+format, args...)
		},
	}
	s.tsnet = ts

	s.log.Info().
		Str("hostname", s.opts.Hostname).
		Str("state_dir", stateDir).
		Bool("has_state", hasState).
		Msg("Starting tsnet node")

	// Start tsnet
	if err := ts.Start(); err != nil {
		return fmt.Errorf("start tsnet: %w", err)
	}

	// Update health for tsnet
	s.setHealth(HealthStatus{
		Tsnet: "ok",
		Store: "ok",
		Tmux:  "ok",
	})

	// Wait for tsnet to come up and log status
	status, err := ts.Up(ctx)
	if err != nil {
		s.log.Warn().Err(err).Msg("Failed to get tsnet status (continuing anyway)")
	} else {
		fields := map[string]any{
			"backend_state": status.BackendState,
		}
		if status.CurrentTailnet != nil {
			fields["tailnet_name"] = status.CurrentTailnet.Name
			fields["magic_dns_suffix"] = status.CurrentTailnet.MagicDNSSuffix
		}
		if len(status.TailscaleIPs) > 0 {
			fields["tailscale_ip"] = status.TailscaleIPs[0].String()
		}
		if status.Self != nil && status.Self.DNSName != "" {
			fields["dns_name"] = status.Self.DNSName
		}
		s.log.Info().Fields(fields).Msg("tsnet node online")
	}

	// Get HTTPS listener
	ln, err := ts.ListenTLS("tcp", ":443")
	if err != nil {
		return fmt.Errorf("tsnet ListenTLS: %w", err)
	}

	s.http = &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		BaseContext:       func(_ net.Listener) context.Context { return ctx },
	}

	errCh := make(chan error, 2) // may get errors from HTTPS and SSH

	go func() {
		s.log.Info().Msg("Gateway HTTPS server listening on tsnet")
		if err := s.http.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("https server: %w", err)
		}
	}()

	// Start SSH server on tsnet
	sshLn, err := ts.Listen("tcp", ":22")
	if err != nil {
		errCh <- fmt.Errorf("ssh listen on tsnet: %w", err)
	} else {
		if err := s.startSSHTsnet(ctx, sshLn, errCh); err != nil {
			errCh <- fmt.Errorf("ssh server: %w", err)
		}
	}

	// Wait for context cancellation or server error
	select {
	case <-ctx.Done():
		return s.shutdown()
	case err := <-errCh:
		s.log.Error().Err(err).Msg("Gateway sub-server failed")
		// Best effort shutdown of other servers
		_ = s.shutdown()
		return err
	}
}

// shutdown gracefully shuts down the server.
func (s *Server) shutdown() error {
	s.log.Info().Msg("Shutting down gateway")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
	defer cancel()

	var errs []error

	if s.http != nil {
		if err := s.http.Shutdown(shutdownCtx); err != nil {
			errs = append(errs, fmt.Errorf("http shutdown: %w", err))
		}
	}

	if s.tsnet != nil {
		if err := s.tsnet.Close(); err != nil {
			errs = append(errs, fmt.Errorf("tsnet close: %w", err))
		}
	}

	// Close terminal hub (disconnects all web terminal clients)
	if s.termHub != nil {
		s.termHub.Close()
	}

	// Close SSH server
	if s.sshSrv != nil {
		_ = s.sshSrv.Close()
	}
	if s.sshRooms != nil {
		s.sshRooms.Close()
	}

	s.setHealth(HealthStatus{
		Tsnet: "stopped",
		Store: "stopped",
		Tmux:  "stopped",
	})

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	s.log.Info().Msg("Gateway stopped")
	return nil
}

// startSSHDev starts the SSH server in dev mode (localhost, no Tailscale).
// Uses a permissive WhoIs that accepts all connections.
func (s *Server) startSSHDev(ctx context.Context, addr string, errCh chan<- error) error {
	// In dev mode, accept all connections (no Tailscale verification)
	devWhoIs := func(_ context.Context, _ string) (*sshterm.IdentityInfo, error) {
		return &sshterm.IdentityInfo{
			UserID:    "dev-user",
			UserLogin: "dev@localhost",
			UserName:  "Dev User",
			NodeName:  "localhost",
			NodeID:    "dev-node",
		}, nil
	}

	sshConfig := sshterm.DefaultSSHServerConfig()
	s.sshSrv = sshterm.NewServer(sshConfig, s.sshRooms, devWhoIs, s.log)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("SSH listen on %s: %w", addr, err)
	}

	go func() {
		if err := s.sshSrv.Serve(ctx, ln); err != nil && ctx.Err() == nil {
			errCh <- fmt.Errorf("ssh serve: %w", err)
		}
	}()

	s.log.Info().Str("addr", addr).Msg("SSH server listening (dev mode)")
	return nil
}

// startSSHTsnet starts the SSH server on a tsnet listener with WhoIs verification.
func (s *Server) startSSHTsnet(ctx context.Context, ln net.Listener, errCh chan<- error) error {
	ts := s.tsnet

	// WhoIs function that uses the tsnet local client for identity verification
	tsnetWhoIs := func(whoCtx context.Context, addr string) (*sshterm.IdentityInfo, error) {
		lc, lcErr := ts.LocalClient()
		if lcErr != nil {
			return nil, fmt.Errorf("get tsnet local client: %w", lcErr)
		}

		who, whoErr := lc.WhoIs(whoCtx, addr)
		if whoErr != nil {
			return nil, fmt.Errorf("tsnet WhoIs failed for %s: %w", addr, whoErr)
		}

		info := &sshterm.IdentityInfo{
			NodeName: who.Node.Name,
			NodeID:   string(who.Node.StableID),
		}
		if who.UserProfile != nil {
			info.UserID = fmt.Sprintf("%d", int64(who.UserProfile.ID))
			info.UserLogin = who.UserProfile.LoginName
			info.UserName = who.UserProfile.DisplayName
		}

		return info, nil
	}

	sshConfig := sshterm.DefaultSSHServerConfig()
	s.sshSrv = sshterm.NewServer(sshConfig, s.sshRooms, tsnetWhoIs, s.log)

	go func() {
		if err := s.sshSrv.Serve(ctx, ln); err != nil && ctx.Err() == nil {
			errCh <- fmt.Errorf("ssh serve: %w", err)
		}
	}()

	s.log.Info().Msg("SSH server listening on tsnet (port 22)")
	return nil
}

// AuthKeyError is returned when no Tailscale auth key is available
// and no existing tsnet state exists.
type AuthKeyError struct {
	StateDir string
}

func (e *AuthKeyError) Error() string {
	return "no Tailscale auth key provided and no existing state"
}

// Envelope returns a JSON-envelope-friendly error representation.
func (e *AuthKeyError) Envelope() map[string]any {
	return map[string]any{
		"error": map[string]string{
			"code":    "EARG",
			"message": e.Error(),
		},
		"data": map[string]string{
			"hint":    "Set TS_AUTHKEY env var or pass --ts-authkey flag. Get a key from https://login.tailscale.com/admin/settings/keys",
			"state":   e.StateDir,
			"command": "foxctl gateway --ts-authkey tskey-auth-xxx",
		},
	}
}


```

File: /Users/joshka/repos/worktrees/feat-longcot-eval-dry-run/internal/interfaces/web/api/agents.go
(lines 426-520: AgentDetailHandlerWithRuntime route dispatch for /api/agents/* actions, including spawn routing and daemon action endpoints.)
```go
func AgentDetailHandler(cfg config.Config, log zerolog.Logger, events agentEventPublisher) http.HandlerFunc {
	return AgentDetailHandlerWithRuntime(cfg, log, events, nil)
}

func AgentDetailHandlerWithRuntime(cfg config.Config, log zerolog.Logger, events agentEventPublisher, runtimeHost OrchestrationRuntimeHost) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract agent ID and action from path: /api/agents/{id}[/daemon/action]
		path := r.URL.Path

		// Handle spawn endpoint specially
		if strings.HasSuffix(path, "/agents/spawn") || strings.HasSuffix(path, "/agents/spawn/") {
			handleAgentSpawn(w, r, cfg, log)
			return
		}

		const prefix = "/api/agents/"
		if !strings.HasPrefix(path, prefix) {
			httpError(w, http.StatusNotFound, "not found")
			return
		}

		remaining := strings.TrimPrefix(path, prefix)
		parts := strings.SplitN(remaining, "/", 4)
		agentID := parts[0]

		if agentID == "" {
			httpError(w, http.StatusBadRequest, "missing agent id")
			return
		}

		// Route based on action
		if len(parts) >= 3 && parts[1] == "daemon" {
			action := parts[2]
			switch action {
			case "start":
				handleAgentDaemonStart(w, r, cfg, log, agentID)
			case "sessions":
				handleAgentDaemonSessions(w, r, log, agentID)
			case "kill":
				handleAgentDaemonKillWithRuntime(w, r, cfg, log, agentID, runtimeHost)
			default:
				httpError(w, http.StatusNotFound, "unknown daemon action")
			}
			return
		}

		// Handle POST /api/agents/{id}/ask - send message to running daemon agent
		if len(parts) >= 2 && parts[1] == "ask" {
			handleAgentAsk(w, r, cfg, log, agentID)
			return
		}

		if len(parts) >= 3 && parts[1] == "ask-stream" && parts[2] == "cancel" {
			handleAgentAskStreamCancel(w, r, log, agentID)
			return
		}

		if len(parts) >= 2 && parts[1] == "ask-stream" {
			handleAgentAskStream(w, r, cfg, log, events, agentID)
			return
		}

		if len(parts) >= 2 && parts[1] == "runtime" {
			if len(parts) >= 4 && parts[2] == "logs" && parts[3] == "stream" {
				handleAgentRuntimeLogsStream(w, r, cfg, log, agentID)
				return
			}
			if len(parts) >= 3 && parts[2] == "logs" {
				handleAgentRuntimeLogsGet(w, r, cfg, log, agentID)
				return
			}
			handleAgentRuntimeGet(w, r, cfg, log, agentID)
			return
		}

		if len(parts) >= 3 && parts[1] == "memory" {
			switch parts[2] {
			case "compress":
				handleAgentMemoryCompress(w, r, cfg, log, agentID)
			case "stats":
				handleAgentMemoryStats(w, r, cfg, log, agentID)
			case "context":
				handleAgentMemoryContext(w, r, cfg, log, agentID)
			case "search":
				handleAgentMemorySearch(w, r, cfg, log, agentID)
			default:
				httpError(w, http.StatusNotFound, "unknown agent memory action")
			}
			return
		}

		// Handle DELETE for soft delete (trash)
		if r.Method == http.MethodDelete {
			handleAgentTrash(w, r, cfg, log, agentID)
			return

```

(lines 889-1136: HTTP agent spawn handler that validates request, ensures daemon availability, creates persistent agent record, and calls daemon.AgentSpawn.)
```go
func handleAgentSpawn(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger) {
	handleAgentSpawnWithRoute(w, r, cfg, log)
}

// handleAgentSpawnLegacy creates a new agent record, launches it via the daemon, and responds with the spawn result.
//
// It only accepts POST requests with a JSON AgentSpawnRequest containing at minimum `role` and `prompt`.
// If no name is provided one is generated; a new ULID is assigned as the agent ID. The workspace is normalized
// (defaults to "default") and the execution mode is validated (must be reactive, autonomous, proactive, or story).
// The handler ensures the daemon is running, persists an agent record in the agents store with an initial state,
// requests the daemon to spawn the agent, updates the agent state to running on success, and returns an
// AgentSpawnResponse containing the session ID, actor ID (the persisted agent ID), status, and name.
// On validation, store, daemon, or spawn failures it writes an appropriate HTTP error response.
func handleAgentSpawnWithRoute(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger) {
	if r.Method != http.MethodPost {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Parse request body
	var req AgentSpawnRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.Role == "" {
		httpError(w, http.StatusBadRequest, "role is required")
		return
	}

	workspaceRoot := strings.TrimSpace(req.WorkspaceRoot)
	if workspaceRoot != "" {
		cleanRoot := filepath.Clean(workspaceRoot)
		if !filepath.IsAbs(cleanRoot) {
			httpError(w, http.StatusBadRequest, "workspace_root must be absolute")
			return
		}
		workspaceRoot = cleanRoot
	}
	workspaceSource := strings.TrimSpace(req.WorkspaceSource)

	// Connect to daemon, auto-start if not running
	client := daemon.NewClient()
	if err := client.EnsureRunning(); err != nil {
		log.Error().Err(err).Msg("failed to start daemon")
		httpError(w, http.StatusServiceUnavailable, "failed to start daemon: "+err.Error())
		return
	}

	preparedSandbox, err := prepareSandboxBackedSpawn(r.Context(), req)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	if preparedSandbox != nil {
		defer preparedSandbox.cleanup(context.Background())
		if strings.TrimSpace(req.WorkspaceID) == "" {
			req.WorkspaceID = preparedSandbox.workspaceID
		}
		workspaceRoot = preparedSandbox.workspaceRoot
		workspaceSource = preparedSandbox.workspaceSource
		req.SandboxProvider = preparedSandbox.sandboxProvider
		req.SandboxID = preparedSandbox.sandboxID
		req.RepoURL = preparedSandbox.repoURL
		req.RepoRef = preparedSandbox.repoRef
	}

	namespace := strings.TrimSpace(req.WorkspaceID)
	if namespace == "" {
		if workspaceRoot != "" {
			namespace = workspace.CanonicalID(workspaceRoot)
		} else {
			namespace = "default"
		}
	} else {
		namespace = workspace.Normalize(namespace)
	}

	resolvedPrompt := resolveAgentSpawnPrompt(req, namespace)
	if resolvedPrompt == "" {
		httpError(w, http.StatusBadRequest, "prompt is required")
		return
	}

	// Generate name if not provided
	name := req.Name
	if name == "" {
		name = agenttypes.GenerateAgentName(rand.New(rand.NewSource(time.Now().UnixNano())))
	}

	// Generate agent ID
	agentID := ulid.Make().String()

	// Determine and validate exec mode
	execMode := agenttypes.ExecutionMode(req.ExecMode)
	if execMode == "" {
		execMode = agenttypes.ModeReactive
	} else {
		switch execMode {
		case agenttypes.ModeReactive, agenttypes.ModeAutonomous, agenttypes.ModeProactive, agenttypes.ModeTick, agenttypes.ModeStory:
			// valid
		default:
			httpError(w, http.StatusBadRequest, "invalid exec_mode: must be reactive, autonomous, proactive, tick, or story")
			return
		}
	}
	if execMode == agenttypes.ModeTick {
		req.LLMProvider = "lmstudio"
		if strings.TrimSpace(req.LLMModel) == "" {
			req.LLMModel = llmproviders.DefaultModelForProvider("lmstudio")
		}
	}

	// Create agent record in store
	store, err := agents.Open(r.Context(), cfg.Storage.Root)
	if err != nil {
		log.Error().Err(err).Msg("failed to open agents store")
		httpError(w, http.StatusInternalServerError, "failed to open agents store")
		return
	}
	defer store.Close()

	agent := agenttypes.Agent{
		ID:              agentID,
		ParentID:        strings.TrimSpace(req.ParentID),
		Namespace:       namespace,
		WorkspaceRoot:   workspaceRoot,
		WorkspaceSource: workspaceSource,
		Name:            name,
		Slug:            req.Slug,
		Role:            req.Role,
		Prompt:          resolvedPrompt,
		SkillsAllow:     req.SkillsAllow,
		Policy:          agenttypes.Policy{},
		ShareBB:         "scoped",
		State:           agenttypes.StateStarting,
		CreatedAt:       time.Now().UTC(),
		LLMProvider:     req.LLMProvider,
		LLMModel:        req.LLMModel,
		LLMBaseURL:      req.LLMBaseURL,
		LLMAuthMode:     req.LLMAuthMode,
		LLMAuthHeader:   req.LLMAuthHeader,
		LLMAuthPrefix:   req.LLMAuthPrefix,
		ExecMode:        execMode,
		ThinkInterval:   req.ThinkInterval,
		MaxIterations:   req.MaxIterations,
		MaxAutoTurns:    req.MaxAutoTurns,
		MemoryScope:     normalizeSpawnMemoryScope(req),
		MemoryRetention: normalizeSpawnMemoryRetention(req),
		SandboxProvider: strings.TrimSpace(req.SandboxProvider),
		SandboxID:       strings.TrimSpace(req.SandboxID),
		RepoURL:         strings.TrimSpace(req.RepoURL),
		RepoRef:         strings.TrimSpace(req.RepoRef),
	}

	if agent.SkillsAllow == nil {
		agent.SkillsAllow = []string{}
	}

	if err := store.Create(r.Context(), agent); err != nil {
		log.Error().Err(err).Str("agent_id", agentID).Msg("failed to create agent record")
		httpError(w, http.StatusInternalServerError, "failed to create agent record: "+err.Error())
		return
	}

	// Spawn agent via daemon - use normalized/validated values
	params := daemon.AgentSpawnParams{
		Role:        req.Role,
		AgentID:     agentID,   // Pass agent ID for linking
		WorkspaceID: namespace, // Use normalized workspace
		WorkspaceRoot: func() string {
			// Sandbox-backed agents currently persist their sandbox workspace
			// metadata but still execute in the local runtime until the
			// runtime-in-sandbox path is wired.
			if workspaceSource == "sandbox" {
				return ""
			}
			return workspaceRoot
		}(),
		Prompt:           resolvedPrompt,
		SkillsAllow:      req.SkillsAllow,
		MemoryScope:      string(normalizeSpawnMemoryScope(req)),
		MemoryRetention:  string(normalizeSpawnMemoryRetention(req)),
		Name:             name,
		Slug:             req.Slug,
		ExecMode:         string(execMode), // Use validated exec mode
		ThinkInterval:    req.ThinkInterval,
		MaxIterations:    req.MaxIterations,
		MaxContextTokens: req.MaxContextTokens,
		MaxAutoTurns:     req.MaxAutoTurns,
		LLMProvider:      req.LLMProvider,
		LLMModel:         req.LLMModel,
		LLMBaseURL:       req.LLMBaseURL,
		LLMAuthMode:      req.LLMAuthMode,
		LLMAuthHeader:    req.LLMAuthHeader,
		LLMAuthPrefix:    req.LLMAuthPrefix,
	}

	result, err := client.AgentSpawn(params)
	if err != nil {
		log.Error().Err(err).Str("agent_id", agentID).Msg("failed to spawn agent")
		// Update agent state to error since spawn failed
		_ = store.UpdateState(r.Context(), agentID, agenttypes.StateError)
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Sync persisted state after a successful daemon spawn. This is best-effort:
	// the spawn already succeeded, so transient SQLite lock contention should not
	// turn a live agent into an HTTP 500 for the caller.
	if err := updateAgentStateAfterSpawn(r.Context(), store, agentID); err != nil {
		log.Warn().Err(err).Str("agent_id", agentID).Msg("failed to update agent state to running after spawn")
	}

	if roomID := strings.TrimSpace(req.RoomID); roomID != "" {
		if err := attachSpawnedAgentToRoom(r.Context(), cfg, namespace, roomID, agentID, strings.TrimSpace(req.Role), strings.TrimSpace(req.RoomRole)); err != nil {
			log.Warn().Err(err).Str("agent_id", agentID).Str("room_id", roomID).Msg("failed to attach spawned agent to room")
		}
	}

	writeJSON(w, http.StatusOK, AgentSpawnResponse{
		SessionID:       result.SessionID,
		ActorID:         agentID, // Return our persisted agent ID, not the daemon's actor ID
		Status:          result.Status,
		Name:            name,
		WorkspaceID:     namespace,
		WorkspaceRoot:   workspaceRoot,
		WorkspaceSource: workspaceSource,
		SandboxProvider: strings.TrimSpace(req.SandboxProvider),
		SandboxID:       strings.TrimSpace(req.SandboxID),
		RepoURL:         strings.TrimSpace(req.RepoURL),
		RepoRef:         strings.TrimSpace(req.RepoRef),
	})
	if preparedSandbox != nil && preparedSandbox.release != nil {
		preparedSandbox.release()
	}
}

func prepareSandboxBackedSpawn(ctx context.Context, req AgentSpawnRequest) (*preparedSandboxSpawn, error) {
	provider := strings.TrimSpace(req.SandboxProvider)
	if provider == "" {
		return nil, nil
	}
	return nil, fmt.Errorf("sandbox_provider %q is temporarily disabled", provider)
}

func updateAgentStateAfterSpawn(ctx context.Context, store agents.Store, agentID string) error {

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
<taskname="Gateway Chat Audit"/>
<task>Audit gateway and daemon architecture for four questions: (1) what is required to make Microsoft Teams and Telegram first-class chat adapter channels, (2) what Tailscale integration would require for direct-network agent interactions, (3) whether agent spawning is already supported through the chat/gateway path or needs new primitives, and (4) whether the HTTP/WebSocket/SSE API layer is robust and decoupled enough from CLI wiring to be extracted as a standalone deployable feature.</task>

<architecture>
- Chat ingress: platform adapters (`teams`, `telegram`) implement chat lifecycle and routing, while `chatadapter.Bridge` maps chat commands to skills and daemon-backed agent actions.
- Web composition layer: `internal/interfaces/web/server.go` wires adapters, route graph, orchestration host, and SSE/console hubs.
- Agent control plane: `internal/interfaces/web/api/agents.go` handles `/api/agents/*` HTTP actions and bridges to daemon RPC.
- Daemon runtime: `internal/runtime/daemon/service.go` owns Unix-socket RPC, agent orchestration init, and spawn/list/ask/kill execution.
- Gateway/Tailscale edge: `internal/interfaces/gateway/server.go` owns tsnet bootstrap, HTTPS/SSH listeners, and tailnet identity checks (`WhoIs`).
- Deployment/wiring: `cmd/foxctl/cmd/{web,gateway}.go`, `cmd/foxctl_web/main.go`, and k8s manifests under `deploy/kubernetes` show process and deploy separation.
</architecture>

<selected_context>
internal/interfaces/chatadapter/bridge.go: `Bridge.HandleCommand`, `handleAgentSpawn`, `handleAgentList` - chat command routing to skills plus daemon HTTP spawn/list calls.
internal/interfaces/chatadapter/teams/driver.go: `Adapter.Connect`, `HTTPHandler`, `handleCommand`, `handleMessage` - Teams webhook auth/gating/tenant-scoped conversation routing and command-message dispatch.
internal/interfaces/chatadapter/teams/auth.go: `tokenManager.Token/exchange` - Bot Framework OAuth token acquisition/caching.
internal/interfaces/chatadapter/teams/jwt.go: `jwtVerifier.Verify`, OpenID/JWKS refresh path - Teams webhook bearer-token verification and tenant/audience checks.
internal/interfaces/chatadapter/teams/botclient.go: `SendActivity`, `ReplyToActivity`, `UpdateActivity`, `SendTyping`, `normalizeServiceURL` - outbound connector calls + trust/retry policy.
internal/interfaces/chatadapter/teams/interactions.go: `HandleInteraction` and action handlers (`stop/retry/details`) - card actions into `/api/agents/*` endpoints.
internal/interfaces/chatadapter/telegram/driver.go: `Connect`, message/callback dispatch, `dispatchAgentAsk`, chat gating and thread-key/session behavior.
internal/interfaces/web/server.go: `NewServer`, `startChatAdapter`, `Handler` - adapter enablement (`--chat teams|telegram|discord`), `/api/teams/messages`, `/api/agents/*`, SSE, middleware seams.
internal/interfaces/web/api/agents.go: `AgentDetailHandlerWithRuntime`, `handleAgentSpawnWithRoute` - `/api/agents/spawn` route, daemon ensure-running, persistence + daemon spawn bridge.
internal/runtime/daemon/service.go: `Run`, `handleConnection`, `startAgentOrchestration`, `handleAgentSpawnWithRoute` - daemon RPC surface and actual spawn runtime/persistence flow.
internal/interfaces/gateway/server.go: `Options`, `startTsnet`, `startSSHTsnet` - tsnet auth/state requirements, HTTPS + SSH over tailnet, identity verification.
cmd/foxctl/cmd/web.go: `runWebServe` - CLI web command wiring into `web.NewServer`.
cmd/foxctl/cmd/gateway.go: `runGateway` - CLI gateway command wiring and tsnet flags.
cmd/foxctl_web/main.go: dedicated standalone web binary entrypoint.
deploy/kubernetes/base/{deployment.yaml,service.yaml,kustomization.yaml}: current base deployment shape.
deploy/kubernetes/overlays/public-gui/{kustomization.yaml,gateway-deployment.yaml,gateway-service.yaml,gateway-configmap.yaml,gateway-network-policy.yaml,foxctl-deployment-patch.yaml}: split public-gui overlay and gateway-specific deployment primitives.
</selected_context>

<relationships>
- Chat spawn path: `teams/telegram Adapter` -> `chatadapter.Bridge.handleAgentSpawn` -> `POST /api/agents/spawn` (`web/api/agents.go`) -> `daemon.Client.AgentSpawn` -> `runtime/daemon/service.handleAgentSpawnWithRoute`.
- Teams webhook security path: `/api/teams/messages` (`web/server.Handler`) -> `teams.Adapter.HTTPHandler` -> `jwtVerifier.Verify` -> handler dispatch.
- Teams outbound delivery path: adapter handlers -> `BotClient` (`SendActivity/ReplyToActivity/UpdateActivity`) using token manager and trusted service URL checks.
- Gateway Tailscale path: `foxctl gateway` CLI -> `gateway.Run/startTsnet` -> `tsnet.Start + ListenTLS(:443) + Listen(:22)` -> `startSSHTsnet` `WhoIs` identity mapping.
- Web extraction seam: both `foxctl web serve` and `cmd/foxctl_web/main.go` construct `web.NewServer`; CLI is one launcher, not the only one.
</relationships>

<ambiguities>
- `internal/interfaces/web/server.go` route graph is broad; selected slices include major registration blocks but not every companion sub-route tail branch. If endpoint-by-endpoint API robustness scoring is needed, expand remaining `Handler()` sections.
- Tailscale is explicit in `gateway` edge, but direct-network agent interaction beyond terminal gateway (for daemon RPC itself) is not represented as a tailnet transport in this selection.
- ATCP transport internals were not selected in final context (only implied via architecture); include `internal/atcp/{daemon,client,transport/httpjson}` if the audit needs protocol-level direct-network comparison.
</ambiguities>
</user_instructions>
