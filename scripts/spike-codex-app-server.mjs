#!/usr/bin/env node

// This is a live integration spike: it creates Codex threads, runs model turns,
// and kills only the app-server processes it starts. Require an explicit flag
// so syntax checks and casual invocations cannot spend model usage.
if (!process.argv.includes("--run-live")) {
  process.stderr.write(
    "Refusing to run a live Codex spike without --run-live. This command uses real model turns.\n",
  );
  process.exit(2);
}

import { spawn, spawnSync } from "node:child_process";
import { once } from "node:events";
import { createServer } from "node:net";
import { setTimeout as delay } from "node:timers/promises";

const cwd = process.cwd();
const model = process.env.ATEAM_CODEX_SPIKE_MODEL || null;
const observations = [];
const clients = new Set();
let server = null;
let serverPort = null;
let serverUrl = null;

function record(name, value) {
  const entry = { at: new Date().toISOString(), name, value };
  observations.push(entry);
  process.stderr.write(`[spike] ${name}: ${JSON.stringify(value)}\n`);
  return value;
}

function runCodex(args, { allowFailure = false } = {}) {
  const result = spawnSync("codex", args, {
    cwd,
    encoding: "utf8",
    env: process.env,
  });
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

function findNativeChild(parentPid) {
  const result = spawnSync("ps", ["-axo", "pid=,ppid=,command="], { encoding: "utf8" });
  if (result.status !== 0) return null;
  for (const line of result.stdout.split("\n")) {
    const match = line.match(/^\s*(\d+)\s+(\d+)\s+(.+)$/);
    if (!match) continue;
    if (Number(match[2]) !== parentPid) continue;
    if (match[3].includes("codex app-server") && match[3].includes(serverUrl)) {
      return Number(match[1]);
    }
  }
  return null;
}

async function waitForProcessExit(pid, timeoutMs = 5_000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      process.kill(pid, 0);
    } catch {
      return;
    }
    await delay(50);
  }
  throw new Error(`process ${pid} did not exit within ${timeoutMs}ms`);
}

async function startServer() {
  if (serverPort === null) {
    const reservation = createServer();
    reservation.listen(0, "127.0.0.1");
    await once(reservation, "listening");
    serverPort = reservation.address().port;
    reservation.close();
    await once(reservation, "close");
    serverUrl = `ws://127.0.0.1:${serverPort}`;
  }
  let stderr = "";
  const proc = spawn("codex", ["app-server", "--listen", serverUrl], {
    cwd,
    env: process.env,
    stdio: ["ignore", "ignore", "pipe"],
  });
  proc.stderr.on("data", (chunk) => {
    stderr += chunk.toString();
  });
  const deadline = Date.now() + 10_000;
  let ready = false;
  while (!ready && Date.now() < deadline) {
    if (proc.exitCode !== null) {
      throw new Error(`transient app-server exited (${proc.exitCode}): ${stderr}`);
    }
    try {
      const response = await fetch(`http://127.0.0.1:${serverPort}/readyz`);
      ready = response.ok;
    } catch {
      // Listener is not ready yet.
    }
    await delay(50);
  }
  if (!ready) {
    proc.kill("SIGTERM");
    throw new Error(`transient app-server did not become ready at ${serverUrl}: ${stderr}`);
  }
  const nativePid = findNativeChild(proc.pid);
  if (nativePid === null) {
    proc.kill("SIGTERM");
    throw new Error(`could not identify native app-server child of launcher ${proc.pid}`);
  }
  server = { proc, nativePid, stderr: () => stderr };
  return { launcherPid: proc.pid, nativePid, serverUrl };
}

async function stopServer() {
  if (!server) return { stopped: false };
  const current = server;
  server = null;
  if (current.proc.exitCode !== null || current.proc.signalCode !== null) {
    return {
      stopped: true,
      code: current.proc.exitCode,
      signal: current.proc.signalCode,
      alreadyExited: true,
      stderr: current.stderr(),
    };
  }
  const exited = once(current.proc, "exit");
  process.kill(current.nativePid, "SIGTERM");
  const [code, signal] = await exited;
  return { stopped: true, code, signal, nativePid: current.nativePid, stderr: current.stderr() };
}

