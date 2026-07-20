// Test harness for kv.js, driven by integration/js_test.go (which
// generates the vectors from the Go implementation and hosts the live
// server). Runnable standalone against a saved vectors file:
//
//   node js/kv_test.mjs <vectors.json>                       vectors only
//   node js/kv_test.mjs <vectors.json> <serverURL> <blob>    + live phases
//
// In live mode the driver sends one JSON command per stdin line and
// reads one reply line per command ("OK" or "FAIL <reason>"):
//
//   {"cmd":"sync","expect":{"key":"value",...},"probeOnly":true|false}
//   {"cmd":"sync","expectError":"substring"}   sync must fail matching
//   {"cmd":"put","key":K,"value":V}
//   {"cmd":"del","key":K}
//   {"cmd":"vault2","rootKey":blob,"registerKey":blob}   switch to a
//       fresh client+store on that RootKey (register key installed)
//   {"cmd":"end"}
//
// The process exit code is 0 iff every vector and live check passed.
import * as kv from "./kv.js";
import { readFileSync } from "node:fs";
import { createInterface } from "node:readline";

const hex = (b) => Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");
const unhex = (s) => new Uint8Array(s.match(/../g)?.map((x) => parseInt(x, 16)) ?? []);
const te = new TextEncoder();
const td = new TextDecoder();

let failed = 0;
function eq(name, got, want) {
  const g = typeof got === "string" ? got : hex(got);
  if (g !== want) {
    failed++;
    console.error("FAIL " + name + "\n  got  " + g + "\n  want " + want);
  }
}

// --- vectors ---

const V = JSON.parse(readFileSync(process.argv[2], "utf8"));
const rk = unhex(V.rootKey);

eq("aeadKey", kv.deriveAEADKey(rk), V.aeadKey);
eq("vaultID", kv.deriveVaultID(rk), V.vaultID);
eq("authKey", kv.deriveAuthKey(rk), V.authKey);
const ak = kv.deriveAuthKey(rk);
eq("c2s", kv.deriveC2S(ak), V.c2s);
eq("s2c", kv.deriveS2C(ak), V.s2c);
eq("reg", kv.deriveRegKey(unhex(V.registerKey)), V.reg);

eq("xofEmpty16", kv.xof(new Uint8Array(0), 16), V.xofEmpty16);
eq("xofMsg40", kv.xof(te.encode(V.xofMsg), 40), V.xofMsg40);

eq("encodeRootKey", kv.encodeRootKey(rk), V.rootKeyBlob);
eq("decodeRootKey", kv.decodeRootKey(V.rootKeyBlob), V.rootKey);
eq("encodeRegisterKey", kv.encodeRegisterKey(unhex(V.registerKey)), V.registerKeyBlob);
eq("decodeRegisterKey", kv.decodeRegisterKey(V.registerKeyBlob), V.registerKey);
eq("encodeBackup", kv.encodeBackup(rk, unhex(V.registerKey)), V.backupBlob);
{
  const b = kv.decodeBackup(V.backupBlob);
  eq("backup rootKey", b.rootKey, V.rootKey);
  eq("backup registerKey", b.registerKey, V.registerKey);
  eq("backup bare", String(kv.decodeBackup(V.rootKeyBlob).registerKey === null), "true");
}

const aeadKey = kv.deriveAEADKey(rk);
const entries = [];
for (let i = 0; i < V.entries.length; i++) {
  const raw = unhex(V.entries[i]);
  const { entry, consumed } = kv.unmarshalEntry(raw);
  if (consumed !== raw.length) { failed++; console.error("FAIL entry " + i + " consumed"); }
  entries.push(entry);
  eq("entryID " + i, kv.entryID(entry), V.entryIDs[i]);
  eq("remarshal " + i, kv.marshalEntry(entry), V.entries[i]);
}
const opened = entries.map((e) => kv.openEntry(aeadKey, e));
eq("e1 key", td.decode(opened[0].key), "alpha");
eq("e1 val", td.decode(kv.parseValue(opened[0].value).value), "first");
eq("e2 key", td.decode(opened[1].key), "beta");
eq("e3 tomb", String(kv.parseValue(opened[2].value).tomb), "true");
{
  const t = { ...entries[0], ct: entries[0].ct.slice() };
  t.ct[0] ^= 1;
  eq("tamper rejected", String(kv.openEntry(aeadKey, t) === null), "true");
}

