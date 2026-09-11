package mcp

import (
	"encoding/json"

	"github.com/meaningforge/metis/serrors"
)

type encodedToolError string

func (e encodedToolError) Error() string { return string(e) }

// encodeToolError keeps the MCP SDK's tool-error behavior (isError=true) while
// giving Agents the same stable JSON error payload exposed over REST.
func encodeToolError(err error) error {
	if err == nil {
		return nil
	}
	payload, marshalErr := json.Marshal(serrors.PayloadFrom(err))
	if marshalErr != nil {
		return err
	}
	return encodedToolError(payload)
}
