import { Chip, Link } from '@heroui/react';
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
    title: 'Curated benchmark runner',
    label: 'bench:go',
    body: 'A repeatable package set with count, time, pattern, and output capture controls.',
    href: '/quality/benchmarks/',
  },
  {
    title: 'Repoindex query paths',
    label: 'search',
    body: 'Measures zero-result fallback, scored search, syntax fallback, and allocation cost.',
    href: '/retrieval/repoindex-and-dag-grep/',
  },
  {
    title: 'Storage hot paths',
    label: 'CAS + DB',
    body: 'Covers CAS buffering, slice preallocation, cancellation checks, and row scan helpers.',
    href: '/storage/cas-and-persistence/',
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

      <section className="fox-section" aria-labelledby="benchmark-title">
        <div className="fox-section-heading">
          <span className="fox-eyebrow">Benchmark solutions</span>
          <h2 id="benchmark-title">Performance checks that protect real hot paths</h2>
          <p>
            Benchmarks are wired as a runnable solution, not a static report: the
            curated runner keeps repo search, storage, execution, and scan helper
            costs visible during local work.
          </p>
        </div>
        <div className="fox-info-grid fox-benchmark-grid">
          {benchmarkSolutions.map(item => (
            <article className="fox-info-card" key={item.title}>
              <div className="fox-info-card-head">
                <span className="fox-status-pill fox-status-pill-warm">{item.label}</span>
                <strong>{item.title}</strong>
              </div>
              <p>{item.body}</p>
              <Link className="fox-inline-link" href={item.href}>
                Review
              </Link>
            </article>
          ))}
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
