package domainembedding

import (
	"errors"
	"testing"
)

func TestBuildValidatedVideoTextPreservesHashInputAndRejectsControls(t *testing.T) {
	text, err := BuildValidatedVideoText(" title ", " description ")
	if err != nil || text != "title\ndescription" {
		t.Fatalf("text=%q err=%v", text, err)
	}
	if _, err := BuildValidatedVideoText("title\x00", "description"); !errors.Is(
		err, ErrInvalidHashText,
	) {
		t.Fatalf("control text error=%v", err)
	}
}
