package ascp

import (
	"encoding/json"
	"errors"
)

// SigningProjection returns the exact top-level JSON object that ASCP signs for
// a quote, receipt, or another signable document. The transport-level
// "signature" member is removed rather than serialized as an empty object.
//
// Keeping this operation in one shared helper prevents language- or
// implementation-specific ambiguity about whether signature metadata is part
// of the protected payload. The resulting object is still embedded in the JWS,
// so a verifier does not need to reproduce a canonical byte sequence.
func SigningProjection(value any) (map[string]json.RawMessage, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}

	var object map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &object); err != nil {
		return nil, err
	}
	if object == nil {
		return nil, errors.New("ASCP signable value must be a JSON object")
	}
	delete(object, "signature")
	return object, nil
}
