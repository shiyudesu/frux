package domainembedding

import (
	"errors"
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	unicodenorm "golang.org/x/text/unicode/norm"
)

const (
	SemanticModelName = "sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2"
	SemanticRevision  = "e8f8c211226b894fcb81acc59f3b34ba3efd5f42"
	SemanticModelKey  = "semantic-minilm-l12-v2@e8f8c211226b894f"
	SemanticDimension = 384

	SemanticJobPending    = "pending"
	SemanticJobProcessing = "processing"
	SemanticJobRetry      = "retry"
	SemanticJobSuspended  = "suspended"
	SemanticJobCompleted  = "completed"
	SemanticJobFailed     = "failed"
)

var (
	ErrInvalidSemanticText   = errors.New("invalid semantic text")
	ErrInvalidSemanticVector = errors.New("invalid semantic vector")
	ErrSemanticJobNotFound   = errors.New("semantic embedding job not found")
	ErrSemanticJobLeaseLost  = errors.New("semantic embedding job lease lost")
)

type SemanticJob struct {
	VideoID        int64
	Model          string
	TextHash       string
	Title          string
	Description    string
	State          string
	Attempts       int
	AvailableAt    time.Time
	LeaseOwner     string
	LeaseUntil     *time.Time
	LastErrorClass string
	CompletedAt    *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type SemanticBacklog struct {
	State    string
	Count    int64
	OldestAt *time.Time
}

func CanonicalVideoText(title, description string) (string, string, string, error) {
	title, err := normalizeSemanticField(title)
	if err != nil || title == "" || len([]rune(title)) > 200 {
		return "", "", "", ErrInvalidSemanticText
	}
	description, err = normalizeSemanticField(description)
	if err != nil || len([]rune(description)) > 2000 {
		return "", "", "", ErrInvalidSemanticText
	}
	return title, description, BuildVideoText(title, description), nil
}

func normalizeSemanticField(value string) (string, error) {
	if !utf8.ValidString(value) {
		return "", ErrInvalidSemanticText
	}
	value = unicodenorm.NFKC.String(value)
	for _, item := range value {
		if unicode.IsControl(item) && !unicode.IsSpace(item) {
			return "", ErrInvalidSemanticText
		}
	}
	return strings.Join(strings.Fields(value), " "), nil
}

func NormalizeSemanticVector(vector []float64) ([]float64, error) {
	if len(vector) != SemanticDimension {
		return nil, ErrInvalidSemanticVector
	}
	result := make([]float64, len(vector))
	normSquared := 0.0
	for index, value := range vector {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, ErrInvalidSemanticVector
		}
		result[index] = value
		normSquared += value * value
	}
	if normSquared <= 0 {
		return nil, ErrInvalidSemanticVector
	}
	length := math.Sqrt(normSquared)
	if math.Abs(length-1) > 1e-4 {
		return nil, ErrInvalidSemanticVector
	}
	for index := range result {
		result[index] /= length
	}
	return result, nil
}

func SemanticRetryDelay(attempt int) time.Duration {
	switch {
	case attempt <= 1:
		return 5 * time.Second
	case attempt == 2:
		return 30 * time.Second
	case attempt == 3:
		return 2 * time.Minute
	case attempt == 4:
		return 10 * time.Minute
	default:
		return 30 * time.Minute
	}
}
