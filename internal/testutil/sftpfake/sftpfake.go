// Package sftpfake provides an in-memory RemoteFS-shaped SFTP stand-in plus
// helpers for unit tests across the SFTP remote-backup feature. It is
// structurally compatible with backupsftp.RemoteFS without importing that
// package, so packages can substitute it through the Dial seam.
package sftpfake

import (
	"bytes"
	"io"
	"os"
	"path"
	"sort"
	"sync"
	"time"
)

// MemFS is an in-memory SFTP-style filesystem. Paths use POSIX slash
// semantics like a real SFTP server. Error injection uses the Errors map:
// a key is either the bare operation name or "op|path" (ops: stat, readdir,
// open, create, readfile, mkdirall, rename, remove). A *op* key fails every
// call of that operation.
type MemFS struct {
	mu     sync.Mutex
	files  map[string][]byte
	dirs   map[string]bool
	closed bool
	// Ops records every operation call as "op path" for assertions.
	Ops []string
	// Errors injects failures; see type doc for key format.
	Errors map[string]error
	// Written records the paths committed by Create writers that closed
	// successfully, in order — lets a test distinguish staged .part bytes
	// from published archives.
	Written []string
}

func New() *MemFS {
	return &MemFS{files: map[string][]byte{}, dirs: map[string]bool{"/": true}}
}

func (f *MemFS) fail(op, name string) error {
	if f.Errors == nil {
		return nil
	}
	if err, ok := f.Errors[op+"|"+name]; ok && err != nil {
		return err
	}
	if err, ok := f.Errors[op]; ok && err != nil {
		return err
	}
	return nil
}

// SetFile seeds remote content, creating parent directories implicitly.
func (f *MemFS) SetFile(name string, data []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setLocked(path.Clean(name), data)
}

func (f *MemFS) setLocked(name string, data []byte) {
	f.files[name] = append([]byte(nil), data...)
	for dir := path.Dir(name); dir != "." && dir != name; dir = path.Dir(dir) {
		if f.dirs[dir] {
			break
		}
		f.dirs[dir] = true
	}
}

// File returns a snapshot of remote content, or nil.
func (f *MemFS) File(name string) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, ok := f.files[path.Clean(name)]
	if !ok {
		return nil
	}
	return append([]byte(nil), data...)
}

// Has reports whether a file exists at name.
func (f *MemFS) Has(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.files[path.Clean(name)]
	return ok
}

// Paths returns all file paths, sorted.
func (f *MemFS) Paths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.files))
	for name := range f.files {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func (f *MemFS) Stat(name string) (os.FileInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Ops = append(f.Ops, "stat "+name)
	if err := f.fail("stat", name); err != nil {
		return nil, err
	}
	name = path.Clean(name)
	if data, ok := f.files[name]; ok {
		return fileInfo{name: path.Base(name), size: int64(len(data))}, nil
	}
	if f.dirs[name] {
		return fileInfo{name: path.Base(name), dir: true}, nil
	}
	return nil, os.ErrNotExist
}

func (f *MemFS) ReadDir(dir string) ([]os.FileInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Ops = append(f.Ops, "readdir "+dir)
	if err := f.fail("readdir", dir); err != nil {
		return nil, err
	}
	dir = path.Clean(dir)
	if !f.dirs[dir] {
		return nil, os.ErrNotExist
	}
	seen := map[string]fileInfo{}
	for name, data := range f.files {
		if path.Dir(name) == dir {
			seen[name] = fileInfo{name: path.Base(name), size: int64(len(data))}
		}
	}
	for d := range f.dirs {
		if d != dir && path.Dir(d) == dir {
			seen[d] = fileInfo{name: path.Base(d), dir: true}
		}
	}
	infos := make([]os.FileInfo, 0, len(seen))
	for _, info := range seen {
		infos = append(infos, info)
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name() < infos[j].Name() })
	return infos, nil
}

