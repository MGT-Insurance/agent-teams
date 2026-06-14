// SSE client registry: add/remove response streams, broadcast events.

import type { ServerResponse } from "node:http";
import type { SnapshotEvent } from "@agent-teams/shared";

export class SseRegistry {
  private clients = new Set<ServerResponse>();

  add(res: ServerResponse): void {
    this.clients.add(res);
    res.on("close", () => this.remove(res));
  }

  remove(res: ServerResponse): void {
    this.clients.delete(res);
  }

  broadcast(event: SnapshotEvent): void {
    const payload = `data: ${JSON.stringify(event)}\n\n`;
    for (const client of this.clients) {
      if (!client.writableEnded && !client.destroyed) {
        client.write(payload);
      } else {
        this.clients.delete(client);
      }
    }
  }

  get size(): number {
    return this.clients.size;
  }
}
