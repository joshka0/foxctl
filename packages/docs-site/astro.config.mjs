import { defineConfig } from 'astro/config';
import react from '@astrojs/react';
import starlight from '@astrojs/starlight';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig({
  site: 'https://foxctl.com',
  vite: {
    plugins: [tailwindcss()],
  },
  integrations: [
    react(),
    starlight({
      title: 'foxctl',
      description:
        'Production documentation for foxctl workflows, architecture, operations, and release checks.',
      logo: {
        src: './src/assets/foxctl-logo.svg',
        alt: 'foxctl',
      },
      social: [
        {
          icon: 'github',
          label: 'GitHub',
          href: 'https://github.com/joshka0/foxctl',
        },
      ],
      customCss: ['./src/styles/starlight-overrides.css'],
      components: {
        SocialIcons: './src/components/DeveloperSocialIcons.astro',
      },
      tableOfContents: {
        minHeadingLevel: 2,
        maxHeadingLevel: 3,
      },
      sidebar: [
        {
          label: 'Start Here',
          items: [
            { slug: 'start/overview' },
            { slug: 'start/install-first-run' },
            { slug: 'start/docs-map' },
          ],
        },
        {
          label: 'Guides',
          items: [
            { slug: 'guides/designing-foxctl-features' },
            { slug: 'guides/add-a-skill' },
            { slug: 'workflows/repo-navigation' },
            { slug: 'workflows/agents-and-rooms' },
          ],
        },
        {
          label: 'Core Workflows',
          items: [
            { slug: 'skills/runtime-and-install' },
            { slug: 'retrieval/search-and-index' },
            { slug: 'retrieval/repoindex-and-dag-grep' },
            { slug: 'retrieval/repoindex-model' },
            { slug: 'retrieval/repoindex-pageindex' },
            { slug: 'context/contextwiki' },
            { slug: 'context/context-engine' },
            { slug: 'memory/continuity' },
          ],
        },
        {
          label: 'Agents and Rooms',
          items: [
            { slug: 'agents/lifecycle' },
            { slug: 'agents/orchestration' },
            { slug: 'collaboration/rooms' },
          ],
        },
        {
          label: 'Integrations',
          items: [
            { slug: 'integrations/status' },
            { slug: 'integrations/providers-and-mcp' },
            { slug: 'integrations/openapi-and-plugins' },
            { slug: 'integrations/chat-platforms' },
            { slug: 'integrations/hooks' },
            { slug: 'context/obsidian-bridge' },
          ],
        },
        {
          label: 'Reference',
          items: [
            { slug: 'reference/cli' },
            { slug: 'reference/command-map' },
            { slug: 'reference/glossary' },
            { slug: 'reference/protocol-v1' },
            { slug: 'storage/cas-and-persistence' },
          ],
        },
        {
          label: 'Architecture',
          items: [
            { slug: 'architecture/system' },
            { slug: 'architecture/design-principles' },
            { slug: 'architecture/runtime' },
            { slug: 'architecture/api-server' },
            { slug: 'architecture/auth-and-identity' },
          ],
        },
        {
          label: 'Operations',
          items: [
            { slug: 'operations/troubleshooting' },
            { slug: 'operations/gotchas' },
            { slug: 'operations/observability' },
            { slug: 'quality/ci-and-evals' },
            { slug: 'quality/benchmarks' },
            { slug: 'deployment/kubernetes' },
          ],
        },
        {
          label: 'Production',
          items: [{ slug: 'production/verification' }],
        },
        {
          label: 'Roadmap and Archive',
          items: [{ slug: 'roadmap/progress' }, { slug: 'roadmap/planned-and-archive' }],
        },
      ],
    }),
  ],
});