async function crashServer() {
  if (!server) return { stopped: false };
  const current = server;
  server = null;
  if (current.proc.exitCode !== null || current.proc.signalCode !== null) {
    process.kill(current.nativePid, "SIGKILL");
    await waitForProcessExit(current.nativePid);
    return {
      stopped: true,
      code: current.proc.exitCode,
      signal: "SIGKILL",
      nativePid: current.nativePid,
      launcherAlreadyExited: true,
      stderr: current.stderr(),
    };
  }
  const exited = once(current.proc, "exit");
  process.kill(current.nativePid, "SIGKILL");
  const [code, signal] = await exited;
  return { stopped: true, code, signal, nativePid: current.nativePid, stderr: current.stderr() };
}

async function crashLauncher() {
  if (!server) return { stopped: false };
  const exited = once(server.proc, "exit");
  server.proc.kill("SIGKILL");
  const [code, signal] = await exited;
  let nativeAlive = true;
  try {
    process.kill(server.nativePid, 0);
  } catch {
    nativeAlive = false;
  }
  return { code, signal, launcherPid: server.proc.pid, nativePid: server.nativePid, nativeAlive };
}

class Client {
  constructor(name) {
    this.name = name;
    this.nextId = 1;
    this.pending = new Map();
    this.messages = [];
    this.waiters = [];
    this.stderr = "";
    this.ws = new WebSocket(serverUrl);
    clients.add(this);
    this.ready = new Promise((resolve, reject) => {
      this.ws.addEventListener("open", resolve, { once: true });
      this.ws.addEventListener("error", reject, { once: true });
    });
    this.ws.addEventListener("close", (event) => {
      clients.delete(this);
      const error = new Error(`${this.name} websocket closed (code=${event.code})`);
      for (const { reject } of this.pending.values()) reject(error);
      this.pending.clear();
      for (const waiter of this.waiters) waiter.reject(error);
      this.waiters = [];
    });
    this.ws.addEventListener("message", (event) => {
      const line = String(event.data);
      let message;
      try {
        message = JSON.parse(line);
      } catch (error) {
        record(`${this.name}.invalid-json`, { line, error: String(error) });
        return;
      }
      this.messages.push(message);
      if (message.id !== undefined && this.pending.has(message.id)) {
        const pending = this.pending.get(message.id);
        this.pending.delete(message.id);
        if (message.error) pending.reject(Object.assign(new Error(message.error.message), { response: message }));
        else pending.resolve(message.result);
      }
      const remaining = [];
      for (const waiter of this.waiters) {
        if (waiter.predicate(message)) waiter.resolve(message);
        else remaining.push(waiter);
      }
      this.waiters = remaining;
    });
  }

  send(message) {
    this.ws.send(JSON.stringify(message));
  }

  async request(method, params = undefined, timeoutMs = 30_000) {
    await this.ready;
    const id = this.nextId++;
    const response = new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject });
    });
    this.send(params === undefined ? { method, id } : { method, id, params });
    let timeout;
    try {
      return await Promise.race([
        response,
        new Promise((_, reject) => {
          timeout = setTimeout(() => {
            this.pending.delete(id);
            reject(new Error(`${this.name} timed out waiting for ${method}`));
          }, timeoutMs);
        }),
      ]);
    } finally {
      clearTimeout(timeout);
    }
  }

  async initialize() {
    await this.ready;
    const result = await this.request("initialize", {
      clientInfo: {
        name: "agent_teams_app_server_spike",
        title: "agent-teams app-server spike",
        version: "0.1.0",
      },
    });
    this.send({ method: "initialized", params: {} });
    return result;
  }

  waitFor(predicate, timeoutMs = 30_000) {
    const existing = this.messages.find(predicate);
    if (existing) return Promise.resolve(existing);
    return new Promise((resolve, reject) => {
      const timeout = setTimeout(() => {
        reject(new Error(`${this.name} timed out waiting for notification`));
      }, timeoutMs);
      this.waiters.push({
        predicate,
        resolve: (message) => {
          clearTimeout(timeout);
          resolve(message);
        },
        reject: (error) => {
          clearTimeout(timeout);
          reject(error);
        },
      });
    });
  }

  disconnect() {
    if (this.ws.readyState < WebSocket.CLOSING) this.ws.close();
  }
}

