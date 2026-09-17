package ghactions

import (
	"strings"
	"testing"
)

// FuzzResolveBase feeds arbitrary event payloads. A resolved base must always
// be a full lowercase hexadecimal commit SHA, whatever the payload holds, so
// nothing but a SHA can reach git.
func FuzzResolveBase(f *testing.F) {
	f.Add("pull_request", `{"pull_request":{"base":{"sha":"1111111111111111111111111111111111111111"}}}`)
	f.Add("push", `{"before":"--upload-pack=evil"}`)
	f.Add("merge_group", `{"merge_group":{"base_sha":null}}`)
	f.Fuzz(func(t *testing.T, event, payload string) {
		e := Env{Actions: true, EventName: event, EventPath: "event.json"}
		base, err := e.ResolveBase(func(string) ([]byte, error) { return []byte(payload), nil })
		if err != nil {
			return
		}
		if !isHexSHA(base.SHA) || strings.HasPrefix(base.SHA, "-") {
			t.Fatalf("resolved a base that is not a SHA: %q", base.SHA)
		}
		if event == "pull_request_target" {
			t.Fatal("pull_request_target was accepted")
		}
	})
}
