package interfaceshttpupload

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	applicationmedia "github.com/shiyudesu/frux/internal/application/media"
	interfaceshttpapierror "github.com/shiyudesu/frux/internal/interfaces/http/apierror"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

func TestWriteUploadSessionErrorDistinguishesUnavailableDependencies(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code string
	}{
		{
			name: "object storage unavailable",
			err:  applicationmedia.ErrDirectUploadUnavailable,
			code: interfaceshttpapierror.CodeUploadStorageUnavailable,
		},
		{
			name: "processing dispatch unavailable",
			err:  applicationmedia.ErrDispatchProcessingFailed,
			code: interfaceshttpapierror.CodeUploadProcessingUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := server.New(server.WithDisablePrintRoute(true))
			h.GET("/error", func(_ context.Context, c *app.RequestContext) {
				writeUploadSessionError(c, tt.err)
			})

			response := ut.PerformRequest(h.Engine, http.MethodGet, "/error", nil)
			if response.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
			}
			var body interfaceshttpapierror.Envelope
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if body.Code != tt.code || body.Error != tt.err.Error() {
				t.Fatalf("unexpected body: %+v raw=%s", body, response.Body.String())
			}
		})
	}
}
