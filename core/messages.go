package core

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Features in hello for the text messages (SMS) of Android phones. A
// computer that keeps them announces MessagesFeature; a phone that gives
// them announces MessageSourceFeature. tetherd and the iPhone app announce
// neither, so they never get sms_sync, sms_batch or sms_changed.
const (
	MessagesFeature      = "sms"
	MessageSourceFeature = "sms_source"
)

// MessageBatchSize is the most messages in one sms_batch.
const MessageBatchSize = 500

const maxMessageBody = 64 << 10

// SMS is one text message on a phone. ID and Thread are the phone's own:
// the _id and thread_id of Android's SMS provider.
type SMS struct {
	ID      int64  `json:"id"`
	Thread  int64  `json:"thread"`
	Address string `json:"address"`
	// The name of the contact, or empty.
	Name string `json:"name,omitempty"`
	// In milliseconds since 1970.
	Date int64 `json:"date"`
	// Sent by the phone's user; else received.
	Sent bool   `json:"sent,omitempty"`
	Body string `json:"body"`
}

func (m SMS) key() [2]int64 { return [2]int64{m.ID, m.Date} }

// The sync: the computer asks a phone for the messages from its cursor on
// (sms_sync, since), and the phone answers with batches, oldest first
// (sms_batch, more). The cursor is the date of the newest message the
// computer has. Messages of that same date come again, and the store drops
// the ones it has. A phone sends sms_changed when it gets a message; the
// computer then asks again. Messages that the phone deletes stay on the
// computer.

// SendMessages sends one batch of messages to the computer with this
// fingerprint, as the answer to MessagesRequested. more tells the computer
// to ask for the next batch.
func (e *Engine) SendMessages(fp string, messages []SMS, more bool) {
	for i := range messages {
		messages[i].Body = cutUTF8(messages[i].Body, maxMessageBody)
	}
	e.queue.Post(func() {
		for _, s := range e.sessionList() {
			if s.fp() == fp && s.state == paired && !s.conn.Closed && s.peerFeatures[MessagesFeature] {
				s.conn.Send(Message{Command: "sms_batch", Messages: messages, More: more}, nil)
				return
			}
		}
	})
}

// MessagesChanged tells the computers that keep messages that the phone has
// new ones.
func (e *Engine) MessagesChanged() {
	e.queue.Post(func() {
		for _, s := range e.sessionList() {
			if s.state == paired && !s.conn.Closed && s.peerFeatures[MessagesFeature] {
				s.conn.Send(Message{Command: "sms_changed"}, nil)
			}
		}
	})
}

// Messages is the store of the computer side, or nil without
// Config.KeepMessages.
func (e *Engine) Messages() *MessageStore { return e.messages }

func (e *Engine) startMessageSync(s *session) {
	if e.messages == nil || !s.peerFeatures[MessageSourceFeature] {
		return
	}
	if s.syncing {
		s.syncAgain = true
		return
	}
	s.syncing = true
	since := e.messages.Cursor(s.fp())
	s.conn.Send(Message{Command: "sms_sync", Since: &since}, nil)
}

func (e *Engine) receivedMessages(m Message, s *session) {
	switch m.Command {
	case "sms_sync":
		if !e.Config.ProvideMessages || !s.peerFeatures[MessagesFeature] {
			return
		}
		fp, since := s.fp(), int64(0)
		if m.Since != nil {
			since = *m.Since
		}
		e.notify(func(d Delegate) { d.MessagesRequested(fp, since) })
	case "sms_changed":
		e.startMessageSync(s)
	case "sms_batch":
		if e.messages == nil || !s.syncing {
			return
		}
		fp := s.fp()
		added, err := e.messages.Add(fp, m.Messages)
		if err != nil {
			e.log("cannot save messages: " + err.Error())
		}
		if len(added) > 0 {
			e.log(fmt.Sprintf("new messages from %s: %d", s.name, len(added)))
			e.notify(func(d Delegate) { d.MessagesAdded(fp, added) })
		}
		s.syncing = false
		// A batch with nothing new would ask for the same batch again.
		if (m.More && len(added) > 0) || s.syncAgain {
			s.syncAgain = false
			e.startMessageSync(s)
		}
	}
}

// MARK: - The store

// MessageStore keeps the messages of every phone in messages.jsonl, one
// message on each line with the fingerprint of its phone. It keeps them all
// in memory too. Its methods may be called from any goroutine.
type MessageStore struct {
	path string
	mu   sync.Mutex
	// By phone, then by key.
	phones map[string]map[[2]int64]SMS
}

type storedSMS struct {
	Peer string `json:"peer"`
	SMS
}

// OpenMessageStore reads the file, if there is one. Lines that it cannot
// read are skipped.
func OpenMessageStore(path string) (*MessageStore, error) {
	st := &MessageStore{path: path, phones: map[string]map[[2]int64]SMS{}}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return st, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64<<10), 2*maxMessageBody)
	for scanner.Scan() {
		var m storedSMS
		if json.Unmarshal(scanner.Bytes(), &m) == nil && m.Peer != "" {
			st.put(m.Peer, m.SMS)
		}
	}
	return st, scanner.Err()
}

