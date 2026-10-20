//go:build !js

package tile

import (
	"context"
	"testing"
)

func TestFetchSendsUserAgent(t *testing.T) {
	s := newTileServer(t)
	l := NewLoader(Options{UserAgent: "ntmaps-test/1.0 (+https://example.test)"})
	if _, err := l.Fetch(context.Background(), s.provider(), ID{1, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if ua := s.userAgent.Load(); ua == nil || *ua != "ntmaps-test/1.0 (+https://example.test)" {
		t.Fatalf("server saw User-Agent %v", ua)
	}
	s2 := newTileServer(t)
	if _, err := NewLoader(Options{}).Fetch(context.Background(), s2.provider(), ID{1, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if ua := s2.userAgent.Load(); ua == nil || *ua != DefaultUserAgent() {
		t.Fatalf("default User-Agent = %v, want %q", ua, DefaultUserAgent())
	}
}
