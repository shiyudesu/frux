package infraofflineevaluation

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	domainofflineevaluation "github.com/shiyudesu/frux/internal/domain/offlineevaluation"
)

type DatasetLimits struct {
	MaxInteractions int64
	MaxItems        int
	MaxCategories   int
	MaxVectorDim    int
}

func DefaultDatasetLimits() DatasetLimits {
	return DatasetLimits{
		MaxInteractions: 100_000_000,
		MaxItems:        10_000_000,
		MaxCategories:   64,
		MaxVectorDim:    8192,
	}
}

func LoadDataset(loaded *LoadedManifest, limits DatasetLimits) (*domainofflineevaluation.Dataset, error) {
	if loaded == nil || !validDatasetLimits(limits) {
		return nil, &InputError{Code: FailureManifest}
	}
	dataset := &domainofflineevaluation.Dataset{
		Kind: loaded.Manifest.Dataset, Release: loaded.Manifest.Release, Schema: loaded.Manifest.Schema,
		Items:             make(map[string]domainofflineevaluation.Item),
		FeatureDimensions: make(map[domainofflineevaluation.FeatureChannel]int),
	}
	var err error
	switch loaded.Manifest.Dataset {
	case domainofflineevaluation.DatasetKuaiRec:
		err = loadKuaiRecDataset(loaded, dataset, limits)
	case domainofflineevaluation.DatasetMicroLens:
		err = loadMicroLensDataset(loaded, dataset, limits)
	default:
		err = &InputError{Code: FailureSchema}
	}
	if err != nil {
		return nil, err
	}
	if len(dataset.Interactions) == 0 || len(dataset.Items) == 0 {
		return nil, &InputError{Code: FailureRows}
	}
	if err := validateInteractionIdentities(dataset.Interactions); err != nil {
		return nil, err
	}
	return dataset, nil
}

func loadKuaiRecDataset(loaded *LoadedManifest, dataset *domainofflineevaluation.Dataset, limits DatasetLimits) error {
	if err := parseKuaiCategories(loaded.Files[domainofflineevaluation.RoleCategories], dataset, limits); err != nil {
		return err
	}
	if path := loaded.Files[domainofflineevaluation.RoleAuthors]; path != "" {
		if err := parseAuthors(path, dataset, limits, "video_id"); err != nil {
			return err
		}
	}
	if err := parseOptionalFeatures(loaded, dataset, limits, "video_id"); err != nil {
		return err
	}
	return parseKuaiInteractions(loaded.Files[domainofflineevaluation.RoleInteractions], dataset, limits)
}

func loadMicroLensDataset(loaded *LoadedManifest, dataset *domainofflineevaluation.Dataset, limits DatasetLimits) error {
	if strings.TrimSpace(loaded.Manifest.NormalizationRecipe) == "" {
		return &InputError{Code: FailureSchema, Role: domainofflineevaluation.RoleItems}
	}
	if err := parseMicroItems(loaded.Files[domainofflineevaluation.RoleItems], dataset, limits); err != nil {
		return err
	}
	if err := parseOptionalFeatures(loaded, dataset, limits, "video_key"); err != nil {
		return err
	}
	return parseMicroInteractions(loaded.Files[domainofflineevaluation.RoleInteractions], dataset, limits)
}

