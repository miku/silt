package textutil

import "testing"

func TestIsAlphanumeric(t *testing.T) {
	for _, tc := range []struct {
		r    rune
		want bool
	}{
		{'a', true},
		{'Z', true},
		{'7', true},
		{'é', true},
		{'端', true},
		{'Ⅻ', true},      // Nl
		{'½', true},      // No
		{'ⓐ', true},      // So, but Other_Lowercase
		{'ͅ', true}, // Mn, but Other_Alphabetic
		{'-', false},
		{'#', false},
		{' ', false},
		{'́', false}, // combining acute: Mn, not alphabetic
	} {
		if got := IsAlphanumeric(tc.r); got != tc.want {
			t.Errorf("IsAlphanumeric(%q) = %v, want %v", tc.r, got, tc.want)
		}
	}
}

func TestToLower(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", ""},
		{"Hello, World", "hello, world"},
		{"CLI Tool", "cli tool"},
		{"İstanbul", "i̇stanbul"},
		{"ΣΑΣ", "σας"},
		{"ΟΔΟΣ ΣΟΦΙΑΣ", "οδος σοφιας"},
		{"Σ", "σ"},         // not preceded by a cased letter
		{"ΑΣ'", "ας'"},     // the apostrophe is case-ignorable
		{"ΑΣ'Α", "ασ'α"},   // ... and a cased letter follows it
		{"ΑΣ́", "ας́"},
	} {
		if got := ToLower(tc.in); got != tc.want {
			t.Errorf("ToLower(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