async function connect(name) {
  const client = new Client(name);
  record(`${name}.initialize`, await client.initialize());
  return client;
}

async function readThread(client, threadId, includeTurns = false) {
  return (await client.request("thread/read", { threadId, includeTurns })).thread;
}

async function waitForStableIdle(client, threadId, timeoutMs = 30_000) {
  const deadline = Date.now() + timeoutMs;
  let consecutive = 0;
  let last;
  while (Date.now() < deadline) {
    last = await readThread(client, threadId, true);
    const hasInProgressTurn = last.turns?.some((turn) => turn.status === "inProgress");
    if (last.status?.type === "idle" && !hasInProgressTurn) consecutive += 1;
    else consecutive = 0;
    if (consecutive >= 3) return last;
    await delay(300);
  }
  throw new Error(`thread ${threadId} did not become stably idle: ${JSON.stringify(last?.status)}`);
}

async function waitForTurnTerminal(client, threadId, turnId, timeoutMs = 60_000) {
  const deadline = Date.now() + timeoutMs;
  let lastThread;
  while (Date.now() < deadline) {
    lastThread = await readThread(client, threadId, true);
    const turn = lastThread.turns?.find((candidate) => candidate.id === turnId);
    if (turn && turn.status !== "inProgress" && turn.completedAt !== null) {
      return { thread: lastThread, turn };
    }
    await delay(500);
  }
  throw new Error(
    `turn ${turnId} did not become terminal; thread=${JSON.stringify(lastThread?.status)}`,
  );
}

function startParams() {
  return {
    cwd,
    approvalPolicy: "never",
    sandbox: "danger-full-access",
    serviceName: "agent_teams_app_server_spike",
    threadSource: "appServer",
    ...(model ? { model } : {}),
  };
}

function turnParams(threadId, text) {
  return {
    threadId,
    input: [{ type: "text", text }],
    cwd,
    approvalPolicy: "never",
    sandboxPolicy: { type: "dangerFullAccess" },
    ...(model ? { model } : {}),
  };
}

