import { Card, Chip, Link } from '@heroui/react';

const workflow = [
  {
    title: 'Orient',
    body: 'Build a ranked map of the repo before changing code.',
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
    body: 'A single entry point for repo search, skill runs, agent control, docs refresh, and local operations.',
    href: '/reference/cli/',
  },
  {
    title: 'Retrieval',
    label: 'repoindex',
    body: 'Semantic search, graph navigation, DAG grep, snippet extraction, and codemaps for grounded context.',
    href: '/retrieval/repoindex-and-dag-grep/',
  },
  {
    title: 'Skills',
    label: 'runtime',
    body: 'Job-tracked, ephemeral, and direct execution paths for installable tools with stable envelopes.',
    href: '/skills/runtime-and-install/',
  },
  {
    title: 'Agents',
    label: 'rooms',
    body: 'Persistent sessions, mailbox asks, overseer coordination, and durable room timelines.',
    href: '/agents/orchestration/',
  },
  {
    title: 'Storage',
    label: 'evidence',
    body: 'CAS, vectors, Turso, and Postgres keep large artifacts and working memory inspectable.',
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
  ['Protocol v1', '/reference/protocol-v1/'],
  ['Repoindex', '/retrieval/repoindex-and-dag-grep/'],
  ['Rooms', '/collaboration/rooms/'],
  ['Skills', '/skills/runtime-and-install/'],
];

export default function FoxctlHome() {
  return (
    <main className="fox-home">
      <section className="fox-hero" aria-labelledby="fox-home-title">
        <div className="fox-hero-copy">
          <Chip className="fox-chip">local control plane for agentic code work</Chip>
          <h1 id="fox-home-title">foxctl</h1>
          <p className="fox-tagline">Local control plane for agentic code work.</p>
          <p className="fox-lede">
            Code retrieval, skills, memory, context, agents, rooms, and operational
            workflows under one inspectable CLI.
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
          <h2 id="surfaces-title">The system pieces foxctl gives you</h2>
        </div>
        <div className="fox-card-grid">
          {surfaces.map(item => (
            <Card className="fox-surface-card" key={item.title}>
              <Card.Header className="fox-card-header">
                <span>{item.title}</span>
                <Chip className="fox-chip fox-chip-muted">{item.label}</Chip>
              </Card.Header>
              <Card.Content className="fox-card-content">
                <Card.Description className="fox-card-description">{item.body}</Card.Description>
              </Card.Content>
              <Card.Footer className="fox-card-footer">
                <Link className="fox-inline-link" href={item.href}>
                  Read the guide
                </Link>
              </Card.Footer>
            </Card>
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
