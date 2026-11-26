package diff

import "testing"

const sample = `diff --git a/a.txt b/a.txt
index 1..2 100644
--- a/a.txt
+++ b/a.txt
@@ -1,6 +1,7 @@ func top()
 one
-two
-three
+TWO
+THREE
+three and a half
 four
 five
 six
diff --git a/old.go b/new.go
similarity index 90%
rename from old.go
rename to new.go
--- a/old.go
+++ b/new.go
@@ -10,3 +10,3 @@
 x
-y
+z
 w
diff --git a/gone.txt b/gone.txt
deleted file mode 100644
--- a/gone.txt
+++ /dev/null
@@ -1,2 +0,0 @@
-bye
-now
`

func TestParse(t *testing.T) {
	files := Parse(sample)
	if len(files) != 3 {
		t.Fatalf("files = %d", len(files))
	}
	a := files[0]
	if a.Path != "a.txt" || a.Additions != 3 || a.Deletions != 2 || a.Hunks[0].Header != "func top()" {
		t.Errorf("a.txt parsed wrong: %+v", a)
	}
	// New-side numbering: one=1, TWO=2, THREE=3, half=4, four=5.
	if l := a.Hunks[0].Lines[5]; l.Kind != "+" || l.New != 4 {
		t.Errorf("line numbering is off: %+v", l)
	}
	if r := files[1]; r.Status != "renamed" || r.Path != "new.go" || r.OldPath != "old.go" {
		t.Errorf("rename parsed wrong: %+v", r)
	}
	if d := files[2]; d.Status != "deleted" || d.Path != "gone.txt" || d.Deletions != 2 {
		t.Errorf("deletion parsed wrong: %+v", d)
	}
	if _, ok := Find(files, "old.go"); !ok {
		t.Error("a renamed file should be found by its old path too")
	}
}

func TestSliceKeepsOnlyTheRangeAndItsContext(t *testing.T) {
	a := Parse(sample)[0]
	hs := Slice(a, [][2]int{{4, 4}}, 1)
	if len(hs) != 1 {
		t.Fatalf("hunks = %d", len(hs))
	}
	var got string
	for _, l := range hs[0].Lines {
		got += l.Kind + l.Text + "|"
	}
	if got != "+THREE|+three and a half| four|" {
		t.Errorf("slice = %q", got)
	}
	// Removed lines belong to the position of the next new line, so a range
	// over the replacement shows what it replaced.
	if n := Changed(Slice(a, [][2]int{{2, 2}}, 0)); n != 3 {
		t.Errorf("changed lines at new line 2 = %d, want the 2 removals and 1 addition", n)
	}
	if !HasNewLine(a, 7) || HasNewLine(a, 8) {
		t.Error("HasNewLine should know the hunk's extent on the new side")
	}
}
