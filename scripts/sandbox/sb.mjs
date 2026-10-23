// Hand-run driver for the ring-1 Vercel sandbox (docs/hosted-beads.md, RUNBOOK).
//
//   node sb.mjs create [--name N] [--gh]   create a persistent sandbox, apply the policy, extend the timeout
//   node sb.mjs exec '<cmd>' [--sudo]      run `bash -lc <cmd>` in the sandbox
//   node sb.mjs put <local> <remote> [mode] copy a local file in (mode like 600); never prints contents
//   node sb.mjs policy [--print] [--gh]    print (no network, no token) or apply the network policy
//   node sb.mjs extend                     extend the session timeout to the 24 h cap
//   node sb.mjs resume                     resume a stopped sandbox, then extend the timeout
//   node sb.mjs stop | info | delete
//
// Sandbox name: --name N, else $SB_NAME, else "ring1". The VM host identity is sandbox-<name>.
// Auth: VERCEL_OIDC_TOKEN (and optionally AI_GATEWAY_API_KEY) come from the env file named by
// $SB_ENV_FILE, default ./.env.pulled next to this script (gitignored). The file is parsed in
// memory and never printed. The gateway token goes only into the firewall transform, so it never
// enters the VM.
import fs from 'node:fs';
import dns from 'node:dns/promises';
import { execFileSync } from 'node:child_process';

const HERE = new URL('.', import.meta.url);
const DB_HOST = process.env.HOSTED_DB_HOST || 'mgt-spike.dbs.hosted.doltdb.com';
const DAY_MS = 24 * 3600e3;
const REDACTED = '<redacted>';

// Hosts listed explicitly: a '*': [] wildcard shadows the gateway transform (ring-0 spike).
const HOSTS = [
  'registry.npmjs.org', 'registry.npmjs.com', '*.npmjs.org', 'nodejs.org',
  'github.com', 'api.github.com', 'codeload.github.com', 'raw.githubusercontent.com',
  'objects.githubusercontent.com', 'release-assets.githubusercontent.com',
  'binaries.prisma.sh', 'claude.ai', 'downloads.claude.ai', 'storage.googleapis.com',
  'api.anthropic.com', '*.ubuntu.com', 'pypi.org', 'files.pythonhosted.org',
];

const argv = process.argv.slice(2);
const flag = (f) => argv.includes(f);
const optVal = (f) => { const i = argv.indexOf(f); return i >= 0 ? argv[i + 1] : undefined; };
const positional = () => {
  const out = [];
  for (let i = 0; i < argv.length; i++) {
    if (argv[i] === '--name') { i++; continue; }
    if (argv[i].startsWith('--')) continue;
    out.push(argv[i]);
  }
  return out;
};
const [cmd, ...args] = positional();
const NAME = optVal('--name') || process.env.SB_NAME || 'ring1';
const t0 = Date.now();
const el = () => ((Date.now() - t0) / 1000).toFixed(1) + 's';

function loadEnv() {
  const file = process.env.SB_ENV_FILE || new URL('.env.pulled', HERE).pathname;
  const E = {};
  for (const l of fs.readFileSync(file, 'utf8').split('\n')) {
    const m = l.match(/^([A-Z_0-9]+)="?(.*?)"?$/);
    if (m) E[m[1]] = m[2];
  }
  if (!E.VERCEL_OIDC_TOKEN) throw new Error(`VERCEL_OIDC_TOKEN missing from ${file}`);
  process.env.VERCEL_OIDC_TOKEN = E.VERCEL_OIDC_TOKEN;
  return E;
}

// F12: the domain AND the /32 of every A record. MySQL speaks first, so a domain-only allow
// cuts the connection.
async function egressSubnets() {
  const ips = [...new Set(await dns.resolve4(DB_HOST))].sort();
  if (ips.length === 0) throw new Error(`no A records for ${DB_HOST}`);
  return ips.map((ip) => `${ip}/32`);
}

