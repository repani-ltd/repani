// Behaviour tests for kv.js's Client: what it promises under
// concurrent async use, how it fails, and its cached view. Driven in
// headless Chrome by integration/js_test.go (TestJSClientBehaviour),
// which hosts the servers and imports this module with them in its
// query:
//
//   import("/client_test.js?url=<server>&quota=<server>&reg=<blob>")
//
// The quota server holds at most two entries per vault. The checks run
// as the module loads; the import resolves once they are done, and
// `fails` lists the ones that failed.
import * as kv from "./kv.js";

const params = new URL(import.meta.url).searchParams;
const url = params.get("url");
const quotaURL = params.get("quota");
const reg = kv.decodeRegisterKey(params.get("reg"));
const td = new TextDecoder();

export const fails = [];
function check(ok, name) {
  if (!ok) fails.push(name);
}

function client(rk, opts = {}) {
  const c = new kv.Client(rk, opts.store || new kv.MemStore(), opts.transport || kv.httpTransport(opts.url || url), opts);
  if (opts.reg !== null) c.setRegisterKey(opts.reg || reg);
  return c;
}

async function state(c) {
  const out = {};
  for (const p of await c.all()) out[td.decode(p.key)] = td.decode(p.value);
  return JSON.stringify(out);
}

async function kindOf(p) {
  try {
    await p;
    return "resolved";
  } catch (e) {
    return e instanceof kv.SyncError ? e.kind : "plain: " + e.message;
  }
}

// A sync covers every write issued before the call, awaited or not;
// calls made while one runs share the next.
{
  const rk = kv.newRootKey();
  const a = client(rk);
  a.put("x", "1");
  const p1 = a.sync();
  a.put("y", "2");
  const p2 = a.sync();
  const p3 = a.sync();
  check(p2 === p3, "calls during a sync share the next one");
  check(p1 !== p2, "a call during a sync gets a later one");
  await p2;
  const b = client(rk);
  await b.sync();
  check(await state(b) === '{"x":"1","y":"2"}', "the shared sync covered both writes: " + await state(b));
  // Once idle, a call starts a sync of its own.
  check(a.sync() !== p2, "an idle client starts a fresh sync");
  await a.sync();
}

// Writes apply in call order, and the last one wins.
{
  const rk = kv.newRootKey();
  const a = client(rk);
  a.put("k", "1");
  a.put("k", "2");
  a.delete("k");
  a.put("k", "3");
  await a.sync();
  const b = client(rk);
  await b.sync();
  check(await state(b) === '{"k":"3"}', "writes in call order: " + await state(b));
}

// A failed sync loses nothing: the next one fast-pushes what the
// failed one could not.
{
  const rk = kv.newRootKey();
  const http = kv.httpTransport(url);
  let down = true;
  const lines = [];
  const c = client(rk, {
    transport: async (f) => {
      if (down) throw new Error("connection refused");
      return http(f);
    },
    trace: (m) => lines.push(m),
  });
  await c.put("z", "1");
  try {
    await c.sync();
    check(false, "a sync with the network down resolved");
  } catch (e) {
    check(e instanceof kv.SyncError && e.kind === "offline" && e.transient, "network down is offline, transient: " + e);
  }
  down = false;
  lines.length = 0;
  await c.sync();
  check(lines.some((l) => l.startsWith("PUSH(new) -> 1 ")), "the retry fast-pushed the entry: " + lines.join(" | "));
  const b = client(rk);
  await b.sync();
  check(await state(b) === '{"z":"1"}', "the entry reached the server");
}

// A client that believes its vault registered, told UNKNOWN_VAULT,
// registers and finishes the sync.
{
  const rk = kv.newRootKey();
  const a = client(rk);
  a.registered = true;
  await a.put("r", "1");
  await a.sync();
  const b = client(rk);
  await b.sync();
  check(await state(b) === '{"r":"1"}', "UNKNOWN_VAULT led to a REGISTER and the push: " + await state(b));
}

// Failures say what kind they are.
{
  const wrong = new Uint8Array(16).fill(9);
  const a = client(kv.newRootKey(), { reg: wrong });
  await a.put("a", "1");
  check(await kindOf(a.sync()) === "auth", "a wrong register key is auth");

  const keyless = client(kv.newRootKey(), { reg: null });
  check(await kindOf(keyless.sync()) === "auth", "an unregistered vault without a register key is auth");

  const nowhere = client(kv.newRootKey(), { url: "http://127.0.0.1:1" });
  check(await kindOf(nowhere.sync()) === "offline", "no server is offline");

  const busy = client(kv.newRootKey(), { transport: async () => new Uint8Array([kv.ERR_TRY_LATER]) });
  check(await kindOf(busy.sync()) === "offline", "TRY_LATER is offline");
  try {
    await busy.sync();
  } catch (e) {
    check(e.pre === kv.ERR_TRY_LATER && e.message === "kv: register: server returned try-later", "a pre-channel error carries its byte and step: " + e.pre + " " + e.message);
  }

  const garbled = client(kv.newRootKey(), { transport: async () => new Uint8Array(80) });
  check(await kindOf(garbled.sync()) === "protocol", "a garbled response is protocol");

  const full = client(kv.newRootKey(), { url: quotaURL });
  for (const k of ["1", "2", "3"]) await full.put(k, k);
  check(await kindOf(full.sync()) === "rejected", "a full vault is rejected");
}

// close waits for the sync under way, then refuses everything.
{
  const rk = kv.newRootKey();
  const c = client(rk);
  await c.put("a", "1");
  const s = c.sync();
  await c.close();
  check(await kindOf(s) === "resolved", "close let the running sync finish");
  check(await kindOf(c.put("b", "2")) === "closed", "a write after close is closed");
  check(await kindOf(c.sync()) === "closed", "a sync after close is closed");
}

// subscribe hears local writes and new entries from a sync, and only
// those.
{
  const rk = kv.newRootKey();
  const a = client(rk);
  const b = client(rk);
  const heard = [];
  b.subscribe((src) => heard.push(src));
  await b.put("mine", "1");
  await b.sync();
  check(heard.join() === "local", "a write is heard as local, a sync with nothing new is silent: " + heard);
  heard.length = 0;
  await a.put("theirs", "1");
  await a.sync();
  await b.sync();
  check(heard.join() === "sync", "entries pulled are heard as sync: " + heard);
}

// The cached view is exactly a full replay, whatever order entries
// arrive in: shuffled timestamps, ties, tombstones, and an entry that
// does not open.
{
  const rk = kv.newRootKey();
  const ak = kv.deriveAEADKey(rk);
  const other = kv.deriveAEADKey(kv.newRootKey());
  const entries = [];
  const keys = ["a", "b", "c"];
  for (let i = 0; i < 60; i++) {
    const key = new TextEncoder().encode(keys[i % 3]);
    const ts = BigInt(1000 + ((i * 37) % 23)); // collisions on purpose
    const value = i % 7 === 0 ? null : kv.wrapValue(kv.FMT_RAW, new TextEncoder().encode("v" + i));
    entries.push(kv.sealEntry(i % 11 === 0 ? other : ak, ts, key, value));
  }
  for (let i = entries.length - 1; i > 0; i--) {
    const j = (i * 7919) % (i + 1);
    [entries[i], entries[j]] = [entries[j], entries[i]];
  }
  const store = new kv.MemStore();
  const c = client(rk, { store });
  let same = true;
  for (const e of entries) {
    store.add(e);
    const fresh = client(rk, { store });
    if (await state(c) !== await state(fresh)) same = false;
  }
  check(same, "the cached view matches a full replay after every arrival");
}
