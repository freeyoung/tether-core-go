package core

import (
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// fakeProvider answers MessagesRequested like Android's SMS provider: the
// messages from since on, the oldest first, batch at a time.
type fakeProvider struct {
	engine *Engine
	batch  int
	sms    []SMS
}

func (p *fakeProvider) answer(fp string, since int64) {
	var list []SMS
	for _, m := range p.sms {
		if m.Date >= since {
			list = append(list, m)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Date < list[j].Date })
	more := len(list) > p.batch
	if more {
		list = list[:p.batch]
	}
	p.engine.SendMessages(fp, list, more)
}

func sms(id, thread int64, date int64, body string) SMS {
	return SMS{ID: id, Thread: thread, Address: "+49 151 0000" + string(rune('0'+thread)), Date: date, Body: body}
}

func TestMessagesSyncInBatchesAndOnChange(t *testing.T) {
	mac := newNode(t, "Mac", func(c *Config) { c.KeepMessages = true })
	phone := newNode(t, "Android", func(c *Config) { c.ProvideMessages = true })
	provider := &fakeProvider{engine: phone.engine, batch: 3}
	for i := int64(1); i <= 10; i++ {
		provider.sms = append(provider.sms, sms(i, i%3, 1000*i, "message"))
	}
	// Two messages of the same millisecond: the cursor sends both again, and
	// the store keeps each once.
	provider.sms = append(provider.sms, sms(11, 1, 10000, "same time"))
	phone.delegate.onMessagesRequested = provider.answer
	for _, n := range []*testNode{mac, phone} {
		n.delegate.setAutoApprove(true)
	}
	pairComputers(t, phone, mac)
	store := mac.engine.Messages()
	if !eventually(5*time.Second, func() bool { return len(store.Thread(phone.engine.Identity.Fingerprint, 1)) == 5 }) {
		t.Fatal(store.Conversations(""))
	}
	if c := store.Conversations(phone.engine.Identity.Fingerprint); len(c) != 3 || c[0].Last.ID != 11 || c[0].Count != 5 {
		t.Fatalf("%+v", c)
	}

	provider.sms = append(provider.sms, sms(12, 2, 20000, "new"))
	phone.engine.MessagesChanged()
	if !eventually(5*time.Second, func() bool { return store.Conversations("")[0].Last.ID == 12 }) {
		t.Fatal("the new message did not arrive")
	}
	mac.delegate.mu.Lock()
	added := 0
	for _, list := range mac.delegate.added {
		added += len(list)
	}
	mac.delegate.mu.Unlock()
	if added != 12 {
		t.Fatal("added", added)
	}
}

func TestMessagesGoOnlyToComputersThatKeepThem(t *testing.T) {
	linux := newNode(t, "Linux")
	phone := newNode(t, "Android", func(c *Config) { c.ProvideMessages = true })
	asked := false
	phone.delegate.onMessagesRequested = func(string, int64) { asked = true }
	for _, n := range []*testNode{linux, phone} {
		n.delegate.setAutoApprove(true)
	}
	pairComputers(t, phone, linux)
	time.Sleep(300 * time.Millisecond)
	if asked || linux.engine.Messages() != nil {
		t.Fatal("messages without the features")
	}
}

func TestMessageStoreKeepsMessagesAcrossRuns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "messages.jsonl")
	st, err := OpenMessageStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if added, _ := st.Add("phone", []SMS{sms(1, 1, 100, "Your code is 482913"), sms(2, 2, 200, "Hi 😀")}); len(added) != 2 {
		t.Fatal(added)
	}
	if added, _ := st.Add("phone", []SMS{sms(2, 2, 200, "Hi 😀")}); len(added) != 0 {
		t.Fatal("added twice")
	}
	again, err := OpenMessageStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if again.Cursor("phone") != 200 || again.Cursor("other") != 0 || !again.Has("phone") || again.Has("other") {
		t.Fatal(again.Cursor("phone"))
	}
	if found := again.Search("phone", "CODE 4829", 10); len(found) != 1 || found[0].ID != 1 {
		t.Fatal(found)
	}
	if c, ok := again.Find("phone", "+4915100002"); !ok || c.Thread != 2 {
		t.Fatal(c, ok)
	}
	if found := again.Search("other", "code", 10); len(found) != 0 {
		t.Fatal(found)
	}
	if list := again.Thread("phone", 2); len(list) != 1 || list[0].Body != "Hi 😀" {
		t.Fatal(list)
	}
}

func TestFindCode(t *testing.T) {
	for text, want := range map[string]string{
		"Your verification code is 482913.":          "482913",
		"【银行】您的验证码为 6731，5分钟内有效。":                    "6731",
		"Ihr Bestätigungscode: 123-456":              "123456",
		"G-552301 is your Google verification code.": "552301",
		"Meeting at 1530 tomorrow":                   "",
		"Your code arrives soon":                     "",
		"Call +49 151 23456789 for the code":         "",
		"2026年 验证码 8812":                             "8812",
	} {
		if got := FindCode(text); got != want {
			t.Errorf("%q: %q, want %q", text, got, want)
		}
	}
}
