#!/usr/bin/env node

const args = new Set(process.argv.slice(2));
const apply = args.has('--apply');

const projectName = process.env.CLOUDFLARE_PAGES_PROJECT || 'foxctl-docs';
const productionBranch = process.env.CLOUDFLARE_PAGES_BRANCH || 'main';
const domain = process.env.CLOUDFLARE_PAGES_DOMAIN || 'foxctl.com';
const zoneName = process.env.CLOUDFLARE_ZONE_NAME || domain.split('.').slice(-2).join('.');
const pagesTarget = `${projectName}.pages.dev`;
const buildConfig = {
  build_command: process.env.CLOUDFLARE_PAGES_BUILD_COMMAND || 'bun run build:docs',
  destination_dir:
    process.env.CLOUDFLARE_PAGES_DESTINATION_DIR || 'packages/docs-site/dist',
  build_caching: true,
};
if (process.env.CLOUDFLARE_PAGES_ROOT_DIR) {
  buildConfig.root_dir = process.env.CLOUDFLARE_PAGES_ROOT_DIR;
}

const token =
  process.env.CLOUDFLARE_API_TOKEN ||
  process.env.CF_API_TOKEN ||
  process.env.TF_VAR_cloudflare_api_token;
const accountId =
  process.env.CLOUDFLARE_ACCOUNT_ID ||
  process.env.CF_ACCOUNT_ID ||
  process.env.TF_VAR_cloudflare_account_id;
const configuredZoneId =
  process.env.CLOUDFLARE_ZONE_ID ||
  process.env.CF_ZONE_ID ||
  process.env.TF_VAR_cloudflare_zone_id;

if (!token || !accountId) {
  console.error('Missing secret requirements:');
  if (!token) {
    console.error(
      '- CLOUDFLARE_API_TOKEN: needed by Cloudflare Pages setup, expected via Infisical or Agent Vault',
    );
  }
  if (!accountId) {
    console.error(
      '- CLOUDFLARE_ACCOUNT_ID: needed by Cloudflare Pages setup, expected via Infisical or Agent Vault',
    );
  }
  process.exit(1);
}

const apiBase = 'https://api.cloudflare.com/client/v4';

async function cloudflare(path, init = {}) {
  const response = await fetch(`${apiBase}${path}`, {
    ...init,
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
      ...(init.headers || {}),
    },
  });

  const text = await response.text();
  const body = text ? JSON.parse(text) : {};

  if (!response.ok || body.success === false) {
    const message =
      body?.errors?.map((error) => error.message).join('; ') ||
      `${response.status} ${response.statusText}`;
    const error = new Error(message);
    error.status = response.status;
    error.body = body;
    throw error;
  }

  return body.result;
}

async function getProject() {
  try {
    return await cloudflare(
      `/accounts/${accountId}/pages/projects/${projectName}`,
    );
  } catch (error) {
    if (error.status === 404) return null;
    throw error;
  }
}

async function getDomain() {
  try {
    return await cloudflare(
      `/accounts/${accountId}/pages/projects/${projectName}/domains/${domain}`,
    );
  } catch (error) {
    if (error.status === 404) return null;
    throw error;
  }
}

function printDomainDiagnostics(projectDomain) {
  if (!projectDomain) return;

  const validation = projectDomain.validation_data;
  if (validation) {
    console.log(`Domain validation: ${validation.status || 'unknown'}`);
    if (validation.error_message) {
      console.log(`Domain validation error: ${validation.error_message}`);
    }
  }

  const verification = projectDomain.verification_data;
  if (verification) {
    console.log(`Domain verification: ${verification.status || 'unknown'}`);
    if (verification.error_message) {
      console.log(`Domain verification error: ${verification.error_message}`);
    }
  }
}

async function getZoneId() {
  if (configuredZoneId) return configuredZoneId;

  const params = new URLSearchParams({
    name: zoneName,
    'account.id': accountId,
    per_page: '1',
  });
  const zones = await cloudflare(`/zones?${params}`);
  return zones[0]?.id || null;
}

