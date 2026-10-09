package utils

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

func LoadJSON(fileName string, dataDir string) ([]byte, error) {
	candidates := []string{}
	if dataDir != "" {
		candidates = append(candidates, filepath.Join(dataDir, fileName))
		if cwd, err := os.Getwd(); err == nil {
			candidates = append(candidates, filepath.Join(cwd, dataDir, fileName))
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(cwd, fileName))
		candidates = append(candidates, filepath.Join(cwd, "data", fileName))
	}
	exe, err := os.Executable()
	if err == nil {
		exeDir := filepath.Dir(exe)
		candidates = append(candidates, filepath.Join(exeDir, fileName))
		candidates = append(candidates, filepath.Join(exeDir, "data", fileName))
		candidates = append(candidates, filepath.Join(filepath.Dir(exeDir), "data", fileName))
	}
	seen := map[string]bool{}
	for _, p := range candidates {
		if seen[p] {
			continue
		}
		seen[p] = true
		if b, err := os.ReadFile(p); err == nil {
			return b, nil
		}
	}
	return nil, errors.New("file not found: " + fileName)
}

func UnmarshalJSON[T any](fileName string, dataDir string, fallback []T) []T {
	b, err := LoadJSON(fileName, dataDir)
	if err != nil {
		return fallback
	}
	var v []T
	if err := json.Unmarshal(b, &v); err != nil {
		return fallback
	}
	return v
}
