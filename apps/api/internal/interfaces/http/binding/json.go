package interfaceshttpbinding

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	infrahttphertz "github.com/shiyudesu/frux/internal/infra/httphertz"

	"github.com/cloudwego/hertz/pkg/app"
)

const MaxJSONBodyBytes = 4 << 20

var ErrJSONBodyTooLarge = errors.New("json request body is too large")

// BindJSON decodes one bounded JSON request body without materializing an unbounded stream.
func BindJSON(c *app.RequestContext, target any) error {
	body, err := readJSONBody(c, MaxJSONBodyBytes)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, target)
}

// BindStrictJSON rejects unknown fields and trailing JSON using a caller-specific size limit.
func BindStrictJSON(c *app.RequestContext, target any, maxBytes int) error {
	body, err := readJSONBody(c, maxBytes)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("json request body contains multiple values")
		}
		return err
	}
	return nil
}

func readJSONBody(c *app.RequestContext, maxBytes int) ([]byte, error) {
	if maxBytes <= 0 || maxBytes > MaxJSONBodyBytes {
		maxBytes = MaxJSONBodyBytes
	}
	if !c.Request.IsBodyStream() {
		body := c.Request.BodyBytes()
		if len(body) > maxBytes {
			return nil, ErrJSONBodyTooLarge
		}
		return body, nil
	}
	limited := &io.LimitedReader{
		R: c.Request.BodyStream(),
		N: int64(maxBytes + 1),
	}
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if len(body) > maxBytes {
		return nil, ErrJSONBodyTooLarge
	}
	infrahttphertz.MarkRequestBodyConsumed(c)
	return body, nil
}
