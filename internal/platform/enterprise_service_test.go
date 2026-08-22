package platform

import "testing"

func TestValidEnterpriseCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		code  string
		valid bool
	}{
		{name: "numeric", code: "123456", valid: true},
		{name: "letters", code: "argus", valid: true},
		{name: "separated", code: "argus-2026", valid: true},
		{name: "single character", code: "1", valid: true},
		{name: "empty", code: "", valid: false},
		{name: "uppercase", code: "Argus", valid: false},
		{name: "leading dash", code: "-argus", valid: false},
		{name: "trailing dash", code: "argus-", valid: false},
		{name: "consecutive dashes", code: "argus--test", valid: false},
		{name: "too long", code: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", valid: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := validEnterpriseCode(test.code); got != test.valid {
				t.Fatalf("validEnterpriseCode(%q) = %v, want %v", test.code, got, test.valid)
			}
		})
	}
}
