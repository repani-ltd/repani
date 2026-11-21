"""Independent reference encoder for ROW (row/ROW.t, draft of 2026-11-21).

Written from ROW.t alone, to produce known-answer vectors. Values are tagged
tuples:

    ('none',) ('bool', b) ('int', n) ('dec', mantissa, scale) ('f64', x)
    ('date', days) ('utc', ms) ('time', ms) ('dur', ms) ('str', s)
    ('bin', bytes) ('row', [items])

Kept independent by how it is changed: whoever updates it reads ROW.t and
nothing else of row/ (no Go, no row.js, no vectors, not extra.py).

Run: python3 -I gen.py OUT.json  -> writes the valid and labelled vectors to
OUT.json, a scratch file; extra.py merges them into ../vectors.json.
"""

import datetime as _dt
import decimal
import json
import math
import struct
import sys

MAXINT = 2**53 - 1
MAXDEPTH = 4  # FOUR LEVELS, counting the value; a path has at most 4 positions
DATE_MIN, DATE_MAX = -719162, 2932896
UTC_MIN, UTC_MAX = -62135596800000, 253402300799999
DAY_MS = 86400000
EPOCH = _dt.date(1970, 1, 1)


class RowError(Exception):
    pass


# ---------------------------------------------------------------- varints (4)

def varint(v):
    if v < 0:
        raise RowError("negative varint")
    out = [v & 0x7F]
    v >>= 7
    while v != 0:
        v -= 1
        out.append(0x80 | (v & 0x7F))
        v >>= 7
    out.reverse()
    if len(out) > 8:
        raise RowError("R003 varint longer than 8 bytes")
    return bytes(out)


def read_varint(buf, i):
    if i >= len(buf):
        raise RowError("R001")
    b = buf[i]
    i += 1
    n = 1
    v = b & 0x7F
    while b & 0x80:
        if i >= len(buf):
            raise RowError("R001")
        if n == 8:
            raise RowError("R003")
        b = buf[i]
        i += 1
        n += 1
        v = ((v + 1) << 7) | (b & 0x7F)
    return v, i


def zigzag(n):
    return 2 * n if n >= 0 else -2 * n - 1


def unzigzag(z):
    return z // 2 if z % 2 == 0 else -(z + 1) // 2


def check_int(n, code="V001"):
    if not (-MAXINT <= n <= MAXINT):
        raise RowError(code)


# ---------------------------------------------------------------- encoding

def check(v):
    k = v[0]
    if k == 'int':
        check_int(v[1])
    elif k == 'dec':
        if not (0 <= v[2] <= 15):
            raise RowError("V002 scale")
        check_int(v[1], "V002")
    elif k == 'f64':
        x = v[1]
        if math.isnan(x) or math.isinf(x) or (x == 0 and math.copysign(1, x) < 0):
            raise RowError("V003")
    elif k == 'date':
        if not (DATE_MIN <= v[1] <= DATE_MAX):
            raise RowError("V004")
    elif k == 'utc':
        if not (UTC_MIN <= v[1] <= UTC_MAX):
            raise RowError("V005")
    elif k == 'time':
        if not (0 <= v[1] < DAY_MS):
            raise RowError("V006")
    elif k == 'dur':
        check_int(v[1], "V007")


