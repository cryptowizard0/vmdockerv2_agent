package utils

import (
	"bytes"
	"encoding/json"
)

func PrettyJSON(s string) string {
	var prettyJSON bytes.Buffer
	if err := json.Indent(&prettyJSON, []byte(s), "", "  "); err != nil {
		return s
	}
	return prettyJSON.String()
}
