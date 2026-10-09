package checksdk

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
)

// conn is the child's side of the pipe. One reader splits what the engine
// writes into requests and the answers to the child's own calls, so a
// check abandoned at its deadline can never take a request's line; one
// writer lock keeps a call and an answer from interleaving on stdout.
type conn struct {
	requests chan []byte
	answers  chan answer

	wmu sync.Mutex
	enc *json.Encoder

	// cmu holds one call in flight at a time.
	cmu    sync.Mutex
	nextID int

	emu     sync.Mutex
	scanErr error
}

type answer struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *string         `json:"error"`
}

type sdkCall struct {
	SDK  string `json:"sdk"`
	ID   int    `json:"id"`
	Args any    `json:"args"`
}

func newConn(in io.Reader, out io.Writer) *conn {
	c := &conn{requests: make(chan []byte), answers: make(chan answer), enc: json.NewEncoder(out)}
	c.enc.SetEscapeHTML(false)
	go c.read(in)
	return c
}

// read routes each line: one carrying an id and neither an op nor a
// proto is an answer to a call, anything else a request.
func (c *conn) read(in io.Reader) {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64<<10), MaxLine)
	for sc.Scan() {
		line := append([]byte{}, sc.Bytes()...)
		var probe struct {
			ID    *int    `json:"id"`
			Op    *string `json:"op"`
			Proto *string `json:"proto"`
		}
		if json.Unmarshal(line, &probe) == nil && probe.ID != nil && probe.Op == nil && probe.Proto == nil {
			var a answer
			if json.Unmarshal(line, &a) == nil {
				c.answers <- a
				continue
			}
		}
		c.requests <- line
	}
	c.emu.Lock()
	c.scanErr = sc.Err()
	c.emu.Unlock()
	close(c.requests)
	close(c.answers)
}

func (c *conn) err() error {
	c.emu.Lock()
	defer c.emu.Unlock()
	return c.scanErr
}

func (c *conn) send(v any) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_ = c.enc.Encode(v)
}

// errClosed is the engine closing the pipe while a call waited.
var errClosed = errors.New("checksdk: the engine closed the pipe before answering")

// call asks the engine and waits for its answer.
func (c *conn) call(method string, args any) (json.RawMessage, error) {
	c.cmu.Lock()
	defer c.cmu.Unlock()
	c.nextID++
	id := c.nextID
	if args == nil {
		args = struct{}{}
	}
	c.send(sdkCall{SDK: method, ID: id, Args: args})
	for a := range c.answers {
		if a.ID != id {
			continue
		}
		if a.Error != nil {
			return nil, fmt.Errorf("%s", *a.Error)
		}
		return a.Result, nil
	}
	return nil, errClosed
}

// pipeHandler answers a Repo's calls over the conn.
type pipeHandler struct{ c *conn }

func (p pipeHandler) handle(method string, args any) (json.RawMessage, error) {
	if p.c == nil {
		return nil, errClosed
	}
	return p.c.call(method, args)
}
