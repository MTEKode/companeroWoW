package bridge

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math/rand"
	"testing"
	"time"
)

// panelStatus is a status payload with recognisable values.
func panelStatus() LiveState {
	return LiveState{Level: 60, XPPct: 33, Money: 123456, MapID: 1429, X: 0.5, Y: 0.25, HealthPct: 80, PowerPct: 50, InCombat: false}
}

// panelRows splits a message body over the rows of a panel: one data row per chunk, a
// status-only row in the middle, then the end frame. It returns the frame bytes and the body.
func panelRows(t *testing.T, n int, shotID uint16, body []byte) [][]byte {
	t.Helper()
	cap := CapacityV5(n)
	var rows [][]byte
	count := (len(body) + cap - 1) / cap
	for i := 0; i < count; i++ {
		lo, hi := i*cap, min((i+1)*cap, len(body))
		f, err := BuildFrameV5(panelStatus(), uint8(i), n, shotID, &ChunkV3{
			Type: MsgQuests, ID: 7, Index: uint16(i), Count: uint16(count), Data: body[lo:hi],
		})
		if err != nil {
			t.Fatalf("BuildFrameV5 row %d: %v", i, err)
		}
		rows = append(rows, f)
		if i == 0 {
			status, err := BuildFrameV5(panelStatus(), uint8(i+1), n, shotID, nil)
			if err != nil {
				t.Fatalf("status row: %v", err)
			}
			rows = append(rows, status)
		}
	}
	end, err := BuildEndFrameV5(panelStatus(), uint8(len(rows)), n, shotID, len(rows))
	if err != nil {
		t.Fatalf("BuildEndFrameV5: %v", err)
	}
	return append(rows, end)
}

// testBody is a JSON-ish body of size bytes.
func testBody(size int) []byte {
	b := make([]byte, size)
	for i := range b {
		b[i] = byte('a' + i%26)
	}
	return b
}

func TestV5FrameRoundTrip(t *testing.T) {
	body := testBody(40)
	f, err := BuildFrameV5(panelStatus(), 9, 128, 0xBEEF, &ChunkV3{Type: MsgCharacter, ID: 3, Index: 1, Count: 2, Data: body})
	if err != nil {
		t.Fatal(err)
	}
	if f[0] != ProtocolV5 || f[4] != 0xEF || f[5] != 0xBE {
		t.Fatalf("header = % x, want version 5 and shot id EF BE after N", f[:6])
	}
	got, err := ParseFrameV5(f)
	if err != nil {
		t.Fatal(err)
	}
	if got.ShotID != 0xBEEF || got.N != 128 || got.Seq != 9 || got.MsgType != MsgCharacter || got.MsgID != 3 ||
		got.ChunkIndex != 1 || got.ChunkCount != 2 || !bytes.Equal(got.Data, body) {
		t.Fatalf("round trip = %+v", got)
	}
	if got.Status.Level != 60 || got.Status.Money != 123456 {
		t.Fatalf("status = %+v", got.Status)
	}
}

func TestV5FrameRejectsCorruption(t *testing.T) {
	f, err := BuildFrameV5(panelStatus(), 1, 64, 5, &ChunkV3{Type: MsgQuests, Count: 1, Data: []byte("hola")})
	if err != nil {
		t.Fatal(err)
	}
	bad := append([]byte(nil), f...)
	bad[len(bad)-3] ^= 0x01
	if _, err := ParseFrameV5(bad); !errors.Is(err, ErrBadCRC) {
		t.Fatalf("corrupt data: err = %v, want ErrBadCRC", err)
	}
	v3 := append([]byte(nil), f...)
	v3[0] = ProtocolV31
	if _, err := ParseFrameV5(v3); !errors.Is(err, ErrBadVersion) {
		t.Fatalf("wrong version: err = %v, want ErrBadVersion", err)
	}
}

