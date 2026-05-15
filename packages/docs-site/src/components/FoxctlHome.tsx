import { Chip, Link } from '@heroui/react';
import { GitHubLight } from 'developer-icons';
import foxLogo from '../assets/foxctl-logo.svg?url';

const workflow = [
  {
    title: 'Orient',
    body: 'Use Go-backed indexers to build a ranked map before changing code.',
    command: 'foxctl run code/semantic_search --input \'{"format":"tree"}\'',
  },
  {
    title: 'Retrieve',
    body: 'Index symbols, imports, calls, references, and files.',
    command: 'foxctl index repo build --workspace . --go --typescript --elixir',
  },
  {
    title: 'Plan',
    body: 'Pull the exact evidence needed for a bounded implementation path.',
    command: 'foxctl run code/dag_grep --input \'{"query":"buildEvidencePack"}\'',
  },
  {
    title: 'Act',
    body: 'Coordinate persistent agents and rooms when the work needs hands.',
    command: 'foxctl agent spawn --role reviewer --exec-mode autonomous',
  },
  {
    title: 'Verify',
    body: 'Keep outputs reproducible, protocol-shaped, and CI-visible.',
    command: 'bun run check:docs && make check-doc-links',
  },
];

const surfaces = [
  {
    title: 'CLI',
    label: 'commands',
    body: 'A Go command surface for repo search, skill runs, agent control, docs refresh, and local operations.',
    href: '/reference/cli/',
    ideas: ['Command palette for repo work', 'Scriptable local operations', 'Envelope-shaped command output'],
    examples: ['foxctl run code/semantic_search', 'foxctl agent spawn', 'foxctl index repo build'],
    map: `$ foxctl ...
      |
      v
+------------------+
| command router   |
+------------------+
  | run skill
  | index repo
  | spawn agent
  | inspect docs
  v
+------------------+
| protocol result  |
| data / error     |
| meta / artifact  |
+------------------+`,
  },
  {
    title: 'Retrieval',
    label: 'repoindex',
    body: 'Semantic search, graph navigation, DAG grep, snippet extraction, and codemaps for grounded context.',
    href: '/retrieval/repoindex-and-dag-grep/',
    ideas: ['Evidence pack before edits', 'Graph-aware search', 'Fallback paths for sparse queries'],
    examples: ['semantic tree view', 'DAG grep trace', 'codemap generation'],
    map: `question
  |
  v
+------------------+
| retrieval plan   |
+------------------+
  | semantic search
  | symbol graph
  | DAG grep
  | snippets
  v
+------------------+
| evidence pack    |
| ranked files     |
| exact anchors    |
+------------------+`,
  },
  {
    title: 'Skills',
    label: 'runtime',
    body: 'Job-tracked, ephemeral, and direct execution paths for installable tools with stable envelopes.',
    href: '/skills/runtime-and-install/',
    ideas: ['Reusable tool contracts', 'Job-tracked execution', 'CAS-backed large output'],
    examples: ['code search skill', 'hook feedback skill', 'OpenAPI skill'],
    map: `skill.yaml
  |
  v
+------------------+
| skill runner     |
+------------------+
  | direct call
  | queued job
  | WASI/native
  | timeout/cancel
  v
+------------------+
| stable envelope  |
| stdout summary   |
| CAS if large     |
+------------------+`,
  },
  {
    title: 'Agents',
    label: 'rooms',
    body: 'Go-managed agent sessions plus durable room collaboration for messages, tasks, relays, and status.',
    href: '/agents/orchestration/',
    ideas: ['Overseer manages agent hierarchy', 'Rooms persist collaboration state', 'Relays deliver room traffic to viewers'],
    examples: ['room create / join', 'room send / inbox', 'task claim / block / complete'],
    map: `agent plane              room plane
-----------              ----------
overseer                 room record
  | spawn/mail             | participants
  v                        | messages
agent sessions            | actor inbox
  | join                   | task board
  | send / inbox           | status
  +------ writes/reads --->| relay config
                           |
                           v
                   tmux | zellij | viewer`,
  },
  {
    title: 'Storage',
    label: 'evidence',
    body: 'CAS, vectors, Turso, and Postgres keep large artifacts and working memory inspectable.',
    href: '/storage/cas-and-persistence/',
    ideas: ['Durable evidence records', 'Disposable projections', 'Inspectable large artifacts'],
    examples: ['CAS digest', 'vector search', 'Turso session state'],
    map: `large output
  |
  v
+------------------+
| storage layer    |
+------------------+
  | CAS artifact
  | vector memory
  | Turso state
  | Postgres rows
  v
+------------------+
| inspectable ref  |
| digest / tags    |
| rebuildable idx  |
+------------------+`,
  },
];

