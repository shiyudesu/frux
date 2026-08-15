package domainmedia

import "testing"

func TestPublicExposureKeyRoundTrip(t *testing.T) {
	variant := &MediaVariant{
		ID:                 42,
		ObjectKey:          "processed/7/v1/checksum/source.mp4",
		ExposureGeneration: "generation-a",
	}
	key, err := BuildPublicExposureKey(variant)
	if err != nil {
		t.Fatal(err)
	}
	if key != "media/v3/generation-a/42/source.mp4" {
		t.Fatalf("key = %q", key)
	}
	generation, variantID, filename, ok := ParsePublicExposureKey(key)
	if !ok || generation != "generation-a" || variantID != 42 || filename != "source.mp4" {
		t.Fatalf(
			"parsed generation=%q variant=%d filename=%q ok=%t",
			generation, variantID, filename, ok,
		)
	}
}

func TestParsePublicExposureKeyRejectsInvalidIdentity(t *testing.T) {
	for _, key := range []string{
		"media/v2/generation/42/source.mp4",
		"media/v3/generation/0/source.mp4",
		"media/v3/generation/42/..",
		"media/v3//42/source.mp4",
	} {
		if _, _, _, ok := ParsePublicExposureKey(key); ok {
			t.Fatalf("accepted invalid key %q", key)
		}
	}
}