const pairs = entries.map((e) => ({ entry: e, id: kv.entryID(e) }));
kv.sortCanonical(pairs);
eq("sortedIDs", pairs.map((p) => hex(p.id)).join(","), V.sortedIDs.join(","));
eq("setHash", kv.setHash(pairs), V.setHash);

const vecStore = new kv.MemStore();
for (const e of entries) vecStore.add(e);
eq("memstore setHash", vecStore.setHash(), V.setHash);
eq("memstore dedup", String(vecStore.add(entries[0])), "false");
eq("memstore len", String(vecStore.len()), "3");

eq("probePayload", kv.marshalSync({ op: kv.OP_PROBE, setHash: unhex(V.setHash) }), V.probePayload);
eq("pullPayload", kv.marshalSync({ op: kv.OP_PULL, curTs: BigInt(V.pullCursorTs), curID: unhex(V.entryIDs[0]) }), V.pullPayload);
eq("pushPayload", kv.marshalSync({ op: kv.OP_PUSH, entries }), V.pushPayload);

const vid = kv.deriveVaultID(rk);
const fn = unhex(V.frameNonce);
eq("probeFrame", kv.sealFrame(kv.KIND_SYNC, vid, fn, kv.deriveC2S(ak), unhex(V.probePayload)), V.probeFrame);
{
  const h = kv.parseFrameHeader(unhex(V.respFrame));
  eq("resp kind", String(h.kind), String(kv.KIND_RESP));
  eq("resp vid", h.vaultID, V.vaultID);
  const payload = kv.openFrame(kv.deriveS2C(ak), h.kind, h.vaultID, h.nonce, h.sealed);
  const resp = kv.parseResp(payload);
  eq("resp status", String(resp.status), String(kv.STATUS_OK));
  eq("resp reqNonce", resp.reqNonce, hex(fn));
  eq("resp entries", String(resp.entries.length), "3");
  eq("resp entry0 id", kv.entryID(resp.entries[0]), hex(pairs[0].id));
}

// Client-level replay: alpha tombstoned at ts 2000 > 1000, beta live.
{
  const c = new kv.Client(rk, vecStore, async () => { throw new Error("no transport"); });
  const all = await c.all();
  eq("live count", String(all.length), "1");
  eq("live key", td.decode(all[0].key), "beta");
  eq("live val", td.decode(all[0].value), "second");
  eq("tombstoned get", String((await c.get("alpha")) === null), "true");
}

// --- live phases (optional) ---

if (process.argv[3]) {
  const serverURL = process.argv[3];
  let client = new kv.Client(kv.decodeRootKey(process.argv[4]), new kv.MemStore(), kv.httpTransport(serverURL));

  const rl = createInterface({ input: process.stdin });
  for await (const line of rl) {
    const cmd = JSON.parse(line);
    if (cmd.cmd === "end") break;
    let reply = "OK";
    try {
      switch (cmd.cmd) {
        case "vault2": {
          client = new kv.Client(kv.decodeRootKey(cmd.rootKey), new kv.MemStore(), kv.httpTransport(serverURL));
          if (cmd.registerKey) client.setRegisterKey(kv.decodeRegisterKey(cmd.registerKey));
          break;
        }
        case "put":
          await client.put(cmd.key, cmd.value);
          break;
        case "del":
          await client.delete(cmd.key);
          break;
        case "sync": {
          if (cmd.expectError) {
            try {
              await client.sync();
              reply = "FAIL expected error, sync succeeded";
            } catch (err) {
              if (!err.message.includes(cmd.expectError)) reply = "FAIL error " + err.message;
            }
            break;
          }
          const st = await client.sync();
          if (cmd.probeOnly && (st.roundTrips !== 1 || st.pulled !== 0 || st.pushed !== 0)) {
            reply = "FAIL steady-state sync did work: " + JSON.stringify(st);
            break;
          }
          const got = {};
          for (const p of await client.all()) got[td.decode(p.key)] = td.decode(p.value);
          const gs = JSON.stringify(Object.entries(got).sort());
          const ws = JSON.stringify(Object.entries(cmd.expect).sort());
          if (gs !== ws) reply = "FAIL state " + gs + " want " + ws;
          break;
        }
        default:
          reply = "FAIL unknown command " + cmd.cmd;
      }
    } catch (err) {
      reply = "FAIL " + err.message;
    }
    if (reply !== "OK") failed++;
    console.log(reply);
  }
}

if (failed > 0) console.error(failed + " failures");
process.exit(failed === 0 ? 0 : 1);
