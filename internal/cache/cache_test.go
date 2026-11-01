package cache

import "testing"

func TestRoundTrip(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	type v struct{ A int }
	if c.Get("jobs/acme/web/1-1", &v{}) {
		t.Fatal("empty cache returned a hit")
	}
	if err := c.Put("jobs/acme/web/1-1", v{A: 7}); err != nil {
		t.Fatal(err)
	}
	var got v
	if !c.Get("jobs/acme/web/1-1", &got) || got.A != 7 {
		t.Fatalf("got %+v", got)
	}
}

func TestRejectsEscapingKeys(t *testing.T) {
	c, _ := Open(t.TempDir())
	for _, k := range []string{"", "../x", "/etc/passwd", "a/../../b"} {
		if err := c.Put(k, 1); err == nil {
			t.Errorf("Put(%q) should fail", k)
		}
	}
}

func TestNilCache(t *testing.T) {
	var c *Cache
	if c.Get("k", new(int)) {
		t.Fatal("nil cache hit")
	}
	if err := c.Put("k", 1); err != nil {
		t.Fatal(err)
	}
}