const integrations = [
  {
    title: 'Providers and MCP',
    status: 'Current',
    body: 'LLM provider detection plus MCP serving for editor and agent tool access.',
    href: '/integrations/providers-and-mcp/',
  },
  {
    title: 'OpenAPI and plugins',
    status: 'Current',
    body: 'Generic OpenAPI calls, auth strategies, pagination, retries, CAS output, and plugin hooks.',
    href: '/integrations/openapi-and-plugins/',
  },
  {
    title: 'Hooks',
    status: 'Current',
    body: 'Session, tool, agent, and lifecycle events wired to structured actions and safety decisions.',
    href: '/integrations/hooks/',
  },
  {
    title: 'Chat platforms',
    status: 'Current',
    body: 'Discord, Telegram, and Teams adapters route commands and conversation into foxctl sessions.',
    href: '/integrations/chat-platforms/',
  },
  {
    title: 'Obsidian bridge',
    status: 'Current',
    body: 'Vault indexing for notes, wikilinks, aliases, tags, and retrieval-oriented memory.',
    href: '/context/obsidian-bridge/',
  },
  {
    title: 'Sandbox and runtime adapters',
    status: 'In progress',
    body: 'OpenSandbox, RLM, durable execution, and Go-native runtime plans are still plan-backed work.',
    href: '/roadmap/progress/',
  },
];

const progress = [
  {
    label: 'Current',
    title: 'Docs and deploy path',
    body: 'Go repo documentation, Starlight site, Cloudflare Pages deploy script, docs checks, and link checks.',
  },
  {
    label: 'Current',
    title: 'Repo evidence loop',
    body: 'Repoindex, semantic search, DAG grep, codemaps, and CAS-backed outputs are documented as operator workflows.',
  },
  {
    label: 'Current',
    title: 'Agent and room operations',
    body: 'Agent lifecycle, orchestration, rooms, storage, and observability have current behavior docs.',
  },
  {
    label: 'In progress',
    title: 'Durable execution recovery',
    body: 'Crash recovery, idempotent side effects, and effect journal work remain tied to active plans.',
  },
  {
    label: 'In progress',
    title: 'Refactor intelligence',
    body: 'Hotspot detection, confidence scoring, target selection, and slop detection are being hardened.',
  },
  {
    label: 'In progress',
    title: 'RLM and helper runtime',
    body: 'LongCoT evals, helper pipelines, recursive fanout, and smolvm runtime work are still experimental.',
  },
];

const benchmarkSolutions = [
  {
    title: 'Context gather speed',
    label: '1 case',
    metric: '31.4x faster',
    body: 'gather_context built the RLM map evidence in 6.50s with 1.00 fact recall and a compact 1,096-character context bundle.',
    href: '/quality/benchmarks/',
  },
  {
    title: 'Shell context reduction',
    label: 'orientation',
    metric: '85.4% less output',
    body: 'foxctl reduced output across command rows that actually shrank: ls, find, cat, head, tail, grep, sed, git, and go test tasks.',
    href: '/quality/benchmarks/',
  },
  {
    title: 'Repoindex and DAG paths',
    label: 'local',
    metric: '< 1ms fixtures',
    body: 'Search fallback and DAG explanation fixtures keep graph retrieval latency and allocation cost visible in the Go benchmark lane.',
    href: '/retrieval/repoindex-and-dag-grep/',
  },
  {
    title: 'Hot runtime overhead',
    label: 'Go',
    metric: '96.5ns runner',
    body: 'The no-hook tool runner, envelope codecs, actor lifecycle, and shell reducer hot paths are measured with allocation-aware Go benchmarks.',
    href: '/quality/benchmarks/',
  },
];

