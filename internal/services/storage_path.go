package services

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func ResolveStoragePath(root, relative string) (string, error) {
	if relative == "" || strings.ContainsAny(relative, "\\\x00") || filepath.IsAbs(relative) {
		return "", errors.New("invalid relative path")
	}
	base, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	target := filepath.Join(base, filepath.Clean(relative))
	within := func(parent, child string) bool {
		rel, e := filepath.Rel(parent, child)
		return e == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
	}
	if !within(base, target) {
		return "", errors.New("path escapes storage")
	}
	realBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		return "", err
	}
	// Resolve the nearest existing ancestor, including for new/missing files.
	ancestor := target
	for {
		realAncestor, e := filepath.EvalSymlinks(ancestor)
		if e == nil {
			if realAncestor != realBase && !within(realBase, realAncestor) {
				return "", errors.New("symlink escapes storage")
			}
			break
		}
		if !os.IsNotExist(e) || ancestor == base {
			return "", e
		}
		ancestor = filepath.Dir(ancestor)
	}
	return target, nil
}
