package openai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

type unreadStreamBody struct {
	first       []byte
	firstRead   chan struct{}
	release     chan struct{}
	closeOnce   sync.Once
	readStarted sync.Once
}

func (b *unreadStreamBody) Read(p []byte) (int, error) {
	if len(b.first) > 0 {
		n := copy(p, b.first)
		b.first = b.first[n:]
		b.readStarted.Do(func() { close(b.firstRead) })
		return n, nil
	}
	<-b.release
	return 0, io.ErrClosedPipe
}

func (b *unreadStreamBody) Close() error {
	b.closeOnce.Do(func() { close(b.release) })
	return nil
}

func TestStreamCancelWithUnreadConsumerReturnsRepeatedly(t *testing.T) {
	const iterations = 100
	streamData := []byte("data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
	c := &client{name: "test", idleTimeout: time.Hour}

	for i := 0; i < iterations; i++ {
		body := &unreadStreamBody{
			first:     append([]byte(nil), streamData...),
			firstRead: make(chan struct{}),
			release:   make(chan struct{}),
		}
		resp := &http.Response{Body: body}
		out := make(chan provider.Chunk)
		ctx, cancel := context.WithCancel(context.Background())
		finished := make(chan struct{})
		var reconnects int
		go func() {
			c.streamWithReconnect(ctx, resp, func(context.Context) (*http.Request, error) {
				reconnects++
				return nil, errors.New("reconnect must not run after cancellation")
			}, out)
			close(finished)
		}()

		select {
		case <-body.firstRead:
		case <-time.After(2 * time.Second):
			cancel()
			t.Fatalf("iteration %d: stream did not reach the unread output", i)
		}
		cancel()
		select {
		case <-finished:
		case <-time.After(2 * time.Second):
			t.Fatalf("iteration %d: cancelled unread stream did not return", i)
		}
		if reconnects != 0 {
			t.Fatalf("iteration %d: cancellation triggered %d reconnects", i, reconnects)
		}
	}
}
