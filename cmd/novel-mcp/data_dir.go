package main

import (
	"os"
	"path/filepath"
)

const portableMarkerName = "portable.flag"

func defaultDataDir() string {
	executable, _ := os.Executable()
	home, _ := os.UserHomeDir()
	return dataDirForExecutable(executable, home)
}

func dataDirForExecutable(executable, home string) string {
	if executable != "" {
		dir := filepath.Dir(executable)
		if info, err := os.Stat(filepath.Join(dir, portableMarkerName)); err == nil && !info.IsDir() {
			return filepath.Join(dir, "data")
		}
	}
	if home != "" {
		return filepath.Join(home, ".novel-mcp")
	}
	return ".novel-mcp"
}

func portableDataDir() (string, bool) {
	executable, err := os.Executable()
	if err != nil {
		return "", false
	}
	dir := filepath.Dir(executable)
	info, err := os.Stat(filepath.Join(dir, portableMarkerName))
	if err != nil || info.IsDir() {
		return "", false
	}
	return filepath.Join(dir, "data"), true
}

func isPortableConfig(path string) (string, bool) {
	dataDir, ok := portableDataDir()
	if !ok {
		return "", false
	}
	return portableConfigDataDir(path, dataDir)
}

func portableConfigDataDir(path, dataDir string) (string, bool) {
	want := filepath.Clean(filepath.Join(dataDir, "config.json"))
	return dataDir, filepath.Clean(path) == want
}
