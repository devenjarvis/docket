package lens

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenSeedsTheBuiltInsOnce(t *testing.T) {
	home := t.TempDir()
	s, err := Open(home)
	if err != nil {
		t.Fatal(err)
	}
	all, _ := s.List()
	if len(all) != len(order) || all[0].ID != "correctness" {
		t.Fatalf("seeded %d lenses, first %q", len(all), all[0].ID)
	}
	on, _ := s.Active()
	if len(on) != len(order)-1 {
		t.Errorf("%d lenses on; performance ships off", len(on))
	}
	// A deleted built-in stays deleted when Docket starts again.
	if err := s.Delete("security"); err != nil {
		t.Fatal(err)
	}
	s, _ = Open(home)
	if _, ok := s.Get("security"); ok {
		t.Error("a deleted built-in came back on its own")
	}
	if err := s.Restore(); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("security"); !ok {
		t.Error("Restore did not bring it back")
	}
}

func TestEditResetAndCustomLenses(t *testing.T) {
	s, _ := Open(t.TempDir())
	l, _ := s.Get("tests")
	l.Text, l.On = "Only look at integration tests.", false
	if _, err := s.Save(l); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get("tests")
	if !got.Edited || got.On || got.Text != "Only look at integration tests." {
		t.Errorf("after edit: %+v", got)
	}
	got, err := s.Reset("tests")
	if err != nil || got.Edited || got.On || !strings.Contains(got.Text, "would still pass") {
		t.Errorf("after reset (keeps off): %+v %v", got, err)
	}

	c, err := s.Save(Lens{Name: "Accessibility & i18n", Text: "Look for missing labels.", On: true})
	if err != nil || c.ID != "accessibility-i18n" || c.Builtin {
		t.Fatalf("custom lens: %+v %v", c, err)
	}
	all, _ := s.List()
	if all[len(all)-1].ID != "accessibility-i18n" {
		t.Errorf("custom lenses list after the built-ins, got last %q", all[len(all)-1].ID)
	}
	if _, err := s.Reset("accessibility-i18n"); err == nil {
		t.Error("reset a lens Docket does not ship")
	}
	if _, err := s.Save(Lens{Name: "!!!", Text: "x"}); err == nil {
		t.Error("saved a lens with no usable id")
	}
	if _, err := s.Save(Lens{Name: "Big", Text: strings.Repeat("x", MaxText+1)}); err == nil {
		t.Error("saved a lens over the limit")
	}
}

func TestHandWrittenFiles(t *testing.T) {
	home := t.TempDir()
	s, _ := Open(home)
	// No front matter: the id is the name, and it is on.
	if err := os.WriteFile(filepath.Join(s.Dir(), "docs.md"), []byte("Check the docs changed with the code.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Front matter turning it off.
	if err := os.WriteFile(filepath.Join(s.Dir(), "i18n.md"), []byte("---\r\nname: Translations\r\non: no\r\n---\r\nStrings.\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Not a lens.
	_ = os.WriteFile(filepath.Join(s.Dir(), "README.txt"), []byte("hi"), 0o644)
	_ = os.WriteFile(filepath.Join(s.Dir(), "Bad Name.md"), []byte("hi"), 0o644)
	d, ok := s.Get("docs")
	if !ok || !d.On || d.Name != "docs" || d.Text != "Check the docs changed with the code." {
		t.Errorf("docs = %+v", d)
	}
	i, ok := s.Get("i18n")
	if !ok || i.On || i.Name != "Translations" || i.Text != "Strings." {
		t.Errorf("i18n = %+v", i)
	}
	all, _ := s.List()
	if len(all) != len(order)+2 {
		t.Errorf("listed %d lenses", len(all))
	}
}
