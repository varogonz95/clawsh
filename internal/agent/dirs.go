package agent

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/varogonz95/clawsh/internal/api"
)

// maxListedDirs caps the subfolders returned for one folder.
const maxListedDirs = 1000

// ListDirs lists the immediate subfolders of path ("" and "~" are the home
// folder, "~/x" is expanded like a session folder). The returned status is the
// HTTP code to answer with when err is set: 403 for an unreadable folder, 400
// for anything else.
func ListDirs(path string) (api.DirList, int, error) {
	dir, err := resolveDir(path)
	if err != nil {
		return api.DirList{}, dirErrStatus(err), err
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return api.DirList{}, http.StatusBadRequest, err
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return api.DirList{}, dirErrStatus(err), err
	}
	names := []string{}
	for _, e := range entries {
		isDir := e.IsDir()
		if !isDir && e.Type()&fs.ModeSymlink != 0 {
			if fi, err := os.Stat(filepath.Join(abs, e.Name())); err == nil && fi.IsDir() {
				isDir = true
			}
		}
		if isDir {
			names = append(names, e.Name())
		}
	}
	sort.SliceStable(names, func(i, j int) bool {
		a, b := strings.ToLower(names[i]), strings.ToLower(names[j])
		if a != b {
			return a < b
		}
		return names[i] < names[j]
	})
	if len(names) > maxListedDirs {
		names = names[:maxListedDirs]
	}
	parent := filepath.Dir(abs)
	if parent == abs {
		parent = ""
	}
	return api.DirList{Path: abs, Parent: parent, Dirs: names}, http.StatusOK, nil
}

func dirErrStatus(err error) int {
	if errors.Is(err, fs.ErrPermission) {
		return http.StatusForbidden
	}
	return http.StatusBadRequest
}
