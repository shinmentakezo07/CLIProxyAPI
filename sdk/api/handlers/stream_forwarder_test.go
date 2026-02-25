package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/interfaces"
)

func TestForwardStream_WritesChunksThenDone(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	handler := NewBaseAPIHandlers(nil, nil)
	data := make(chan []byte, 1)
	errs := make(chan *interfaces.ErrorMessage)
	close(errs)

	data <- []byte("alpha")
	close(data)

	cancelCalled := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.ForwardStream(c, recorder, func(err error) {
			cancelCalled <- err
		}, data, errs, StreamForwardOptions{
			KeepAliveInterval: durationPtr(0),
			WriteChunk: func(chunk []byte) {
				_, _ = c.Writer.Write([]byte("chunk:"))
				_, _ = c.Writer.Write(chunk)
				_, _ = c.Writer.Write([]byte(";"))
			},
			WriteDone: func() {
				_, _ = c.Writer.Write([]byte("done"))
			},
		})
	}()

	waitForwardStreamDone(t, done)

	if got := recorder.Body.String(); got != "chunk:alpha;done" {
		t.Fatalf("body = %q, want %q", got, "chunk:alpha;done")
	}
	if !recorder.Flushed {
		t.Fatalf("expected recorder to be flushed")
	}
	select {
	case err := <-cancelCalled:
		if err != nil {
			t.Fatalf("cancel err = %v, want nil", err)
		}
	default:
		t.Fatalf("cancel was not called")
	}
}

func TestForwardStream_PrefersPendingTerminalErrorWhenDataCloses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	handler := NewBaseAPIHandlers(nil, nil)
	data := make(chan []byte)
	close(data)
	errs := make(chan *interfaces.ErrorMessage, 1)
	expected := errors.New("upstream failed")
	errs <- &interfaces.ErrorMessage{StatusCode: http.StatusBadGateway, Error: expected}

	cancelCalled := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.ForwardStream(c, recorder, func(err error) {
			cancelCalled <- err
		}, data, errs, StreamForwardOptions{
			KeepAliveInterval: durationPtr(0),
			WriteDone: func() {
				_, _ = c.Writer.Write([]byte("done"))
			},
			WriteTerminalError: func(errMsg *interfaces.ErrorMessage) {
				if errMsg != nil && errMsg.Error != nil {
					_, _ = c.Writer.Write([]byte("err:" + errMsg.Error.Error()))
				}
			},
		})
	}()

	waitForwardStreamDone(t, done)

	if got := recorder.Body.String(); got != "err:upstream failed" {
		t.Fatalf("body = %q, want %q", got, "err:upstream failed")
	}
	select {
	case err := <-cancelCalled:
		if !errors.Is(err, expected) {
			t.Fatalf("cancel err = %v, want %v", err, expected)
		}
	default:
		t.Fatalf("cancel was not called")
	}
}

func TestForwardStream_TerminalErrorChannelWritesAndCancels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	handler := NewBaseAPIHandlers(nil, nil)
	data := make(chan []byte)
	errs := make(chan *interfaces.ErrorMessage, 1)
	expected := errors.New("stream error")
	errs <- &interfaces.ErrorMessage{StatusCode: http.StatusInternalServerError, Error: expected}
	close(errs)

	cancelCalled := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.ForwardStream(c, recorder, func(err error) {
			cancelCalled <- err
		}, data, errs, StreamForwardOptions{
			KeepAliveInterval: durationPtr(0),
			WriteTerminalError: func(errMsg *interfaces.ErrorMessage) {
				if errMsg != nil && errMsg.Error != nil {
					_, _ = c.Writer.Write([]byte("terminal:" + errMsg.Error.Error()))
				}
			},
		})
	}()

	waitForwardStreamDone(t, done)

	if got := recorder.Body.String(); got != "terminal:stream error" {
		t.Fatalf("body = %q, want %q", got, "terminal:stream error")
	}
	select {
	case err := <-cancelCalled:
		if !errors.Is(err, expected) {
			t.Fatalf("cancel err = %v, want %v", err, expected)
		}
	default:
		t.Fatalf("cancel was not called")
	}
}

func durationPtr(d time.Duration) *time.Duration { return &d }

func waitForwardStreamDone(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ForwardStream did not return")
	}
}