// tokens: { gateway, gh } are real values when applying, REDACTED sentinels when printing.
async function buildPolicy({ gateway, gh }) {
  const allow = {};
  for (const h of HOSTS) allow[h] = [];
  allow[DB_HOST] = [];
  allow['ai-gateway.vercel.sh'] = [{ transform: [{ headers: { authorization: `Bearer ${gateway}` } }] }];
  if (gh) {
    const basic = gh === REDACTED ? REDACTED : Buffer.from(`x-access-token:${gh}`).toString('base64');
    allow['github.com'] = [{ transform: [{ headers: { authorization: `Basic ${basic}` } }] }];
    allow['api.github.com'] = [{ transform: [{ headers: { authorization: `Bearer ${gh}` } }] }];
  }
  return { allow, subnets: { allow: await egressSubnets() } };
}

function ghToken() {
  return process.env.GH_TOKEN || execFileSync('gh', ['auth', 'token']).toString().trim();
}

async function realPolicy() {
  const E = loadEnv();
  return buildPolicy({ gateway: E.AI_GATEWAY_API_KEY || E.VERCEL_OIDC_TOKEN, gh: flag('--gh') ? ghToken() : null });
}

let SDK;
async function sandboxes() {
  if (!SDK) { SDK = (await import('@vercel/sandbox')).Sandbox; }
  return SDK;
}
async function get(resume = true) {
  const S = await sandboxes();
  return S.get({ name: NAME, resume });
}

async function extendToCap(sb) {
  const add = DAY_MS - (sb.timeout || 0);
  if (add <= 0) { console.log('timeout already at cap', sb.timeout); return; }
  try { await sb.extendTimeout(add); console.log('extended to 24h cap', el()); }
  catch (e) { console.log('extend error:', e.message); }
}

if (cmd === 'policy' && flag('--print')) {
  const p = await buildPolicy({ gateway: REDACTED, gh: flag('--gh') ? REDACTED : null });
  console.log(JSON.stringify(p, null, 2));
} else if (cmd === 'policy') {
  const sb = await get();
  await sb.update({ networkPolicy: await realPolicy() });
  console.log('policy updated', el());
} else if (cmd === 'create') {
  loadEnv();
  const S = await sandboxes();
  const sb = await S.create({
    name: NAME, image: 'vercel/sandbox/universal', timeout: 2 * 3600e3, resources: { vcpus: 8 },
    networkPolicy: await realPolicy(), persistent: true, tags: { purpose: 'ring1-hosted-beads' },
  });
  console.log('created', sb.name, 'host identity sandbox-' + sb.name, el());
  await extendToCap(sb);
} else if (cmd === 'exec') {
  loadEnv();
  const sb = await get();
  const r = await sb.runCommand({ cmd: 'bash', args: ['-lc', args[0]], sudo: flag('--sudo') });
  console.log(await r.stdout());
  const e = await r.stderr(); if (e) console.error(e);
  console.log('exit', r.exitCode, el());
  process.exitCode = r.exitCode;
} else if (cmd === 'put') {
  loadEnv();
  const [local, remote, mode] = args;
  if (!local || !remote) throw new Error('usage: put <local> <remote> [mode]');
  const sb = await get();
  let dest = remote;
  if (!dest.startsWith('/')) {
    const h = await sb.runCommand({ cmd: 'bash', args: ['-lc', 'printf %s "$HOME"'] });
    dest = `${(await h.stdout()).trim()}/${remote}`;
  }
  await sb.writeFiles([{ path: dest, content: fs.readFileSync(local) }]);
  if (mode) await sb.runCommand({ cmd: 'chmod', args: [mode, dest] });
  console.log('put ok', dest, mode || '', el());
} else if (cmd === 'extend') {
  loadEnv();
  await extendToCap(await get());
} else if (cmd === 'resume') {
  loadEnv();
  const sb = await get(true);
  console.log('resumed', sb.name, sb.status, el());
  await extendToCap(sb);
} else if (cmd === 'stop') {
  loadEnv();
  const sb = await get(false);
  const r = await sb.stop();
  console.log('stopped', JSON.stringify(r).slice(0, 600), el());
} else if (cmd === 'info') {
  loadEnv();
  const sb = await get(false);
  console.log(sb.name, sb.status, 'timeout', sb.timeout, 'snapshot', sb.currentSnapshotId, 'persistent', sb.persistent);
} else if (cmd === 'delete') {
  loadEnv();
  const sb = await get(false);
  await sb.delete();
  console.log('deleted', NAME, el());
} else {
  console.error('usage: node sb.mjs create|exec|put|policy|extend|resume|stop|info|delete (see header)');
  process.exitCode = 2;
}