async function main() {
  record("codex.version", runCodex(["--version"]));
  const before = runCodex(["app-server", "daemon", "version"], { allowFailure: true });
  record("daemon.before", before);
  if (before.status === 0) {
    throw new Error("A managed Codex app-server daemon is already running; refusing to disrupt it");
  }

  const managedStart = runCodex(["app-server", "daemon", "start"], { allowFailure: true });
  record("managed-daemon.start", managedStart);
  if (managedStart.status === 0) {
    runCodex(["app-server", "daemon", "stop"], { allowFailure: true });
    throw new Error("Managed daemon unexpectedly started; rerun after confirming no other client uses it");
  }
  record("transient-server.start", await startServer());

  let client = await connect("client-a");
  const started = await client.request("thread/start", startParams());
  const threadId = started.thread.id;
  record("thread.start", {
    threadId,
    sessionId: started.thread.sessionId,
    source: started.thread.source,
    status: started.thread.status,
  });

  const disconnectTurn = await client.request(
    "turn/start",
    turnParams(
      threadId,
      "Run the shell command `sleep 8`. Do not answer before it finishes. Then reply exactly CLIENT_DISCONNECT_SURVIVED.",
    ),
  );
  record("disconnect.turn-start", disconnectTurn.turn);
  await delay(1_000);
  client.disconnect();
  record("disconnect.client-a", "proxy terminated while turn was active");

  await delay(1_000);
  client = await connect("client-b");
  const afterDisconnect = await readThread(client, threadId, true);
  record("disconnect.thread-read", {
    status: afterDisconnect.status,
    lastTurn: afterDisconnect.turns?.at(-1),
  });

  let concurrentTurnId = null;
  try {
    const concurrent = await client.request(
      "turn/start",
      turnParams(threadId, "Reply exactly CONCURRENT_TURN_WAS_ACCEPTED."),
    );
    concurrentTurnId = concurrent.turn.id;
    record("concurrent-turn", { accepted: true, turn: concurrent.turn });
  } catch (error) {
    record("concurrent-turn", { accepted: false, response: error.response });
  }

  const afterDisconnectCompleted = await waitForTurnTerminal(
    client,
    threadId,
    disconnectTurn.turn.id,
    60_000,
  );
  record("disconnect.completed", {
    status: afterDisconnectCompleted.thread.status,
    turn: afterDisconnectCompleted.turn,
    concurrentResponseBecameDistinctTurn: concurrentTurnId
      ? afterDisconnectCompleted.thread.turns?.some((turn) => turn.id === concurrentTurnId)
      : false,
  });
  await waitForStableIdle(client, threadId);

  const steerTurn = await client.request(
    "turn/start",
    turnParams(
      threadId,
      "Run the shell command `sleep 8`. After it finishes, report any additional instruction that arrived while you were working.",
    ),
  );
  record("steer.turn-start", steerTurn.turn);
  await delay(1_000);
  const steer = await client.request("turn/steer", {
    threadId,
    expectedTurnId: steerTurn.turn.id,
    input: [{ type: "text", text: "MAIL_WAKE_NONCE_7f3a9c arrived while the turn was active." }],
  });
  record("steer.response", steer);
  const steerCompleted = await waitForTurnTerminal(client, threadId, steerTurn.turn.id, 60_000);
  record("steer.completed", {
    status: steerCompleted.thread.status,
    turn: steerCompleted.turn,
  });
  await waitForStableIdle(client, threadId);

  const idleWake = await client.request(
    "turn/start",
    turnParams(threadId, "Reply exactly IDLE_MAIL_WAKE_NONCE_51c2d8."),
  );
  record("idle-wake.turn-start", idleWake.turn);
  const idleWakeCompleted = await waitForTurnTerminal(client, threadId, idleWake.turn.id, 60_000);
  record("idle-wake.completed", {
    status: idleWakeCompleted.thread.status,
    turn: idleWakeCompleted.turn,
  });
  await waitForStableIdle(client, threadId);

  const interruptTurn = await client.request(
    "turn/start",
    turnParams(threadId, "Run the shell command `sleep 15`, then reply INTERRUPT_FAILED."),
  );
  record("interrupt.turn-start", interruptTurn.turn);
  await delay(1_000);
  record(
    "interrupt.response",
    await client.request("turn/interrupt", { threadId, turnId: interruptTurn.turn.id }),
  );
  const interrupted = await waitForTurnTerminal(client, threadId, interruptTurn.turn.id, 30_000);
  record("interrupt.completed", {
    status: interrupted.thread.status,
    turn: interrupted.turn,
  });
  await waitForStableIdle(client, threadId);

  const gracefulStopTurn = await client.request(
    "turn/start",
    turnParams(threadId, "Run the shell command `sleep 8`, then reply GRACEFUL_STOP_DRAINED."),
  );
  record("graceful-stop.turn-start", gracefulStopTurn.turn);
  await delay(1_000);
  const gracefulStopStartedAt = Date.now();
  record("graceful-stop.result", {
    ...(await stopServer()),
    elapsedMs: Date.now() - gracefulStopStartedAt,
  });
  await delay(1_000);
  record("graceful-stop.restart", await startServer());
  client = await connect("client-c");
  const afterGracefulStop = await readThread(client, threadId, true);
  record("graceful-stop.thread-read", {
    status: afterGracefulStop.status,
    turn: afterGracefulStop.turns?.find((turn) => turn.id === gracefulStopTurn.turn.id),
  });

  await client.request("thread/resume", { threadId });
  await waitForStableIdle(client, threadId);
  const launcherDeathTurn = await client.request(
    "turn/start",
    turnParams(
      threadId,
      "Run the shell command `sleep 8`. Then report any newer instruction you received.",
    ),
  );
  record("launcher-death.turn-start", launcherDeathTurn.turn);
  await delay(1_000);
  record("launcher-death.result", await crashLauncher());
  client.disconnect();
  client = await connect("client-d");
  const afterLauncherDeath = await readThread(client, threadId, true);
  record("launcher-death.thread-read", {
    status: afterLauncherDeath.status,
    turn: afterLauncherDeath.turns?.find((turn) => turn.id === launcherDeathTurn.turn.id),
  });
  record(
    "launcher-death.steer",
    await client.request("turn/steer", {
      threadId,
      expectedTurnId: launcherDeathTurn.turn.id,
      input: [
        {
          type: "text",
          text: "MAIL_AFTER_LAUNCHER_DEATH_83b0ca. Reply with that nonce now.",
        },
      ],
    }),
  );
  const launcherDeathCompleted = await waitForTurnTerminal(
    client,
    threadId,
    launcherDeathTurn.turn.id,
    60_000,
  );
  record("launcher-death.completed", {
    status: launcherDeathCompleted.thread.status,
    turn: launcherDeathCompleted.turn,
  });
  await waitForStableIdle(client, threadId);

  const crashTurn = await client.request(
    "turn/start",
    turnParams(threadId, "Run the shell command `sleep 20`, then reply CRASH_DID_NOT_STOP_TURN."),
  );
  record("crash.turn-start", crashTurn.turn);
  await delay(1_000);
  record("crash.result", await crashServer());
  await delay(1_000);
  record("crash.restart", await startServer());
  client = await connect("client-e");
  const afterCrash = await readThread(client, threadId, true);
  record("crash.thread-read", {
    status: afterCrash.status,
    turn: afterCrash.turns?.find((turn) => turn.id === crashTurn.turn.id),
  });

  const resumed = await client.request("thread/resume", { threadId });
  record("crash.thread-resume", {
    threadId: resumed.thread.id,
    status: resumed.thread.status,
  });
  let restartCompleted;
  if (resumed.thread.status?.type === "active") {
    record(
      "crash.recovery",
      await client.request("turn/steer", {
        threadId,
        expectedTurnId: crashTurn.turn.id,
        input: [
          {
            type: "text",
            text: "RECOVERED_AFTER_APP_SERVER_CRASH_4e91b7. Reply with that nonce now.",
          },
        ],
      }),
    );
    restartCompleted = await waitForTurnTerminal(client, threadId, crashTurn.turn.id, 60_000);
  } else {
    await waitForStableIdle(client, threadId);
    const recoveryTurn = await client.request(
      "turn/start",
      turnParams(threadId, "Reply exactly RECOVERED_AFTER_APP_SERVER_CRASH_4e91b7."),
    );
    record("crash.recovery", { strategy: "new-turn", turn: recoveryTurn.turn });
    restartCompleted = await waitForTurnTerminal(client, threadId, recoveryTurn.turn.id, 60_000);
  }
  record("crash.completed", {
    status: restartCompleted.thread.status,
    turn: restartCompleted.turn,
  });
  await waitForStableIdle(client, threadId);

  const listed = await client.request("thread/list", {
    limit: 100,
    cwd,
  });
  record("thread.list", {
    found: listed.data.some((thread) => thread.id === threadId),
    match: listed.data.find((thread) => thread.id === threadId),
  });

  client.disconnect();
  record("result", "completed");
}

try {
  await main();
} catch (error) {
  record("result", { error: error.stack || String(error) });
  process.exitCode = 1;
} finally {
  for (const client of clients) client.disconnect();
  if (server) {
    record("daemon.cleanup", await stopServer());
  }
  process.stdout.write(`${JSON.stringify({ cwd, model, observations }, null, 2)}\n`);
}
