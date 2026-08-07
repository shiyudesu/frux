package interfaceshttpupload

import "testing"

func TestProtectedUploadKindIncludesModerationSamples(t *testing.T) {
	for _, path := range []string{
		"/uploads/video/source.mp4",
		"/uploads/cover/cover.jpg",
		"/uploads/moderation/1/1/1/frames-v1/attempt-001/frame.jpg",
	} {
		if !protectedUploadKind(path) {
			t.Fatalf("path %q was not protected", path)
		}
	}
	if protectedUploadKind("/uploads/avatar/user.jpg") {
		t.Fatal("avatar path unexpectedly protected by video asset handler")
	}
}
