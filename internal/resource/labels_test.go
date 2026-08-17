package resource

import (
	"bytes"
	"errors"
	"testing"
)

func TestNormalizeUserLabelsIsDeterministic(t *testing.T) {
	first, firstHash, err := NormalizeUserLabels(map[string]string{"zone": "us-east-1", "env": "prod"})
	if err != nil {
		t.Fatal(err)
	}
	second, secondHash, err := NormalizeUserLabels(map[string]string{"env": "prod", "zone": "us-east-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) || !bytes.Equal(firstHash, secondHash) {
		t.Fatal("normalization depends on map iteration order")
	}
	if string(first) != `{"env":"prod","zone":"us-east-1"}` {
		t.Fatalf("unexpected normalized labels %s", first)
	}
}

func TestNormalizeUserLabelsRejectsReservedKeys(t *testing.T) {
	_, _, err := NormalizeUserLabels(map[string]string{"argus.io/source": "connector"})
	if !errors.Is(err, ErrInvalidLabels) {
		t.Fatalf("expected reserved label rejection, got %v", err)
	}
	if _, _, err := NormalizeStoredLabels(map[string]string{"argus.io/source": "connector"}); err != nil {
		t.Fatalf("server label should be accepted: %v", err)
	}
}

func TestNormalizeUserLabelsBoundaries(t *testing.T) {
	invalid := []map[string]string{
		{"UPPER": "value"},
		{"valid": "Upper"},
		{"valid/key": "value"},
		{"valid": "-value"},
	}
	for _, labels := range invalid {
		if _, _, err := NormalizeUserLabels(labels); !errors.Is(err, ErrInvalidLabels) {
			t.Fatalf("expected invalid labels for %#v, got %v", labels, err)
		}
	}
}