func TestCapacityV5(t *testing.T) {
	n := 256
	cap := CapacityV5(n)
	if cap != 3*(n-7)/2-49 {
		t.Fatalf("CapacityV5(%d) = %d", n, cap)
	}
	if _, err := BuildFrameV5(panelStatus(), 0, n, 1, &ChunkV3{Type: 1, Count: 1, Data: make([]byte, cap)}); err != nil {
		t.Fatalf("full chunk: %v", err)
	}
	if _, err := BuildFrameV5(panelStatus(), 0, n, 1, &ChunkV3{Type: 1, Count: 1, Data: make([]byte, cap+1)}); err == nil {
		t.Fatal("a chunk one byte over capacity was built")
	}
}

// reassemble collects the message of the frames without the JSON step (the test bodies are not JSON).
func reassemble(t *testing.T, frames []FrameV3) []byte {
	t.Helper()
	re := NewReassembler()
	for _, f := range frames {
		if typ, body, ok := re.Add(f, time.Unix(100, 0)); ok && typ == MsgQuests {
			return body
		}
	}
	t.Fatal("no complete message")
	return nil
}

func decodeAndCheck(t *testing.T, img image.Image, n int, shotID uint16, body []byte) {
	t.Helper()
	res, err := DecodePanel(img)
	if err != nil {
		t.Fatalf("DecodePanel: %v", err)
	}
	if !res.Complete {
		t.Fatalf("panel not complete: stop = %v, rows = %d", res.Stop, len(res.Frames))
	}
	if res.ShotID != shotID {
		t.Fatalf("shot id = %d, want %d", res.ShotID, shotID)
	}
	if !IsEndFrame(res.Frames[len(res.Frames)-1]) {
		t.Fatal("last frame is not the end frame")
	}
	for _, f := range res.Frames {
		if f.N != n {
			t.Fatalf("row of %d blocks, want %d", f.N, n)
		}
	}
	if got := reassemble(t, res.Frames); !bytes.Equal(got, body) {
		t.Fatalf("reassembled %d bytes, want %d: %q...", len(got), len(body), got[:min(len(got), 20)])
	}
}

func TestDecodePanel2pxBlocks(t *testing.T) {
	const n = 256 // 512 px wide
	body := testBody(900)
	rows := panelRows(t, n, 4242, body)
	img := RenderPanel(rows, n, PanelOptions{BlockPx: 2, Width: 1280, Height: 720})
	res, err := DecodePanel(img)
	if err != nil {
		t.Fatalf("DecodePanel: %v", err)
	}
	if res.Pitch < 1.99 || res.Pitch > 2.01 {
		t.Fatalf("pitch = %.4f, want about 2", res.Pitch)
	}
	if len(res.Frames) != len(rows) {
		t.Fatalf("rows read = %d, want %d (stop %v)", len(res.Frames), len(rows), res.Stop)
	}
	decodeAndCheck(t, img, n, 4242, body)
}

func TestDecodePanel4pxBlocks(t *testing.T) {
	const n = 128 // 512 px wide
	body := testBody(300)
	rows := panelRows(t, n, 77, body)
	img := RenderPanel(rows, n, PanelOptions{BlockPx: 4, Width: 1024, Height: 600})
	decodeAndCheck(t, img, n, 77, body)
}

func TestDecodePanelNoise(t *testing.T) {
	const n = 200
	body := testBody(500)
	rows := panelRows(t, n, 9, body)
	img := RenderPanel(rows, n, PanelOptions{BlockPx: 2, Noise: 2, Seed: 3, Width: 800, Height: 600})
	decodeAndCheck(t, img, n, 9, body)
}

// jpegPanel encodes the panel as a JPEG at quality q and decodes it back.
func jpegPanel(t *testing.T, src image.Image, q int) image.Image {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: q}); err != nil {
		t.Fatal(err)
	}
	img, err := jpeg.Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// TestDecodePanelJPEG8pxBlocks: the JPEG fallback reads 8 px blocks at quality 95. Go's encoder
// stores colour at 4:2:0 and quantises it in 16 px cells, so 4 px blocks do not survive JPEG at
// any quality (see TestDecodePanelJPEG4pxNeverWrong).
func TestDecodePanelJPEG8pxBlocks(t *testing.T) {
	const n = 128
	body := testBody(260)
	rows := panelRows(t, n, 31, body)
	src := RenderPanel(rows, n, PanelOptions{BlockPx: 8, Width: 1024, Height: 640})
	decodeAndCheck(t, jpegPanel(t, src, 95), n, 31, body)
}

