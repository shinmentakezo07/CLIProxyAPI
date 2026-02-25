package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/interfaces"
)

type StreamForwardOptions struct {
	// KeepAliveInterval overrides the configured streaming keep-alive interval.
	// If nil, the configured default is used. If set to <= 0, keep-alives are disabled.
	KeepAliveInterval *time.Duration

	// WriteChunk writes a single data chunk to the response body. It should not flush.
	WriteChunk func(chunk []byte)

	// WriteTerminalError writes an error payload to the response body when streaming fails
	// after headers have already been committed. It should not flush.
	WriteTerminalError func(errMsg *interfaces.ErrorMessage)

	// WriteDone optionally writes a terminal marker when the upstream data channel closes
	// without an error (e.g. OpenAI's `[DONE]`). It should not flush.
	WriteDone func()

	// WriteKeepAlive optionally writes a keep-alive heartbeat. It should not flush.
	// When nil, a standard SSE comment heartbeat is used.
	WriteKeepAlive func()
}

type resolvedStreamForwardOptions struct {
	keepAliveInterval   time.Duration
	writeChunk          func([]byte)
	writeTerminalError  func(*interfaces.ErrorMessage)
	writeDone           func()
	writeKeepAlive      func()
}

func (h *BaseAPIHandler) ForwardStream(c *gin.Context, flusher http.Flusher, cancel func(error), data <-chan []byte, errs <-chan *interfaces.ErrorMessage, opts StreamForwardOptions) {
	if c == nil {
		return
	}
	if cancel == nil {
		return
	}

	resolved := h.resolveStreamForwardOptions(c, opts)
	keepAlive, keepAliveC := startStreamKeepAliveTicker(resolved.keepAliveInterval)
	if keepAlive != nil {
		defer keepAlive.Stop()
	}

	errCh := errs
	var terminalErr *interfaces.ErrorMessage
	for {
		select {
		case <-c.Request.Context().Done():
			cancel(c.Request.Context().Err())
			return
		case chunk, ok := <-data:
			if !ok {
				// Prefer surfacing a terminal error if one is pending.
				if terminalErr == nil {
					terminalErr = drainPendingStreamTerminalError(errCh)
				}
				finishForwardStream(flusher, cancel, terminalErr, resolved)
				return
			}
			resolved.writeChunk(chunk)
			flusher.Flush()
		case errMsg, ok := <-errCh:
			if !ok {
				// Disable the closed case to avoid a hot select loop while data is still streaming.
				errCh = nil
				continue
			}
			if errMsg != nil {
				terminalErr = errMsg
				if resolved.writeTerminalError != nil {
					resolved.writeTerminalError(errMsg)
					flusher.Flush()
				}
			}
			var execErr error
			if errMsg != nil {
				execErr = errMsg.Error
			}
			cancel(execErr)
			return
		case <-keepAliveC:
			resolved.writeKeepAlive()
			flusher.Flush()
		}
	}
}

func (h *BaseAPIHandler) resolveStreamForwardOptions(c *gin.Context, opts StreamForwardOptions) resolvedStreamForwardOptions {
	resolved := resolvedStreamForwardOptions{
		writeChunk:         opts.WriteChunk,
		writeTerminalError: opts.WriteTerminalError,
		writeDone:          opts.WriteDone,
		writeKeepAlive:     opts.WriteKeepAlive,
		keepAliveInterval:  StreamingKeepAliveInterval(h.Cfg),
	}
	if resolved.writeChunk == nil {
		resolved.writeChunk = func([]byte) {}
	}
	if resolved.writeKeepAlive == nil {
		resolved.writeKeepAlive = func() {
			_, _ = c.Writer.Write([]byte(": keep-alive\n\n"))
		}
	}
	if opts.KeepAliveInterval != nil {
		resolved.keepAliveInterval = *opts.KeepAliveInterval
	}
	return resolved
}

func startStreamKeepAliveTicker(interval time.Duration) (*time.Ticker, <-chan time.Time) {
	if interval <= 0 {
		return nil, nil
	}
	ticker := time.NewTicker(interval)
	return ticker, ticker.C
}

func drainPendingStreamTerminalError(errs <-chan *interfaces.ErrorMessage) *interfaces.ErrorMessage {
	if errs == nil {
		return nil
	}
	select {
	case errMsg, ok := <-errs:
		if ok && errMsg != nil {
			return errMsg
		}
	default:
	}
	return nil
}

func finishForwardStream(flusher http.Flusher, cancel func(error), terminalErr *interfaces.ErrorMessage, opts resolvedStreamForwardOptions) {
	if terminalErr != nil {
		if opts.writeTerminalError != nil {
			opts.writeTerminalError(terminalErr)
		}
		flusher.Flush()
		cancel(terminalErr.Error)
		return
	}
	if opts.writeDone != nil {
		opts.writeDone()
	}
	flusher.Flush()
	cancel(nil)
}