def enc_item(v, depth):
    """depth: level of the row holding this item (1 = the value)."""
    check(v)
    k = v[0]
    if k == 'none':
        return b'\xc0'
    if k == 'bool':
        return b'\xc2' if v[1] else b'\xc1'
    if k == 'int':
        return b'\xc3' + varint(zigzag(v[1]))
    if k == 'f64':
        return b'\xc4' + struct.pack('<d', v[1])
    if k == 'date':
        return b'\xc5' + varint(zigzag(v[1]))
    if k == 'utc':
        return b'\xc6' + varint(zigzag(v[1]))
    if k == 'time':
        return b'\xc7' + varint(v[1])
    if k == 'dur':
        return b'\xc8' + varint(zigzag(v[1]))
    if k == 'dec':
        return bytes([0xD0 | v[2]]) + varint(zigzag(v[1]))
    if k == 'str':
        b = v[1].encode('utf-8')
        if len(b) < 128:
            return bytes([len(b)]) + b
        return b'\xc9' + varint(len(b) - 128) + b
    if k == 'bin':
        b = v[1]
        if len(b) < 64:
            return bytes([0x80 + len(b)]) + b
        return b'\xca' + varint(len(b) - 64) + b
    if k == 'row':
        if depth + 1 > MAXDEPTH:
            raise RowError("R005")
        items = v[1]
        body = b''.join(enc_item(x, depth + 1) for x in items)
        if len(items) < 16:
            return bytes([0xE0 | len(items)]) + body
        return b'\xcb' + varint(len(items) - 16) + body
    raise RowError("V009 " + k)


def encode(row):
    assert row[0] == 'row'
    return varint(len(row[1])) + b''.join(enc_item(x, 1) for x in row[1])


# ---------------------------------------------------------------- decoding

def dec_items(buf, i, count, depth):
    items = []
    for _ in range(count):
        v, i = dec_item(buf, i, depth)
        items.append(v)
    return items, i


def dec_item(buf, i, depth):
    if i >= len(buf):
        raise RowError("R001")
    t = buf[i]
    i += 1

    def take(n, i):
        if i + n > len(buf):
            raise RowError("R001")
        return buf[i:i + n], i + n

    if t < 0x80:
        b, i = take(t, i)
        v = ('str', b.decode('utf-8'))
    elif t < 0xC0:
        b, i = take(t - 0x80, i)
        v = ('bin', bytes(b))
    elif t == 0xC0:
        v = ('none',)
    elif t == 0xC1:
        v = ('bool', False)
    elif t == 0xC2:
        v = ('bool', True)
    elif t in (0xC3, 0xC5, 0xC6, 0xC8):
        z, i = read_varint(buf, i)
        v = ({0xC3: 'int', 0xC5: 'date', 0xC6: 'utc', 0xC8: 'dur'}[t], unzigzag(z))
    elif t == 0xC4:
        b, i = take(8, i)
        v = ('f64', struct.unpack('<d', b)[0])
    elif t == 0xC7:
        z, i = read_varint(buf, i)
        v = ('time', z)
    elif t == 0xC9:
        n, i = read_varint(buf, i)
        b, i = take(n + 128, i)
        v = ('str', b.decode('utf-8'))
    elif t == 0xCA:
        n, i = read_varint(buf, i)
        b, i = take(n + 64, i)
        v = ('bin', bytes(b))
    elif t == 0xCB or 0xE0 <= t <= 0xEF:
        if depth + 1 > MAXDEPTH:
            raise RowError("R005")
        if t == 0xCB:
            n, i = read_varint(buf, i)
            n += 16
        else:
            n = t & 0x0F
        items, i = dec_items(buf, i, n, depth + 1)
        v = ('row', items)
    elif 0xD0 <= t <= 0xDF:
        z, i = read_varint(buf, i)
        v = ('dec', unzigzag(z), t & 0x0F)
    else:
        raise RowError("R004")
    check(v)
    return v, i


def decode(buf):
    n, i = read_varint(buf, 0)
    items, i = dec_items(buf, i, n, 1)
    if i != len(buf):
        raise RowError("R002")
    return ('row', items)


# ---------------------------------------------------------------- text (6)

def group(whole):
    """Underscore groups of three from the right, when five digits or more."""
    if len(whole) < 5:
        return whole
    head = len(whole) % 3 or 3
    parts = [whole[:head]] + [whole[j:j + 3] for j in range(head, len(whole), 3)]
    return '_'.join(parts)


