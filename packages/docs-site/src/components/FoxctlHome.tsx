import { Card, Chip, Link } from '@heroui/react';

const capabilities = [
  {
    title: 'Run local skills',
    label: 'skills',
    body: 'Execute installable tools through job-tracked, ephemeral, or direct command paths without losing the protocol envelope.',
    href: '/skills/runtime-and-install/',
  },
  {
    title: 'Navigate code graphs',
    label: 'repoindex',
    body: 'Build symbol, call, reference, import, and concept indexes for smart context and small explanation subgraphs.',
    href: '/retrieval/repoindex-and-dag-grep/',
  },
  {
    title: 'Coordinate agents',
    label: 'rooms',
    body: 'Spawn persistent agents, route mailbox asks, and keep room timelines durable enough for long-running work.',
    href: '/agents/orchestration/',
  },
  {
    title: 'Carry context forward',
    label: 'aca',
    body: 'Use the dual-plane context model, Obsidian bridge, and continuity summaries to keep evidence and memory inspectable.',
    href: '/context/aca/',
  },
];

const workflow = [
  ['Orient', 'semantic tree, docs map, repo graph'],
  ['Retrieve', 'smart search, DAG grep, snippet extract'],
  ['Plan', 'context engine, evidence, impact'],
  ['Act', 'skills, agents, rooms, MCP'],
  ['Verify', 'protocol, CAS, CI, evals'],
];

const commandRows = [
  {
    command: 'foxctl run code/semantic_search --input \'{"format":"tree"}\'',
    output: 'ranked repo map with file summaries',
  },
  {
    command: 'foxctl index repo build --workspace . --go --typescript --elixir',
    output: 'symbols, imports, calls, references',
  },
  {
    command: 'foxctl run code/dag_grep --input \'{"query":"buildEvidencePack"}\'',
    output: 'bounded explanation subgraph',
  },
];

const architecture = [
  ['CLI', 'commands, hooks, local shells'],
  ['Skills', 'WASI, native, job tracking'],
  ['Retrieval', 'semantic search, repoindex, codemaps'],
  ['Agents', 'daemon, rooms, overseer flow'],
  ['Storage', 'Turso, Postgres, CAS, vectors'],
];

export default function FoxctlHome() {
  return (
    <main className="fox-home">
      <section className="fox-hero" aria-labelledby="fox-home-title">
        <div className="fox-hero-copy">
          <div className="fox-kicker">
            <Chip className="fox-chip">local control plane</Chip>
            <Chip className="fox-chip fox-chip-muted">agentic code work</Chip>
          </div>
          <h1 id="fox-home-title">foxctl</h1>
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
        </div>

        <div className="fox-product-shot" aria-label="foxctl command surface preview">
          <div className="fox-window">
            <div className="fox-window-bar">
              <span />
              <span />
              <span />
              <strong>repo navigation</strong>
            </div>
            <div className="fox-terminal">
              {commandRows.map(row => (
                <div className="fox-command" key={row.command}>
                  <code>$ {row.command}</code>
                  <span>{row.output}</span>
                </div>
              ))}
            </div>
          </div>
          <div className="fox-graph-strip" aria-label="System path">
            {architecture.map(([name, body]) => (
              <div className="fox-node" key={name}>
                <strong>{name}</strong>
                <span>{body}</span>
              </div>
            ))}
          </div>
        </div>
      </section>

      <section className="fox-section fox-section-tight" aria-labelledby="ways-title">
        <div className="fox-section-heading">
          <Chip className="fox-chip fox-chip-muted">ways to use it</Chip>
          <h2 id="ways-title">A working loop for code agents</h2>
        </div>
        <div className="fox-workflow">
          {workflow.map(([title, body], index) => (
            <div className="fox-step" key={title}>
              <span>{String(index + 1).padStart(2, '0')}</span>
              <strong>{title}</strong>
              <p>{body}</p>
            </div>
          ))}
        </div>
      </section>

      <section className="fox-section" aria-labelledby="capabilities-title">
        <div className="fox-section-heading">
          <Chip className="fox-chip fox-chip-muted">main surfaces</Chip>
          <h2 id="capabilities-title">What foxctl does</h2>
        </div>
        <div className="fox-card-grid">
          {capabilities.map(item => (
            <Card className="fox-capability-card" key={item.title}>
              <Card.Header className="fox-card-header">
                <span className="fox-card-icon">{item.label.slice(0, 2)}</span>
                <Chip className="fox-chip fox-chip-muted">{item.label}</Chip>
              </Card.Header>
              <Card.Content className="fox-card-content">
                <Card.Title className="fox-card-title">{item.title}</Card.Title>
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
          <Chip className="fox-chip fox-chip-muted">architecture</Chip>
          <h2 id="architecture-title">Canonical storage, disposable context, durable evidence</h2>
        </div>
        <div className="fox-architecture-grid">
          <div className="fox-architecture-copy">
            <p>
              foxctl keeps protocol boundaries explicit: commands return envelopes,
              large artifacts go to CAS, repo metadata stays rebuildable, and agent
              work is anchored to durable room and session records.
            </p>
            <div className="fox-actions fox-actions-compact">
              <Link className="fox-action" href="/architecture/system/">
                System overview
              </Link>
              <Link className="fox-action" href="/reference/protocol-v1/">
                Protocol v1
              </Link>
            </div>
          </div>
          <div className="fox-stack" aria-label="foxctl architecture stack">
            {architecture.map(([name, body]) => (
              <div className="fox-stack-row" key={name}>
                <strong>{name}</strong>
                <span>{body}</span>
              </div>
            ))}
          </div>
        </div>
      </section>
    </main>
  );
}
