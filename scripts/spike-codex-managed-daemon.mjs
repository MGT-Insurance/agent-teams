#!/usr/bin/env node

// Live integration spike: creates Codex threads and kills only the managed
// app-server daemon it starts. Require an explicit flag to spend model usage.
if (!process.argv.includes("--run-live")) {
  process.stderr.write(
    "Refusing to run a live Codex spike without --run-live. This command uses real model turns.\n",
  );
  process.exit(2);
}

import { spawnSync } from "node:child_process";
import { randomBytes } from "node:crypto";
import { createConnection } from "node:net";
import { setTimeout as delay } from "node:timers/promises";

const cwd = process.cwd();
const socketPath = `${process.env.HOME}/.codex/app-server-control/app-server-control.sock`;
const observations = [];
const clients = new Set();
let daemonOwned = false;

function record(name, value) {
  const entry = { at: new Date().toISOString(), name, value };
  observations.push(entry);
  process.stderr.write(`[managed-spike] ${name}: ${JSON.stringify(value)}\n`);
  return value;
}

function runCodex(args, { allowFailure = false } = {}) {
  const result = spawnSync("codex", args, { cwd, encoding: "utf8", env: process.env });
  if (!allowFailure && result.status !== 0) {
    throw new Error(
      `codex ${args.join(" ")} failed (${result.status}): ${result.stderr || result.stdout}`,
    );
  }
  return {
    status: result.status,
    stdout: result.stdout.trim(),
    stderr: result.stderr.trim(),
  };
}

function parsedStdout(result) {
  return result.stdout ? JSON.parse(result.stdout) : null;
}

function daemonVersion() {
  return runCodex(["app-server", "daemon", "version"], { allowFailure: true });
}

function processSnapshot(pid) {
  const result = spawnSync("ps", ["-o", "pid=,ppid=,command=", "-p", String(pid)], {
    encoding: "utf8",
  });
  return { status: result.status, line: result.stdout.trim(), stderr: result.stderr.trim() };
}

class UnixWebSocket {
  constructor(path) {
    this.path = path;
    this.buffer = Buffer.alloc(0);
    this.messages = [];
    this.waiters = [];
  }

  async connect() {
    this.socket = createConnection(this.path);
    await new Promise((resolve, reject) => {
      this.socket.once("connect", resolve);
      this.socket.once("error", reject);
    });
    const key = randomBytes(16).toString("base64");
    this.socket.write(
      `GET / HTTP/1.1\r\nHost: localhost\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: ${key}\r\nSec-WebSocket-Version: 13\r\n\r\n`,
    );
    let handshake = Buffer.alloc(0);
    await new Promise((resolve, reject) => {
      const onData = (chunk) => {
        handshake = Buffer.concat([handshake, chunk]);
        const end = handshake.indexOf("\r\n\r\n");
        if (end < 0) return;
        this.socket.off("data", onData);
        const head = handshake.subarray(0, end + 4).toString("utf8");
        if (!head.startsWith("HTTP/1.1 101")) {
          reject(new Error(`WebSocket handshake failed: ${head}`));
          return;
        }
        this.socket.on("data", (data) => this.onData(data));
        const rest = handshake.subarray(end + 4);
        if (rest.length) this.onData(rest);
        resolve();
      };
      this.socket.on("data", onData);
      this.socket.once("error", reject);
    });
  }

  frame(opcode, payload) {
    const body = Buffer.isBuffer(payload) ? payload : Buffer.from(payload);
    const mask = randomBytes(4);
    let header;
    if (body.length < 126) {
      header = Buffer.from([0x80 | opcode, 0x80 | body.length]);
    } else if (body.length <= 0xffff) {
      header = Buffer.alloc(4);
      header[0] = 0x80 | opcode;
      header[1] = 0x80 | 126;
      header.writeUInt16BE(body.length, 2);
    } else {
      header = Buffer.alloc(10);
      header[0] = 0x80 | opcode;
      header[1] = 0x80 | 127;
      header.writeBigUInt64BE(BigInt(body.length), 2);
    }
    const masked = Buffer.alloc(body.length);
    for (let i = 0; i < body.length; i += 1) masked[i] = body[i] ^ mask[i % 4];
    return Buffer.concat([header, mask, masked]);
  }

  send(message) {
    this.socket.write(this.frame(0x1, JSON.stringify(message)));
  }