def fmt_int(n):
    return ('-' if n < 0 else '') + group(str(abs(n)))


def fmt_dec(m, s):
    digits = str(abs(m))
    if s == 0:
        whole, frac = digits, ''
    else:
        digits = digits.rjust(s + 1, '0')
        whole, frac = digits[:-s], digits[-s:]
    out = group(whole) + ('.' + frac if s else '')
    return ('-' if m < 0 else '') + out


def fmt_f64(x):
    if x == 0:
        return '0'
    sign = '-' if x < 0 else ''
    t = decimal.Decimal(repr(abs(x))).as_tuple()
    D = ''.join(map(str, t.digits)).lstrip('0')
    e = t.exponent
    while D.endswith('0'):
        D = D[:-1]
        e += 1
    n = len(D)
    if 1e-6 <= abs(x) < 1e21:
        if e >= 0:
            whole, frac = D + '0' * e, ''
        elif n + e > 0:
            whole, frac = D[:n + e], D[n + e:]
        else:
            whole, frac = '0', '0' * (-(n + e)) + D
        return sign + group(whole) + ('.' + frac if frac else '')
    E = n - 1 + e
    mant = D[0] + ('.' + D[1:] if n > 1 else '')
    return sign + mant + 'e' + str(E)


def fmt_date(days):
    d = EPOCH + _dt.timedelta(days=days)
    return '%04d-%02d-%02d' % (d.year, d.month, d.day)


def fmt_time(ms):
    h, r = divmod(ms, 3600000)
    m, r = divmod(r, 60000)
    s, r = divmod(r, 1000)
    return '%02d:%02d:%02d.%03d' % (h, m, s, r)


def fmt_utc(ms):
    days, r = divmod(ms, DAY_MS)
    return fmt_date(days) + 'T' + fmt_time(r) + 'Z'


def fmt_dur(ms):
    if ms == 0:
        return '0s'
    sign = '-' if ms < 0 else ''
    a = abs(ms)
    h, r = divmod(a, 3600000)
    m, r = divmod(r, 60000)
    out = sign
    if h:
        out += '%dh' % h
    if m:
        out += '%dm' % m
    if r:
        s, f = divmod(r, 1000)
        out += str(s) + ('.' + ('%03d' % f).rstrip('0') if f else '') + 's'
    return out


ESC = {'"': '\\"', '\\': '\\\\', '\b': '\\b', '\f': '\\f', '\n': '\\n',
       '\r': '\\r', '\t': '\\t'}


def fmt_str(s):
    out = ['"']
    for c in s:
        if c in ESC:
            out.append(ESC[c])
        elif ord(c) < 0x20:
            out.append('\\u%04x' % ord(c))
        else:
            out.append(c)
    out.append('"')
    return ''.join(out)


def fmt_bin(b):
    return '0x' + b.hex()


TYPENAME = {'none': 'none', 'bool': 'bool', 'int': 'int', 'dec': 'dec',
            'f64': 'f64', 'date': 'date', 'utc': 'utc', 'time': 'time',
            'dur': 'dur', 'str': 'str', 'bin': 'bin', 'row': 'row'}


def fmt_value(v):
    k = v[0]
    return {
        'bool': lambda: 'true' if v[1] else 'false',
        'int': lambda: fmt_int(v[1]),
        'dec': lambda: fmt_dec(v[1], v[2]),
        'f64': lambda: fmt_f64(v[1]),
        'date': lambda: fmt_date(v[1]),
        'utc': lambda: fmt_utc(v[1]),
        'time': lambda: fmt_time(v[1]),
        'dur': lambda: fmt_dur(v[1]),
        'str': lambda: fmt_str(v[1]),
        'bin': lambda: fmt_bin(v[1]),
    }[k]()