const commandComparisonRows = [
  {
    binary: 'ls',
    task: 'List the internal package tree',
    command: 'ls -la internal',
    native: '483 tokens / 1,002 bytes',
    foxctl: '30 tokens / 106 bytes',
    gain: '93.8% less',
  },
  {
    binary: 'find',
    task: 'Find Go files under tooling',
    command: "find internal/tooling -name '*.go'",
    native: '825 tokens / 3,061 bytes',
    foxctl: '66 tokens / 237 bytes',
    gain: '92.0% less',
  },
  {
    binary: 'cat',
    task: 'Read module metadata',
    command: 'cat go.mod',
    native: '7,520 tokens / 19,723 bytes',
    foxctl: '1,011 tokens / 2,216 bytes',
    gain: '86.6% less',
  },
  {
    binary: 'head',
    task: 'Read the start of shell command source',
    command: 'head -n 80 cmd/foxctl/cmd/shell.go',
    native: '679 tokens / 2,680 bytes',
    foxctl: '580 tokens / 2,245 bytes',
    gain: '14.6% less',
  },
  {
    binary: 'tail',
    task: 'Read the end of shell command source',
    command: 'tail -n 80 cmd/foxctl/cmd/shell.go',
    native: '655 tokens / 2,376 bytes',
    foxctl: '623 tokens / 2,245 bytes',
    gain: '4.9% less',
  },
  {
    binary: 'grep',
    task: 'Find Go functions in shellreduce',
    command: "grep -rn 'func ' internal/tooling/shellreduce",
    native: '4,779 tokens / 18,632 bytes',
    foxctl: '53 tokens / 209 bytes',
    gain: '98.9% less',
  },
  {
    binary: 'sed',
    task: 'Read the shell command source slice',
    command: "sed -n '1,120p' cmd/foxctl/cmd/shell.go",
    native: '1,148 tokens / 4,617 bytes',
    foxctl: '556 tokens / 2,216 bytes',
    gain: '51.6% less',
  },
  {
    binary: 'git status',
    task: 'Inspect worktree status',
    command: 'git status --short',
    native: '1,422 tokens / 5,095 bytes',
    foxctl: '72 tokens / 215 bytes',
    gain: '94.9% less',
  },
  {
    binary: 'git diff',
    task: 'Inspect changed-file stats',
    command: 'git diff --stat',
    native: '1,760 tokens / 6,458 bytes',
    foxctl: '182 tokens / 503 bytes',
    gain: '89.7% less',
  },
  {
    binary: 'git diff',
    task: 'List changed file names',
    command: 'git diff --name-only',
    native: '1,313 tokens / 4,755 bytes',
    foxctl: '225 tokens / 768 bytes',
    gain: '82.9% less',
  },
  {
    binary: 'git log',
    task: 'Review recent commit stats',
    command: 'git log --stat -5',
    native: '3,303 tokens / 11,464 bytes',
    foxctl: '89 tokens / 337 bytes',
    gain: '97.3% less',
  },
  {
    binary: 'go test',
    task: 'Run the shellreduce package tests',
    command: 'go test ./internal/tooling/shellreduce',
    native: '22 tokens / 68 bytes',
    foxctl: '14 tokens / 41 bytes',
    gain: '36.4% less',
  },
  {
    binary: 'total',
    task: 'All command-output rows where foxctl reduced output',
    command: 'twelve native commands, same tasks through foxctl shell reduction',
    native: '23,910 tokens / 79,932 bytes',
    foxctl: '3,501 tokens / 11,338 bytes',
    gain: '85.4% less',
  },
];

