package targets

import (
	"os"
	"strings"
	"testing"
)

func TestLaunchdTargetAddListRemove(t *testing.T) {
	// Create a temp directory for home
	tmpHome, err := os.MkdirTemp("", "envset_launchd_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpHome)

	lt, err := NewLaunchdTarget(tmpHome)
	if err != nil {
		t.Fatalf("failed to create launchd target: %v", err)
	}

	// 1. Initially it should not exist
	if lt.Exists() {
		t.Fatal("expected target to not exist initially")
	}

	// 2. Add export
	if _, err := lt.Add("TEST_VAR", "my val \"quote\"", VarTypeExport, WriteOptions{}); err != nil {
		t.Fatalf("add failed: %v", err)
	}

	// 3. Should now exist
	if !lt.Exists() {
		t.Fatal("expected target to exist after add")
	}

	// 4. List it
	entries, err := lt.List("TEST_VAR")
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Key != "TEST_VAR" || entries[0].Value != "my val \"quote\"" {
		t.Fatalf("unexpected entry: %+v", entries[0])
	}
	if entries[0].IsComment {
		t.Fatal("expected active entry, got comment")
	}

	// 5. Verify the script content
	scriptData, err := os.ReadFile(lt.Path())
	if err != nil {
		t.Fatal(err)
	}
	scriptStr := string(scriptData)
	if !strings.Contains(scriptStr, "#!/bin/sh") {
		t.Errorf("expected script header, got: %q", scriptStr)
	}
	if !strings.Contains(scriptStr, "launchctl setenv TEST_VAR \"my val \\\"quote\\\"\"") {
		t.Errorf("expected script to set variable correctly, got: %q", scriptStr)
	}

	// 6. Verify plist exists and contains correct path
	plistData, err := os.ReadFile(lt.plistPath)
	if err != nil {
		t.Fatal(err)
	}
	plistStr := string(plistData)
	if !strings.Contains(plistStr, lt.Path()) {
		t.Errorf("expected plist to point to %s, got: %s", lt.Path(), plistStr)
	}

	// 7. Comment out
	if _, err := lt.Remove("TEST_VAR", RemoveOptions{Comment: true}); err != nil {
		t.Fatalf("comment out failed: %v", err)
	}

	entries, err = lt.List("TEST_VAR")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if !entries[0].IsComment {
		t.Fatal("expected commented entry")
	}

	// 8. Delete permanently
	if _, err := lt.Remove("TEST_VAR", RemoveOptions{Comment: false}); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	entries, err = lt.List("TEST_VAR")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries after deletion, got %d", len(entries))
	}
}

func TestLaunchdTargetPathAppendPrepend(t *testing.T) {
	tmpHome, err := os.MkdirTemp("", "envset_launchd_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpHome)

	lt, _ := NewLaunchdTarget(tmpHome)

	// Append
	if _, err := lt.Add("PATH", "/my/append/path", VarTypePathAppend, WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	// Prepend
	if _, err := lt.Add("PATH", "/my/prepend/path", VarTypePathPrepend, WriteOptions{}); err != nil {
		t.Fatal(err)
	}

	scriptData, _ := os.ReadFile(lt.Path())
	scriptStr := string(scriptData)
	if !strings.Contains(scriptStr, "launchctl setenv PATH \"/my/prepend/path:$PATH\"") {
		t.Errorf("expected path prepend, got: %q", scriptStr)
	}

	entries, err := lt.List("PATH")
	if err != nil {
		t.Fatal(err)
	}
	// Since both append/prepend targeted PATH, the last one overwrote it in the file.
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry for PATH, got %d", len(entries))
	}
	if entries[0].Value != "/my/prepend/path" || entries[0].VarType != VarTypePathPrepend {
		t.Fatalf("unexpected entry: %+v", entries[0])
	}
}

func TestLaunchdTargetPreview(t *testing.T) {
	lt := &LaunchdTarget{}

	p, err := lt.Preview("MY_VAR", "val", VarTypeExport, QuoteExpand)
	if err != nil {
		t.Fatal(err)
	}
	if p != `launchctl setenv MY_VAR "val"` {
		t.Errorf("unexpected preview: %s", p)
	}

	p, err = lt.Preview("PATH", "/my/path", VarTypePathAppend, QuoteExpand)
	if err != nil {
		t.Fatal(err)
	}
	if p != `launchctl setenv PATH "$PATH:/my/path"` {
		t.Errorf("unexpected path append preview: %s", p)
	}
}