def lines(row, prefix=''):
    out = []
    for i, v in enumerate(row[1]):
        check(v)
        path = prefix + str(i)
        if v[0] in ('none', 'row'):
            out.append('%s: %s' % (path, v[0]))
        else:
            out.append('%s: %s = %s' % (path, TYPENAME[v[0]], fmt_value(v)))
        if v[0] == 'row':
            out.extend(lines(v, path + '.'))
    return out


def fmt_text(row):
    encode(row)  # depth and value checks
    return ''.join(l + '\n' for l in lines(row))


def fmt_labeled(pairs):
    out = []
    for label, row in pairs:
        encode(row)
        out.append(fmt_str(label) + ': row\n')  # the row's own line
        out.extend(fmt_str(label) + ' ' + l + '\n' for l in lines(row))
    return ''.join(out)


# ---------------------------------------------------------------- builders

def R(*items):
    return ('row', list(items))


def S(s): return ('str', s)
def B(b): return ('bin', bytes(b))
def I(n): return ('int', n)
def D(m, s): return ('dec', m, s)
def F(x): return ('f64', float(x))
def T(ms): return ('time', ms)
def U(ms): return ('dur', ms)


NONE = ('none',)
TRUE = ('bool', True)
FALSE = ('bool', False)


def DATE(y, m, d):
    return ('date', (_dt.date(y, m, d) - EPOCH).days)


def UTC(y, mo, d, h=0, mi=0, s=0, ms=0):
    days = (_dt.date(y, mo, d) - EPOCH).days
    return ('utc', days * DAY_MS + ((h * 60 + mi) * 60 + s) * 1000 + ms)


def TIME(h, m, s, ms=0):
    return T(((h * 60 + m) * 60 + s) * 1000 + ms)


# ---------------------------------------------------------------- vectors

