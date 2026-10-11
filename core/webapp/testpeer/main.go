// Local integration fixture. Uses the unmodified Android/Windows shared engine.
package main

import (
	"blizko/core/mobile"
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) != 3 {
		panic("base and temporary directory required")
	}
	os.Setenv("BLIZKO_SERVER_URL", os.Args[1])
	key := make([]byte, 32)
	key[0] = 17
	node, err := mobile.NewNode(os.Args[2], key)
	if err != nil {
		panic(err)
	}
	defer node.Stop()
	if err = node.Start(); err != nil {
		panic(err)
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var req struct {
			Op   string `json:"op"`
			Name string `json:"name"`
			Code string `json:"code"`
			Peer string `json:"peer"`
			Text string `json:"text"`
		}
		if json.Unmarshal(scanner.Bytes(), &req) != nil {
			panic("bad command")
		}
		var value any
		var e error
		switch req.Op {
		case "code":
			value, e = node.MyCode()
		case "add":
			e = node.AddContact(req.Name, req.Code)
		case "send":
			e = node.Send(req.Peer, req.Text)
		case "snapshot":
			value = json.RawMessage(node.Snapshot())
		case "stop":
			node.Stop()
		case "start":
			e = node.Start()
		default:
			e = fmt.Errorf("unknown op")
		}
		answer := map[string]any{"value": value}
		if e != nil {
			answer["error"] = e.Error()
		}
		b, _ := json.Marshal(answer)
		fmt.Println(string(b))
	}
}
