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

	"blizko/core/mobile"
)

type command struct {
	ID     int    `json:"id"`
	Op     string `json:"op"`
	First  string `json:"first"`
	Second string `json:"second"`
	Value  bool   `json:"value"`
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
	for scanner.Scan() {
		var request command
		var result response
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			result.Error = "Неверная команда"
		} else {
			result.ID = request.ID
			var err error
			switch request.Op {
			case "snapshot":
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
			case "check":
				result.Code, err = node.CheckContact(request.First)
			case "relay":
				err = node.SetRelayOnly(request.Value)
			case "network":
				node.NetworkChanged()
			case "quit":
				node.Stop()
				return writer.Encode(response{ID: request.ID})
			default:
				result.Error = "Неизвестная команда"
			}
			if err != nil {
				result.Error = err.Error()
			}
			result.Snapshot = json.RawMessage(node.Snapshot())
		}
		if err := writer.Encode(result); err != nil {
			return err
		}
	}
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