func parseKuaiInteractions(path string, dataset *domainofflineevaluation.Dataset, limits DatasetLimits) error {
	return readCSV(path, domainofflineevaluation.RoleInteractions,
		[]string{"user_id", "video_id", "play_duration", "video_duration", "time", "date", "timestamp", "watch_ratio"},
		func(record []string, ordinal int64) error {
			user := normalizedDatasetToken(record[0], 128)
			item := normalizedDatasetToken(record[1], 128)
			playDuration, playErr := strconv.ParseFloat(strings.TrimSpace(record[2]), 64)
			videoDuration, videoErr := strconv.ParseFloat(strings.TrimSpace(record[3]), 64)
			timestamp, timeErr := strconv.ParseInt(strings.TrimSpace(record[6]), 10, 64)
			watchRatio, ratioErr := strconv.ParseFloat(strings.TrimSpace(record[7]), 64)
			if user == "" || item == "" || playErr != nil || videoErr != nil || timeErr != nil || ratioErr != nil ||
				!finite(playDuration) || !finite(videoDuration) || !finite(watchRatio) || playDuration < 0 ||
				videoDuration <= 0 || watchRatio < 0 || watchRatio > 100 || timestamp <= 0 ||
				math.Abs(playDuration/videoDuration-watchRatio) > 0.01 {
				return &InputError{Code: FailureValue, Role: domainofflineevaluation.RoleInteractions}
			}
			itemKey := domainofflineevaluation.DatasetItemKey(dataset.Kind, item)
			if _, exists := dataset.Items[itemKey]; !exists {
				return &InputError{Code: FailureValue, Role: domainofflineevaluation.RoleInteractions}
			}
			ratio := watchRatio
			dataset.Interactions = append(dataset.Interactions, domainofflineevaluation.Interaction{
				UserKey: domainofflineevaluation.DatasetUserKey(dataset.Kind, user), ItemKey: itemKey,
				OccurredAt: time.Unix(timestamp, 0).UTC(), WatchRatio: &ratio, SourceOrder: ordinal,
			})
			if int64(len(dataset.Interactions)) > limits.MaxInteractions {
				return &InputError{Code: FailureRows, Role: domainofflineevaluation.RoleInteractions}
			}
			return nil
		})
}

func parseMicroInteractions(path string, dataset *domainofflineevaluation.Dataset, limits DatasetLimits) error {
	return readCSV(path, domainofflineevaluation.RoleInteractions,
		[]string{"user_key", "video_key", "occurred_at", "watch_ratio"},
		func(record []string, ordinal int64) error {
			user := normalizedDatasetToken(record[0], 128)
			item := normalizedDatasetToken(record[1], 128)
			occurredAt, err := time.Parse(time.RFC3339, strings.TrimSpace(record[2]))
			if user == "" || item == "" || err != nil {
				return &InputError{Code: FailureValue, Role: domainofflineevaluation.RoleInteractions}
			}
			var ratio *float64
			if value := strings.TrimSpace(record[3]); value != "" {
				parsed, parseErr := strconv.ParseFloat(value, 64)
				if parseErr != nil || !finite(parsed) || parsed < 0 || parsed > 100 {
					return &InputError{Code: FailureValue, Role: domainofflineevaluation.RoleInteractions}
				}
				ratio = &parsed
			}
			itemKey := domainofflineevaluation.DatasetItemKey(dataset.Kind, item)
			if _, exists := dataset.Items[itemKey]; !exists {
				return &InputError{Code: FailureValue, Role: domainofflineevaluation.RoleInteractions}
			}
			dataset.Interactions = append(dataset.Interactions, domainofflineevaluation.Interaction{
				UserKey: domainofflineevaluation.DatasetUserKey(dataset.Kind, user), ItemKey: itemKey,
				OccurredAt: occurredAt.UTC(), WatchRatio: ratio, SourceOrder: ordinal,
			})
			if int64(len(dataset.Interactions)) > limits.MaxInteractions {
				return &InputError{Code: FailureRows, Role: domainofflineevaluation.RoleInteractions}
			}
			return nil
		})
}

func parseKuaiCategories(path string, dataset *domainofflineevaluation.Dataset, limits DatasetLimits) error {
	return readCSV(path, domainofflineevaluation.RoleCategories, []string{"video_id", "feat"}, func(record []string, _ int64) error {
		item := normalizedDatasetToken(record[0], 128)
		var raw []int64
		if item == "" || json.Unmarshal([]byte(strings.TrimSpace(record[1])), &raw) != nil || len(raw) == 0 || len(raw) > limits.MaxCategories {
			return &InputError{Code: FailureValue, Role: domainofflineevaluation.RoleCategories}
		}
		categories := make([]string, 0, len(raw))
		seen := make(map[int64]struct{}, len(raw))
		for _, value := range raw {
			if value < 0 {
				return &InputError{Code: FailureValue, Role: domainofflineevaluation.RoleCategories}
			}
			if _, duplicate := seen[value]; duplicate {
				return &InputError{Code: FailureDuplicate, Role: domainofflineevaluation.RoleCategories}
			}
			seen[value] = struct{}{}
			categories = append(categories, domainofflineevaluation.DatasetCategoryKey(dataset.Kind, strconv.FormatInt(value, 10)))
		}
		sort.Strings(categories)
		return addItem(dataset, domainofflineevaluation.DatasetItemKey(dataset.Kind, item), "", categories, limits)
	})
}

