package main

import (
	"io"
	"log"
	"os"
	"path/filepath"
)

// maxLogSize is the size above which the log file is moved to <name>.old at startup, so it
// never grows past about twice this.
const maxLogSize = 5 << 20

// setupLog writes the log to LOG_PATH as well as stderr. A build without a console (the Windows
// tray build) has no stderr, so there the log goes to ukemochi.log beside the binary by default.
// It returns the open file, or nil for stderr only.
func setupLog() (*os.File, error) {
	path := os.Getenv("LOG_PATH")
	_, statErr := os.Stderr.Stat()
	stderr := statErr == nil
	if path == "" && !stderr {
		exe, err := os.Executable()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(filepath.Dir(exe), "ukemochi.log")
	}
	if path == "" {
		return nil, nil
	}
	f, err := openLog(path)
	if err != nil {
		return nil, err
	}
	if stderr {
		log.SetOutput(io.MultiWriter(os.Stderr, f))
	} else {
		log.SetOutput(f)
	}
	return f, nil
}

func openLog(path string) (*os.File, error) {
	if fi, err := os.Stat(path); err == nil && fi.Size() > maxLogSize {
		os.Remove(path + ".old")
		if err := os.Rename(path, path+".old"); err != nil {
			return nil, err
		}
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
}
