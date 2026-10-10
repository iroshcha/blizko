//go:build windows

// desktop is a private stdio bridge for the native Windows UI. No HTTP port,
// credentials or storage secrets are exposed to the interface.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"blizko/core/mobile"
)

type command struct {
	ID     int    `json:"id"`
	Op     string `json:"op"`
	First  string `json:"first"`
	Second string `json:"second"`
	Value  bool   `json:"value"`
	Before int64  `json:"before"`
	Limit  int    `json:"limit"`
}

type response struct {
	ID       int             `json:"id"`
	Error    string          `json:"error,omitempty"`
	Code     string          `json:"code,omitempty"`
	Snapshot json.RawMessage `json:"snapshot,omitempty"`
}

func run(input io.Reader, output io.Writer, node *mobile.Node) error {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), 65536)
	writer := json.NewEncoder(output)
	var outputMu sync.Mutex
	var checks sync.WaitGroup
	checkSlots := make(chan struct{}, 4)
	write := func(result response) error { outputMu.Lock(); defer outputMu.Unlock(); return writer.Encode(result) }
	defer checks.Wait()
	for scanner.Scan() {
		var request command
		var result response
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			result.Error = "Неверная команда"
		} else {
			result.ID = request.ID
			if request.Op == "check" || request.Op == "search" {
				select {
				case checkSlots <- struct{}{}:
					checks.Add(1)
					go func(request command) {
						defer checks.Done()
						defer func() { <-checkSlots }()
						result := response{ID: request.ID}
						if request.Op == "search" {
							result.Snapshot = json.RawMessage(node.SearchPage(request.First, request.Second, request.Before, request.Limit))
						} else {
							code, err := node.CheckContact(request.First)
							result.Code = code
							if err != nil {
								result.Error = err.Error()
							}
							result.Snapshot = json.RawMessage(node.Status())
						}
						_ = write(result)
					}(request)
				default:
					_ = write(response{ID: request.ID, Error: "Дождитесь завершения предыдущих проверок"})
				}
				continue
			}
			var err error
			switch request.Op {
			case "snapshot", "page", "status", "search", "unread":
			case "clear":
				err = node.BeginClearHistory(request.First)
			case "clear-cancel":
				node.CancelClearHistory()
			case "cancel":
				err = node.CancelMessage(request.First, request.Second)
			case "retry":
				err = node.RetryMessage(request.First, request.Second)
			case "draft":
				err = node.SaveDraft(request.First, request.Second)
			case "draft-sent":
				err = node.ClearSentDraft(request.First, request.Second)
			case "read":
				err = node.MarkRead(request.First, request.Before)
			case "check-cancel":
				node.CancelContactCheck(request.First)
			case "network-state":
				node.SetNetworkAvailable(request.Value)
			case "start":
				err = node.Start()
			case "stop":
				node.Stop()
			case "add":
				err = node.AddContact(request.First, request.Second)
			case "send":
				err = node.Send(request.First, request.Second)
			case "code":
				result.Code, err = node.MyCode()
			case "relay":
				err = node.SetRelayOnly(request.Value)
			case "server":
				err = node.SetRelayURL(request.First)
			case "network":
				node.NetworkChanged()
			case "quit":
				node.CancelClearHistory()
				node.Stop()
				return write(response{ID: request.ID})
			default:
				result.Error = "Неизвестная команда"
			}
			if err != nil {
				result.Error = err.Error()
			}
			if request.Op == "search" {
				result.Snapshot = json.RawMessage(node.SearchPage(request.First, request.Second, request.Before, request.Limit))
			} else if request.Op == "unread" {
				result.Snapshot = json.RawMessage(node.UnreadPage(request.First, request.Limit))
			} else if request.Op == "status" || request.Op == "draft" || request.Op == "draft-sent" || request.Op == "read" {
				result.Snapshot = json.RawMessage(node.Status())
			} else {
				peer := ""
				if request.Op == "page" || request.Op == "send" || request.Op == "clear" {
					peer = request.First
				}
				result.Snapshot = json.RawMessage(node.SnapshotPage(peer, request.Before, request.Limit))
			}
		}
		if err := write(result); err != nil {
			return err
		}
	}
	node.Stop()
	node.CancelClearHistory()
	return scanner.Err()
}

func main() {
	dataDirectory := flag.String("data-dir", "", "private local data directory")
	flag.Parse()
	if *dataDirectory == "" {
		*dataDirectory = filepath.Join(os.Getenv("LOCALAPPDATA"), "Blizko")
	}
	err := func() error {
		if err := os.MkdirAll(*dataDirectory, 0700); err != nil {
			return err
		}
		unlock, err := lockStorage(*dataDirectory)
		if err != nil {
			return err
		}
		defer unlock()
		key, err := storageKey(*dataDirectory)
		if err != nil {
			return err
		}
		node, err := mobile.NewNode(filepath.Join(*dataDirectory, "core"), key)
		if err != nil {
			return err
		}
		defer node.Stop()
		return run(os.Stdin, os.Stdout, node)
	}()
	if err != nil {
		_ = json.NewEncoder(os.Stdout).Encode(response{Error: err.Error()})
		fmt.Fprintln(os.Stderr, "Blizko engine failed to open or complete its session")
		os.Exit(1)
	}
}
