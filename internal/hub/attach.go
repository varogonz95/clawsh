package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/term"

	"github.com/varogonz95/clawsh/internal/api"
)

// DetachKey is Ctrl-] (0x1d), same as telnet.
const DetachKey = 0x1d

// writeTimeout bounds each websocket write, so a dead connection (host gone
// without a RST) ends the attach instead of hanging it. A var for tests.
var writeTimeout = 10 * time.Second

// Attach takes over the local terminal and wires it to a remote session until
// the user presses DetachKey, the session exits, or the connection drops.
func Attach(ctx context.Context, c *Client, id string) error {
	return attach(ctx, c, id, os.Stdin, os.Stdout)
}

// AttachExit is Attach that also reports the session's exit code. The code is
// nil when the user detached or the connection ended without one.
func AttachExit(ctx context.Context, c *Client, id string) (*int, error) {
	return attachExit(ctx, c, id, os.Stdin, os.Stdout)
}

func attach(ctx context.Context, c *Client, id string, stdin, stdout *os.File) error {
	_, err := attachExit(ctx, c, id, stdin, stdout)
	return err
}

func attachExit(ctx context.Context, c *Client, id string, stdin, stdout *os.File) (*int, error) {
	inFd, outFd := int(stdin.Fd()), int(stdout.Fd())
	if !term.IsTerminal(inFd) || !term.IsTerminal(outFd) {
		return nil, errors.New("attach needs an interactive terminal")
	}
	in, err := newCancelReader(stdin)
	if err != nil {
		return nil, err
	}
	defer in.Close()

	conn, err := c.Dial(ctx, id)
	if err != nil {
		return nil, err
	}
	restoreConsole := prepareConsole(stdout)
	oldState, err := term.MakeRaw(inFd)
	if err != nil {
		restoreConsole()
		conn.Close()
		return nil, err
	}

	// teardown leaves nothing running once Attach returns, so a caller that
	// keeps going (the TUI) gets the terminal back clean: no reader left on
	// stdin, no late output. Closing the connection unblocks any goroutine
	// stuck reading or writing it. The terminal is restored only after the
	// stdin reader is gone (on Windows, cooked mode would let ReadConsole
	// block until Enter), and before any status line is printed.
	var wg sync.WaitGroup
	stop := make(chan struct{})
	var once sync.Once
	teardown := func() {
		once.Do(func() {
			close(stop)
			in.Cancel()
			conn.Close()
			wg.Wait()
			term.Restore(inFd, oldState)
			restoreConsole()
		})
	}
	defer teardown()

	var wmu sync.Mutex // gorilla/websocket: one writer at a time
	send := func(mt int, b []byte) error {
		wmu.Lock()
		defer wmu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
		return conn.WriteMessage(mt, b)
	}
	sendSize := func(cols, rows int) {
		msg, _ := json.Marshal(api.Control{Type: api.ControlResize, Cols: cols, Rows: rows})
		_ = send(websocket.TextMessage, msg)
	}

	cols, rows, _ := term.GetSize(outFd)
	sendSize(cols, rows)

	// Poll for resizes: portable (Windows has no SIGWINCH) and cheap.
	wg.Add(1)
	go func() {
		defer wg.Done()
		t := time.NewTicker(250 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				if cc, rr, err := term.GetSize(outFd); err == nil && (cc != cols || rr != rows) {
					cols, rows = cc, rr
					sendSize(cols, rows)
				}
			}
		}
	}()

	var detached bool
	var inputErr error
	inputDone := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(inputDone)
		detached, inputErr = pumpInput(in, func(b []byte) error { return send(websocket.BinaryMessage, b) })
	}()

	type result struct {
		exitCode *int
		err      error
	}
	done := make(chan result, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			mt, data, err := conn.ReadMessage()
			if err != nil {
				done <- result{err: err}
				return
			}
			switch mt {
			case websocket.BinaryMessage:
				_, _ = stdout.Write(data)
			case websocket.TextMessage:
				var ctl api.Control
				if json.Unmarshal(data, &ctl) == nil && ctl.Type == api.ControlExit {
					done <- result{exitCode: ctl.Code}
					return
				}
			}
		}
	}()

	var res result
	select {
	case <-inputDone:
		if !detached {
			teardown()
			return nil, inputErr
		}
		// WriteControl is safe alongside a writer holding wmu, and has its own
		// deadline, so a stuck connection can't hang the detach.
		_ = conn.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, "detach"),
			time.Now().Add(time.Second))
		teardown()
		fmt.Fprintf(stdout, "\r\n[detached from %s/%s]\r\n", c.Host.Name, id)
		return nil, nil
	case res = <-done:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	teardown()
	if res.exitCode != nil {
		fmt.Fprintf(stdout, "\r\n[session %s/%s exited with code %d]\r\n", c.Host.Name, id, *res.exitCode)
		return res.exitCode, nil
	}
	if websocket.IsCloseError(res.err, websocket.CloseNormalClosure) {
		return nil, nil
	}
	return nil, fmt.Errorf("connection lost: %w", res.err)
}

// pumpInput forwards r to send until the user presses DetachKey (detached is
// true), r fails, or send fails. Input before DetachKey in the same read is
// still sent; anything after it is dropped. send must not keep the slice.
func pumpInput(r io.Reader, send func([]byte) error) (detached bool, err error) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			if i := bytes.IndexByte(chunk, DetachKey); i >= 0 {
				if i > 0 {
					_ = send(chunk[:i])
				}
				return true, nil
			}
			if err := send(chunk); err != nil {
				return false, fmt.Errorf("connection lost: %w", err)
			}
		}
		if err != nil {
			return false, fmt.Errorf("read input: %w", err)
		}
	}
}
