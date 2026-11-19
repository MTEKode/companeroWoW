package shots

import (
	"errors"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
)

var t0 = time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)

// The body a panel carries in these tests: a message of more than one row.
func testBody(size int) []byte {
	b := make([]byte, size)
	for i := range b {
		b[i] = byte('A' + i%26)
	}
	return b
}

func TestProcessPanelFeedsFramesAndDeletes(t *testing.T) {
	body := testBody(500)
	cases := map[string]struct {
		name string
		data []byte
	}{
		"png 2px":      {"WoWScrnShot_1.png", pngBytes(t, panelImage(t, body, 11, 2))},
		"tga 2px rle":  {"WoWScrnShot_2.tga", tgaBytes(panelImage(t, body, 12, 2), true, true)},
		"jpeg 8px q95": {"WoWScrnShot_3.jpg", jpegBytes(t, panelImage(t, body, 13, 8), 95)},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			mfs := newMemFS()
			mfs.put(`C:\WoW\Screenshots\`+c.name, c.data, t0)
			sink := &recSink{}
			res, err := Process(mfs, sink, `C:\WoW\Screenshots\`+c.name, t0)
			if err != nil {
				t.Fatalf("Process: %v", err)
			}
			if !res.Complete || res.Rows < 2 {
				t.Fatalf("result = %+v", res)
			}
			if sink.count() != res.Rows {
				t.Fatalf("fed %d frames, read %d rows", sink.count(), res.Rows)
			}
			if !res.Deleted || mfs.has(`C:\WoW\Screenshots\`+c.name) {
				t.Fatal("the shot was not deleted after its decode")
			}
			if res.Bytes != len(body) {
				t.Fatalf("bytes = %d, want %d", res.Bytes, len(body))
			}
		})
	}
}

func TestProcessLeavesUserScreenshotsAlone(t *testing.T) {
	mfs := newMemFS()
	png := `C:\WoW\Screenshots\WoWScrnShot_user.png`
	tga := `C:\WoW\Screenshots\WoWScrnShot_user.tga`
	mfs.put(png, pngBytes(t, gameImage(640, 480)), t0)
	mfs.put(tga, tgaBytes(gameImage(320, 240), false, true), t0)
	for _, p := range []string{png, tga} {
		sink := &recSink{}
		_, err := Process(mfs, sink, p, t0)
		if !errors.Is(err, errNotPanel) {
			t.Fatalf("%s: err = %v, want errNotPanel", p, err)
		}
		if sink.count() != 0 {
			t.Fatalf("%s: frames were fed from a user screenshot", p)
		}
		if !mfs.has(p) {
			t.Fatalf("%s was deleted", p)
		}
	}
	if len(mfs.removed) != 0 {
		t.Fatalf("removed %v", mfs.removed)
	}
}

func TestProcessDeletesOnlyTheShotFile(t *testing.T) {
	mfs := newMemFS()
	dir := `C:\WoW\Screenshots`
	shot := dir + `\WoWScrnShot_shot.png`
	others := []string{dir + `\WoWScrnShot_user.png`, dir + `\notes.txt`, dir + `\WoWScrnShot_user.tga`}
	mfs.put(shot, pngBytes(t, panelImage(t, testBody(300), 5, 2)), t0)
	mfs.put(others[0], pngBytes(t, gameImage(200, 100)), t0)
	mfs.put(others[1], []byte("texto"), t0)
	mfs.put(others[2], tgaBytes(gameImage(64, 64), true, false), t0)
	if _, err := Process(mfs, &recSink{}, shot, t0); err != nil {
		t.Fatal(err)
	}
	if mfs.has(shot) {
		t.Fatal("the shot is still there")
	}
	for _, p := range others {
		if !mfs.has(p) {
			t.Fatalf("%s was touched", p)
		}
	}
}

func TestProcessFailedDeleteKeepsTheRows(t *testing.T) {
	mfs := newMemFS()
	shot := `C:\WoW\Screenshots\WoWScrnShot_locked.png`
	mfs.put(shot, pngBytes(t, panelImage(t, testBody(120), 6, 2)), t0)
	mfs.removeErr = errors.New("access denied")
	sink := &recSink{}
	res, err := Process(mfs, sink, shot, t0)
	if err == nil {
		t.Fatal("a failed delete was not reported")
	}
	if sink.count() == 0 || res.Deleted {
		t.Fatalf("rows fed %d, deleted %v: the rows must go in even when the delete fails", sink.count(), res.Deleted)
	}
}

func TestProcessIncompletePanelIsStillOurs(t *testing.T) {
	// A panel cut before its end row: the rows that decode are fed, and the file is ours.
	mfs := newMemFS()
	shot := `C:\WoW\Screenshots\WoWScrnShot_cut.png`
	rows := panelRowFrames(t, 200, 8, bridge.MsgQuests, testBody(900))
	img := bridge.RenderPanel(rows[:len(rows)-1], 200, bridge.PanelOptions{BlockPx: 2, Width: 800, Height: 600})
	mfs.put(shot, pngBytes(t, img), t0)
	res, err := Process(mfs, &recSink{}, shot, t0)
	if err != nil {
		t.Fatal(err)
	}
	if res.Complete || res.Stop == nil || !res.Deleted {
		t.Fatalf("result = %+v", res)
	}
}

func TestProcessBrokenFileIsKept(t *testing.T) {
	mfs := newMemFS()
	bad := `C:\WoW\Screenshots\WoWScrnShot_bad.png`
	mfs.put(bad, []byte("\x89PNG\r\n\x1a\nnot really"), t0)
	if _, err := Process(mfs, &recSink{}, bad, t0); err == nil {
		t.Fatal("a broken PNG decoded")
	}
	if !mfs.has(bad) {
		t.Fatal("a broken file was deleted")
	}
}
