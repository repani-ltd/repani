# Hand-written vectors from ROW.t's tables (6.5 lenient input, 7 errors),
# merged with gen.py's output into ../vectors.json:
#   python3 -I extra.py VALID.json ../vectors.json
import json, struct, sys

def bij(v):
    out = [v & 0x7F]
    v >>= 7
    while v:
        v -= 1
        out.append(0x80 | (v & 0x7F))
        v >>= 7
    return bytes(reversed(out))

def zz(n): return (n << 1) if n >= 0 else ((-n << 1) - 1)

def one(tag, payload=b""):  # a value row of one item
    return (b"\x01" + bytes([tag]) + payload).hex()

M = 2**53 - 1
lenient = [
    ("0 : int = +007\n", "0: int = 7\n"),
    ("0: int = 1_2345\n", "0: int = 12_345\n"),
    ("0: int = -0\n", "0: int = 0\n"),
    ("0: dec = +09.99\n", "0: dec = 9.99\n"),
    ("0: dec = .5\n", "0: dec = 0.5\n"),
    ("0: dec = -0.00\n", "0: dec = 0.00\n"),
    ("0: dec = 1500.\n", "0: dec = 1500\n"),
    ("0: dec = 1_234.5_0\n", "0: dec = 1234.50\n"),
    ("0: f64 = 9.99e0\n", "0: f64 = 9.99\n"),
    ("0: f64 = 1E3\n", "0: f64 = 1000\n"),
    ("0: f64 = +1.5\n", "0: f64 = 1.5\n"),
    ("0: f64 = .5\n", "0: f64 = 0.5\n"),
    ("0: f64 = 5.\n", "0: f64 = 5\n"),
    ("0: f64 = 1_000.5\n", "0: f64 = 1000.5\n"),
    ("0: f64 = 0.000000999\n", "0: f64 = 9.99e-7\n"),
    ("0: f64 = 1e+21\n", "0: f64 = 1e21\n"),
    ("0: utc = 2026-11-21T09:30:00Z\n", "0: utc = 2026-11-21T09:30:00.000Z\n"),
    ("0: utc = 2026-11-21t09:30:00.5z\n", "0: utc = 2026-11-21T09:30:00.500Z\n"),
    ("0: utc = 2026-11-21 09:30:00.000Z\n", "0: utc = 2026-11-21T09:30:00.000Z\n"),
    ("0: utc = 2026-11-21T11:30:00+02:00\n", "0: utc = 2026-11-21T09:30:00.000Z\n"),
    ("0: utc = 2026-11-21T00:30:00+01:00\n", "0: utc = 2026-11-20T23:30:00.000Z\n"),
    ("0: utc = 2026-11-21T09:30:00.123000Z\n", "0: utc = 2026-11-21T09:30:00.123Z\n"),
    ("0: time = 09:30\n", "0: time = 09:30:00.000\n"),
    ("0: time = 09:30:05.5\n", "0: time = 09:30:05.500\n"),
    ("0: dur = 90m\n", "0: dur = 1h30m\n"),
    ("0: dur = 1500ms\n", "0: dur = 1.5s\n"),
    ("0: dur = 1.5h\n", "0: dur = 1h30m\n"),
    ("0: dur = 1h90m\n", "0: dur = 2h30m\n"),
    ("0: dur = 1_000ms\n", "0: dur = 1s\n"),
    ("0: dur = -0s\n", "0: dur = 0s\n"),
    ('0: str = "\\u00e9\\u00E9\\/"\n', '0: str = "éé/"\n'),
    ('0: str = "\\ud83d\\ude00"\n', '0: str = "\U0001F600"\n'),
    ("0: bin = 0X00FF\n", "0: bin = 0x00ff\n"),
    ('\n1:\tstr  =  "b"   \n\n0: str = "a"\n', '0: str = "a"\n1: str = "b"\n'),
    ("0.1: int = 2\n0.0: int = 1\n0: row\n", "0: row\n0.0: int = 1\n0.1: int = 2\n"),
    ("0: row\n0.10: int = 10\n" + "".join(f"0.{i}: int = {i}\n" for i in range(10)),
     "0: row\n" + "".join(f"0.{i}: int = {i}\n" for i in range(11))),
]
bad_binary = [
    ("", "R001", ""),
    ("01", "R001", "0"),
    ("0105616263", "R001", "0"),
    ("010000", "R002", ""),
    ("01c3" + "ff" * 8 + "00", "R003", "0"),
    ("01cc", "R004", "0"),
    ("01f0", "R004", "0"),
    ("01e1e1e1e100", "R005", "0.0.0.0"),
    (one(0xC3, bij(zz(M + 1))), "V001", "0"),
    ("01e2c300c3" + bij(zz(-M - 1)).hex(), "V001", "0.1"),
    (one(0xD2, bij(zz(M + 1))), "V002", "0"),
    (one(0xC4, struct.pack("<d", float("nan"))), "V003", "0"),
    (one(0xC4, struct.pack("<d", -0.0)), "V003", "0"),
    (one(0xC5, bij(zz(2932897))), "V004", "0"),
    (one(0xC6, bij(zz(253402300800000))), "V005", "0"),
    (one(0xC7, bij(86400000)), "V006", "0"),
    (one(0xC8, bij(zz(M + 1))), "V007", "0"),
    ("0101ff", "V008", "0"),
]
bad_text = [
    ('0 str = "a"\n', "T001", "", 1),
    ("01: int = 1\n", "T002", "", 1),
    ("0.0.0.0.0: int = 1\n", "T002", "", 1),
    ('0: strg = "a"\n', "T003", "0", 1),
    ("0: int = abc\n", "T004", "0", 1),
    ("0: date = 2026-02-30\n", "T004", "0", 1),
    ("0: date = 2026_11_21\n", "T004", "0", 1),
    ("0: none = 1\n", "T004", "0", 1),
    ("0: int\n", "T004", "0", 1),
    ("0: int = 1\n2: int = 2\n", "T005", "1", 0),
    ("0: int = 1\n0: int = 2\n", "T005", "0", 2),
    ("0.0: int = 1\n", "T006", "0.0", 1),
    ("0: int = 1\n0.0: int = 2\n", "T006", "0.0", 2),
    ('"k" 0: int = 1\n', "T007", "", 1),
    ("0: int = 9_007_199_254_740_992\n", "V001", "0", 1),
    ("0: dec = 1.0000000000000000\n", "V002", "0", 1),
    ("0: f64 = 1e400\n", "V003", "0", 1),
    ("0: f64 = -0\n", "V003", "0", 1),
    ("0: date = 0000-01-01\n", "V004", "0", 1),
    ("0: utc = 9999-12-31T23:30:00-02:00\n", "V005", "0", 1),
    ("0: utc = 2026-06-30T23:59:60Z\n", "V005", "0", 1),
    ("0: utc = 2026-11-21T09:30:00.0001Z\n", "V005", "0", 1),
    ("0: time = 09:30:00.0005\n", "V006", "0", 1),
    ("0: dur = 0.5ms\n", "V007", "0", 1),
    ('0: str = "\\ud800"\n', "V008", "0", 1),
    ("0: row\n0.0: row\n0.0.0: row\n0.0.0.0: row\n", "R005", "0.0.0.0", 4),
]
bad_labeled = [
    ('"k" 0: int = 1\n', "T006", "", 1),
    ('"k": int = 1\n', "T002", "", 1),
    ('"k": row = 1\n', "T004", "", 1),
    ('"k": row\n"k": row\n', "T005", "", 2),
    ('"k": row\n0: int = 1\n', "T007", "", 2),
]
bad_text.append((": row\n", "T002", "", 1))
d = json.load(open(sys.argv[1]))
d["lenient"] = [{"text": t, "canonical": c} for t, c in lenient]
d["badBinary"] = [{"hex": h, "code": c, "path": p} for h, c, p in bad_binary]
d["badText"] = [{"text": t, "code": c, "path": p, "line": l} for t, c, p, l in bad_text]
d["badLabeled"] = [{"text": t, "code": c, "path": p, "line": l} for t, c, p, l in bad_labeled]
json.dump(d, open(sys.argv[2], "w"), ensure_ascii=False, indent=1)
open(sys.argv[2], "a").write("\n")
