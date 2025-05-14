package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/utilitywarehouse/git-mirror/repopool"
)

type GitHubEvent struct {
	Repository struct {
		Name  string `json:"name"`
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
		HtmlURL string `json:"html_url"`
		GitURL  string `json:"git_url"`
	} `json:"repository"`

	// The full git ref that was pushed. Example: refs/heads/main or refs/tags/v3.14.1.
	Ref string `json:"ref"`
	// The SHA of the most recent commit on ref before the push.
	Before string `json:"before"`
	// The SHA of the most recent commit on ref after the push.
	After string `json:"after"`
}

type GithubWebhookHandler struct {
	repoPool *repopool.RepoPool
	secret   string
	log      *slog.Logger
}

func (wh *GithubWebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		wh.log.Error("cannot read request body", "error", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if !wh.isValidSignature(body, r.Header.Get("X-Hub-Signature-256")) {
		wh.log.Error("invalid signature")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	event := r.Header.Get("X-GitHub-Event")

	var payload GitHubEvent
	if err := json.Unmarshal(body, &payload); err != nil {
		wh.log.Error("cannot unmarshal json payload", "error", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// The ping event is a confirmation from GitHub that
	// the webhook is configured correctly.
	if event == "ping" {
		w.Write([]byte("pong"))
		return
	}

	// only process 'push' event but but return ok for all events to mark
	// successful delivery
	if event == "push" {
		go wh.processPushEvent(payload)
		return
	}
}

func (wh *GithubWebhookHandler) isValidSignature(message []byte, signature string) bool {
	return hmac.Equal([]byte(signature), []byte(wh.computeHMAC(message, wh.secret)))
}

func (wh *GithubWebhookHandler) computeHMAC(message []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))

	if _, err := mac.Write(message); err != nil {
		wh.log.Error("cannot compute hmac for request", "error", err)
		return ""
	}

	// GH adds `sha256=` prefix in header value
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func (wh *GithubWebhookHandler) processPushEvent(event GitHubEvent) {
	err := wh.repoPool.QueueMirrorRun(event.Repository.HtmlURL)
	if err != nil {
		if errors.Is(err, repopool.ErrNotExist) {
			return
		}
		wh.log.Error("unable to process push event", "repo", event.Repository.HtmlURL, "err", err)
		return
	}
}
