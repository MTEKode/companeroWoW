package shots

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io/fs"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
)

// memFile is one file of memFS.
type memFile struct {
	data    []byte
	modTime time.Time
	locked  bool // Probe fails while the writer holds the file
}

// memFS is an in-memory FS: a Screenshots folder that tests can fill, lock and inspect.
type memFS struct {
	mu        sync.Mutex
	files     map[string]*memFile
	removeErr error
	removed   []string
}

func newMemFS() *memFS { return &memFS{files: map[string]*memFile{}} }

func (m *memFS) put(path string, data []byte, modTime time.Time) *memFile {
	m.mu.Lock()
	defer m.mu.Unlock()
	f := &memFile{data: data, modTime: modTime}
	m.files[path] = f
	return f
}

func (m *memFS) has(path string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.files[path]
	return ok
}

func (m *memFS) Stat(path string) (Info, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.files[path]
	if !ok {
		return Info{}, fs.ErrNotExist
	}
	return Info{Path: path, Name: baseName(path), Size: int64(len(f.data)), ModTime: f.modTime}, nil
}

func (m *memFS) List(dir string) ([]Info, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Info
	for p, f := range m.files {
		if dirOf(p) == dir {
			out = append(out, Info{Path: p, Name: baseName(p), Size: int64(len(f.data)), ModTime: f.modTime})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func (m *memFS) ReadFile(path string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.files[path]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return append([]byte(nil), f.data...), nil
}

func (m *memFS) Probe(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.files[path]
	if !ok {
		return fs.ErrNotExist
	}
	if f.locked {
		return errors.New("the file is in use")
	}
	return nil
}

func (m *memFS) Remove(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.removeErr != nil {
		return m.removeErr
	}
	if _, ok := m.files[path]; !ok {
		return fs.ErrNotExist
	}
	delete(m.files, path)
	m.removed = append(m.removed, path)
	return nil
}

func baseName(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[i+1:]
		}
	}
	return p
}

func dirOf(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[:i]
		}
	}
	return "."
}

// recSink keeps the frames it is given.
type recSink struct {
	mu     sync.Mutex
	frames []bridge.FrameV3
	times  []time.Time
}

func (s *recSink) FeedFrame(f bridge.FrameV3, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.frames = append(s.frames, f)
	s.times = append(s.times, now)
}

func (s *recSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.frames)
}

// testStatus is a status payload with recognisable values.
func testStatus() bridge.LiveState {
	return bridge.LiveState{Level: 60, XPPct: 25, Money: 12345, MapID: 1404, X: 0.4, Y: 0.6, HealthPct: 90, PowerPct: 70, ShareRemainingSec: 20}
}

// panelRowFrames composes the rows of a panel that carries body as one message of type typ:
// one data row per chunk, then the end row. n is the block count of a row.
func panelRowFrames(t *testing.T, n int, shotID uint16, typ uint8, body []byte) [][]byte {
	t.Helper()
	cap := bridge.CapacityV5(n)
	count := (len(body) + cap - 1) / cap
	var rows [][]byte
	for i := 0; i < count; i++ {
		lo, hi := i*cap, min((i+1)*cap, len(body))
		f, err := bridge.BuildFrameV5(testStatus(), uint8(i), n, shotID, &bridge.ChunkV3{
			Type: typ, ID: 9, Index: uint16(i), Count: uint16(count), Data: body[lo:hi],
		})
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, f)
	}
	end, err := bridge.BuildEndFrameV5(testStatus(), uint8(count), n, shotID, count)
	if err != nil {
		t.Fatal(err)
	}
	return append(rows, end)
}

// panelImage draws the panel of body as an image: blocks of blockPx, 1024 px wide, on a
// game-like canvas. A row holds 1024/(2*blockPx) blocks.
func panelImage(t *testing.T, body []byte, shotID uint16, blockPx int) *image.RGBA {
	t.Helper()
	n := 1024 / (2 * blockPx)
	rows := panelRowFrames(t, n, shotID, bridge.MsgQuests, body)
	return bridge.RenderPanel(rows, n, bridge.PanelOptions{BlockPx: blockPx, Width: 1024, Height: 640, Noise: 2, Seed: 7})
}

// gameImage is a screenshot with no panel: a gradient, like a scene.
func gameImage(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{uint8(x), uint8(y), uint8(x ^ y), 255})
		}
	}
	return img
}

func pngBytes(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func jpegBytes(t *testing.T, img image.Image, q int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: q}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// tgaBytes encodes img as a 32-bit TGA: rle chooses run-length packets, bottomLeft the origin.
func tgaBytes(img *image.RGBA, rle, bottomLeft bool) []byte {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	hdr := make([]byte, 18)
	hdr[2] = 2
	if rle {
		hdr[2] = 10
	}
	binary.LittleEndian.PutUint16(hdr[12:], uint16(w))
	binary.LittleEndian.PutUint16(hdr[14:], uint16(h))
	hdr[16] = 32
	if !bottomLeft {
		hdr[17] = 0x20
	}
	// The file order: rows from the bottom when bottomLeft, else from the top.
	var px [][4]byte
	for k := 0; k < w*h; k++ {
		y := k / w
		if bottomLeft {
			y = h - 1 - y
		}
		x := k % w
		o := img.PixOffset(x, y)
		px = append(px, [4]byte{img.Pix[o+2], img.Pix[o+1], img.Pix[o], 255})
	}
	out := hdr
	if !rle {
		for _, p := range px {
			out = append(out, p[:]...)
		}
		return out
	}
	// Runs of equal pixels become run packets (up to 128), the rest raw packets (up to 128).
	for i := 0; i < len(px); {
		j := i + 1
		for j < len(px) && j-i < 128 && px[j] == px[i] {
			j++
		}
		if j-i >= 2 {
			out = append(out, byte(0x80|(j-i-1)))
			out = append(out, px[i][:]...)
			i = j
			continue
		}
		k := i
		for k < len(px) && k-i < 128 && (k+1 >= len(px) || px[k+1] != px[k]) {
			k++
		}
		if k == i {
			k = i + 1
		}
		out = append(out, byte(k-i-1))
		for _, p := range px[i:k] {
			out = append(out, p[:]...)
		}
		i = k
	}
	return out
}

// quiet returns a Quiet duration for the watcher tests: short, so they run fast.
const quiet = 30 * time.Millisecond