async function getApexAddressRecords(zoneId) {
  const params = new URLSearchParams({
    name: domain,
    per_page: '100',
  });
  const records = await cloudflare(`/zones/${zoneId}/dns_records?${params}`);
  return records.filter((record) => ['A', 'AAAA', 'CNAME'].includes(record.type));
}

async function ensurePagesDns() {
  const zoneId = await getZoneId();
  if (!zoneId) {
    console.log(`No Cloudflare zone found for ${zoneName}; skipping DNS record check.`);
    return;
  }

  const records = await getApexAddressRecords(zoneId);
  const current = records.find(
    (record) => record.type === 'CNAME' && record.content === pagesTarget,
  );
  const stale = records.filter((record) => record.id !== current?.id);

  if (current && stale.length === 0) {
    console.log(`DNS is already pointed at ${pagesTarget}.`);
    return;
  }

  if (!apply) {
    if (stale.length > 0) {
      console.log(
        `Would replace ${stale.length} apex address record(s) with CNAME ${domain} -> ${pagesTarget}.`,
      );
    } else {
      console.log(`Would create CNAME ${domain} -> ${pagesTarget}.`);
    }
    return;
  }

  for (const record of stale) {
    await cloudflare(`/zones/${zoneId}/dns_records/${record.id}`, {
      method: 'DELETE',
    });
  }

  if (!current) {
    await cloudflare(`/zones/${zoneId}/dns_records`, {
      method: 'POST',
      body: JSON.stringify({
        type: 'CNAME',
        name: domain,
        content: pagesTarget,
        proxied: true,
      }),
    });
    console.log(`Created CNAME ${domain} -> ${pagesTarget}.`);
  } else {
    console.log(`Kept existing CNAME ${domain} -> ${pagesTarget}.`);
  }

  if (stale.length > 0) {
    console.log(`Removed ${stale.length} stale apex address record(s).`);
  }
}

async function retryDomainValidation(projectDomain) {
  if (!apply || !projectDomain || projectDomain.status === 'active') return;

  const retriedDomain = await cloudflare(
    `/accounts/${accountId}/pages/projects/${projectName}/domains/${domain}`,
    {
      method: 'PATCH',
      body: JSON.stringify({}),
    },
  );
  console.log(
    `Requested custom domain validation retry: ${retriedDomain.name} (${retriedDomain.status})`,
  );
}

async function main() {
  console.log(
    `${apply ? 'Applying' : 'Dry run for'} Cloudflare Pages project '${projectName}'`,
  );
  console.log(`Production branch: ${productionBranch}`);
  console.log(`Build command: ${buildConfig.build_command}`);
  console.log(`Root directory: ${buildConfig.root_dir || '(repo root)'}`);
  console.log(`Output directory: ${buildConfig.destination_dir}`);
  console.log(`Custom domain: ${domain}`);
  console.log(`DNS target: ${pagesTarget}`);

  let project = await getProject();
  if (!project) {
    if (!apply) {
      console.log('Would create Pages project.');
    } else {
      project = await cloudflare(`/accounts/${accountId}/pages/projects`, {
        method: 'POST',
        body: JSON.stringify({
          name: projectName,
          production_branch: productionBranch,
          build_config: buildConfig,
        }),
      });
      console.log(`Created Pages project: ${project.name}`);
    }
  } else {
    console.log(`Pages project exists: ${project.name}`);
  }

  if (!project && !apply) {
    console.log('Would add custom domain after project creation.');
    return;
  }

  const projectDomain = await getDomain();
  if (!projectDomain) {
    if (!apply) {
      console.log('Would add custom domain.');
    } else {
      const createdDomain = await cloudflare(
        `/accounts/${accountId}/pages/projects/${projectName}/domains`,
        {
          method: 'POST',
          body: JSON.stringify({ name: domain }),
        },
      );
      console.log(
        `Added custom domain: ${createdDomain.name} (${createdDomain.status})`,
      );
    }
  } else {
    console.log(
      `Custom domain exists: ${projectDomain.name} (${projectDomain.status})`,
    );
    printDomainDiagnostics(projectDomain);
  }

  await ensurePagesDns();
  await retryDomainValidation(projectDomain);
}

main().catch((error) => {
  console.error(`Cloudflare Pages setup failed: ${error.message}`);
  process.exit(1);
});
