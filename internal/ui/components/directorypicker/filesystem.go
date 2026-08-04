package directorypicker

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type DirEntry struct {
	Name  string
	Path  string
	IsDir bool
}

func readDir(path string, showFiles bool) ([]DirEntry, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	var result []DirEntry
	for _, e := range entries {
		name := e.Name()
		isDir := e.IsDir()

		// Skip hidden entries unless they're ".."
		if strings.HasPrefix(name, ".") && name != ".." {
			continue
		}

		// In dir mode, skip files; in file mode, skip directories
		if isDir {
			result = append(result, DirEntry{
				Name:  name,
				Path:  filepath.Join(path, name),
				IsDir: true,
			})
		} else if showFiles {
			result = append(result, DirEntry{
				Name:  name,
				Path:  filepath.Join(path, name),
				IsDir: false,
			})
		}
	}

	// Sort: directories first, then files, alphabetically, hidden last
	sort.Slice(result, func(i, j int) bool {
		a, b := result[i], result[j]
		hi := strings.HasPrefix(a.Name, ".")
		hj := strings.HasPrefix(b.Name, ".")
		if hi != hj {
			return !hi
		}
		if a.IsDir != b.IsDir {
			return a.IsDir
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})

	return result, nil
}

func parentPath(path string) string {
	parent := filepath.Dir(path)
	if parent == path {
		return path
	}
	return parent
}

func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/"
	}
	return home
}

func pathExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
