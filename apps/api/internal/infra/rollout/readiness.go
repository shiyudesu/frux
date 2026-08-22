package rollout

import (
	"bufio"
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const sessionSemanticRuntimeReadyMetric = "frux_recommendation_session_semantic_runtime_ready"

var ErrInvalidReadinessEndpoint = errors.New("invalid session semantic readiness endpoint")
var ErrInvalidReadinessResponse = errors.New("invalid session semantic readiness response")

func ProbeSessionSemanticRuntimeReady(
	ctx context.Context,
	endpoint string,
	timeout time.Duration,
	maxResponseBytes int64,
) (bool, error) {
	parsed, err := validateReadinessEndpoint(endpoint)
	if err != nil || timeout < 100*time.Millisecond || timeout > 30*time.Second ||
		maxResponseBytes < 64<<10 || maxResponseBytes > 8<<20 {
		return false, ErrInvalidReadinessEndpoint
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return false, ErrInvalidReadinessEndpoint
	}
	client := &http.Client{Timeout: timeout}
	response, err := client.Do(request)
	if err != nil {
		return false, ErrInvalidReadinessResponse
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return false, ErrInvalidReadinessResponse
	}
	limited := io.LimitReader(response.Body, maxResponseBytes+1)
	payload, err := io.ReadAll(limited)
	if err != nil || int64(len(payload)) > maxResponseBytes {
		return false, ErrInvalidReadinessResponse
	}
	return parseSessionSemanticRuntimeReady(payload)
}

func parseSessionSemanticRuntimeReady(payload []byte) (bool, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(payload)))
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	found := false
	ready := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != sessionSemanticRuntimeReadyMetric {
			continue
		}
		if found || len(fields) != 2 {
			return false, ErrInvalidReadinessResponse
		}
		value, err := strconv.ParseFloat(fields[1], 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || (value != 0 && value != 1) {
			return false, ErrInvalidReadinessResponse
		}
		found = true
		ready = value == 1
	}
	if scanner.Err() != nil || !found {
		return false, ErrInvalidReadinessResponse
	}
	return ready, nil
}

func validateReadinessEndpoint(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || parsed.Hostname() == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, ErrInvalidReadinessEndpoint
	}
	if parsed.Scheme == "http" && !localReadinessHost(parsed.Hostname()) {
		return nil, ErrInvalidReadinessEndpoint
	}
	return parsed, nil
}

func localReadinessHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	address, err := netip.ParseAddr(host)
	return err == nil && address.IsLoopback()
}
