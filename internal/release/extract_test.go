package release

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func tarball(t *testing.T, headers ...*tar.Header) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, h := range headers {
		body := []byte("x")
		if h.Typeflag == tar.TypeReg {
			h.Size = int64(len(body))
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			tw.Write(body)
		}
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func TestExtractAllowsInternalSymlink(t *testing.T) {
	dest := t.TempDir()
	data := tarball(t,
		&tar.Header{Name: "b/lib/libx.0.dylib", Typeflag: tar.TypeReg, Mode: 0o644},
		&tar.Header{Name: "b/lib/libx.dylib", Typeflag: tar.TypeSymlink, Linkname: "libx.0.dylib"},
	)
	if err := extract(data, dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "b/lib/libx.dylib")); err != nil {
		t.Fatal(err)
	}
}

func TestExtractRejectsEscapes(t *testing.T) {
	for name, h := range map[string]*tar.Header{
		"dotdot":       {Name: "../evil", Typeflag: tar.TypeReg, Mode: 0o644},
		"absolute":     {Name: "/tmp/evil", Typeflag: tar.TypeReg, Mode: 0o644},
		"symlink-out":  {Name: "b/link", Typeflag: tar.TypeSymlink, Linkname: "../../etc/passwd"},
		"symlink-abs":  {Name: "b/link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"},
		"hardlink-out": {Name: "b/link", Typeflag: tar.TypeLink, Linkname: "../x"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := extract(tarball(t, h), t.TempDir()); err == nil {
				t.Fatal("expected the archive to be rejected")
			}
		})
	}
}
