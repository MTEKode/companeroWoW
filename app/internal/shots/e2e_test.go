package shots_test

import (
	"bytes"
	"context"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/ai"
	"github.com/MTEKode/companeroWoW/app/internal/bridge"
	"github.com/MTEKode/companeroWoW/app/internal/gamemode"
	"github.com/MTEKode/companeroWoW/app/internal/shots"
)

// fakeAsker is the model: it records the questions and answers with a fixed block.
type fakeAsker struct {
	asked chan string
}

func (a *fakeAsker) Ask(_ context.Context, question string, _ bool) (ai.Response, error) {
	a.asked <- question
	return ai.Response{Text: "📍 /compa marca 51.2 38.7 Entregar\nFuente: wowforevertalents.com", Provider: "fake/model"}, nil
}

// fakeClip keeps the texts put on the clipboard.
type fakeClip struct {
	mu    sync.Mutex
	texts []string
}

func (c *fakeClip) SetText(text string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.texts = append(c.texts, text)
	return nil
}

func (c *fakeClip) all() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.texts...)
}

// fakeSound records the cues played.
type fakeSound struct {
	mu   sync.Mutex
	cues []gamemode.Cue
}

func (s *fakeSound) Play(cue gamemode.Cue) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cues = append(s.cues, cue)
	return nil
}

// questionPanel draws a screenshot whose panel carries one question (msgType 15), as the addon
// sends it: a data row, then the end row.
func questionPanel(t *testing.T, q string, shotID uint16) []byte {
	t.Helper()
	body := []byte(`{"id":42,"text":"` + q + `","at":1700000000,"mapID":85}`)
	n := 200
	status := bridge.LiveState{Level: 60, MapID: 85, ShareRemainingSec: 20}
	f, err := bridge.BuildFrameV5(status, 0, n, shotID, &bridge.ChunkV3{Type: bridge.MsgQuestion, ID: 1, Index: 0, Count: 1, Data: body})
	if err != nil {
		t.Fatal(err)
	}
	end, err := bridge.BuildEndFrameV5(status, 1, n, shotID, 1)
	if err != nil {
		t.Fatal(err)
	}
	img := bridge.RenderPanel([][]byte{f, end}, n, bridge.PanelOptions{BlockPx: 2, Width: 1024, Height: 640, Noise: 2, Seed: 5})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// A screenshot with a question in its panel goes through the watcher into the reader's channel,
// the question reaches the game-mode answerer, the answer goes to the clipboard and the sound says
// it is ready, and the screenshot is deleted.
func TestShotWithQuestionIsAnswered(t *testing.T) {
	dir := t.TempDir()
	shot := filepath.Join(dir, "WoWScrnShot_010101_120000.png")
	if err := os.WriteFile(shot, questionPanel(t, "¿Dónde entrego?", 77), 0o644); err != nil {
		t.Fatal(err)
	}

	reader := bridge.NewReader(time.Second)
	asker := &fakeAsker{asked: make(chan string, 4)}
	clip := &fakeClip{}
	sound := &fakeSound{}
	gate, err := gamemode.NewGate("")
	if err != nil {
		t.Fatal(err)
	}
	ctl := gamemode.NewController(asker, clip, sound, gate, func() string { return "Ana" }, nil)
	reader.Channel().OnMessage(func(typ uint8, body []byte, _ time.Time) {
		if typ != bridge.MsgQuestion {
			return
		}
		q, err := bridge.ParseQuestion(body)
		if err != nil {
			t.Errorf("question: %v", err)
			return
		}
		ctl.Submit(q)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := shots.New(shots.Options{
		Dir:   dir,
		Since: time.Now().Add(-time.Minute),
		Sink:  reader,
		Quiet: 20 * time.Millisecond, Tick: 5 * time.Millisecond, Rescan: 20 * time.Millisecond,
		Watch: func(ctx context.Context, _ string, _ chan<- string) error {
			<-ctx.Done()
			return nil
		},
	})
	go w.Run(ctx)

	select {
	case got := <-asker.asked:
		if got != "¿Dónde entrego?" {
			t.Fatalf("asked %q", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the question did not reach the answerer")
	}
	deadline := time.Now().Add(10 * time.Second)
	for len(clip.all()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("no answer reached the clipboard")
		}
		time.Sleep(5 * time.Millisecond)
	}
	ctl.Wait()
	text := clip.all()[0]
	if !strings.Contains(text, "[CWOW1 #42") || !strings.Contains(text, "@marca 85 51.2 38.7 Entregar") {
		t.Fatalf("clipboard = %q", text)
	}
	if len(sound.cues) == 0 {
		t.Fatal("the answer sound did not play")
	}
	if _, err := os.Stat(shot); !os.IsNotExist(err) {
		t.Fatalf("the shot is still there (%v)", err)
	}
	if st := w.Status(); st.Shots != 1 || st.Last.ShotID != 77 {
		t.Fatalf("status = %+v", st)
	}
}
