package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/exotermo/prelo-executor-worker/internal/workspace"
)

// This trusted helper is baked into the pinned container image. It has no shell, network
// client, credentials or API for changing permissions. The VM worker invokes one command
// only after Prelo authorizes that exact operation.
func main() {
	if len(os.Args) == 2 && os.Args[1] == "hold" {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT)
		select {
		case <-ch:
		case <-time.After(time.Hour):
		}
		return
	}
	if len(os.Args) != 3 {
		fail(errors.New("usage: workspace-helper <list|read|mkdir|create> <relative-path>"))
	}
	w, err := workspace.Open("/workspace")
	if err != nil {
		fail(err)
	}
	defer w.Close()
	enc := json.NewEncoder(os.Stdout)
	switch os.Args[1] {
	case "list":
		items, err := w.List(os.Args[2])
		if err != nil {
			fail(err)
		}
		err = enc.Encode(struct {
			Entries []workspace.Entry `json:"entries"`
		}{items})
	case "read":
		data, e := w.Read(os.Args[2])
		if e != nil {
			fail(e)
		}
		err = enc.Encode(struct {
			ContentBase64 string `json:"contentBase64"`
			Size          int    `json:"size"`
		}{base64.StdEncoding.EncodeToString(data), len(data)})
	case "mkdir":
		err = w.Mkdir(os.Args[2])
		if err != nil {
			fail(err)
		}
		err = enc.Encode(struct {
			Created bool `json:"created"`
		}{true})
	case "create":
		created, e := w.Create(os.Args[2], os.Stdin)
		if e != nil {
			fail(e)
		}
		err = enc.Encode(created)
	default:
		fail(errors.New("unsupported workspace operation"))
	}
	if err != nil {
		fail(err)
	}
}

func fail(err error) {
	// Paths/content may be untrusted; do not echo them to stderr or logs.
	fmt.Fprintln(os.Stderr, "workspace operation failed:", classify(err))
	os.Exit(1)
}
func classify(err error) string {
	switch {
	case errors.Is(err, workspace.ErrInvalidPath):
		return "invalid_path"
	case errors.Is(err, workspace.ErrLimit):
		return "limit_exceeded"
	case errors.Is(err, os.ErrExist):
		return "already_exists"
	case errors.Is(err, os.ErrNotExist):
		return "not_found"
	default:
		return "operation_failed"
	}
}
