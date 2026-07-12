package client

import "testing"

func TestNewRequiresHTTPSOutsideLoopback(t *testing.T) {
	t.Parallel()

	allowed := []string{
		"https://service.example",
		"http://localhost:8080",
		"http://127.0.0.1:8080",
		"http://[::1]:8080",
	}
	for _, rawURL := range allowed {
		if _, err := New(rawURL, "token"); err != nil {
			t.Errorf("New(%q) unexpectedly failed: %v", rawURL, err)
		}
	}

	rejected := []string{
		"http://service.example",
		"https://user:password@service.example",
		"https://service.example/base-path",
		"https://service.example?token=secret",
	}
	for _, rawURL := range rejected {
		if _, err := New(rawURL, "token"); err == nil {
			t.Errorf("New(%q) unexpectedly succeeded", rawURL)
		}
	}
}
