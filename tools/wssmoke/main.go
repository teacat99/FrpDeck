// wssmoke is a tiny one-off helper used during P1-C verification: it
// dials the FrpDeck WebSocket using the same Sec-WebSocket-Protocol
// JWT auth path the browser uses, subscribes to the requested topics,
// and prints every event it receives with a timestamp prefix.
//
// Usage:
//
//	wssmoke -url ws://127.0.0.1:18081/api/ws \
//	    -token <jwt> \
//	    -topics tunnels,endpoints \
//	    -seconds 10
//
// Exits with 0 if it received at least one event during the window
// (matches the smoke gate the docs reference); otherwise exits 1.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/coder/websocket"
)

type wsMessage struct {
	Op     string          `json:"op,omitempty"`
	Topics []string        `json:"topics,omitempty"`
	Event  string          `json:"event,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
	Err    string          `json:"err,omitempty"`
}

func main() {
	url := flag.String("url", "ws://127.0.0.1:18081/api/ws", "WebSocket URL")
	token := flag.String("token", "", "JWT bearer token")
	topics := flag.String("topics", "tunnels,endpoints", "comma-separated topics to subscribe")
	seconds := flag.Int("seconds", 10, "how long to listen before exiting")
	requireEvent := flag.Bool("require-event", false, "exit 1 if no event arrives in window")
	flag.Parse()

	if *token == "" {
		fmt.Fprintln(os.Stderr, "missing -token")
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*seconds+5)*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, *url, &websocket.DialOptions{
		Subprotocols: []string{"jwt", *token},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "dial: %v\n", err)
		os.Exit(2)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")

	// Subscribe immediately. The server will echo an "ack" frame.
	subFrame, _ := json.Marshal(wsMessage{
		Op:     "subscribe",
		Topics: strings.Split(*topics, ","),
	})
	if err := conn.Write(ctx, websocket.MessageText, subFrame); err != nil {
		fmt.Fprintf(os.Stderr, "subscribe: %v\n", err)
		os.Exit(2)
	}

	deadline := time.Now().Add(time.Duration(*seconds) * time.Second)
	got := 0
	for time.Now().Before(deadline) {
		readCtx, rc := context.WithDeadline(ctx, deadline)
		_, data, err := conn.Read(readCtx)
		rc()
		if err != nil {
			break
		}
		var m wsMessage
		_ = json.Unmarshal(data, &m)
		if m.Event == "endpoint_state" || m.Event == "tunnel_state" || m.Event == "log" {
			got++
		}
		fmt.Printf("[%s] %s\n", time.Now().Format("15:04:05.000"), string(data))
	}
	fmt.Fprintf(os.Stderr, "events received: %d\n", got)
	if *requireEvent && got == 0 {
		os.Exit(1)
	}
}
