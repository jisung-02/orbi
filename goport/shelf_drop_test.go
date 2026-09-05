package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestShelfDropCommandImportsFilePathsAndPublishesList(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "선반 테스트.txt")
	if err := os.WriteFile(path, []byte("drop fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	events := &recordingEvents{}
	st := newState(&memoryConfigStore{cfg: defaultConfig()}, events, "")
	payload, _ := json.Marshal(map[string]any{"paths": []string{path}})
	var args Args
	if err := json.Unmarshal(payload, &args); err != nil {
		t.Fatal(err)
	}
	result, err := cmdShelfAdd(st, args, -1)
	if err != nil {
		t.Fatal(err)
	}
	list := result.([]ShelfItem)
	if len(list) != 1 || list[0].Path != path {
		t.Fatalf("drop result: %+v", list)
	}
	last := events.events[len(events.events)-1]
	if last.name != "shelf" || len(last.payload.([]ShelfItem)) != 1 {
		t.Fatal("drop did not publish visible shelf list")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("adding to shelf changed the source file")
	}
	if _, err := cmdShelfAdd(st, args, -1); err != nil || len(shelfList(st)) != 1 {
		t.Fatal("repeated drop duplicated the file")
	}
}

func TestShelfWindowStaysBelowDragImage(t *testing.T) {
	if level := popoverLevelFor("shelf"); level <= 0 || level >= 500 {
		t.Fatalf("drop target level: %d", level)
	}
	if popoverLevelFor("timer") != popoverLevel {
		t.Fatal("unrelated panel level changed")
	}
}