func (st *MessageStore) put(peer string, m SMS) bool {
	phone := st.phones[peer]
	if phone == nil {
		phone = map[[2]int64]SMS{}
		st.phones[peer] = phone
	}
	if _, ok := phone[m.key()]; ok {
		return false
	}
	phone[m.key()] = m
	return true
}

// Add saves the messages it does not have yet, and returns them.
func (st *MessageStore) Add(peer string, messages []SMS) ([]SMS, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	var added []SMS
	var lines []byte
	for _, m := range messages {
		if !st.put(peer, m) {
			continue
		}
		added = append(added, m)
		line, _ := json.Marshal(storedSMS{Peer: peer, SMS: m})
		lines = append(append(lines, line...), '\n')
	}
	if len(lines) == 0 {
		return added, nil
	}
	if err := os.MkdirAll(filepath.Dir(st.path), 0o700); err != nil {
		return added, err
	}
	f, err := os.OpenFile(st.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return added, err
	}
	defer f.Close()
	_, err = f.Write(lines)
	return added, err
}

// Has tells whether the store has messages from this phone.
func (st *MessageStore) Has(peer string) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	return len(st.phones[peer]) > 0
}

// Cursor is the date of the newest message from this phone, or 0.
func (st *MessageStore) Cursor(peer string) int64 {
	st.mu.Lock()
	defer st.mu.Unlock()
	var newest int64
	for _, m := range st.phones[peer] {
		newest = max(newest, m.Date)
	}
	return newest
}

// Conversation is one thread of one phone, as the list of conversations
// shows it.
type Conversation struct {
	Peer    string `json:"peer"`
	Thread  int64  `json:"thread"`
	Address string `json:"address"`
	Name    string `json:"name,omitempty"`
	Last    SMS    `json:"last"`
	Count   int    `json:"count"`
}

// Conversations returns the threads of one phone, or of all phones for "",
// the newest first.
func (st *MessageStore) Conversations(peer string) []Conversation {
	st.mu.Lock()
	defer st.mu.Unlock()
	type id struct {
		peer   string
		thread int64
	}
	byThread := map[id]*Conversation{}
	for p, phone := range st.phones {
		if peer != "" && p != peer {
			continue
		}
		for _, m := range phone {
			c := byThread[id{p, m.Thread}]
			if c == nil {
				c = &Conversation{Peer: p, Thread: m.Thread}
				byThread[id{p, m.Thread}] = c
			}
			c.Count++
			if c.Count == 1 || newer(m, c.Last) {
				c.Last = m
			}
		}
	}
	list := make([]Conversation, 0, len(byThread))
	for _, c := range byThread {
		c.Address, c.Name = c.Last.Address, c.Last.Name
		list = append(list, *c)
	}
	sort.Slice(list, func(i, j int) bool { return newer(list[i].Last, list[j].Last) })
	return list
}

// Thread returns the messages of one thread, the oldest first.
func (st *MessageStore) Thread(peer string, thread int64) []SMS {
	st.mu.Lock()
	defer st.mu.Unlock()
	var list []SMS
	for _, m := range st.phones[peer] {
		if m.Thread == thread {
			list = append(list, m)
		}
	}
	sort.Slice(list, func(i, j int) bool { return newer(list[j], list[i]) })
	return list
}

// Find returns the thread with this address or contact name, the newest
// one if there are several.
func (st *MessageStore) Find(peer, who string) (Conversation, bool) {
	who = strings.TrimSpace(who)
	if who == "" {
		return Conversation{}, false
	}
	for _, c := range st.Conversations(peer) {
		if strings.EqualFold(c.Name, who) || sameNumber(c.Address, who) {
			return c, true
		}
	}
	return Conversation{}, false
}

// Search returns at most limit messages of one phone, or of all phones for
// "", whose text, address or contact name holds every word of the query,
// ignoring case, the newest first.
func (st *MessageStore) Search(peer, query string, limit int) []Found {
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return nil
	}
	st.mu.Lock()
	var found []Found
	for p, phone := range st.phones {
		if peer != "" && p != peer {
			continue
		}
		for _, m := range phone {
			text := strings.ToLower(m.Body + "\n" + m.Name + "\n" + m.Address)
			all := true
			for _, w := range words {
				if !strings.Contains(text, w) {
					all = false
					break
				}
			}
			if all {
				found = append(found, Found{Peer: p, SMS: m})
			}
		}
	}
	st.mu.Unlock()
	sort.Slice(found, func(i, j int) bool { return newer(found[i].SMS, found[j].SMS) })
	if len(found) > limit {
		found = found[:limit]
	}
	return found
}

// Found is a message that Search found, with its phone.
type Found struct {
	Peer string `json:"peer"`
	SMS
}

func newer(a, b SMS) bool {
	if a.Date != b.Date {
		return a.Date > b.Date
	}
	return a.ID > b.ID
}

// sameNumber compares phone numbers by their digits, so that "+49 151
// 2345678" and "+491512345678" match. Text such as a sender name compares
// as it is.
func sameNumber(a, b string) bool {
	digits := func(s string) string {
		return strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, s)
	}
	da, db := digits(a), digits(b)
	if len(da) < 5 || len(db) < 5 {
		return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
	}
	return da == db
}
