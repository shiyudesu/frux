package domainofflineevaluation

import "time"

type FeatureChannel string

const (
	FeatureText       FeatureChannel = "text"
	FeatureImage      FeatureChannel = "image"
	FeatureMultimodal FeatureChannel = "multimodal"
)

type Interaction struct {
	UserKey     string
	ItemKey     string
	OccurredAt  time.Time
	WatchRatio  *float64
	SourceOrder int64
}

type Item struct {
	Key        string
	AuthorKey  string
	Categories []string
	Features   map[FeatureChannel][]float64
}

type Dataset struct {
	Kind              DatasetKind
	Release           string
	Schema            string
	Interactions      []Interaction
	Items             map[string]Item
	FeatureDimensions map[FeatureChannel]int
}

func DatasetUserKey(kind DatasetKind, value string) string {
	return string(kind) + ":user:" + value
}

func DatasetItemKey(kind DatasetKind, value string) string {
	return string(kind) + ":item:" + value
}

func DatasetAuthorKey(kind DatasetKind, value string) string {
	if value == "" {
		return ""
	}
	return string(kind) + ":author:" + value
}

func DatasetCategoryKey(kind DatasetKind, value string) string {
	return string(kind) + ":category:" + value
}