func parseMicroItems(path string, dataset *domainofflineevaluation.Dataset, limits DatasetLimits) error {
	return readCSV(path, domainofflineevaluation.RoleItems, []string{"video_key", "author_key", "categories"}, func(record []string, _ int64) error {
		item := normalizedDatasetToken(record[0], 128)
		author := normalizedOptionalDatasetToken(record[1], 128)
		var raw []string
		if item == "" || json.Unmarshal([]byte(strings.TrimSpace(record[2])), &raw) != nil || len(raw) == 0 || len(raw) > limits.MaxCategories {
			return &InputError{Code: FailureValue, Role: domainofflineevaluation.RoleItems}
		}
		categories := make([]string, 0, len(raw))
		seen := make(map[string]struct{}, len(raw))
		for _, value := range raw {
			value = normalizedDatasetToken(value, 128)
			if value == "" {
				return &InputError{Code: FailureValue, Role: domainofflineevaluation.RoleItems}
			}
			if _, duplicate := seen[value]; duplicate {
				return &InputError{Code: FailureDuplicate, Role: domainofflineevaluation.RoleItems}
			}
			seen[value] = struct{}{}
			categories = append(categories, domainofflineevaluation.DatasetCategoryKey(dataset.Kind, value))
		}
		sort.Strings(categories)
		return addItem(
			dataset, domainofflineevaluation.DatasetItemKey(dataset.Kind, item),
			domainofflineevaluation.DatasetAuthorKey(dataset.Kind, author), categories, limits,
		)
	})
}

func parseAuthors(path string, dataset *domainofflineevaluation.Dataset, _ DatasetLimits, itemHeader string) error {
	return readCSV(path, domainofflineevaluation.RoleAuthors, []string{itemHeader, "author_key"}, func(record []string, _ int64) error {
		item := normalizedDatasetToken(record[0], 128)
		author := normalizedDatasetToken(record[1], 128)
		itemKey := domainofflineevaluation.DatasetItemKey(dataset.Kind, item)
		stored, exists := dataset.Items[itemKey]
		if item == "" || author == "" || !exists || stored.AuthorKey != "" {
			return &InputError{Code: FailureValue, Role: domainofflineevaluation.RoleAuthors}
		}
		stored.AuthorKey = domainofflineevaluation.DatasetAuthorKey(dataset.Kind, author)
		dataset.Items[itemKey] = stored
		return nil
	})
}

func parseOptionalFeatures(loaded *LoadedManifest, dataset *domainofflineevaluation.Dataset, limits DatasetLimits, itemHeader string) error {
	for _, value := range []struct {
		role    string
		channel domainofflineevaluation.FeatureChannel
	}{
		{domainofflineevaluation.RoleTextFeatures, domainofflineevaluation.FeatureText},
		{domainofflineevaluation.RoleImageFeatures, domainofflineevaluation.FeatureImage},
		{domainofflineevaluation.RoleMultimodalFeatures, domainofflineevaluation.FeatureMultimodal},
	} {
		if path := loaded.Files[value.role]; path != "" {
			if err := parseFeatureFile(path, value.role, itemHeader, value.channel, dataset, limits); err != nil {
				return err
			}
		}
	}
	return nil
}

