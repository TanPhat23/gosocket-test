package runner

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"strings"
	"sync/atomic"
	"unicode"
	"unicode/utf8"
)

// ErrImageBuildFailed is the fixed-text error for any failed or unverifiable image build.
// Detail from the Docker engine is logged (sanitized and truncated), never returned.
var ErrImageBuildFailed = errors.New("runtime image build failed")

const (
	// maxBuildStreamBytes caps how much build output the builder will read.
	maxBuildStreamBytes int64 = 32 << 20
	// maxBuildDetailBytes caps the engine detail written to the log.
	maxBuildDetailBytes = 512
)

type logfFunc = func(format string, args ...any)

// buildLogSink holds the log sink for sanitized build detail. Tests swap it while build goroutines
// abandoned by the resolve watchdog may still be logging, so access is atomic.
var buildLogSink atomic.Pointer[logfFunc]

func init() {
	f := logfFunc(log.Printf)
	buildLogSink.Store(&f)
}

func buildLogf(format string, args ...any) { (*buildLogSink.Load())(format, args...) }

// setBuildLogf installs f as the build log sink and returns the previous sink.
func setBuildLogf(f logfFunc) logfFunc { return *buildLogSink.Swap(&f) }

// buildMessage is the subset of the Docker build stream we interpret. Everything else
// (stream, status, progress, BuildKit trace payloads) is ignored.
type buildMessage struct {
	ErrorMessage string          `json:"error"`
	ErrorDetail  *buildErrDetail `json:"errorDetail"`
	Aux          json.RawMessage `json:"aux"`
}

type buildErrDetail struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
}

// consumeBuildStream reads the JSON message stream returned by ImageBuild and fails closed
// on any engine error, malformed or truncated JSON, reader error, an empty stream, or output
// beyond maxBytes. It returns the legacy aux image ID when one was emitted; BuildKit streams
// carry trace events instead, so an empty ID with a nil error is valid.
func consumeBuildStream(r io.Reader, maxBytes int64) (string, error) {
	lr := &io.LimitedReader{R: r, N: maxBytes + 1}
	dec := json.NewDecoder(lr)
	var (
		auxID string
		count int
	)
	for {
		// A pointer target lets a bare JSON null (which leaves it nil) be told apart from an object.
		var msg *buildMessage
		err := dec.Decode(&msg)
		if lr.N <= 0 {
			return "", buildFail("build output exceeded size cap")
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", buildFail("build output unreadable: " + err.Error())
		}
		if msg == nil {
			return "", buildFail("build output contained a null message")
		}
		count++
		if msg.ErrorDetail != nil {
			return "", buildFail("engine error: " + msg.ErrorDetail.Message + " " + msg.ErrorMessage)
		}
		if msg.ErrorMessage != "" {
			return "", buildFail("engine error: " + msg.ErrorMessage)
		}
		if id := parseAuxID(msg.Aux); id != "" {
			auxID = id
		}
	}
	if count == 0 {
		return "", buildFail("build output empty")
	}
	return auxID, nil
}

// parseAuxID returns the image ID from a legacy {"ID":"sha256:..."} aux payload. Other aux
// shapes (BuildKit trace strings or objects) are tolerated and ignored.
func parseAuxID(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var a struct {
		ID string `json:"ID"`
	}
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	return a.ID
}

// buildFail logs sanitized detail and returns the fixed error.
func buildFail(detail string) error {
	buildLogf("runtime image build failed: %s", sanitizeBuildDetail(detail))
	return ErrImageBuildFailed
}

// sanitizeBuildDetail strips ANSI escape sequences and control characters (replaced by a
// space) and truncates to maxBuildDetailBytes on a rune boundary.
func sanitizeBuildDetail(s string) string {
	s = stripANSI(s)
	var b strings.Builder
	for _, r := range s {
		if r == utf8.RuneError || unicode.IsControl(r) {
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(r)
	}
	out := b.String()
	if len(out) > maxBuildDetailBytes {
		cut := maxBuildDetailBytes
		for cut > 0 && !utf8.RuneStart(out[cut]) {
			cut--
		}
		out = out[:cut]
	}
	return out
}

// stripANSI removes ESC-introduced CSI sequences (ESC [ ... final byte) and bare ESC pairs.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != 0x1b {
			b.WriteByte(s[i])
			continue
		}
		i++
		if i < len(s) && s[i] == '[' {
			for i++; i < len(s) && !(s[i] >= 0x40 && s[i] <= 0x7e); i++ {
			}
		}
	}
	return b.String()
}
