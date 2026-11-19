package main

import (
	"os"
	"path/filepath"

	"github.com/MTEKode/companeroWoW/app/internal/config"
	"github.com/MTEKode/companeroWoW/app/internal/shots"
)

// screenshotDiag is the screenshot channel part of --diag: the folder WoW saves its screenshots
// to, the screenshots waiting there, and what the running app last wrote about the channel.
type screenshotDiag struct {
	Channel     string        `json:"channel"`
	Folder      string        `json:"folder,omitempty"`
	FolderFound bool          `json:"folderFound"`
	Images      int           `json:"images"`
	ImagesError string        `json:"imagesError,omitempty"`
	RunningApp  *shots.Status `json:"runningApp,omitempty"`
	StateError  string        `json:"stateError,omitempty"`
}

// diagScreenshots reads the screenshot channel's state for --diag. It lists the folder but does not
// open any screenshot.
func diagScreenshots(cfg *config.Config, dir string) *screenshotDiag {
	d := &screenshotDiag{Channel: cfg.Channel}
	if cfg.WoWPath == "" {
		d.ImagesError = "no hay carpeta de WoW configurada"
		return d
	}
	d.Folder = filepath.Join(cfg.WoWPath, "Screenshots")
	if fi, err := os.Stat(d.Folder); err != nil {
		d.ImagesError = err.Error()
	} else if fi.IsDir() {
		d.FolderFound = true
		n, err := shots.CountImages(d.Folder)
		if err != nil {
			d.ImagesError = err.Error()
		}
		d.Images = n
	}
	st, ok, err := shots.ReadState(filepath.Join(dir, shots.StateFile))
	switch {
	case err != nil:
		d.StateError = err.Error()
	case ok:
		d.RunningApp = &st
	}
	return d
}
