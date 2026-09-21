// Reads generated JSON Lines named by the first argument and prints, for each
// event in sorted order, its name, how many records carry it and the sum of
// their dur_ms. It uses only node:fs and node:process, which Node.js, Bun
// and Deno all provide, so the three runtimes run the same file unchanged.
import { readFileSync } from "node:fs";
import process from "node:process";

const events = new Map();
for (const line of readFileSync(process.argv[2], "utf8").split("\n")) {
  if (line === "") continue;
  const record = JSON.parse(line);
  const e = events.get(record.event) ?? { count: 0, ms: 0 };
  e.count++;
  e.ms += record.dur_ms;
  events.set(record.event, e);
}
for (const name of [...events.keys()].sort()) {
  const e = events.get(name);
  process.stdout.write(`${name} ${e.count} ${e.ms}\n`);
}
