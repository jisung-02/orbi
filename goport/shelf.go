package main

// 파일 선반/드롭존 (Rust widgets/shelf.rs 포트)

/*
#include <stdio.h>
#include <stdlib.h>
*/
import "C"

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

type ShelfItem struct {
	Path   string  `json:"path"`
	Name   string  `json:"name"`
	SizeMb float64 `json:"size_mb"`
	IsDir  bool    `json:"is_dir"`
	Added  int64   `json:"added"`
}

const maxShelfItems = 30

func shelfAdd(st *AppState, paths []string) []ShelfItem {
	st.shelfMu.Lock()
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		dup := false
		for _, it := range st.shelf {
			if it.Path == p {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		meta, err := os.Stat(p)
		isDir := err == nil && meta.IsDir()
		var size int64
		if err == nil && !meta.IsDir() {
			size = meta.Size()
		}
		name := filepath.Base(p)
		st.shelf = append(st.shelf, ShelfItem{
			Path:   p,
			Name:   name,
			SizeMb: float64(size) / 1048576.0,
			IsDir:  isDir,
			Added:  nowMs(),
		})
	}
	for len(st.shelf) > maxShelfItems {
		st.shelf = st.shelf[1:]
	}
	out := make([]ShelfItem, len(st.shelf))
	copy(out, st.shelf)
	st.shelfMu.Unlock()
	return out
}

func shelfList(st *AppState) []ShelfItem {
	st.shelfMu.Lock()
	defer st.shelfMu.Unlock()
	out := make([]ShelfItem, len(st.shelf))
	copy(out, st.shelf)
	return out
}

func shelfRemove(st *AppState, idx int) []ShelfItem {
	st.shelfMu.Lock()
	if idx >= 0 && idx < len(st.shelf) {
		st.shelf = append(st.shelf[:idx], st.shelf[idx+1:]...)
	}
	out := make([]ShelfItem, len(st.shelf))
	copy(out, st.shelf)
	st.shelfMu.Unlock()
	return out
}

func shelfClear(st *AppState) []ShelfItem {
	st.shelfMu.Lock()
	st.shelf = []ShelfItem{}
	st.shelfMu.Unlock()
	return []ShelfItem{}
}

// 파일 참조로 클립보드에 복사 (Finder에 붙여넣기 가능)
func shelfCopyRefs(st *AppState, idx int, hasIdx bool) (int, error) {
	st.shelfMu.Lock()
	var paths []string
	if hasIdx {
		if idx < 0 || idx >= len(st.shelf) {
			st.shelfMu.Unlock()
			return 0, fmt.Errorf("항목이 없습니다")
		}
		paths = []string{st.shelf[idx].Path}
	} else {
		for _, it := range st.shelf {
			paths = append(paths, it.Path)
		}
	}
	st.shelfMu.Unlock()
	if len(paths) == 0 {
		return 0, fmt.Errorf("클립보드 복사 실패")
	}
	if !CopyFileRefs(paths) {
		return 0, fmt.Errorf("클립보드 복사 실패")
	}
	return len(paths), nil
}

func shelfReveal(st *AppState, idx int) error {
	st.shelfMu.Lock()
	if idx < 0 || idx >= len(st.shelf) {
		st.shelfMu.Unlock()
		return fmt.Errorf("항목이 없습니다")
	}
	path := st.shelf[idx].Path
	st.shelfMu.Unlock()
	return execOpen("-R", path)
}

func shelfOpen(st *AppState, idx int) error {
	st.shelfMu.Lock()
	if idx < 0 || idx >= len(st.shelf) {
		st.shelfMu.Unlock()
		return fmt.Errorf("항목이 없습니다")
	}
	path := st.shelf[idx].Path
	st.shelfMu.Unlock()
	return execOpen(path)
}

func shelfOpenPath(path string) error {
	expanded := path
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			expanded = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path, "~"), "/"))
		}
	}
	if _, err := os.Stat(expanded); err == nil {
		return execOpen(expanded)
	}
	return fmt.Errorf("경로가 존재하지 않습니다: %s", expanded)
}

func pickFolder(title string) (string, error) {
	folderMu.Lock()
	folderSeq++
	id := folderSeq
	ch := make(chan string, 1)
	folderRes[id] = ch
	folderMu.Unlock()

	defer func() {
		folderMu.Lock()
		delete(folderRes, id)
		folderMu.Unlock()
	}()

	PickFolder(id, title)

	select {
	case path := <-ch:
		if path == "" {
			return "", fmt.Errorf("폴더가 선택되지 않았습니다")
		}
		return path, nil
	case <-time.After(10 * time.Minute):
		return "", fmt.Errorf("폴더 선택 취소")
	}
}

// 선반의 모든 파일(또는 한 개)을 선택한 폴더로 이동. 반환: (성공, 실패)
func shelfMoveTo(st *AppState, idx int, hasIdx bool) (int, int, error) {
	dir, err := pickFolder("이동할 폴더 선택")
	if err != nil {
		return 0, 0, err
	}
	st.shelfMu.Lock()
	type item struct{ path, name string }
	var items []item
	if hasIdx {
		if idx < 0 || idx >= len(st.shelf) {
			st.shelfMu.Unlock()
			return 0, 0, fmt.Errorf("항목이 없습니다")
		}
		items = []item{{st.shelf[idx].Path, st.shelf[idx].Name}}
	} else {
		for _, it := range st.shelf {
			items = append(items, item{it.Path, it.Name})
		}
	}
	st.shelfMu.Unlock()

	ok, fail := 0, 0
	movedPaths := make(map[string]bool)
	for _, it := range items {
		dst := filepath.Join(dir, it.name)
		if moveFile(it.path, dst) == nil {
			ok++
			movedPaths[it.path] = true
		} else {
			fail++
		}
	}
	st.shelfMu.Lock()
	kept := st.shelf[:0]
	for _, it := range st.shelf {
		if !movedPaths[it.Path] {
			kept = append(kept, it)
		}
	}
	st.shelf = kept
	st.shelfMu.Unlock()
	return ok, fail, nil
}

func moveFile(src, dst string) error {
	from, to := C.CString(src), C.CString(dst)
	defer C.free(unsafe.Pointer(from))
	defer C.free(unsafe.Pointer(to))
	// 동명 파일을 덮어쓰지 않는 원자적 이동.
	if result, err := C.renamex_np(from, to, C.RENAME_EXCL); result == 0 {
		return nil
	} else if !errors.Is(err, syscall.EXDEV) {
		return err
	}

	// 장치 경계에서 일반 파일만 복사한다. 실패 시 원본은 보존한다.
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("다른 볼륨으로 폴더를 이동할 수 없습니다")
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		_ = os.Remove(dst)
		return err
	}
	return os.Remove(src)
}
