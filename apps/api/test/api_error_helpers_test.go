package test

import (
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/common/ut"
)

type apiErrorResponse struct {
	Code  string `json:"code"`
	Error string `json:"error"`
}

func assertAPIError(t *testing.T, resp *ut.ResponseRecorder, expectedStatus int, expectedCode, expectedLegacy string) apiErrorResponse {
	t.Helper()

	requireStatus(t, resp, expectedStatus)
	var body apiErrorResponse
	decodeJSON(t, resp, &body)
	if body.Code != expectedCode || body.Error != expectedLegacy {
		t.Fatalf("unexpected api error: %+v raw=%s", body, resp.Body.String())
	}
	return body
}

func assertAPIErrorNoLeak(t *testing.T, resp *ut.ResponseRecorder, expectedStatus int, expectedCode, expectedLegacy string, forbidden ...string) {
	t.Helper()

	assertAPIError(t, resp, expectedStatus, expectedCode, expectedLegacy)
	body := resp.Body.String()
	for _, value := range forbidden {
		if value != "" && strings.Contains(body, value) {
			t.Fatalf("response leaked forbidden detail %q: %s", value, body)
		}
	}
}
