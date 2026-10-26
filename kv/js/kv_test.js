// kv_test.js -- kv.js checked against the Go implementation, in a
// browser. integration/js_test.go serves this module beside kv.js, a
// vectors.json made by the Go packages, and a live server on the same
// origin, then calls in headless Chrome:
//
//   vectors()          every vector check; resolves to the failures
//   live(rootKeyBlob)  starts a client on this origin's server
//   step(c)            one live step; resolves to "OK" or "FAIL <reason>"
//
// The live steps:
//
//   {"cmd":"sync","expect":{"key":"value",...},"probeOnly":true|false}
//   {"cmd":"sync","expectError":"substring"}   sync must fail matching
//   {"cmd":"put","key":K,"value":V}
//   {"cmd":"del","key":K}
//   {"cmd":"vault2","rootKey":blob,"registerKey":blob}   switch to a
//       fresh client+store on that RootKey (register key installed)
//
// Stores are IDBStore throughout: the browser's store, under the real
// sync loop.
import * as kv from "./kv.js";

const hex = (b) => Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");
const unhex = (s) => new Uint8Array(s.match(/../g)?.map((x) => parseInt(x, 16)) ?? []);
const te = new TextEncoder();
const td = new TextDecoder();

let fails = [];
function eq(name, got, want) {
  const g = typeof got === "string" ? got : hex(got);
  if (g !== want) fails.push(name + ": got " + g + ", want " + want);
}

const storeName = () => "kv-test-" + Date.now().toString(36) + Math.random().toString(36).slice(2);

export async function vectors() {
  fails = [];
  const V = await (await fetch("/vectors.json")).json();
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
  eq("cxofEmpty16", kv.cxof("", new Uint8Array(0), 16), V.cxofEmpty16);
  eq("cxofMsg40", kv.cxof("kv-test", te.encode(V.xofMsg), 40), V.cxofMsg40);

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
  {
    const a = kv.newRootKey(), b = kv.newRootKey();
    eq("newRootKey size", String(a.length), String(kv.ROOT_KEY_SIZE));
    eq("newRootKey fresh", String(hex(a) !== hex(b)), "true");
    eq("newRootKey backup", kv.decodeBackup(kv.encodeBackup(a)).rootKey, hex(a));
  }

  const aeadKey = kv.deriveAEADKey(rk);
  const entries = [];
  for (let i = 0; i < V.entries.length; i++) {
    const raw = unhex(V.entries[i]);
    const { entry, consumed } = kv.unmarshalEntry(raw);
    eq("entry " + i + " consumed", String(consumed), String(raw.length));
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

  // IDBStore: write-through persistence and reload.
  {
    const name = storeName();
    const s1 = await kv.IDBStore.open(name);
    for (const e of entries) await s1.add(e);
    eq("idb dedup", String(await s1.add(entries[0])), "false");
    eq("idb setHash", s1.setHash(), V.setHash);
    s1.close();
    const s2 = await kv.IDBStore.open(name);
    eq("idb reload len", String(s2.len()), "3");
    eq("idb reload setHash", s2.setHash(), V.setHash);
    s2.close();
    indexedDB.deleteDatabase(name);
  }

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
  return fails;
}

// --- live steps ---

let client = null;

async function newClient(rootKeyBlob) {
  return new kv.Client(kv.decodeRootKey(rootKeyBlob), await kv.IDBStore.open(storeName()), kv.httpTransport(location.origin));
}

export async function live(rootKeyBlob) {
  client = await newClient(rootKeyBlob);
}

export async function step(c) {
  try {
    switch (c.cmd) {
      case "vault2":
        client = await newClient(c.rootKey);
        if (c.registerKey) client.setRegisterKey(kv.decodeRegisterKey(c.registerKey));
        return "OK";
      case "put":
        await client.put(c.key, c.value);
        return "OK";
      case "del":
        await client.delete(c.key);
        return "OK";
      case "sync": {
        if (c.expectError) {
          try {
            await client.sync();
            return "FAIL expected error, sync succeeded";
          } catch (err) {
            return err.message.includes(c.expectError) ? "OK" : "FAIL error " + err.message;
          }
        }
        const st = await client.sync();
        if (c.probeOnly && (st.roundTrips !== 1 || st.pulled !== 0 || st.pushed !== 0)) {
          return "FAIL steady-state sync did work: " + JSON.stringify(st);
        }
        const got = {};
        for (const p of await client.all()) got[td.decode(p.key)] = td.decode(p.value);
        const gs = JSON.stringify(Object.entries(got).sort());
        const ws = JSON.stringify(Object.entries(c.expect).sort());
        return gs === ws ? "OK" : "FAIL state " + gs + " want " + ws;
      }
      default:
        return "FAIL unknown command " + c.cmd;
    }
  } catch (err) {
    return "FAIL " + err.message;
  }
}
