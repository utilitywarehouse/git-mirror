package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func Test_webhook(t *testing.T) {
	wh := &GithubWebhookHandler{
		secret: "a1b2c3d4e5",
	}

	body := []byte(`{"foo":"bar", "action": "foo"}`)
	signature := wh.computeHMAC(body, wh.secret)

	t.Run("validate signature", func(t *testing.T) {

		if !wh.isValidSignature(body, signature) {
			t.Errorf("isValidSignature() expected true")
		}

		invalidSig := wh.computeHMAC(body, "invalid-secret")

		if wh.isValidSignature(body, invalidSig) {
			t.Errorf("isValidSignature() expected false")
		}

		if wh.isValidSignature([]byte{}, "") {
			t.Errorf("isValidSignature() expected false for emtpy signature")
		}
	})

	t.Run("invalid method", func(t *testing.T) {
		server := httptest.NewServer(http.Handler(wh))
		defer server.Close()

		req, err := http.NewRequest("GET", server.URL, strings.NewReader(string(body)))
		if err != nil {
			t.Fatalf("Failed to make a request: %v", err)
		}
		req.Header.Set("X-Hub-Signature-256", signature)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to send request: %v", err)
		}

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status %v, got %v", http.StatusBadRequest, resp.StatusCode)
		}
	})

	t.Run("ping event", func(t *testing.T) {
		server := httptest.NewServer(http.Handler(wh))
		defer server.Close()

		req, err := http.NewRequest("POST", server.URL, strings.NewReader(string(body)))
		if err != nil {
			t.Fatalf("Failed to make a request: %v", err)
		}
		req.Header.Set("X-Hub-Signature-256", signature)
		req.Header.Set("X-GitHub-Event", "ping")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to send request: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status %v, got %v", http.StatusOK, resp.StatusCode)
		}

		reply, _ := io.ReadAll(resp.Body)
		if string(reply) != "pong" {
			t.Errorf("Expected pong for ping event")
		}
	})
}
