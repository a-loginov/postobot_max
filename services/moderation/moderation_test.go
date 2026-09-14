package moderation

import (
	"testing"
)

func TestNormalize(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"  Нужно заменить бутылку  воды!  ", "нужно заменить бутылку воды"},
		{"Déjà Vu: «Тест»", "déjà vu тест"},
		{"Привет! Как дела??", "привет как дела"},
	}
	for _, c := range cases {
		got := Normalize(c.in)
		if got != c.want {
			t.Errorf("Normalize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSimilarity(t *testing.T) {
	zero := Similarity("привет", "")
	ident := Similarity("привет", "привет")
	sim := Similarity("замени бутылку воды", "замени бутылку воды пожалуйста")
	diff := Similarity("замени бутылку воды", "почини стул")

	if zero != 0 {
		t.Errorf("Similarity with empty: %v, want 0", zero)
	}
	if ident != 1 {
		t.Errorf("Similarity identical: %v, want 1", ident)
	}
	if sim < 0.8 {
		t.Errorf("Similarity similar: %v, want >= 0.8", sim)
	}
	if diff > 0.5 {
		t.Errorf("Similarity different: %v, want < 0.5", diff)
	}
}

func TestCheckMat(t *testing.T) {
	m := New([]string{"хуй", "пизда"})
	res := m.Check("нужно заменить хуй")
	if !res.HasMat {
		t.Error("expected HasMat=true")
	}
	if res.Clean {
		t.Error("expected Clean=false")
	}

	res2 := m.Check("нужно заменить стул")
	if res2.HasMat {
		t.Error("expected HasMat=false for clean text")
	}
	if !res2.Clean {
		t.Error("expected Clean=true")
	}
}

func TestCheckDuplicate(t *testing.T) {
	m := New([]string{})
	res := m.Check("замени бутылку воды пожалуйста")
	res2 := m.Check("замени бутылку воды, пожалуйста")
	// After normalization these should be identical.
	if res.Text != res2.Text {
		t.Errorf("normalized texts differ: %q vs %q", res.Text, res2.Text)
	}
}

func TestIsSimilar(t *testing.T) {
	m := New([]string{})
	if !m.IsSimilar("замени бутылку воды пожалуйста", "замени бутылку воды") {
		t.Error("expected similar")
	}
	if m.IsSimilar("замени бутылку воды", "почини стул") {
		t.Error("expected not similar")
	}
}