const cleanupWorkflows = [
  {
    label: 'Scout',
    title: 'Find refactor targets before editing',
    body: 'Use refactor status, snapshots, hot paths, dependency expansion, and scout evidence to choose narrow cleanup targets.',
    command: 'foxctl refactor scout --path ./internal --language go --focus slop',
    href: '/quality/refactor-scouts/',
  },
  {
    label: 'Slop',
    title: 'Reduce AI-generated sprawl',
    body: 'Look for duplicated guards, repeated remapping, overgrown functions, noisy adapters, and unclear package boundaries.',
    command: 'foxctl refactor advisor --path ./internal --language go --focus slop',
    href: '/quality/refactor-scouts/',
  },
  {
    label: 'Tighten',
    title: 'Clean repo boundaries',
    body: 'Tighten package placement, command surfaces, docs ownership, and runtime boundaries without broad unrelated rewrites.',
    command: 'foxctl refactor deps --path ./internal --language go --query Run --direction in',
    href: '/quality/refactor-scouts/',
  },
  {
    label: 'Evidence',
    title: 'Keep cleanup reviewable',
    body: 'Attach snapshots, change ranges, dependency evidence, and benchmark/doc checks so cleanup work stays auditable.',
    command: 'foxctl refactor evidence --artifact sha256:<digest>',
    href: '/quality/refactor-scouts/',
  },
];

const architecture = [
  ['CLI', 'commands, hooks, local shells'],
  ['Skills', 'WASI, native, job tracking'],
  ['Retrieval', 'semantic search, repoindex, codemaps'],
  ['Agents', 'daemon, rooms, overseer flow'],
  ['Storage', 'Turso, Postgres, CAS, vectors'],
];

const deepLinks = [
  ['Quickstart', '/start/install-first-run/'],
  ['Integrations', '/integrations/status/'],
  ['Progress', '/roadmap/progress/'],
  ['Benchmarks', '/quality/benchmarks/'],
  ['Refactor scouts', '/quality/refactor-scouts/'],
  ['Protocol v1', '/reference/protocol-v1/'],
  ['Repoindex', '/retrieval/repoindex-and-dag-grep/'],
];