func parseFeatureFile(
	path string,
	role string,
	itemHeader string,
	channel domainofflineevaluation.FeatureChannel,
	dataset *domainofflineevaluation.Dataset,
	limits DatasetLimits,
) error {
	return readCSV(path, role, []string{itemHeader, "dimension", "values"}, func(record []string, _ int64) error {
		item := normalizedDatasetToken(record[0], 128)
		dimension, err := strconv.Atoi(strings.TrimSpace(record[1]))
		var vector []float64
		if item == "" || err != nil || dimension < 2 || dimension > limits.MaxVectorDim ||
			json.Unmarshal([]byte(strings.TrimSpace(record[2])), &vector) != nil || len(vector) != dimension {
			return &InputError{Code: FailureValue, Role: role}
		}
		norm := 0.0
		for _, component := range vector {
			if !finite(component) {
				return &InputError{Code: FailureValue, Role: role}
			}
			norm += component * component
		}
		if norm <= 0 {
			return &InputError{Code: FailureValue, Role: role}
		}
		if expected := dataset.FeatureDimensions[channel]; expected != 0 && expected != dimension {
			return &InputError{Code: FailureValue, Role: role}
		}
		dataset.FeatureDimensions[channel] = dimension
		itemKey := domainofflineevaluation.DatasetItemKey(dataset.Kind, item)
		stored, exists := dataset.Items[itemKey]
		if !exists {
			return &InputError{Code: FailureValue, Role: role}
		}
		if stored.Features == nil {
			stored.Features = make(map[domainofflineevaluation.FeatureChannel][]float64)
		}
		if _, duplicate := stored.Features[channel]; duplicate {
			return &InputError{Code: FailureDuplicate, Role: role}
		}
		stored.Features[channel] = append([]float64(nil), vector...)
		dataset.Items[itemKey] = stored
		return nil
	})
}

func addItem(dataset *domainofflineevaluation.Dataset, key, author string, categories []string, limits DatasetLimits) error {
	if _, duplicate := dataset.Items[key]; duplicate {
		return &InputError{Code: FailureDuplicate, Role: domainofflineevaluation.RoleItems}
	}
	if len(dataset.Items) >= limits.MaxItems {
		return &InputError{Code: FailureRows, Role: domainofflineevaluation.RoleItems}
	}
	dataset.Items[key] = domainofflineevaluation.Item{
		Key: key, AuthorKey: author, Categories: append([]string(nil), categories...),
		Features: make(map[domainofflineevaluation.FeatureChannel][]float64),
	}
	return nil
}

func readCSV(path, role string, expectedHeader []string, consume func([]string, int64) error) error {
	file, err := os.Open(path)
	if err != nil {
		return &InputError{Code: FailureFile, Role: role}
	}
	defer file.Close()
	reader := csv.NewReader(file)
	reader.FieldsPerRecord = len(expectedHeader)
	reader.ReuseRecord = true
	header, err := reader.Read()
	if err != nil || !slices.Equal(header, expectedHeader) {
		return &InputError{Code: FailureSchema, Role: role}
	}
	seenInteractions := map[string]struct{}{}
	ordinal := int64(0)
	for {
		record, readErr := reader.Read()
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return &InputError{Code: FailureCSV, Role: role}
		}
		ordinal++
		if role == domainofflineevaluation.RoleInteractions {
			identity := strings.Join(record[:min(len(record), 3)], "\x00")
			if _, duplicate := seenInteractions[identity]; duplicate {
				return &InputError{Code: FailureDuplicate, Role: role}
			}
			seenInteractions[identity] = struct{}{}
		}
		if err := consume(record, ordinal); err != nil {
			return err
		}
	}
}

func normalizedDatasetToken(value string, maximum int) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maximum || strings.ContainsAny(value, "\r\n\x00") {
		return ""
	}
	return value
}

func normalizedOptionalDatasetToken(value string, maximum int) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return normalizedDatasetToken(value, maximum)
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func validDatasetLimits(limits DatasetLimits) bool {
	return limits.MaxInteractions >= 1 && limits.MaxInteractions <= 100_000_000 &&
		limits.MaxItems >= 1 && limits.MaxItems <= 10_000_000 &&
		limits.MaxCategories >= 1 && limits.MaxCategories <= 256 &&
		limits.MaxVectorDim >= 2 && limits.MaxVectorDim <= 8192
}

func validateInteractionIdentities(interactions []domainofflineevaluation.Interaction) error {
	seen := make(map[string]struct{}, len(interactions))
	for _, interaction := range interactions {
		identity := interaction.UserKey + "\x00" + interaction.ItemKey + "\x00" + interaction.OccurredAt.Format(time.RFC3339Nano)
		if _, duplicate := seen[identity]; duplicate {
			return &InputError{Code: FailureDuplicate, Role: domainofflineevaluation.RoleInteractions}
		}
		seen[identity] = struct{}{}
	}
	return nil
}