  onData(chunk) {
    this.buffer = Buffer.concat([this.buffer, chunk]);
    while (this.buffer.length >= 2) {
      const first = this.buffer[0];
      const second = this.buffer[1];
      let length = second & 0x7f;
      let offset = 2;
      if (length === 126) {
        if (this.buffer.length < 4) return;
        length = this.buffer.readUInt16BE(2);
        offset = 4;
      } else if (length === 127) {
        if (this.buffer.length < 10) return;
        length = Number(this.buffer.readBigUInt64BE(2));
        offset = 10;
      }
      const masked = Boolean(second & 0x80);
      const maskLength = masked ? 4 : 0;
      if (this.buffer.length < offset + maskLength + length) return;
      const mask = masked ? this.buffer.subarray(offset, offset + 4) : null;
      offset += maskLength;
      const payload = Buffer.from(this.buffer.subarray(offset, offset + length));
      this.buffer = this.buffer.subarray(offset + length);
      if (mask) {
        for (let i = 0; i < payload.length; i += 1) payload[i] ^= mask[i % 4];
      }
      const opcode = first & 0x0f;
      if (opcode === 0x9) this.socket.write(this.frame(0x0a, payload));
      else if (opcode === 0x1) this.deliver(JSON.parse(payload.toString("utf8")));
      else if (opcode === 0x8) this.socket.end();
    }
  }

  deliver(message) {
    this.messages.push(message);
    const remaining = [];
    for (const waiter of this.waiters) {
      if (waiter.predicate(message)) waiter.resolve(message);
      else remaining.push(waiter);
    }
    this.waiters = remaining;
  }

  waitFor(predicate, timeoutMs = 30_000) {
    const existing = this.messages.find(predicate);
    if (existing) return Promise.resolve(existing);
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error("timed out waiting for WebSocket message")), timeoutMs);
      this.waiters.push({
        predicate,
        resolve: (message) => {
          clearTimeout(timer);
          resolve(message);
        },
      });
    });
  }

  close() {
    if (!this.socket.destroyed) this.socket.end(this.frame(0x8, Buffer.alloc(0)));
  }
}

class Client {
  constructor(name) {
    this.name = name;
    this.nextId = 1;
    this.ws = new UnixWebSocket(socketPath);
    clients.add(this);
  }

  async initialize() {
    await this.ws.connect();
    const result = await this.request("initialize", {
      clientInfo: {
        name: "agent_teams_managed_daemon_spike",
        title: "agent-teams managed daemon spike",
        version: "0.1.0",
      },
    });
    this.ws.send({ method: "initialized", params: {} });
    return result;
  }

  async request(method, params = undefined, timeoutMs = 30_000) {
    const id = this.nextId++;
    this.ws.send(params === undefined ? { method, id } : { method, id, params });
    const response = await this.ws.waitFor((message) => message.id === id, timeoutMs);
    if (response.error) throw Object.assign(new Error(response.error.message), { response });
    return response.result;
  }

  close() {
    clients.delete(this);
    this.ws.close();
  }
}

async function connect(name) {
  const client = new Client(name);
  record(`${name}.initialize`, await client.initialize());
  return client;
}

function turnParams(threadId, text) {
  return {
    threadId,
    input: [{ type: "text", text }],
    cwd,
    approvalPolicy: "never",
    sandboxPolicy: { type: "dangerFullAccess" },
  };
}

async function readThread(client, threadId) {
  return (await client.request("thread/read", { threadId, includeTurns: true })).thread;
}

async function waitForTurnTerminal(client, threadId, turnId, timeoutMs = 60_000) {
  const deadline = Date.now() + timeoutMs;
  let last;
  while (Date.now() < deadline) {
    last = await readThread(client, threadId);
    const turn = last.turns?.find((candidate) => candidate.id === turnId);
    if (turn && turn.status !== "inProgress" && turn.completedAt !== null) {
      return { thread: last, turn };
    }
    await delay(500);
  }
  throw new Error(`turn ${turnId} did not become terminal: ${JSON.stringify(last?.status)}`);
}

async function waitForStableIdle(client, threadId, timeoutMs = 30_000) {
  const deadline = Date.now() + timeoutMs;
  let consecutive = 0;
  let last;
  while (Date.now() < deadline) {
    last = await readThread(client, threadId);
    const hasInProgressTurn = last.turns?.some((turn) => turn.status === "inProgress");
    if (last.status?.type === "idle" && !hasInProgressTurn) consecutive += 1;
    else consecutive = 0;
    if (consecutive >= 3) return last;
    await delay(300);
  }
  throw new Error(`thread ${threadId} did not become stably idle: ${JSON.stringify(last?.status)}`);
}

async function waitForDaemonDown(timeoutMs = 10_000) {
  const deadline = Date.now() + timeoutMs;
  let last;
  while (Date.now() < deadline) {
    last = daemonVersion();
    if (last.status !== 0) return last;
    await delay(100);
  }
  throw new Error(`managed daemon remained reachable: ${JSON.stringify(last)}`);
}

