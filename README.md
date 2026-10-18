# tether-core-go

The engine of [Tether Desktop](https://github.com/freeyoung/tether-desktop) and Tether for Android: the computer side of the [Tether](https://github.com/zackb/tether) protocol, in Go. It does TLS, pairing, sessions, clipboard sync and files, and has no platform code, so its tests run on any OS.

The engine started in Tether Desktop. It is a module of its own now that more than one app uses it.

## Use

```go
import "github.com/freeyoung/tether-core-go/core"

engine, err := core.NewEngine(core.Config{SupportDir: dir, DownloadsDir: downloads}, clipboard, discovery)
engine.Delegate = app
engine.Start()
```

The app gives the engine three things, all in `core/ports.go`:

- `Clipboard`: the system clipboard.
- `Discovery`: DNS-SD, to publish `_tether._tcp` and find the other devices. `Browser` in `core/mdns.go` finds them on any OS.
- `Delegate`: the user interface, for pairing prompts, files and links.

The app sets its version and the oldest peer version it syncs with at build time:

```bash
go build -ldflags "-X github.com/freeyoung/tether-core-go/core.Version=0.4.0 -X github.com/freeyoung/tether-core-go/core.MinPeerVersion=0.0.0"
```

[docs/protocol.md](docs/protocol.md) lists the rules that the engine follows, so that several computers sync without loops.

## Tools

- `cmd/tether-headless`: the engine without a user interface, with commands on stdin and events as JSON lines on stdout. Use it to debug the engine, or as one more computer on a test machine.
- `cmd/tether-browse`: lists the Tether services on the network.

## Test

```bash
go test -race ./...
```

The tests run several engines on one machine: a fake iPhone, two computers and a ring of three. `scripts/tetherd-container.sh` runs a real `tetherd` in Docker for `core/tetherd_test.go`.

## License

MIT, see [LICENSE](LICENSE).
