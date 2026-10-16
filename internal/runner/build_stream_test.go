package runner

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

const bsCap = 1 << 20

// Synthetic, obviously fake image ID.
var bsID = "sha256:" + strings.Repeat("ab", 32)

func okLines() string {
	return `{"stream":"Step 1/3 : FROM base\n"}` + "\n" + `{"stream":"Step 2/3 : RUN x\n"}` + "\n"
}

func mustFail(t *testing.T, in string) error {
	t.Helper()
	id, err := consumeBuildStream(strings.NewReader(in), bsCap)
	if err == nil {
		t.Fatalf("expected error, got nil (aux=%q)", id)
	}
	if !errors.Is(err, ErrImageBuildFailed) {
		t.Fatalf("error must wrap ErrImageBuildFailed, got %v", err)
	}
	return err
}

func TestBuildStream_ErrorDetailFails(t *testing.T) {
	mustFail(t, `{"errorDetail":{"message":"boom","code":1}}`+"\n")
}

func TestBuildStream_ErrorFieldOnlyFails(t *testing.T) {
	mustFail(t, `{"error":"boom"}`+"\n")
}

func TestBuildStream_DeprecatedErrorFieldFails(t *testing.T) {
	// ErrorMessage is the deprecated top-level "error" string, alone, without errorDetail.
	mustFail(t, okLines()+`{"error":"deprecated style failure"}`+"\n")
}

func TestBuildStream_ErrorAfterSuccessfulStepsFails(t *testing.T) {
	in := strings.Repeat(okLines(), 50) + `{"errorDetail":{"message":"late"},"error":"late"}` + "\n"
	mustFail(t, in)
}

func TestBuildStream_MalformedJSONFails(t *testing.T) {
	mustFail(t, okLines()+`{not json}`+"\n")
}

func TestBuildStream_TruncatedMidObjectFails(t *testing.T) {
	mustFail(t, okLines()+`{"stream":"half`)
}

func TestBuildStream_IncompleteJSONErrors(t *testing.T) {
	mustFail(t, `{"aux":{"ID":"`+bsID)
}

func TestBuildStream_OversizeFails(t *testing.T) {
	const cap = 64
	atCap := `{"stream":"` + strings.Repeat("a", cap-14) + `"}` + "\n"
	if len(atCap) != cap {
		t.Fatalf("fixture length %d != %d", len(atCap), cap)
	}
	if _, err := consumeBuildStream(strings.NewReader(atCap), cap); err != nil {
		t.Fatalf("stream exactly at cap must succeed: %v", err)
	}
	over := atCap + " "
	if _, err := consumeBuildStream(strings.NewReader(over), cap); !errors.Is(err, ErrImageBuildFailed) {
		t.Fatalf("cap+1 must fail closed, got %v", err)
	}
}

func TestBuildStream_BareNullMessageFails(t *testing.T) {
	// A JSON null decodes into a zero message without error; it is not a valid build message.
	mustFail(t, "null\n")
	mustFail(t, okLines()+"null\n")
	mustFail(t, "null\n"+okLines())
}

func TestBuildStream_EmptyStreamRejected(t *testing.T) {
	for _, in := range []string{"", "   \n\n"} {
		mustFail(t, in)
	}
}

func TestBuildStream_ValidLegacyAuxID(t *testing.T) {
	in := okLines() + `{"aux":{"ID":"` + bsID + `"}}` + "\n"
	id, err := consumeBuildStream(strings.NewReader(in), bsCap)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != bsID {
		t.Fatalf("aux id = %q, want %q", id, bsID)
	}
}

func TestBuildStream_ValidBuildKitTrace(t *testing.T) {
	in := `{"id":"moby.buildkit.trace","aux":"c29tZSB0cmFjZQ=="}` + "\n" +
		`{"id":"moby.buildkit.trace","aux":{"vertexes":[]}}` + "\n"
	id, err := consumeBuildStream(strings.NewReader(in), bsCap)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "" {
		t.Fatalf("aux id = %q, want empty", id)
	}
}

type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }

func TestBuildStream_ReaderErrorPropagates(t *testing.T) {
	r := io.MultiReader(strings.NewReader(okLines()), errReader{errors.New("conn reset")})
	_, err := consumeBuildStream(r, bsCap)
	if !errors.Is(err, ErrImageBuildFailed) {
		t.Fatalf("expected ErrImageBuildFailed, got %v", err)
	}
}

func TestBuildStream_ErrorTextSanitized(t *testing.T) {
	var logged []string
	defer SetBuildLogfForTest(func(format string, args ...any) {
		logged = append(logged, fmt.Sprintf(format, args...))
	})()

	evil := "\x1b[31mred\x1b[0m\x07bell\r\nnewline " + strings.Repeat("Z", 2000)
	q, _ := json.Marshal(evil)
	in := `{"errorDetail":{"message":` + string(q) + `}}` + "\n"
	err := mustFail(t, in)
	if err.Error() != ErrImageBuildFailed.Error() {
		t.Fatalf("error text must be fixed, got %q", err.Error())
	}
	if len(logged) == 0 {
		t.Fatal("expected detail to be logged")
	}
	all := strings.Join(logged, "\n")
	if !strings.Contains(all, "red") {
		t.Fatalf("log should contain sanitized detail: %q", all)
	}
	for _, r := range all {
		if r == 0x1b || r == 0x07 || r == '\r' {
			t.Fatalf("control characters leaked into log: %q", all)
		}
	}
	if strings.Contains(all, "[31m") {
		t.Fatalf("ANSI sequence leaked into log: %q", all)
	}
	if strings.Count(all, "Z") > 512 {
		t.Fatalf("logged detail exceeds 512 bytes")
	}
}

func TestSanitizeBuildDetail(t *testing.T) {
	got := sanitizeBuildDetail("\x1b[1;31mfail\x1b[0m\x00\x07a\tb\r\nc\x7f")
	if got != "fail  a b  c " {
		t.Fatalf("got %q", got)
	}
	if strings.ContainsAny(got, "\x1b\x00\x07\r\n\x7f") || strings.Contains(got, "[1;31m") {
		t.Fatalf("not sanitized: %q", got)
	}
	if !strings.HasPrefix(got, "fail") {
		t.Fatalf("content lost: %q", got)
	}
	long := sanitizeBuildDetail(strings.Repeat("é", 1000))
	if len(long) > 512 {
		t.Fatalf("len %d > 512", len(long))
	}
	if strings.ContainsRune(long, '�') {
		t.Fatalf("truncation split a rune: %q", long)
	}
}

// A build goroutine abandoned by the resolve watchdog can still log after its caller returned,
// while a later test swaps the sink. Swapping must be safe concurrently with logging.
func TestBuildLogSink_SwapConcurrentWithLogging(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 200 {
			_ = buildFail("detail")
		}
	}()
	for range 200 {
		restore := SetBuildLogfForTest(func(string, ...any) {})
		restore()
	}
	<-done
}
