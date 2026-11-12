package httpapi

import (
	"net/http/httptest"
	"testing"
)

// The API and Web clients parse these exact bytes, so pin them.
func TestSSEFramesKeepWireFormat(t *testing.T) {
	recorder := httptest.NewRecorder()
	if !startSSE(recorder) {
		t.Fatal("startSSE failed")
	}
	if recorder.Flushed {
		t.Fatal("startSSE flushed; callers flush it with their first events")
	}
	if !writeSSE(recorder, recorder, "ready", struct{}{}) ||
		!writeSSE(recorder, recorder, "log", map[string]uint64{"id": 42}) ||
		!writeSSEKeepalive(recorder, recorder) {
		t.Fatal("writing a frame failed")
	}
	want := "retry: 2000\n\n" +
		"event: ready\ndata: {}\n\n" +
		"event: log\ndata: {\"id\":42}\n\n" +
		": keep-alive\n\n"
	if got := recorder.Body.String(); got != want {
		t.Fatalf("stream = %q, want %q", got, want)
	}
	if !recorder.Flushed {
		t.Fatal("frames were not flushed")
	}
}

func TestWriteSSERejectsUnencodableValue(t *testing.T) {
	recorder := httptest.NewRecorder()
	if writeSSE(recorder, recorder, "log", make(chan int)) {
		t.Fatal("writeSSE accepted a value JSON cannot encode")
	}
	if recorder.Body.Len() != 0 || recorder.Flushed {
		t.Fatalf("wrote %q for a rejected value", recorder.Body.String())
	}
}
