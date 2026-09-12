// Package pathsafe contains filesystem containment helpers used by handlers
// and workers before reading tenant-controlled output paths.
package pathsafe

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ResolveExistingWithin resolves candidate and verifies that both the
// requested path and its resolved target remain below root. Symlink escapes
// are rejected before callers open the returned path.
func ResolveExistingWithin(root, candidate string) (string, error) {
	rootReal, err := resolveExisting(root)
	if err != nil {
		return "", fmt.Errorf("resolve root: %w", err)
	}

	candidatePath := candidate
	if !filepath.IsAbs(candidatePath) {
		candidatePath = filepath.Join(rootReal, candidatePath)
	}
	candidateAbs, err := filepath.Abs(candidatePath)
	if err != nil {
		return "", err
	}
	candidateAbs = filepath.Clean(candidateAbs)
	if ok, err := within(rootReal, candidateAbs); err != nil {
		return "", err
	} else if !ok {
		return "", fmt.Errorf("path %q escapes root %q", candidate, root)
	}

	realPath, err := resolveExisting(candidateAbs)
	if err != nil {
		return "", err
	}
	if ok, err := within(rootReal, realPath); err != nil {
		return "", err
	} else if !ok {
		return "", fmt.Errorf("path %q resolves outside root %q", candidate, root)
	}
	return realPath, nil
}

// RealPath returns an absolute, symlink-resolved path for an existing path.
func RealPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return resolveExisting(abs)
}

func resolveExisting(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	realPath, err := filepath.EvalSymlinks(abs)
	if err == nil {
		return filepath.Clean(realPath), nil
	}
	// Windows sandboxed runners can deny the final-path query while still
	// allowing Lstat. Walk every component in that case and fail closed if any
	// component is a symlink or cannot be inspected.
	if !os.IsPermission(err) {
		return "", err
	}
	if err := rejectSymlinkComponents(abs); err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func rejectSymlinkComponents(abs string) error {
	volume := filepath.VolumeName(abs)
	root := volume + string(os.PathSeparator)
	relative := strings.TrimPrefix(abs, root)
	current := root
	if relative == "" {
		info, err := os.Lstat(filepath.Clean(abs))
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to follow symlink: %s", abs)
		}
		return nil
	}
	for _, part := range strings.Split(relative, string(os.PathSeparator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to follow symlink: %s", current)
		}
	}
	return nil
}

func within(root, target string) (bool, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false, err
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(filepath.Clean(rootAbs), filepath.Clean(targetAbs))
	if err != nil {
		return false, err
	}
	if rel == "." {
		return true, nil
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel), nil
}
