package release

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// unsafe reports whether an archive path could escape the destination directory.
func unsafe(p string) bool {
	return p == "" || strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) ||
		strings.Contains(p, `\`) || containsDotDot(p)
}

func containsDotDot(p string) bool {
	for _, part := range strings.Split(p, "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

// extract unpacks a .tar.gz into dest. Links are allowed only if they stay inside
// the archive (release tarballs contain versioned library symlinks).
func extract(data []byte, dest string) error {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if unsafe(h.Name) {
			return fmt.Errorf("unsafe path %q", h.Name)
		}
		target := filepath.Join(dest, filepath.FromSlash(h.Name))
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(h.Mode)&0o755)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		case tar.TypeSymlink:
			// A symlink target is relative to the link's own directory.
			resolved := path.Join(path.Dir(h.Name), h.Linkname)
			if strings.HasPrefix(h.Linkname, "/") || unsafe(resolved) {
				return fmt.Errorf("unsafe link %q -> %q", h.Name, h.Linkname)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(h.Linkname, target); err != nil {
				return err
			}
		case tar.TypeLink:
			if unsafe(h.Linkname) {
				return fmt.Errorf("unsafe hard link %q -> %q", h.Name, h.Linkname)
			}
			if err := os.Link(filepath.Join(dest, filepath.FromSlash(h.Linkname)), target); err != nil {
				return err
			}
		}
	}
}
