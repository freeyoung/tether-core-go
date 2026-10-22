// tether-headless is the engine without a GUI: an in-memory
// clipboard, commands on stdin, events as JSON lines on stdout. Use it to
// debug the engine, or as one more computer on a test machine.
//
//	tether-headless --dir <support dir> [--port 5134] [--name NAME]
//	                [--downloads DIR] [--approve] [--notifications]
//	                [--keep-messages] [--sms FILE]
//
// --keep-messages keeps the text messages of phones, as Tether Desktop does.
// --sms makes it a phone that gives the messages in FILE, a JSON array of
// core.SMS, as Tether for Android does.
//
// Commands, one per line:
//
//	pair <host> [port]      ask a computer to pair
//	copy <text>             the user copies text
//	copypng <path>          the user copies the PNG in <path>
//	send <fingerprint> <path>
//	notify <json>           post a phone notification, such as
//	                        {"ID":"1","AppName":"Chat","Title":"Alice","Text":"hi"}
//	unnotify <id>           remove it
//	smschanged              tell the computers that the messages changed
//	peers                   print the paired peers
//	quit
package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/freeyoung/tether-core-go/core"
)

var outMu sync.Mutex

func emit(v map[string]any) {
	outMu.Lock()
	defer outMu.Unlock()
	line, _ := json.Marshal(v)
	fmt.Println(string(line))
}

type memoryClipboard struct {
	mu      sync.Mutex
	count   uint64
	content core.Content
	has     bool
}

func (c *memoryClipboard) ChangeCount() uint64 { c.mu.Lock(); defer c.mu.Unlock(); return c.count }

func (c *memoryClipboard) Read() (core.Content, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.content, c.has
}

func (c *memoryClipboard) Write(v core.Content) uint64 {
	c.mu.Lock()
	c.content, c.has = v, true
	c.count++
	count := c.count
	c.mu.Unlock()
	if v.IsPNG {
		sum := sha256.Sum256(v.PNG)
		emit(map[string]any{"event": "clipboard", "kind": "png", "value": hex.EncodeToString(sum[:])})
	} else {
		emit(map[string]any{"event": "clipboard", "kind": "text", "value": v.Text})
	}
	return count
}

func (c *memoryClipboard) userCopies(v core.Content) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.content, c.has = v, true
	c.count++
}

type events struct {
	engine  *core.Engine
	approve bool
	sms     []core.SMS
}

func (e *events) PairingRequested(r core.PairingRequest) {
	emit(map[string]any{"event": "pairing_requested", "fingerprint": r.Fingerprint, "name": r.Name, "suggested": string(r.SuggestedKind)})
	if e.approve {
		e.engine.Approve(r.Fingerprint)
	}
}
func (e *events) PairingResolved(string) {}
func (e *events) OutboundPairing(r core.OutboundResult) {
	result := r.Kind
	switch r.Kind {
	case "accepted", "waiting":
		result += " " + r.Peer
	case "failed":
		result += " " + r.Reason
	}
	emit(map[string]any{"event": "outbound", "result": result})
}
func (e *events) OpenURL(u string) { emit(map[string]any{"event": "open_url", "url": u}) }
func (e *events) FileReceived(path, peer string) {
	emit(map[string]any{"event": "file_received", "path": path, "peer": peer})
}
func (e *events) FileSent(name, peer string, ok bool, reason string) {
	emit(map[string]any{"event": "file_sent", "name": name, "peer": peer, "ok": ok})
}
func (e *events) Failed(m string) { emit(map[string]any{"event": "failed", "message": m}) }
func (e *events) StateChanged()   {}
func (e *events) PeerOutdated(name, version string) {
	emit(map[string]any{"event": "peer_outdated", "name": name, "version": version})
}

func (e *events) NotificationPosted(n core.Notification) {
	emit(map[string]any{"event": "notification_posted", "notification": n})
}
func (e *events) NotificationRemoved(fp, id string) {
	emit(map[string]any{"event": "notification_removed", "peer": fp, "id": id})
}

func (e *events) MessagesRequested(fp string, since int64) {
	var list []core.SMS
	for _, m := range e.sms {
		if m.Date >= since {
			list = append(list, m)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Date < list[j].Date })
	more := len(list) > core.MessageBatchSize
	if more {
		list = list[:core.MessageBatchSize]
	}
	e.engine.SendMessages(fp, list, more)
}
func (e *events) MessagesAdded(fp string, added []core.SMS) {
	emit(map[string]any{"event": "messages_added", "peer": fp, "count": len(added)})
}

func main() {
	dir := flag.String("dir", "", "support directory (required)")
	port := flag.Int("port", core.DefaultPort, "port")
	name := flag.String("name", "", "device name")
	downloads := flag.String("downloads", "", "downloads directory")
	approve := flag.Bool("approve", false, "approve every pairing request")
	notifications := flag.Bool("notifications", false, "show the notifications of phones")
	keepMessages := flag.Bool("keep-messages", false, "keep the text messages of phones")
	smsFile := flag.String("sms", "", "give the text messages in this JSON file, as a phone")
	flag.Parse()
	if *dir == "" {
		fmt.Fprintln(os.Stderr, "--dir is required")
		os.Exit(2)
	}
	if *downloads == "" {
		*downloads = filepath.Join(*dir, "downloads")
	}
	var sms []core.SMS
	if *smsFile != "" {
		data, err := os.ReadFile(*smsFile)
		if err == nil {
			err = json.Unmarshal(data, &sms)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	}
	clipboard := &memoryClipboard{}
	engine, err := core.NewEngine(core.Config{
		SupportDir: *dir, DownloadsDir: *downloads, DeviceName: *name, Port: *port, ShowNotifications: *notifications,
		KeepMessages: *keepMessages, ProvideMessages: *smsFile != "",
		Log: func(s string) { fmt.Fprintln(os.Stderr, s) },
	}, clipboard, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	engine.Delegate = &events{engine: engine, approve: *approve, sms: sms}
	engine.Start()
	emit(map[string]any{"event": "ready", "fingerprint": engine.Identity.Fingerprint, "port": *port})

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 64*1024*1024)
	for scanner.Scan() {
		command, rest, _ := strings.Cut(strings.TrimSpace(scanner.Text()), " ")
		switch command {
		case "pair":
			fields := strings.Fields(rest)
			p := core.DefaultPort
			if len(fields) > 1 {
				p, _ = strconv.Atoi(fields[1])
			}
			engine.PairWithComputer(fields[0], p)
		case "copy":
			clipboard.userCopies(core.TextContent(rest))
			engine.CheckLocalClipboard()
		case "copypng":
			png, err := os.ReadFile(rest)
			if err != nil {
				emit(map[string]any{"event": "failed", "message": err.Error()})
				continue
			}
			clipboard.userCopies(core.PNGContent(png))
			engine.CheckLocalClipboard()
		case "send":
			fp, path, _ := strings.Cut(rest, " ")
			engine.SendFiles([]string{path}, fp)
		case "notify":
			var n core.Notification
			if err := json.Unmarshal([]byte(rest), &n); err != nil {
				emit(map[string]any{"event": "failed", "message": err.Error()})
				continue
			}
			engine.PostNotification(n)
		case "unnotify":
			engine.RemoveNotification(rest)
		case "smschanged":
			engine.MessagesChanged()
		case "peers":
			emit(map[string]any{"event": "peers", "peers": engine.SnapshotNow().Peers})
		case "quit":
			engine.Stop()
			return
		}
	}
	engine.Stop()
}
