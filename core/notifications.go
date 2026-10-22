package core

import "unicode/utf8"

// NotificationsFeature in hello: the peer shows the notifications of Android
// phones. tetherd and the iPhone app do not announce it, so they never get
// notification_posted or notification_removed.
const NotificationsFeature = "notifications"

// Limits of a notification, in bytes. Longer text is cut, on both sides.
const (
	maxNotificationID   = 512
	maxNotificationName = 256
	maxNotificationText = 4096
)

// Notification is a notification on an Android phone.
type Notification struct {
	// The phone's key: a notification with the same ID replaces it.
	ID string
	// The package, such as com.whatsapp, and the name the user sees.
	App     string
	AppName string
	Title   string
	Text    string
	// When the phone posted it, in milliseconds since 1970.
	Time int64
	// The phone that sent it. The receiving engine sets these.
	PeerFingerprint string
	PeerName        string
}

func (n Notification) cut() Notification {
	n.App = cutUTF8(n.App, maxNotificationName)
	n.AppName = cutUTF8(n.AppName, maxNotificationName)
	n.Title = cutUTF8(n.Title, maxNotificationName)
	n.Text = cutUTF8(n.Text, maxNotificationText)
	return n
}

// cutUTF8 cuts s to at most n bytes, on a character boundary.
func cutUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func validNotificationID(id string) bool { return id != "" && len(id) <= maxNotificationID }

// PostNotification sends a notification of this phone to every connected
// peer that shows them. Computers do not forward them to each other.
func (e *Engine) PostNotification(n Notification) {
	if !validNotificationID(n.ID) {
		return
	}
	n = n.cut()
	m := Message{Command: "notification_posted", NotificationID: n.ID, App: n.App, AppName: n.AppName,
		Title: n.Title, Content: &n.Text, Time: &n.Time}
	e.queue.Post(func() { e.sendToNotificationPeers(m) })
}

// RemoveNotification tells the same peers that the notification is gone.
func (e *Engine) RemoveNotification(id string) {
	if !validNotificationID(id) {
		return
	}
	m := Message{Command: "notification_removed", NotificationID: id}
	e.queue.Post(func() { e.sendToNotificationPeers(m) })
}

func (e *Engine) sendToNotificationPeers(m Message) {
	for _, s := range e.sessionList() {
		if s.state == paired && !s.conn.Closed && s.peerFeatures[NotificationsFeature] {
			s.conn.Send(m, nil)
		}
	}
}

func (e *Engine) receivedNotification(m Message, s *session) {
	if !e.Config.ShowNotifications || !validNotificationID(m.NotificationID) {
		return
	}
	fp := s.fp()
	if m.Command == "notification_removed" {
		e.notify(func(d Delegate) { d.NotificationRemoved(fp, m.NotificationID) })
		return
	}
	n := Notification{ID: m.NotificationID, App: m.App, AppName: m.AppName, Title: m.Title, Text: m.content(),
		PeerFingerprint: fp, PeerName: s.name}
	if m.Time != nil {
		n.Time = *m.Time
	}
	n = n.cut()
	e.notify(func(d Delegate) { d.NotificationPosted(n) })
}