func (f *MemFS) Open(name string) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Ops = append(f.Ops, "open "+name)
	if err := f.fail("open", name); err != nil {
		return nil, err
	}
	data, ok := f.files[path.Clean(name)]
	if !ok {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(append([]byte(nil), data...))), nil
}

func (f *MemFS) ReadFile(name string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Ops = append(f.Ops, "readfile "+name)
	if err := f.fail("readfile", name); err != nil {
		return nil, err
	}
	data, ok := f.files[path.Clean(name)]
	if !ok {
		return nil, os.ErrNotExist
	}
	return append([]byte(nil), data...), nil
}

func (f *MemFS) Create(name string) (io.WriteCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Ops = append(f.Ops, "create "+name)
	if err := f.fail("create", name); err != nil {
		return nil, err
	}
	name = path.Clean(name)
	return &memWriter{fs: f, name: name}, nil
}

func (f *MemFS) MkdirAll(dir string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Ops = append(f.Ops, "mkdirall "+dir)
	if err := f.fail("mkdirall", dir); err != nil {
		return err
	}
	for d := path.Clean(dir); d != "." && d != "/"; d = path.Dir(d) {
		if f.dirs[d] {
			break
		}
		f.dirs[d] = true
	}
	f.dirs[path.Clean(dir)] = true
	return nil
}

// Rename replaces the destination atomically (POSIX rename semantics).
func (f *MemFS) Rename(oldname, newname string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Ops = append(f.Ops, "rename "+oldname+" -> "+newname)
	if err := f.fail("rename", newname); err != nil {
		return err
	}
	oldname, newname = path.Clean(oldname), path.Clean(newname)
	data, ok := f.files[oldname]
	if !ok {
		return os.ErrNotExist
	}
	delete(f.files, oldname)
	f.files[newname] = data
	return nil
}

func (f *MemFS) Remove(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Ops = append(f.Ops, "remove "+name)
	if err := f.fail("remove", name); err != nil {
		return err
	}
	name = path.Clean(name)
	if _, ok := f.files[name]; !ok {
		return os.ErrNotExist
	}
	delete(f.files, name)
	return nil
}

func (f *MemFS) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

// Closed reports whether Close was called (lets tests assert the engine
// releases the connection).
func (f *MemFS) Closed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

// memWriter buffers bytes and commits them to the MemFS on Close, matching
// how SFTP writes materialize on the wire.
type memWriter struct {
	fs     *MemFS
	name   string
	buf    bytes.Buffer
	closed bool
}

func (w *memWriter) Write(p []byte) (int, error) { return w.buf.Write(p) }

func (w *memWriter) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	w.fs.mu.Lock()
	defer w.fs.mu.Unlock()
	w.fs.setLocked(w.name, w.buf.Bytes())
	w.fs.Written = append(w.fs.Written, w.name)
	return nil
}

type fileInfo struct {
	name string
	size int64
	dir  bool
}

func (i fileInfo) Name() string { return i.name }
func (i fileInfo) Size() int64  { return i.size }
func (i fileInfo) Mode() os.FileMode {
	if i.dir {
		return os.ModeDir | 0o755
	}
	return 0o600
}
func (i fileInfo) ModTime() time.Time { return time.Unix(0, 0) }
func (i fileInfo) IsDir() bool        { return i.dir }
func (i fileInfo) Sys() any           { return nil }

// compile-time check that fileInfo satisfies os.FileInfo and MemFS exposes
// the full surface callers need.
var _ os.FileInfo = fileInfo{}
var _ interface {
	Stat(string) (os.FileInfo, error)
	ReadDir(string) ([]os.FileInfo, error)
	Open(string) (io.ReadCloser, error)
	Create(string) (io.WriteCloser, error)
	ReadFile(string) ([]byte, error)
	MkdirAll(string) error
	Rename(string, string) error
	Remove(string) error
	Close() error
} = (*MemFS)(nil)