export default function FoxctlHome() {
  return (
    <main className="fox-home">
      <section className="fox-hero" aria-labelledby="fox-home-title">
        <div className="fox-hero-copy">
          <div className="fox-brand-row">
            <img className="fox-hero-logo" src={foxLogo} alt="" aria-hidden="true" />
            <Chip className="fox-chip">Go-based framework for agentic code work</Chip>
          </div>
          <h1 id="fox-home-title">foxctl</h1>
          <p className="fox-tagline">Go-based framework for agentic code work.</p>
          <p className="fox-lede">
            A Go CLI and runtime framework for code retrieval, skills, memory,
            context, agents, rooms, and operational workflows.
          </p>
          <div className="fox-actions" aria-label="Primary documentation links">
            <Link className="fox-action fox-action-primary" href="/start/install-first-run/">
              Start with foxctl
            </Link>
            <Link className="fox-action" href="/start/docs-map/">
              Browse docs
            </Link>
            <Link
              className="fox-action fox-action-github"
              href="https://github.com/joshka0/foxctl"
              rel="noreferrer"
              target="_blank"
            >
              <GitHubLight className="fox-devicon" aria-hidden="true" size={18} />
              GitHub
            </Link>
          </div>
          <div className="fox-start-command" aria-label="First successful action">
            <span>Start here</span>
            <code>foxctl run code/semantic_search --input &apos;{'{"format":"tree"}'}&apos;</code>
          </div>
        </div>

        <div className="fox-terminal-panel" aria-label="foxctl workflow preview">
          <div className="fox-window-bar">
            <span />
            <span />
            <span />
            <strong>workflow preview</strong>
          </div>
          <div className="fox-terminal">
            {workflow.slice(0, 4).map((row, index) => (
              <div className="fox-command" key={row.title}>
                <div className="fox-command-head">
                  <span>{String(index + 1).padStart(2, '0')}</span>
                  <strong>{row.title}</strong>
                  <em>{row.body}</em>
                </div>
                <code>$ {row.command}</code>
              </div>
            ))}
          </div>
        </div>
      </section>

      <section className="fox-section" aria-labelledby="progress-title">
        <div className="fox-section-heading">
          <span className="fox-eyebrow">Progress</span>
          <h2 id="progress-title">What is current, and what is still moving</h2>
          <p>
            The site separates shipped behavior from plan-backed work so agents can
            avoid treating experimental runtime and evaluation work as production
            operator guidance.
          </p>
        </div>
        <div className="fox-info-grid fox-progress-grid">
          {progress.map(item => (
            <article className="fox-info-card" key={item.title}>
              <div className="fox-info-card-head">
                <span className="fox-status-pill">{item.label}</span>
                <strong>{item.title}</strong>
              </div>
              <p>{item.body}</p>
            </article>
          ))}
        </div>
        <Link className="fox-inline-link fox-section-link" href="/roadmap/progress/">
          Read progress details
        </Link>
      </section>

      <section className="fox-section" aria-labelledby="integrations-title">
        <div className="fox-section-heading">
          <span className="fox-eyebrow">Integrations</span>
          <h2 id="integrations-title">Connectors that make foxctl useful in real workflows</h2>
          <p>
            Integrations are grouped by status: current surfaces are safe to build on,
            while runtime adapter work remains explicitly marked in progress.
          </p>
        </div>
        <div className="fox-info-grid fox-integration-grid">
          {integrations.map(item => (
            <article className="fox-info-card" key={item.title}>
              <div className="fox-info-card-head">
                <span className="fox-status-pill">{item.status}</span>
                <strong>{item.title}</strong>
              </div>
              <p>{item.body}</p>
              <Link className="fox-inline-link" href={item.href}>
                Open docs
              </Link>
            </article>
          ))}
        </div>
      </section>

      <section className="fox-section fox-workflow-section" aria-labelledby="workflow-title">
        <div className="fox-section-heading">
          <span className="fox-eyebrow">Ways to use it</span>
          <h2 id="workflow-title">A practical loop for agentic code work</h2>
          <p>
            foxctl makes the repeatable path explicit: orient the agent, retrieve
            grounded evidence, plan with context, act through tools, then verify the
            result.
          </p>
        </div>
        <div className="fox-workflow">
          {workflow.map((item, index) => (
            <article className="fox-step" key={item.title}>
              <span>{String(index + 1).padStart(2, '0')}</span>
              <strong>{item.title}</strong>
              <p>{item.body}</p>
            </article>
          ))}
        </div>
      </section>

      <section className="fox-section" aria-labelledby="surfaces-title">
        <div className="fox-section-heading">
          <span className="fox-eyebrow">Main surfaces</span>
          <h2 id="surfaces-title">The Go-backed system pieces foxctl gives you</h2>
        </div>
        <div className="fox-card-grid fox-surface-grid">
          {surfaces.map(item => (
            <details className="fox-surface-card" key={item.title}>
              <summary className="fox-surface-summary">
                <span className="fox-card-header">
                  <span>{item.title}</span>
                  <Chip className="fox-chip fox-chip-muted">{item.label}</Chip>
                </span>
                <span className="fox-card-description">{item.body}</span>
              </summary>
              <div className="fox-surface-expanded">
                <pre aria-label={`${item.title} ASCII map`}>{item.map}</pre>
                <div className="fox-surface-columns">
                  <div>
                    <strong>Ideas</strong>
                    <ul>
                      {item.ideas.map(idea => (
                        <li key={idea}>{idea}</li>
                      ))}
                    </ul>
                  </div>
                  <div>
                    <strong>Examples</strong>
                    <ul>
                      {item.examples.map(example => (
                        <li key={example}>{example}</li>
                      ))}
                    </ul>
                  </div>
                </div>
                <Link className="fox-inline-link" href={item.href}>
                  Read the guide
                </Link>
              </div>
            </details>
          ))}
        </div>
      </section>

      <section className="fox-section" aria-labelledby="cleanup-title">
        <div className="fox-section-heading">
          <span className="fox-eyebrow">Repo cleanup</span>
          <h2 id="cleanup-title">Refactor scouts for tightening real codebases</h2>
          <p>
            foxctl treats cleanup as an evidence workflow: scout the shape,
            identify slop, tighten boundaries, and keep each refactor reviewable.
          </p>
        </div>
        <div className="fox-info-grid fox-cleanup-grid">
          {cleanupWorkflows.map(item => (
            <article className="fox-info-card" key={item.title}>
              <div className="fox-info-card-head">
                <span className="fox-status-pill fox-status-pill-cool">{item.label}</span>
                <strong>{item.title}</strong>
              </div>
              <p>{item.body}</p>
              <code className="fox-inline-command">{item.command}</code>
              <Link className="fox-inline-link" href={item.href}>
                Open workflow
              </Link>
            </article>
          ))}
        </div>
      </section>

      <section className="fox-section" aria-labelledby="benchmark-title">
        <div className="fox-section-heading">
          <span className="fox-eyebrow">Benchmark solutions</span>
          <h2 id="benchmark-title">Measured evidence for why the harness matters</h2>
          <p>
            The current evidence separates hot runtime cost, cold CLI startup,
            context size, and agent-baseline comparison so the site can make claims
            without hiding the tradeoffs.
          </p>
        </div>
        <div className="fox-info-grid fox-benchmark-grid">
          {benchmarkSolutions.map(item => (
            <article className="fox-info-card" key={item.title}>
              <div className="fox-info-card-head">
                <span className="fox-status-pill fox-status-pill-warm">{item.label}</span>
                <strong>{item.title}</strong>
              </div>
              <span className="fox-benchmark-metric">{item.metric}</span>
              <p>{item.body}</p>
              <Link className="fox-inline-link" href={item.href}>
                Review
              </Link>
            </article>
          ))}
        </div>
        <div className="fox-product-table-shell" aria-label="Command output comparison table">
          <table className="fox-product-table">
            <thead>
              <tr>
                <th scope="col">Binary</th>
                <th scope="col">Same task</th>
                <th scope="col">Native output</th>
                <th scope="col">foxctl output</th>
              </tr>
            </thead>
            <tbody>
              {commandComparisonRows.map(row => (
                <tr key={`${row.binary}-${row.command}`}>
                  <th scope="row">
                    <span>{row.binary}</span>
                    <em>{row.gain}</em>
                  </th>
                  <td>
                    <span>{row.task}</span>
                    <code>{row.command}</code>
                  </td>
                  <td>{row.native}</td>
                  <td>{row.foxctl}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      <section className="fox-section fox-architecture" aria-labelledby="architecture-title">
        <div className="fox-section-heading">
          <span className="fox-eyebrow">Architecture</span>
          <h2 id="architecture-title">Canonical storage, disposable context, durable evidence</h2>
          <p>
            Commands return envelopes, large artifacts go to CAS, repo metadata stays
            rebuildable, and agent work is anchored to durable room and session records.
          </p>
        </div>
        <div className="fox-stack" aria-label="foxctl architecture layers">
          {architecture.map(([name, body]) => (
            <div className="fox-stack-row" key={name}>
              <strong>{name}</strong>
              <span>{body}</span>
            </div>
          ))}
        </div>
      </section>

      <section className="fox-section fox-deep-links" aria-labelledby="links-title">
        <div className="fox-section-heading">
          <span className="fox-eyebrow">Deep links</span>
          <h2 id="links-title">Keep moving</h2>
        </div>
        <div className="fox-link-grid">
          {deepLinks.map(([title, href]) => (
            <Link className="fox-deep-link" href={href} key={title}>
              {title}
            </Link>
          ))}
        </div>
      </section>
    </main>
  );
}