async function main() {
  record("codex.version", runCodex(["--version"]));
  const before = daemonVersion();
  record("daemon.before", before);
  if (before.status === 0) {
    throw new Error("managed daemon already running; refusing to disrupt a daemon the spike did not start");
  }

  const started = runCodex(["app-server", "daemon", "start"]);
  const startInfo = parsedStdout(started);
  daemonOwned = true;
  record("daemon.start", startInfo);
  record("daemon.process", processSnapshot(startInfo.pid));
  record("daemon.idempotent-start", parsedStdout(runCodex(["app-server", "daemon", "start"])));

  let client = await connect("client-a");
  const threadStart = await client.request("thread/start", {
    cwd,
    approvalPolicy: "never",
    sandbox: "danger-full-access",
    serviceName: "agent_teams_managed_daemon_spike",
  });
  const threadId = threadStart.thread.id;
  record("thread.start", { threadId, status: threadStart.thread.status, source: threadStart.thread.source });

  const disconnectTurn = await client.request(
    "turn/start",
    turnParams(
      threadId,
      "Run the shell command `sleep 8`. Then report any newer instruction received while running.",
    ),
  );
  record("disconnect.turn-start", disconnectTurn.turn);
  await delay(1_000);
  client.close();
  client = await connect("client-b");
  const afterDisconnect = await readThread(client, threadId);
  record("disconnect.thread-read", {
    status: afterDisconnect.status,
    turn: afterDisconnect.turns?.find((turn) => turn.id === disconnectTurn.turn.id),
  });
  record(
    "disconnect.steer",
    await client.request("turn/steer", {
      threadId,
      expectedTurnId: disconnectTurn.turn.id,
      input: [{ type: "text", text: "MANAGED_RECONNECT_MAIL_2d59af. Reply with that nonce now." }],
    }),
  );
  const disconnectCompleted = await waitForTurnTerminal(client, threadId, disconnectTurn.turn.id);
  record("disconnect.completed", disconnectCompleted);
  await waitForStableIdle(client, threadId);

  const gracefulTurn = await client.request(
    "turn/start",
    turnParams(threadId, "Run the shell command `sleep 8`, then reply MANAGED_GRACEFUL_DRAIN."),
  );
  record("graceful.turn-start", gracefulTurn.turn);
  await delay(1_000);
  const gracefulStartedAt = Date.now();
  record("graceful.stop", {
    ...runCodex(["app-server", "daemon", "stop"]),
    elapsedMs: Date.now() - gracefulStartedAt,
  });
  daemonOwned = false;
  const restartedAfterGrace = parsedStdout(runCodex(["app-server", "daemon", "start"]));
  daemonOwned = true;
  record("graceful.restart", restartedAfterGrace);
  client = await connect("client-c");
  const afterGrace = await readThread(client, threadId);
  record("graceful.thread-read", {
    status: afterGrace.status,
    turn: afterGrace.turns?.find((turn) => turn.id === gracefulTurn.turn.id),
  });

  await client.request("thread/resume", { threadId });
  await waitForStableIdle(client, threadId);
  const crashTurn = await client.request(
    "turn/start",
    turnParams(threadId, "Run the shell command `sleep 20`, then reply MANAGED_NATIVE_CRASH_FAILED."),
  );
  record("crash.turn-start", crashTurn.turn);
  await delay(1_000);
  process.kill(restartedAfterGrace.pid, "SIGKILL");
  record("crash.signal", { pid: restartedAfterGrace.pid, signal: "SIGKILL" });
  record("crash.daemon-down", await waitForDaemonDown());
  daemonOwned = false;

  const restartedAfterCrash = parsedStdout(runCodex(["app-server", "daemon", "start"]));
  daemonOwned = true;
  record("crash.restart", restartedAfterCrash);
  client = await connect("client-d");
  const afterCrash = await readThread(client, threadId);
  record("crash.thread-read", {
    status: afterCrash.status,
    turn: afterCrash.turns?.find((turn) => turn.id === crashTurn.turn.id),
  });
  const resumed = await client.request("thread/resume", { threadId });
  record("crash.thread-resume", { status: resumed.thread.status, threadId: resumed.thread.id });
  await waitForStableIdle(client, threadId);
  const recoveryTurn = await client.request(
    "turn/start",
    turnParams(threadId, "Reply exactly MANAGED_DAEMON_RECOVERED_71cb4e."),
  );
  record("crash.recovery-turn", recoveryTurn.turn);
  record("crash.completed", await waitForTurnTerminal(client, threadId, recoveryTurn.turn.id));

  const listed = await client.request("thread/list", { limit: 100, cwd });
  record("thread.list", {
    found: listed.data.some((thread) => thread.id === threadId),
    match: listed.data.find((thread) => thread.id === threadId),
  });
  client.close();
  record("result", "completed");
}

try {
  await main();
} catch (error) {
  record("result", { error: error.stack || String(error) });
  process.exitCode = 1;
} finally {
  for (const client of clients) client.close();
  if (daemonOwned) {
    record("daemon.cleanup", runCodex(["app-server", "daemon", "stop"], { allowFailure: true }));
  }
  process.stdout.write(`${JSON.stringify({ cwd, socketPath, observations }, null, 2)}\n`);
}
