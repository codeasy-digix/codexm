import { lstatSync, realpathSync } from "node:fs";
import { join } from "node:path";
import { MongoClient } from "mongodb";

export class CacheDatabase {
  private client?: MongoClient;

  constructor(private readonly state: string) {}

  get() {
    if (this.client) return this.client.db();
    let socket: string;
    try {
      const path = realpathSync(join(this.state, "history-cache"));
      const directory = lstatSync(path);
      socket = join(path, "mongodb.sock");
      const endpoint = lstatSync(socket);
      const uid = process.getuid?.();
      if (!directory.isDirectory() || !endpoint.isSocket() || directory.uid !== uid ||
        endpoint.uid !== uid || (directory.mode & 0o077) !== 0) return;
    } catch { return; }
    this.client = new MongoClient("mongodb://" + encodeURIComponent(socket) + "/codexm", {
      directConnection: true, serverSelectionTimeoutMS: 250, connectTimeoutMS: 250,
      socketTimeoutMS: 500, maxPoolSize: 4, waitQueueTimeoutMS: 250, retryReads: false, retryWrites: false,
    });
    return this.client.db();
  }

  async close() { await this.client?.close(); }
}
