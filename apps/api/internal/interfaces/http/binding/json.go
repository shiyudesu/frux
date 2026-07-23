package interfaceshttpbinding

import (
	"encoding/json"
	"errors"
	"io"

	infrahttphertz "GCFeed/internal/infra/httphertz"

	"github.com/cloudwego/hertz/pkg/app"
)

const MaxJSONBodyBytes = 4 << 20

var ErrJSONBodyTooLarge = errors.New("json request body is too large")

// BindJSON decodes one bounded JSON request body without materializing an unbounded stream.
func BindJSON(c *app.RequestContext, target any) error {
	if !c.Request.IsBodyStream() {
		body := c.Request.BodyBytes()
		if len(body) > MaxJSONBodyBytes {
			return ErrJSONBodyTooLarge
		}
		return json.Unmarshal(body, target)
	}

	limited := &io.LimitedReader{
		R: c.Request.BodyStream(),
		N: MaxJSONBodyBytes + 1,
	}
	body, err := io.ReadAll(limited)
	if err != nil {
		return err
	}
	if len(body) > MaxJSONBodyBytes {
		return ErrJSONBodyTooLarge
	}
	infrahttphertz.MarkRequestBodyConsumed(c)
	return json.Unmarshal(body, target)
}
