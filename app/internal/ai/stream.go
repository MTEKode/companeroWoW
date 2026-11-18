package ai

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strings"
)

// Stream receives an answer while a provider writes it. The zero value streams nothing.
type Stream struct {
	// Delta gets each piece of the answer text, in order. It may be nil.
	Delta func(text string)
	// Restart says that the text delta so far is void: a provider or a round failed after it
	// wrote part of the answer, or the model moved on to a new step, and the next deltas start
	// the answer again. It may be nil.
	Restart func()
}

// on reports whether the caller wants deltas.
func (s Stream) on() bool { return s.Delta != nil }

func (s Stream) delta(text string) {
	if s.Delta != nil && text != "" {
		s.Delta(text)
	}
}

func (s Stream) restart() {
	if s.Restart != nil {
		s.Restart()
	}
}

// streamer is implemented by the providers that can write the answer as it comes. The
// others are asked with Ask, and their answer is shown whole.
type streamer interface {
	AskStream(ctx context.Context, req Request, s Stream) (Response, error)
}

// errStreamUnsupported means the endpoint refused the streaming request before it wrote
// anything; the caller asks again without streaming.
var errStreamUnsupported = errors.New("streaming no soportado por el endpoint")

// readSSE reads a text/event-stream body and calls fn with the data of each event. The data
// lines of one event are joined with newlines; comments and the other fields (event, id,
// retry) are skipped. fn returns errSSEStop to end the read without an error.
func readSSE(r io.Reader, fn func(data string) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	var data []string
	flush := func() error {
		if len(data) == 0 {
			return nil
		}
		ev := strings.Join(data, "\n")
		data = data[:0]
		return fn(ev)
	}
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		switch {
		case line == "":
			if err := flush(); err != nil {
				return finishSSE(err)
			}
		case strings.HasPrefix(line, ":"):
			// comment (keep-alive)
		case strings.HasPrefix(line, "data:"):
			v := strings.TrimPrefix(line, "data:")
			data = append(data, strings.TrimPrefix(v, " "))
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return finishSSE(flush())
}

// errSSEStop ends readSSE early, without an error.
var errSSEStop = errors.New("fin del flujo SSE")

func finishSSE(err error) error {
	if errors.Is(err, errSSEStop) {
		return nil
	}
	return err
}
