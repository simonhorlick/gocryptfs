package fusefrontend

import (
	"fmt"
	"log"
	"sync"
	"syscall"
	"time"

	"github.com/simonhorlick/gocryptfs/internal/nametransform"
	"github.com/simonhorlick/gocryptfs/internal/tlog"
)

const (
	// Number of entries in the dirCache. Three entries work well for two
	// (probably also three) parallel tar extracts (hit rate around 92%).
	// Keep in sync with test_helpers.maxCacheFds !
	// TODO: How to share this constant without causing an import cycle?
	dirCacheSize = 3
	// Enable Lookup/Store/Clear debug messages
	enableDebugMessages = false
	// Enable hit rate statistics printing
	enableStats = false
)

type dirCacheEntryStruct struct {
	// relative plaintext path to the directory
	dirRelPath string
	// fd to the directory (opened with O_PATH!)
	fd int
	// content of gocryptfs.diriv in this directory
	iv []byte
}

func (e *dirCacheEntryStruct) Clear() {
	// An earlier clear may have already closed the fd, or the cache
	// has never been filled (fd is 0 in that case).
	// Note: package ensurefds012, imported from main, guarantees that dirCache
	// can never get fds 0,1,2.
	if e.fd > 0 {
		err := syscall.Close(e.fd)
		if err != nil {
			tlog.Warn.Printf("dirCache.Clear: Close failed: %v", err)
		}
	}
	e.fd = -1
	e.dirRelPath = ""
	e.iv = nil
}

type dirCacheStruct struct {
	sync.Mutex
	// Cache entries
	entries [dirCacheSize]dirCacheEntryStruct
	// Where to store the next entry (index into entries)
	nextIndex int
	// On the first Lookup(), the expire thread is started, and this flag is set
	// to true.
	expireThreadRunning bool
	// Hit rate stats. Evaluated and reset by the expire thread.
	lookups uint64
	hits    uint64
}

// Clear clears the cache contents.
func (d *dirCacheStruct) Clear() {
	d.Lock()
	defer d.Unlock()
	for i := range d.entries {
		d.entries[i].Clear()
	}
}

// Store the entry in the cache. The passed "fd" will be Dup()ed, and the caller
// can close their copy at will.
func (d *dirCacheStruct) Store(dirRelPath string, fd int, iv []byte) {
	// Note: package ensurefds012, imported from main, guarantees that dirCache
	// can never get fds 0,1,2.
	if fd <= 0 || len(iv) != nametransform.DirIVLen {
		log.Panicf("Store sanity check failed: fd=%d len=%d", fd, len(iv))
	}
	d.Lock()
	defer d.Unlock()
	e := &d.entries[d.nextIndex]
	// Round-robin works well enough
	d.nextIndex = (d.nextIndex + 1) % dirCacheSize
	// Close the old fd
	e.Clear()
	fd2, err := syscall.Dup(fd)
	if err != nil {
		tlog.Warn.Printf("dirCache.Store: Dup failed: %v", err)
		return
	}
	d.dbg("Store: %q %d %x\n", dirRelPath, fd2, iv)
	e.fd = fd2
	e.dirRelPath = dirRelPath
	e.iv = iv
	// expireThread is started on the first Lookup()
	if !d.expireThreadRunning {
		d.expireThreadRunning = true
		go d.expireThread()
	}
}

// Lookup checks if relPath is in the cache, and returns an (fd, iv) pair.
// It returns (-1, nil) if not found. The fd is internally Dup()ed and the
// caller must close it when done.
func (d *dirCacheStruct) Lookup(dirRelPath string) (fd int, iv []byte) {
	d.Lock()
	defer d.Unlock()
	if enableStats {
		d.lookups++
	}
	for i := range d.entries {
		e := &d.entries[i]
		if e.fd <= 0 {
			// Cache slot is empty
			continue
		}
		if dirRelPath != e.dirRelPath {
			// Not the right path
			continue
		}
		var err error
		fd, err = syscall.Dup(e.fd)
		if err != nil {
			tlog.Warn.Printf("dirCache.Lookup: Dup failed: %v", err)
			return -1, nil
		}
		iv = e.iv
		break
	}
	if fd == 0 {
		d.dbg("Lookup %q: miss\n", dirRelPath)
		return -1, nil
	}
	if enableStats {
		d.hits++
	}
	if fd <= 0 || len(iv) != nametransform.DirIVLen {
		log.Panicf("Lookup sanity check failed: fd=%d len=%d", fd, len(iv))
	}
	d.dbg("Lookup %q: hit %d %x\n", dirRelPath, fd, iv)
	return fd, iv
}

// expireThread is started on the first Lookup()
func (d *dirCacheStruct) expireThread() {
	for {
		time.Sleep(1 * time.Second)
		d.Clear()
		if enableStats {
			d.Lock()
			lookups := d.lookups
			hits := d.hits
			d.lookups = 0
			d.hits = 0
			d.Unlock()
			if lookups > 0 {
				fmt.Printf("dirCache: hits=%3d lookups=%3d, rate=%3d%%\n", hits, lookups, (hits*100)/lookups)
			}
		}
	}
}

// dbg prints a debug message. Usually disabled.
func (d *dirCacheStruct) dbg(format string, a ...interface{}) {
	if enableDebugMessages {
		fmt.Printf(format, a...)
	}
}
