package core

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
)

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func TestEncodeIsFlatAndKeepsZeroSizeAndText(t *testing.T) {
	got := string(Encode(FileStart("a.txt", 0, "T1", "")))
	want := `{"command":"file_start","filename":"a.txt","size":0,"transfer_id":"T1"}` + "\n"
	if got != want {
		t.Fatalf("got %s", got)
	}
	if s := string(Encode(ClipboardSet("引吭 <&> 高歌"))); !strings.Contains(s, "引吭 <&> 高歌") {
		t.Fatalf("text escaped: %s", s)
	}
	if s := string(Encode(TextMessage("clipboard_content", ""))); !strings.Contains(s, `"content":""`) {
		t.Fatalf("empty content dropped: %s", s)
	}
}

func TestLineBufferDropsOKAndBrokenLines(t *testing.T) {
	var b LineBuffer
	b.Append([]byte("OK\n{\"command\":\"hello\",\"features\":[\"clipboard_image\"]}\nnot json\n{\"content\":\"x\"}\n{\"comm"))
	got := b.TakeMessages()
	if len(got) != 1 || got[0].Command != "hello" {
		t.Fatalf("got %+v", got)
	}
	b.Append([]byte("and\":\"clipboard_get\"}\n"))
	if got := b.TakeMessages(); len(got) != 1 || got[0].Command != "clipboard_get" || b.Len() != 0 {
		t.Fatalf("got %+v, %d left", got, b.Len())
	}
}

func TestSanitize(t *testing.T) {
	cases := map[string]string{
		"../../etc/passwd":      "passwd",
		`..\..\Windows\win.ini`: "win.ini",
		`C:\Users\x\photo.jpg`:  "photo.jpg",
		".bashrc":               "bashrc",
		"..":                    "received-file",
		"   ":                   "received-file",
		`a<b>c:d"e|f?g*h.txt`:   "abcdefgh.txt",
		"CON":                   "_CON",
		"nul.txt":               "_nul.txt",
		"report.pdf. . ":        "report.pdf",
		"a\x00b\nc.txt":         "abc.txt",
	}
	for in, want := range cases {
		if got := Sanitize(in); got != want {
			t.Errorf("Sanitize(%q) = %q, want %q", in, got, want)
		}
	}
	long := Sanitize(strings.Repeat("é", 300) + ".pdf")
	if len(long) > 200 || !strings.HasSuffix(long, ".pdf") || !strings.HasPrefix(long, "é") {
		t.Errorf("long name: %d bytes", len(long))
	}
}

func TestUnusedPathCountsUp(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), nil, 0o644)
	os.WriteFile(filepath.Join(dir, "a (1).txt"), nil, 0o644)
	if got := filepath.Base(UnusedPath(dir, "a.txt")); got != "a (2).txt" {
		t.Fatal(got)
	}
}

func TestDigestAndLimits(t *testing.T) {
	if TextContent("hello\n").Digest() != TextContent("hello").Digest() {
		t.Error("trailing whitespace changed the digest")
	}
	if TextContent(" hello").Digest() == TextContent("hello").Digest() {
		t.Error("leading whitespace ignored")
	}
	if TextContent("x").Digest() == PNGContent([]byte("x")).Digest() {
		t.Error("text and image collide")
	}
	if !TextContent(strings.Repeat("a", MaxTextBytes)).Fits() || TextContent(strings.Repeat("a", MaxTextBytes+1)).Fits() {
		t.Error("text limit")
	}
	if PNGContent(make([]byte, MaxImageBytes+1)).Fits() {
		t.Error("image limit")
	}
}

func TestIdentityIsCreatedOnceAndPrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "id")
	first, err := LoadOrCreateIdentity(dir, FilePermissionsOnly{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadOrCreateIdentity(dir, FilePermissionsOnly{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Fingerprint != second.Fingerprint || len(first.Fingerprint) != 64 || strings.ToLower(first.Fingerprint) != first.Fingerprint {
		t.Fatalf("fingerprints %s %s", first.Fingerprint, second.Fingerprint)
	}
	if cn := first.Certificate.Leaf.Subject.CommonName; cn != "Tether" {
		t.Fatal(cn)
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(filepath.Join(dir, "identity.pfx"))
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("mode %v", info.Mode())
		}
	}
	if names := listDir(t, dir); len(names) != 1 {
		t.Fatalf("left behind: %v", names)
	}
}

func TestFormatFingerprintMatchesThePhone(t *testing.T) {
	fp := "c520fd11138d23a7f98fa828fd4a7b5851c4325cfa1bf9517a85abd60d3ce729"
	text := FormatFingerprint(fp, 8)
	lines := strings.Split(text, "\n")
	if len(lines) != 4 || lines[0] != "c5:20:fd:11:13:8d:23:a7:" {
		t.Fatalf("%q", text)
	}
	var pairs []string
	for i := 0; i < 64; i += 2 {
		pairs = append(pairs, fp[i:i+2])
	}
	if strings.ReplaceAll(text, "\n", "") != strings.Join(pairs, ":") {
		t.Fatal("not the phone's format")
	}
}

func TestChooseWindows(t *testing.T) {
	const html, png, rtf, hint = 49300, 49400, 49500, 49600
	f := func(fs ...WindowsFormat) Choice { return ChooseWindows(fs) }
	cases := []struct {
		got, want Choice
		what      string
	}{
		{f(WindowsFormat{13, ""}, WindowsFormat{1, ""}), ChooseText, "text"},
		{f(WindowsFormat{png, "PNG"}, WindowsFormat{8, ""}), ChooseImage, "png"},
		{f(WindowsFormat{html, "HTML Format"}), ChooseSkip, "html only"},
		{f(), ChooseSkip, "empty"},
		{f(WindowsFormat{html, "HTML Format"}, WindowsFormat{png, "PNG"}, WindowsFormat{8, ""}, WindowsFormat{13, ""}), ChooseImage, "browser copy image"},
		{f(WindowsFormat{rtf, "Rich Text Format"}, WindowsFormat{13, ""}, WindowsFormat{png, "PNG"}), ChooseText, "word"},
		{f(WindowsFormat{15, ""}, WindowsFormat{13, ""}), ChooseSkip, "explorer files"},
		{f(WindowsFormat{13, ""}, WindowsFormat{hint, "ExcludeClipboardContentFromMonitorProcessing"}), ChooseSkip, "password manager"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: got %v, want %v", c.what, c.got, c.want)
		}
	}
}

func TestInstanceName(t *testing.T) {
	if got := instanceName(`Alex\226\128\153s MacBook._tether._tcp.local.`); got != "Alex’s MacBook" {
		t.Fatal(got)
	}
	if got := instanceName(`a\.b._tether._tcp.local.`); got != "a.b" {
		t.Fatal(got)
	}
}

func decodeB64(s string) ([]byte, error) { return base64.StdEncoding.DecodeString(s) }

func TestChooseMac(t *testing.T) {
	for _, c := range []struct {
		types []string
		want  Choice
	}{
		{[]string{"public.utf8-plain-text"}, ChooseText},
		{[]string{"public.png"}, ChooseImage},
		{[]string{"public.tiff"}, ChooseImage},
		{nil, ChooseSkip},
		{[]string{"com.adobe.pdf"}, ChooseSkip},
		// An editor: rich text, plain text, then a picture of it.
		{[]string{"public.rtf", "public.utf8-plain-text", "public.png"}, ChooseText},
		// A browser's "Copy Image": the image, then its address.
		{[]string{"public.tiff", "public.url", "public.utf8-plain-text"}, ChooseImage},
		// Finder file copies and secrets.
		{[]string{"public.file-url", "public.utf8-plain-text", "com.apple.icns"}, ChooseSkip},
		{[]string{"org.nspasteboard.ConcealedType", "public.utf8-plain-text"}, ChooseSkip},
		{[]string{"public.utf8-plain-text", "org.nspasteboard.TransientType"}, ChooseSkip},
	} {
		if got := ChooseMac(c.types); got != c.want {
			t.Errorf("%v: %v, want %v", c.types, got, c.want)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"0.2.0", "0.2.0", 0},
		{"v0.2.0", "0.2.0", 0},
		{"0.2.0", "0.10.0", -1},
		{"1.0.0", "0.9.9", 1},
		{"0.3.0-rc.1", "0.3.0", -1},
		{"0.3.0-rc.2", "0.3.0-rc.1", 1},
		{"0.0.0-dev", "0.0.0", -1},
	} {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestEngineVersionOf(t *testing.T) {
	dep := func(version string, replace *debug.Module) *debug.BuildInfo {
		return &debug.BuildInfo{
			Main: debug.Module{Path: "tetherandroid"},
			Deps: []*debug.Module{{Path: "golang.org/x/net", Version: "v0.61.0"}, {Path: modulePath, Version: version, Replace: replace}},
		}
	}
	for name, c := range map[string]struct {
		bi   *debug.BuildInfo
		want string
	}{
		"a release":           {dep("v0.5.0", nil), "0.5.0"},
		"a pseudo-version":    {dep("v0.5.1-0.20261018090000-abcdef123456", nil), "0.5.1-0.20261018090000-abcdef123456"},
		"a local copy":        {dep("v0.5.0", &debug.Module{Path: "../tether-core-go"}), devVersion},
		"another version":     {dep("v0.5.0", &debug.Module{Path: modulePath, Version: "v0.5.2"}), "0.5.2"},
		"the module itself":   {&debug.BuildInfo{Main: debug.Module{Path: modulePath, Version: "(devel)"}}, devVersion},
		"a tool at a version": {&debug.BuildInfo{Main: debug.Module{Path: modulePath, Version: "v0.5.0"}}, "0.5.0"},
		"without this module": {&debug.BuildInfo{Main: debug.Module{Path: "other"}}, devVersion},
	} {
		if got := engineVersionOf(c.bi); got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}
	// A pseudo-version comes before the release it leads to.
	if CompareVersions("0.5.1-0.20261018090000-abcdef123456", "0.5.1") >= 0 {
		t.Error("a pseudo-version sorts after its release")
	}
}
