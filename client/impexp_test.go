package client

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestExportCSV_StableSortedHeader(t *testing.T) {
	c, _ := newPair(t)
	if err := c.Put([]byte("b"), []byte("2")); err != nil {
		t.Fatal(err)
	}
	if err := c.Put([]byte("a"), []byte("1")); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := c.ExportCSV(&buf); err != nil {
		t.Fatal(err)
	}
	want := "Key,Value\na,1\nb,2\n"
	if buf.String() != want {
		t.Fatalf("export:\n got %q\nwant %q", buf.String(), want)
	}
}

func TestExportCSV_OmitsTombstones(t *testing.T) {
	c, _ := newPair(t)
	_ = c.Put([]byte("keep"), []byte("yes"))
	_ = c.Put([]byte("gone"), []byte("no"))
	_ = c.Delete([]byte("gone"))
	var buf bytes.Buffer
	if err := c.ExportCSV(&buf); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "gone") {
		t.Fatalf("export included tombstoned key:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "keep,yes") {
		t.Fatalf("export missing live key:\n%s", buf.String())
	}
}

func TestImportCSV_KeyValueHeader(t *testing.T) {
	c, _ := newPair(t)
	in := "Key,Value\napple,green\nbanana,yellow\n"
	n, err := c.ImportCSV(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("imported %d rows, want 2", n)
	}
	for _, k := range []string{"apple", "banana"} {
		_, ok, _ := c.Get([]byte(k))
		if !ok {
			t.Fatalf("key %s missing after import", k)
		}
	}
}

// Accept qvlt's export header shape: User,Bucket,Key,Value,Uid. Only
// Key and Value columns are read.
func TestImportCSV_QvltHeader(t *testing.T) {
	c, _ := newPair(t)
	in := "User,Bucket,Key,Value,Uid\n" +
		"alice,VAULT,github_token,ghp_xxx,uid1\n" +
		"alice,VAULT,email_pass,hunter2,uid2\n"
	n, err := c.ImportCSV(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("imported %d rows, want 2", n)
	}
	v, ok, _ := c.Get([]byte("github_token"))
	if !ok || !bytes.Equal(v, []byte("ghp_xxx")) {
		t.Fatalf("qvlt-format import lost github_token: ok=%v v=%q", ok, v)
	}
}

// Case-insensitive header lookup.
func TestImportCSV_MixedCaseHeader(t *testing.T) {
	c, _ := newPair(t)
	in := "key,VALUE\nk,v\n"
	n, err := c.ImportCSV(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("imported %d rows, want 1", n)
	}
}

func TestImportCSV_MissingColumns(t *testing.T) {
	c, _ := newPair(t)
	_, err := c.ImportCSV(strings.NewReader("A,B\n1,2\n"))
	if err == nil {
		t.Fatal("expected error on CSV without Key/Value header")
	}
}

func TestImportCSV_SkipsEmptyKeyAndValue(t *testing.T) {
	c, _ := newPair(t)
	in := "Key,Value\n,v\nk,\n ,  \nreal,secret\n"
	n, err := c.ImportCSV(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		// "" + "v" → skip, "k" + "" → skip (no silent tombstone),
		// " " + "  " → whitespace is kept verbatim (valid),
		// "real" + "secret" → imported.
		// That's 2 Puts.
		t.Fatalf("n=%d want 2 (skip empty key and empty value)", n)
	}
}

// Round-trip: export then import into a fresh client must converge.
func TestExportImport_RoundTrip(t *testing.T) {
	ctx := context.Background()

	src, _ := newPair(t)
	_ = src.Put([]byte("a"), []byte("1"))
	_ = src.Put([]byte("b"), []byte("two"))
	_ = src.Put([]byte("c"), []byte("three, with comma"))
	_ = src.Put([]byte("quoted"), []byte(`he said "hi"`))

	var buf bytes.Buffer
	if err := src.ExportCSV(&buf); err != nil {
		t.Fatal(err)
	}

	dst, _ := newPair(t)
	n, err := dst.ImportCSV(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Fatalf("imported %d, want 4", n)
	}
	// Sync to ensure nothing about the server path breaks the data.
	if err := dst.Sync(ctx); err != nil {
		t.Fatal(err)
	}

	for _, kv := range []struct{ k, v string }{
		{"a", "1"},
		{"b", "two"},
		{"c", "three, with comma"},
		{"quoted", `he said "hi"`},
	} {
		got, ok, err := dst.Get([]byte(kv.k))
		if err != nil || !ok {
			t.Fatalf("Get %q: ok=%v err=%v", kv.k, ok, err)
		}
		if string(got) != kv.v {
			t.Fatalf("Get %q: got %q, want %q", kv.k, got, kv.v)
		}
	}
}
