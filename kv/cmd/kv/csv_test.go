package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"repani.com/kv/client"
	"repani.com/kv/server"
	"repani.com/kv/store"
	"repani.com/kv/vault"
)

var testRegKey = vault.RegisterKey{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}

// testClient is a client of a fresh vault on a fresh in-process server.
func testClient(t *testing.T) *client.Client {
	t.Helper()
	srv, err := server.New(server.Options{RegisterKeys: []vault.RegisterKey{testRegKey}})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	rk, _ := vault.NewRandomRootKey()
	c, err := client.New(rk, store.NewMemStore(), &client.HTTPTransport{Base: ts.URL}, client.Options{RegisterKey: &testRegKey})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func get(t *testing.T, c *client.Client, key string) (string, bool) {
	t.Helper()
	v, ok, err := c.Get([]byte(key))
	if err != nil {
		t.Fatal(err)
	}
	return string(v), ok
}

func TestExportCSV_SortedWithHeader(t *testing.T) {
	c := testClient(t)
	c.Put([]byte("b"), []byte("2"))
	c.Put([]byte("a"), []byte("1"))
	c.Put([]byte("gone"), []byte("no"))
	c.Delete([]byte("gone"))
	var buf bytes.Buffer
	if err := exportCSV(c, &buf); err != nil {
		t.Fatal(err)
	}
	if want := "Key,Value\na,1\nb,2\n"; buf.String() != want {
		t.Fatalf("export:\n got %q\nwant %q", buf.String(), want)
	}
}

func TestImportCSV(t *testing.T) {
	for _, tc := range []struct {
		name, in string
		n        int
		want     map[string]string
	}{
		{"key and value", "Key,Value\napple,green\nbanana,yellow\n", 2,
			map[string]string{"apple": "green", "banana": "yellow"}},
		{"extra columns", "User,Bucket,Key,Value,Uid\nalice,V,token,ghp_x,u1\n", 1,
			map[string]string{"token": "ghp_x"}},
		{"header case", "key,VALUE\nk,v\n", 1, map[string]string{"k": "v"}},
		// An empty key or value is skipped (no silent tombstone);
		// whitespace is a value like any other.
		{"empty skipped", "Key,Value\n,v\nk,\n ,  \nreal,secret\n", 2,
			map[string]string{" ": "  ", "real": "secret"}},
		{"later row wins", "Key,Value\nk,first\nk,second\n", 1, map[string]string{"k": "second"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := testClient(t)
			n, err := importCSV(c, strings.NewReader(tc.in))
			if err != nil || n != tc.n {
				t.Fatalf("imported %d, %v; want %d", n, err, tc.n)
			}
			for k, v := range tc.want {
				if got, ok := get(t, c, k); !ok || got != v {
					t.Errorf("%q = %q, %v; want %q", k, got, ok, v)
				}
			}
		})
	}
}

func TestImportCSV_MissingColumns(t *testing.T) {
	if _, err := importCSV(testClient(t), strings.NewReader("A,B\n1,2\n")); err == nil {
		t.Fatal("a CSV without Key and Value columns was accepted")
	}
}

// TestExportImport_RoundTrip: what one vault exports, another imports
// whole, across chunks, and syncs.
func TestExportImport_RoundTrip(t *testing.T) {
	src := testClient(t)
	want := map[string]string{"c": "three, with comma", "quoted": `he said "hi"`}
	for i := range importChunk + 10 {
		want[fmt.Sprintf("k%04d", i)] = fmt.Sprint("v", i)
	}
	for k, v := range want {
		if err := src.Put([]byte(k), []byte(v)); err != nil {
			t.Fatal(err)
		}
	}
	var buf bytes.Buffer
	if err := exportCSV(src, &buf); err != nil {
		t.Fatal(err)
	}
	dst := testClient(t)
	if n, err := importCSV(dst, &buf); err != nil || n != len(want) {
		t.Fatalf("imported %d, %v; want %d", n, err, len(want))
	}
	if err := dst.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	for k, v := range want {
		if got, ok := get(t, dst, k); !ok || got != v {
			t.Fatalf("%q = %q, %v; want %q", k, got, ok, v)
		}
	}
}
