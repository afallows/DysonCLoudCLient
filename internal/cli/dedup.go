package cli

import "encoding/json"

func dedupKey(b []byte) string {
	var v struct {
		Msg  string `json:"msg"`
		Time string `json:"time"`
	}
	if err := json.Unmarshal(b, &v); err == nil && v.Msg != "" && v.Time != "" {
		return v.Msg + "|" + v.Time
	}
	return string(b)
}
