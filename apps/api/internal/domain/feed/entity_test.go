package domainfeed

import (
	"reflect"
	"testing"
)

func TestFeedCardPlaybackSourcesAreIgnoredByGORM(t *testing.T) {
	field, ok := reflect.TypeOf(FeedCard{}).FieldByName("PlaybackSources")
	if !ok {
		t.Fatal("FeedCard.PlaybackSources is missing")
	}
	if field.Tag.Get("gorm") != "-" {
		t.Fatalf("FeedCard.PlaybackSources must be ignored by GORM, got %q", field.Tag.Get("gorm"))
	}
}
