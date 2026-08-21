package applicationacceptance

import "time"

type Config struct {
	APIEndpoint           string        `json:"-"`
	AdapterEndpoint       string        `json:"-"`
	APIMetricsEndpoint    string        `json:"-"`
	WorkerMetricsEndpoint string        `json:"-"`
	PostgresDSN           string        `json:"-"`
	UserAccount           string        `json:"-"`
	UserPassword          string        `json:"-"`
	AdminAccount          string        `json:"-"`
	AdminPassword         string        `json:"-"`
	VideoFixturePath      string        `json:"-"`
	CoverFixturePath      string        `json:"-"`
	ExpectedProfile       string        `json:"-"`
	Query                 string        `json:"-"`
	PollInterval          time.Duration `json:"-"`
	StageTimeout          time.Duration `json:"-"`
	HTTPTimeout           time.Duration `json:"-"`
	MaxResponseBytes      int64         `json:"-"`
}
