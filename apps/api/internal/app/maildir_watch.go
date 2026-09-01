package app

import (
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/fsnotify/fsnotify"
)

func watchMaildirChanges(root string, log *slog.Logger) (<-chan struct{}, func()) {
	changes := make(chan struct{}, 1)
	root = strings.TrimSpace(root)
	if root == "" {
		return changes, func() {}
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Warn("maildir file watcher unavailable; periodic sync remains active", "error", err)
		return changes, func() {}
	}
	addTree := func(path string) {
		if err := filepath.WalkDir(path, func(current string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil || !entry.IsDir() {
				return nil
			}
			if err := watcher.Add(current); err != nil {
				log.Warn("failed to watch maildir directory", "path", current, "error", err)
			}
			return nil
		}); err != nil {
			log.Warn("failed to inspect maildir for file watching", "path", path, "error", err)
		}
	}
	addTree(root)
	done := make(chan struct{})
	go func() {
		defer close(changes)
		for {
			select {
			case <-done:
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Has(fsnotify.Create) {
					if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
						addTree(event.Name)
					}
				}
				if event.Has(fsnotify.Create) || event.Has(fsnotify.Write) || event.Has(fsnotify.Rename) {
					select {
					case changes <- struct{}{}:
					default:
					}
				}
			case err, ok := <-watcher.Errors:
				if ok {
					log.Warn("maildir file watcher error; periodic sync remains active", "error", err)
				}
			}
		}
	}()
	return changes, func() {
		close(done)
		_ = watcher.Close()
	}
}