def build():
    V = []
    add = V.append

    # Empty value row and single items of every type.
    add(R())
    add(R(NONE))
    add(R(TRUE))
    add(R(FALSE))
    add(R(NONE, TRUE, FALSE))

    # Strings.
    for n in (0, 1, 127, 128, 200):
        add(R(S(''.join(chr(ord('a') + i % 26) for i in range(n)))))
    add(R(S('héllo wörld — ΑΒΓ 日本語 🙂')))
    # 128 UTF-8 bytes from 64 two-byte characters: long by bytes, not chars.
    add(R(S('é' * 64)))
    # 127 bytes: 63 two-byte characters and one ASCII.
    add(R(S('é' * 63 + 'x')))
    add(R(S('quote" backslash\\ bs\b ff\f nl\n cr\r tab\t nul\x00 soh\x01 '
            'us\x1f esc\x1b space slash/ del\x7f ls  nbsp ')))
    add(R(S('"'), S('\\'), S('\n'), S('\x00'), S('\x1f')))

    # Bytes.
    for n in (0, 1, 63, 64, 100):
        add(R(B(range(n))))
    add(R(B(b'\x00\xff\x10\xab')))

    # Ints.
    for n in (0, 1, -1, 63, -64, 64, 1234, 12345, -12345, MAXINT, -MAXINT,
              -8256, 8256, -1056832, 1056832, 100000, -1000000, 99999,
              123456789):
        add(R(I(n)))

    # Decimals.
    add(R(D(1500, 0)))
    add(R(D(999, 2)))
    add(R(D(-5, 2)))
    add(R(D(0, 2)))
    add(R(D(0, 0)))
    add(R(D(1234550, 2)))           # 12_345.50
    add(R(D(123450, 2)))            # 1234.50
    add(R(D(-1234550, 2)))          # -12_345.50
    add(R(D(1, 15)))
    add(R(D(123456789012345, 15)))
    add(R(D(MAXINT, 15)))
    add(R(D(-MAXINT, 15)))
    add(R(D(MAXINT, 0)))
    add(R(D(-MAXINT, 2)))
    add(R(D(9000, 2), D(900, 1)))  # 90.00 and 90.0: different values

    # Floats.
    for x in (0.0, 3.0, 9.99, -1.5, 0.1 + 0.2, 1e-6, 9.99e-7, 1e20, 1e21,
              123456789.5, -1.5e-7, 5e-324, 1.7976931348623157e308, 0.001,
              12345.6, 1234.5, -12345678.25, 2.0**53, 1e100, 0.5):
        add(R(F(x)))

    # Dates.
    for ymd in ((1, 1, 1), (1970, 1, 1), (2026, 11, 21), (9999, 12, 31),
                (1969, 12, 31)):
        add(R(DATE(*ymd)))

    # Datetimes.
    add(R(UTC(1, 1, 1)))
    add(R(UTC(9999, 12, 31, 23, 59, 59, 999)))
    add(R(UTC(1970, 1, 1)))
    add(R(UTC(2026, 11, 21, 9, 30, 0, 123)))
    add(R(UTC(1969, 12, 31, 23, 59, 59, 999)))

    # Times.
    add(R(TIME(0, 0, 0)))
    add(R(TIME(23, 59, 59, 999)))
    add(R(TIME(9, 30, 0)))

    # Durations.
    for ms in (0, -500, 5415250, MAXINT, -MAXINT, 1, 60000, 3600000, 90061001,
               -3600500):
        add(R(U(ms)))

    # Nested rows of 0, 1, 15, 16, 20 items.
    for n in (0, 1, 15, 16, 20):
        add(R(R(*[I(i) for i in range(n)])))
    add(R(R(), R(), R(R())))
    # Three levels deep (four-level vectors are at the end).
    add(R(S('top'), R(S('mid'), R(S('leaf'), I(1)), R(), I(2)), I(3)))
    add(R(R(R(I(7)))))
    # Path ordering: 3.2 before 3.10 (numeric).
    add(R(I(0), I(1), I(2), R(*[I(i * 10) for i in range(12)])))

    # Value rows of 0, 1 and 200 items (0 and 1 above too).
    add(R(I(42)))
    add(R(*[I(i) for i in range(200)]))
    add(R(*[S(str(i)) for i in range(16)]))
    add(R(*[NONE for _ in range(128)]))

    # One row of every type.
    add(R(NONE, FALSE, TRUE, I(-7), F(9.99), DATE(2026, 11, 21),
          UTC(2026, 11, 21, 9, 30), TIME(9, 30, 0), U(5415250), S('x'),
          B(b'\x00\xff'), D(999, 2), R(S('nested'))))

    # Realistic records.
    add(R(S('invoice'), DATE(2026, 11, 21), S('EUR'),
          R(R(S('Widget'), I(3), D(999, 2)),
            R(S('Gadget, large'), I(1), D(1234550, 2)),
            R(S('Shipping'), I(1), D(0, 2)))))
    add(R(S('invoice'), DATE(2026, 1, 2), S('EUR'), R()))
    add(R(S('expense'), UTC(2026, 11, 20, 19, 45, 0, 0), S('Dinner'),
          D(8640, 2), S('EUR'), NONE,
          R(R(S('alice'), D(4320, 2)), R(S('bob'), D(4320, 2)))))
    add(R(S('expense'), DATE(2026, 11, 21), S('Taxi'), D(2500, 2), S('GBP'),
          S('receipt 0042'), R(R(S('carol'), D(2500, 2), NONE))))

    # Four levels (FOUR LEVELS, counting the value); appended last so the
    # vectors above keep their order.
    # (a) four levels with a scalar at the bottom: path 0.0.0.0.
    add(R(R(R(R(I(7))))))
    # (b) an empty row at level four (path 0.0.0, no items).
    add(R(R(R(R()))))
    add(R(S('a'), R(S('b'), R(S('c'), R(), I(3)), I(2)), I(1)))
    # (c) an edit carrying a record of three levels whole: a pair at 2.5.0,
    # its items at 2.5.0.0 and 2.5.0.1.
    add(R(S('r'), S('id'),
          R(S('x'), DATE(2026, 11, 21), S('EUR'), D(9000, 2), NONE,
            R(R(S('PAV0000S'), I(1)), R(S('BEN00000'), I(1))),
            S('taxi'))))
    # (d) long rows (16 and 20 items, form cb) at level four; path order
    # 0.0.0.2 before 0.0.0.10.
    add(R(R(R(R(*[I(i) for i in range(16)])))))
    add(R(R(R(R(*[S(str(i)) for i in range(20)])))))
    # Every type at level four.
    add(R(R(R(R(NONE, FALSE, TRUE, I(-7), F(9.99), DATE(2026, 11, 21),
                UTC(2026, 11, 21, 9, 30), TIME(9, 30, 0), U(5415250),
                S('x'), B(b'\x00\xff'), D(999, 2))))))
    return V


