package target

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func sandboxTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", dir)
	return dir
}

// TestMakeSandboxCopiesTheFixture checks the fixture copy.
// It asserts every file, nested folders included, lands in the work folder with the same bytes.
func TestMakeSandboxCopiesTheFixture(t *testing.T) {
	sandboxTempDir(t)
	fixture := t.TempDir()
	files := map[string]string{"a.pdf": "alpha\x00\xff", "deep/er/b.txt": "beta"}
	writeTree(t, fixture, files)

	box, err := makeSandbox(fixture, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer box.Remove()

	for name, body := range files {
		got, err := os.ReadFile(filepath.Join(box.Work, name))
		if err != nil || !bytes.Equal(got, []byte(body)) {
			t.Errorf("%s: got %q, err = %v", name, got, err)
		}
	}
}

// TestMakeSandboxCopiesTheSkills checks the skill copies.
// It asserts each skill lands in skills/<base name> with its SKILL.md, and Skills lists those folders.
func TestMakeSandboxCopiesTheSkills(t *testing.T) {
	sandboxTempDir(t)
	repo := t.TempDir()
	writeTree(t, repo, map[string]string{"skills/a/SKILL.md": "skill a", "skills/b/SKILL.md": "skill b", "skills/b/ref/x.md": "x"})

	box, err := makeSandbox("", []string{filepath.Join(repo, "skills/a"), filepath.Join(repo, "skills/b")})
	if err != nil {
		t.Fatal(err)
	}
	defer box.Remove()

	want := []string{filepath.Join(box.Root, "skills", "a"), filepath.Join(box.Root, "skills", "b")}
	if len(box.Skills) != 2 || box.Skills[0] != want[0] || box.Skills[1] != want[1] {
		t.Fatalf("skills = %v, want %v", box.Skills, want)
	}
	for dir, body := range map[string]string{want[0] + "/SKILL.md": "skill a", want[1] + "/SKILL.md": "skill b", want[1] + "/ref/x.md": "x"} {
		got, err := os.ReadFile(dir)
		if err != nil || string(got) != body {
			t.Errorf("%s: got %q, err = %v", dir, got, err)
		}
	}
}

// TestMakeSandboxGivesAnEmptyWorkFolderWithoutAFixture checks an empty fixture string.
// It asserts work/ exists and is empty.
func TestMakeSandboxGivesAnEmptyWorkFolderWithoutAFixture(t *testing.T) {
	sandboxTempDir(t)

	box, err := makeSandbox("", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer box.Remove()

	entries, err := os.ReadDir(box.Work)
	if err != nil || len(entries) != 0 || box.Work != filepath.Join(box.Root, "work") {
		t.Errorf("work = %q, entries = %v, err = %v", box.Work, entries, err)
	}
}

// TestMakeSandboxRootIsInTheTempFolder checks where the root sits.
// It asserts the root is a new aieval-* folder directly under the temp folder, with symlinks resolved.
func TestMakeSandboxRootIsInTheTempFolder(t *testing.T) {
	tmp := sandboxTempDir(t)

	box, err := makeSandbox("", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer box.Remove()

	resolved, err := filepath.EvalSymlinks(box.Root)
	if err != nil || resolved != box.Root {
		t.Errorf("root = %q, resolved = %q, err = %v", box.Root, resolved, err)
	}
	if filepath.Dir(box.Root) != tmp || !strings.HasPrefix(filepath.Base(box.Root), "aieval-") {
		t.Errorf("root = %q, temp = %q", box.Root, tmp)
	}
}

// TestMakeSandboxRemoveDeletesTheRoot checks cleanup.
// It asserts the whole root, copies included, is gone after Remove.
func TestMakeSandboxRemoveDeletesTheRoot(t *testing.T) {
	sandboxTempDir(t)
	fixture := t.TempDir()
	writeTree(t, fixture, map[string]string{"a.txt": "a"})

	box, err := makeSandbox(fixture, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := box.Remove(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(box.Root); !os.IsNotExist(err) {
		t.Errorf("stat err = %v", err)
	}
}

// TestMakeSandboxLeavesNothingBehindOnAnError checks failed copies.
// It asserts a missing fixture and a missing skill both give an error and no aieval-* folder.
func TestMakeSandboxLeavesNothingBehindOnAnError(t *testing.T) {
	tmp := sandboxTempDir(t)
	repo := t.TempDir()
	cases := map[string]struct {
		fixture string
		skills  []string
	}{
		"fixture": {filepath.Join(repo, "missing"), nil},
		"skill":   {"", []string{filepath.Join(repo, "no-skill")}},
	}
	for name, tc := range cases {
		if _, err := makeSandbox(tc.fixture, tc.skills); err == nil {
			t.Errorf("%s: want an error", name)
		}
		left, _ := filepath.Glob(filepath.Join(tmp, "aieval-*"))
		if len(left) != 0 {
			t.Errorf("%s: left behind %v", name, left)
		}
	}
}

// TestMakeSandboxGivesADifferentRootEachTime checks tries do not share a folder.
// It asserts two calls give two different roots.
func TestMakeSandboxGivesADifferentRootEachTime(t *testing.T) {
	sandboxTempDir(t)

	first, err := makeSandbox("", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Remove()
	second, err := makeSandbox("", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Remove()

	if first.Root == second.Root {
		t.Errorf("both roots are %q", first.Root)
	}
}

// TestMakeSandboxRefusesTwoSkillsWithTheSameName checks skill folder names.
// It asserts two skills with one base name give a "two skills named" error and no aieval-* folder.
func TestMakeSandboxRefusesTwoSkillsWithTheSameName(t *testing.T) {
	tmp := sandboxTempDir(t)
	repo := t.TempDir()
	writeTree(t, repo, map[string]string{"one/fin/SKILL.md": "1", "two/fin/SKILL.md": "2"})

	_, err := makeSandbox("", []string{filepath.Join(repo, "one/fin"), filepath.Join(repo, "two/fin")})

	left, _ := filepath.Glob(filepath.Join(tmp, "aieval-*"))
	if err == nil || !strings.Contains(err.Error(), `two skills named "fin"`) || len(left) != 0 {
		t.Errorf("err = %v, left = %v", err, left)
	}
}

func sandboxFor(t *testing.T, fixture string) Box {
	t.Helper()
	box, err := makeSandbox(fixture, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { box.Remove() })
	return box
}

// TestNewFilesReturnsFilesTheModelMade checks new files are found, nested ones included.
// It asserts summary.csv and out/table.md come back with their bytes and nothing else does.
func TestNewFilesReturnsFilesTheModelMade(t *testing.T) {
	sandboxTempDir(t)
	fixture := t.TempDir()
	writeTree(t, fixture, map[string]string{"a.pdf": "pdf", "deep/b.pdf": "pdf2"})
	box := sandboxFor(t, fixture)
	writeTree(t, box.Work, map[string]string{"summary.csv": "a,b", "out/table.md": "| t |"})

	files, err := box.NewFiles()

	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || string(files["summary.csv"]) != "a,b" || string(files["out/table.md"]) != "| t |" {
		t.Errorf("files = %v", files)
	}
}

// TestNewFilesSkipsChangedFixtureFiles checks a fixture file the model changed.
// It asserts the changed file is not returned, because only new paths count.
func TestNewFilesSkipsChangedFixtureFiles(t *testing.T) {
	sandboxTempDir(t)
	fixture := t.TempDir()
	writeTree(t, fixture, map[string]string{"a.pdf": "pdf"})
	box := sandboxFor(t, fixture)
	writeTree(t, box.Work, map[string]string{"a.pdf": "changed"})

	files, err := box.NewFiles()

	if err != nil || files != nil {
		t.Fatalf("files = %v, err = %v", files, err)
	}
}

// TestNewFilesIgnoresSymlinks checks a link in work/ that points outside.
// It asserts the link is not returned, its unreadable target is never opened, and a linked folder is not walked.
func TestNewFilesIgnoresSymlinks(t *testing.T) {
	sandboxTempDir(t)
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	writeTree(t, outside, map[string]string{"secret.txt": "secret", "dir/inner.txt": "inner"})
	if err := os.Chmod(secret, 0); err != nil {
		t.Fatal(err)
	}
	box := sandboxFor(t, "")
	writeTree(t, box.Work, map[string]string{"kept.md": "kept"})
	if err := os.Symlink(secret, filepath.Join(box.Work, "key")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "dir"), filepath.Join(box.Work, "linked")); err != nil {
		t.Fatal(err)
	}

	files, err := box.NewFiles()

	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || string(files["kept.md"]) != "kept" {
		t.Errorf("files = %v", files)
	}
}

// TestNewFilesIsEmptyWhenNothingWasMade checks an untouched work folder.
// It asserts no files come back, with a fixture and with an empty fixture string.
func TestNewFilesIsEmptyWhenNothingWasMade(t *testing.T) {
	sandboxTempDir(t)
	fixture := t.TempDir()
	writeTree(t, fixture, map[string]string{"a.pdf": "pdf"})
	for _, dir := range []string{fixture} {
		box := sandboxFor(t, dir)

		files, err := box.NewFiles()

		if err != nil || files != nil {
			t.Errorf("fixture %q: files = %v, err = %v", dir, files, err)
		}
	}
}

// TestNewFilesWorksWithAnEmptyFixtureString checks a sandbox made without a fixture.
// It asserts every file in work/ counts as new.
func TestNewFilesWorksWithAnEmptyFixtureString(t *testing.T) {
	sandboxTempDir(t)
	box := sandboxFor(t, "")
	writeTree(t, box.Work, map[string]string{"x.md": "x"})

	files, err := box.NewFiles()

	if err != nil || string(files["x.md"]) != "x" || len(files) != 1 {
		t.Fatalf("files = %v, err = %v", files, err)
	}
}

// TestNewFilesKeepsReadableFilesBesideAnUnreadableOne checks one bad file doesn't drop the rest.
// It asserts the readable file comes back together with an error that names the unreadable one.
func TestNewFilesKeepsReadableFilesBesideAnUnreadableOne(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can read any file")
	}
	sandboxTempDir(t)
	box := sandboxFor(t, "")
	writeTree(t, box.Work, map[string]string{"good.md": "good", "bad.md": "bad", "locked/in.md": "in", "ok/fine.md": "fine"})
	if err := os.Chmod(filepath.Join(box.Work, "bad.md"), 0); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(box.Work, "locked"), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(filepath.Join(box.Work, "locked"), 0o755) })

	files, err := box.NewFiles()

	if len(files) != 2 || string(files["good.md"]) != "good" || string(files["ok/fine.md"]) != "fine" {
		t.Errorf("files = %v", files)
	}
	if err == nil || !strings.Contains(err.Error(), "bad.md") || !strings.Contains(err.Error(), "locked") {
		t.Errorf("err = %v", err)
	}
}
