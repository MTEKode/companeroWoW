package gamemode

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"

	"github.com/MTEKode/companeroWoW/app/internal/ai"
	"github.com/MTEKode/companeroWoW/app/internal/bridge"
)

// Asker answers a question. The assistant satisfies it; tests pass a fake.
type Asker interface {
	Ask(ctx context.Context, question string, withScreenshot bool) (ai.Response, error)
}

// Deduper is the part of Gate the controller uses.
type Deduper interface {
	Accept(key string, id uint64) (bool, error)
}

// errEmptyAnswer is the error of a provider that answered with no text.
var errEmptyAnswer = errors.New("respuesta vacía")

// Controller answers the questions of the game frame, one at a time. Each new question takes
// over: a question still being answered is cancelled, and its answer is never delivered (latest
// wins). A question with an id that was already answered is ignored. A finished answer goes to
// the clipboard as a CWOW1 block, and then the announcer plays its cue. A failed answer goes to
// the clipboard as an error block, with the error cue.
type Controller struct {
	asker    Asker
	clip     Clipboard
	sound    Sounder
	gate     Deduper
	key      func() string
	errText  func(error) string
	logf     func(format string, args ...any)
	wg       sync.WaitGroup
	mu       sync.Mutex // guards the fields below, and the clipboard write (see deliver)
	seq      uint64     // the number of accepted questions; the job that matches it is the current one
	cancel   context.CancelFunc
	currentQ uint64 // the id of the question being answered
}

// NewController returns a controller. key names the character whose question ids are kept; it is
// called for each question. errText says what the player reads for an error (the assistant's
// error in Spanish). A nil logf logs with the standard logger.
func NewController(asker Asker, clip Clipboard, sound Sounder, gate Deduper, key func() string, errText func(error) string) *Controller {
	return &Controller{
		asker:   asker,
		clip:    clip,
		sound:   sound,
		gate:    gate,
		key:     key,
		errText: errText,
		logf:    log.Printf,
	}
}

// Submit takes a question from the channel. It returns at once: the answer is made in the
// background. It reports whether the question was new (and is being answered).
func (c *Controller) Submit(q bridge.Question) bool {
	key := ""
	if c.key != nil {
		key = c.key()
	}
	fresh, err := c.gate.Accept(key, q.ID)
	if err != nil {
		c.logf("gamemode: no pude guardar el id de la pregunta #%d: %v", q.ID, err)
	}
	if !fresh {
		c.logf("gamemode: pregunta #%d ya respondida, la ignoro", q.ID)
		return false
	}

	ctx, cancel := context.WithCancel(context.Background())
	c.mu.Lock()
	if c.cancel != nil {
		c.cancel()
		c.logf("gamemode: pregunta #%d cancelada por la nueva #%d", c.currentQ, q.ID)
	}
	c.seq++
	mine := c.seq
	c.cancel = cancel
	c.currentQ = q.ID
	c.mu.Unlock()

	c.logf("gamemode: pregunta #%d recibida (%d caracteres)", q.ID, len([]rune(q.Text)))
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.run(ctx, mine, q)
	}()
	return true
}

// run answers one question and delivers the result if it is still the current one.
func (c *Controller) run(ctx context.Context, seq uint64, q bridge.Question) {
	defer c.finish(seq)
	resp, err := c.asker.Ask(ctx, q.Text, false)
	if err == nil && strings.TrimSpace(resp.Text) == "" {
		err = errEmptyAnswer
	}
	if ctx.Err() != nil {
		c.logf("gamemode: respuesta a la pregunta #%d descartada (llegó otra más nueva)", q.ID)
		return
	}

	var block string
	cue := CueReady
	if err != nil {
		block = FormatError(q.ID, c.errorText(err))
		cue = CueError
		c.logf("gamemode: pregunta #%d sin respuesta: %v", q.ID, err)
	} else {
		block = FormatAnswer(Answer{QuestionID: q.ID, Provider: resp.Provider, MapID: q.MapID, Text: resp.Text})
	}

	if derr := c.deliver(seq, block); errors.Is(derr, errStale) {
		c.logf("gamemode: respuesta a la pregunta #%d descartada (llegó otra más nueva)", q.ID)
		return
	} else if derr != nil {
		c.logf("gamemode: la respuesta #%d no llegó al portapapeles: %v", q.ID, derr)
		cue = CueError
	} else if err == nil {
		c.logf("gamemode: respuesta #%d copiada al portapapeles (%d caracteres)", q.ID, len([]rune(block)))
	}
	if c.sound != nil {
		if perr := c.sound.Play(cue); perr != nil {
			c.logf("gamemode: sonido de la respuesta #%d: %v", q.ID, perr)
		}
	}
}

// errorText is the text of an error block: the configured message, or a generic one.
func (c *Controller) errorText(err error) string {
	if c.errText != nil {
		if s := strings.TrimSpace(c.errText(err)); s != "" {
			return s
		}
	}
	return "No pude responder. Revisa companero.log."
}

// errStale is returned by deliver for an answer that a newer question has replaced.
var errStale = errors.New("respuesta reemplazada")

// deliver puts block on the clipboard when job seq is still the current one. The check and the
// write happen under the same lock, so an older answer cannot overwrite a newer one. It returns
// errStale for a replaced answer, and the clipboard's error when the copy failed.
func (c *Controller) deliver(seq uint64, block string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if seq != c.seq {
		return errStale
	}
	if c.clip == nil {
		return errors.New("no hay portapapeles configurado")
	}
	return c.clip.SetText(block)
}

// finish releases the cancel func of job seq when it is still the current one.
func (c *Controller) finish(seq uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if seq == c.seq && c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
}

// Wait blocks until every question that was accepted has been answered or cancelled. It is for
// tests and for the orderly shutdown.
func (c *Controller) Wait() {
	c.wg.Wait()
}