def build_labeled():
    L = []
    L.append([
        ('g/X/start', R(S('start'), R(S('raiders')))),
        ('m/0001', R(S('say'), S('hello'), UTC(2026, 11, 21, 9, 30, 0, 5))),
        ('0', R(I(0))),
        ('10', R(I(10), NONE)),
        ('9', R(I(9), R(R(D(-5, 2))))),
    ])
    L.append([
        ('10', R(S('ten'))),
        ('9', R(S('nine'), F(9.99))),
        ('0', R(S('zero'), TRUE)),
        ('1', R(S('one'), B(b'\x01'))),
    ])
    L.append([
        ('m/0002', R(S('line\nbreak "quoted"'), U(-500))),
        ('label with "quote" and \\', R(DATE(9999, 12, 31))),
        ('', R(TIME(23, 59, 59, 999))),
        ('m/0001', R(*[I(i) for i in range(16)])),
        ('é/日本', R(S('é' * 64))),
    ])
    # An empty row between non-empty ones.
    L.append([
        ('g/X/start', R(S('start'))),
        ('empty', R()),
        ('m/0001', R(S('say'), R())),
    ])
    # An empty row first.
    L.append([
        ('empty', R()),
        ('9', R(I(9))),
        ('10', R()),
    ])
    # A single empty row alone.
    L.append([('', R())])
    # Rows reaching level four, labelled.
    L.append([
        ('e/0001', R(S('r'), S('id'),
                     R(S('x'), DATE(2026, 11, 21), S('EUR'), D(9000, 2), NONE,
                       R(R(S('PAV0000S'), I(1)), R(S('BEN00000'), I(1))),
                       S('taxi')))),
        ('deep', R(R(R(R())))),
    ])
    return L


# ---------------------------------------------------------------- self-check

