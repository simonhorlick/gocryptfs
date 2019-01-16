// +build linux

// Package fusefrontend interfaces directly with the go-fuse library.
package fusefrontend

import (
	"fmt"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/hanwen/go-fuse/fuse"

	"github.com/simonhorlick/gocryptfs/internal/syscallcompat"
)

// Only allow the "user" namespace, block "trusted" and "security", as
// these may be interpreted by the system, and we don't want to cause
// trouble with our encrypted garbage.
const xattrUserPrefix = "user."

func disallowedXAttrName(attr string) bool {
	return !strings.HasPrefix(attr, xattrUserPrefix)
}

func filterXattrSetFlags(flags int) int {
	return flags
}

func (fs *FS) getXAttr(relPath string, cAttr string, context *fuse.Context) ([]byte, fuse.Status) {
	dirfd, cName, err := fs.openBackingDir(relPath)
	if err != nil {
		return nil, fuse.ToStatus(err)
	}
	defer syscall.Close(dirfd)

	procPath := fmt.Sprintf("/proc/self/fd/%d/%s", dirfd, cName)
	cData, err := syscallcompat.Lgetxattr(procPath, cAttr)
	if err != nil {
		return nil, fuse.ToStatus(err)
	}
	return cData, fuse.OK
}

func (fs *FS) setXAttr(relPath string, cAttr string, cData []byte, flags int, context *fuse.Context) fuse.Status {
	dirfd, cName, err := fs.openBackingDir(relPath)
	if err != nil {
		return fuse.ToStatus(err)
	}
	defer syscall.Close(dirfd)

	procPath := fmt.Sprintf("/proc/self/fd/%d/%s", dirfd, cName)
	err = unix.Lsetxattr(procPath, cAttr, cData, flags)
	return fuse.ToStatus(err)
}

func (fs *FS) removeXAttr(relPath string, cAttr string, context *fuse.Context) fuse.Status {
	dirfd, cName, err := fs.openBackingDir(relPath)
	if err != nil {
		return fuse.ToStatus(err)
	}
	defer syscall.Close(dirfd)

	procPath := fmt.Sprintf("/proc/self/fd/%d/%s", dirfd, cName)
	err = unix.Lremovexattr(procPath, cAttr)
	return fuse.ToStatus(err)
}

func (fs *FS) listXAttr(relPath string, context *fuse.Context) ([]string, fuse.Status) {
	dirfd, cName, err := fs.openBackingDir(relPath)
	if err != nil {
		return nil, fuse.ToStatus(err)
	}
	defer syscall.Close(dirfd)

	procPath := fmt.Sprintf("/proc/self/fd/%d/%s", dirfd, cName)
	cNames, err := syscallcompat.Llistxattr(procPath)
	if err != nil {
		return nil, fuse.ToStatus(err)
	}
	return cNames, fuse.OK
}