// TestDecodePanelJPEG4pxNeverWrong: a 4 px JPEG at quality 10 (the fallback the contract names)
// is not decodable by the block means. The CRC must keep it from yielding a wrong body: the
// decode either fails or gives the right message.
func TestDecodePanelJPEG4pxNeverWrong(t *testing.T) {
	const n = 128
	body := testBody(260)
	rows := panelRows(t, n, 31, body)
	src := RenderPanel(rows, n, PanelOptions{BlockPx: 4, Width: 640, Height: 480})
	res, err := DecodePanel(jpegPanel(t, src, 10))
	if err != nil {
		return // refused: nothing is read, nothing is deleted
	}
	if res.Complete {
		if got := reassemble(t, res.Frames); !bytes.Equal(got, body) {
			t.Fatal("a complete JPEG panel decoded to a wrong body")
		}
	}
}

func TestDecodePanelPNGRoundTrip(t *testing.T) {
	const n = 160
	body := testBody(420)
	rows := panelRows(t, n, 1, body)
	src := RenderPanel(rows, n, PanelOptions{BlockPx: 2, Width: 800, Height: 500, Noise: 2, Seed: 11})
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	decodeAndCheck(t, img, n, 1, body)
}

func TestDecodePanelTruncatedHasNoEnd(t *testing.T) {
	const n = 200
	body := testBody(400)
	rows := panelRows(t, n, 12, body)
	cut := rows[:len(rows)-1] // without the end frame
	img := RenderPanel(cut, n, PanelOptions{BlockPx: 2, Width: 800, Height: 600})
	res, err := DecodePanel(img)
	if err != nil {
		t.Fatalf("DecodePanel: %v", err)
	}
	if res.Complete || res.Stop == nil {
		t.Fatalf("a panel without its end frame is complete (stop %v)", res.Stop)
	}
	if len(res.Frames) != len(cut) {
		t.Fatalf("rows read = %d, want the %d above the missing end", len(res.Frames), len(cut))
	}
}

func TestDecodePanelWrongEndCount(t *testing.T) {
	const n = 200
	rows := panelRows(t, n, 12, testBody(100))
	end, err := BuildEndFrameV5(panelStatus(), 0, n, 12, 99)
	if err != nil {
		t.Fatal(err)
	}
	rows[len(rows)-1] = end
	res, err := DecodePanel(RenderPanel(rows, n, PanelOptions{BlockPx: 2, Width: 800, Height: 600}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Complete {
		t.Fatal("an end frame naming 99 rows completed a panel of fewer rows")
	}
}

func TestDecodePanelNoPanel(t *testing.T) {
	// A screenshot of the game: no magic at the top-left.
	img := image.NewRGBA(image.Rect(0, 0, 640, 480))
	for y := 0; y < 480; y++ {
		for x := 0; x < 640; x++ {
			img.SetRGBA(x, y, color.RGBA{uint8(x), uint8(y), 128, 255})
		}
	}
	if _, err := DecodePanel(img); !errors.Is(err, ErrNoPanel) {
		t.Fatalf("err = %v, want ErrNoPanel", err)
	}
	if _, err := DecodePanel(image.NewRGBA(image.Rect(0, 0, 8, 1))); err == nil {
		t.Fatal("a tiny image decoded")
	}
}

func TestDecodePanelRandomPayloads(t *testing.T) {
	rng := rand.New(rand.NewSource(5))
	for i := 0; i < 4; i++ {
		n := 96 + rng.Intn(200)
		body := make([]byte, 1+rng.Intn(CapacityV5(n)*3))
		rng.Read(body)
		shotID := uint16(rng.Intn(65535))
		rows := panelRows(t, n, shotID, body)
		img := RenderPanel(rows, n, PanelOptions{BlockPx: 2, Noise: 2, Seed: int64(i), Width: 1024, Height: 512})
		decodeAndCheck(t, img, n, shotID, body)
	}
}