def self_check(valid, labeled):
    known = {0: '00', 127: '7f', 128: '8000', 255: '807f', 300: '812c',
             16511: 'ff7f', 16512: '808000'}
    for v, h in known.items():
        assert varint(v).hex() == h, (v, varint(v).hex())
        assert read_varint(bytes.fromhex(h), 0) == (v, len(h) // 2)
    assert len(varint(2113663)) == 3 and len(varint(2113664)) == 4
    assert len(varint(72624976668147839)) == 8
    assert len(varint(zigzag(MAXINT))) == 8 and zigzag(MAXINT) == 2**54 - 2
    for n in (0, -1, 1, -2, MAXINT, -MAXINT):
        assert unzigzag(zigzag(n)) == n
    assert [zigzag(n) for n in (0, -1, 1, -2)] == [0, 1, 2, 3]
    assert zigzag(-64) == 127 and zigzag(64) == 128
    assert zigzag(-8256) == 16511 and zigzag(8256) == 16512
    assert (DATE(1, 1, 1)[1], DATE(9999, 12, 31)[1]) == (DATE_MIN, DATE_MAX)
    assert UTC(1, 1, 1)[1] == UTC_MIN
    assert UTC(9999, 12, 31, 23, 59, 59, 999)[1] == UTC_MAX
    assert encode(R()) == b'\x00' and enc_item(R(), 1) == b'\xe0'
    # Spellings quoted in ROW.t.
    assert fmt_int(12345) == '12_345' and fmt_int(1234) == '1234'
    assert fmt_int(-7) == '-7'
    assert fmt_dec(1234550, 2) == '12_345.50' and fmt_dec(123450, 2) == '1234.50'
    assert fmt_dec(999, 2) == '9.99' and fmt_dec(-5, 2) == '-0.05'
    assert fmt_dec(0, 2) == '0.00' and fmt_dec(1500, 0) == '1500'
    for x, s in ((9.99, '9.99'), (0.001, '0.001'), (12345.6, '12_345.6'),
                 (3.0, '3'), (0.0, '0'), (1e21, '1e21'), (-1.5e-7, '-1.5e-7')):
        assert fmt_f64(x) == s, (x, fmt_f64(x))
    assert fmt_dur(5415250) == '1h30m15.25s' and fmt_dur(-500) == '-0.5s'
    assert fmt_dur(0) == '0s'
    assert fmt_utc(UTC(2026, 11, 21, 9, 30)[1]) == '2026-11-21T09:30:00.000Z'
    assert fmt_time(TIME(9, 30, 0)[1]) == '09:30:00.000'
    assert fmt_bin(b'\x00\xff') == '0x00ff' and fmt_bin(b'') == '0x'
    # Depth: four levels are a row, a fifth is refused (R005).
    assert encode(R(R(R(R())))).hex() == '01e1e1e0'
    assert decode(bytes.fromhex('01e1e1e0')) == R(R(R(R())))
    assert encode(R(R(R(R(I(1)))))).hex() == '01e1e1e1c302'
    for bad in (R(R(R(R(R())))), R(I(0), R(R(R(S('x'), R(I(1))))))):
        try:
            encode(bad)
        except RowError as e:
            assert str(e).startswith('R005'), e
        else:
            raise AssertionError("five levels encoded")
        try:
            fmt_text(bad)
        except RowError as e:
            assert str(e).startswith('R005'), e
        else:
            raise AssertionError("five levels formatted")
    # 01 e1 e1 e1 e1 e0: value, then four nested rows; the last is level five.
    # Also the long form (cb) at level five, and the refusal before any of its
    # items is read (bytes end there).
    for h in ('01e1e1e1e1e0', '01e1e1e1cb00', '01e1e1e1e1'):
        try:
            decode(bytes.fromhex(h))
        except RowError as e:
            assert str(e).startswith('R005'), (h, e)
        else:
            raise AssertionError("five levels decoded: " + h)
    # Round trips.
    for e in valid:
        b = bytes.fromhex(e['hex'])
        r = decode(b)
        assert encode(r) == b, e
        assert fmt_text(r) == e['text'], (fmt_text(r), e['text'])
    for e in labeled:
        rows = [(x['label'], decode(bytes.fromhex(x['hex']))) for x in e['rows']]
        for (_, r), x in zip(rows, e['rows']):
            assert encode(r).hex() == x['hex']
        assert fmt_labeled(rows) == e['text']


def main():
    valid = []
    for r in build():
        valid.append({'text': fmt_text(r), 'hex': encode(r).hex()})
    labeled = []
    for pairs in build_labeled():
        labeled.append({
            'text': fmt_labeled(pairs),
            'rows': [{'label': l, 'hex': encode(r).hex()} for l, r in pairs],
        })
    self_check(valid, labeled)
    path = sys.argv[1]
    with open(path, 'w', encoding='utf-8') as f:
        json.dump({'valid': valid, 'labeled': labeled}, f, ensure_ascii=False,
                  indent=1)
        f.write('\n')
    print('ok: %d valid, %d labelled -> %s' % (len(valid), len(labeled), path))


if __name__ == '__main__':
    main()
