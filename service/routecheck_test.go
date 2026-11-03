package service

import (
	"strings"
	"testing"
)

func TestCheckRouteRefs(t *testing.T) {
	const noRefs = `{"route":{"rules":[]}}`
	cases := []struct {
		name    string
		cfg     string
		out, ep []string
		old     string
		wantErr string // "" = 通过
	}{
		{"rule to existing outbound", `{"route":{"rules":[{"inbound":["a"],"action":"route","outbound":"land"}]}}`, []string{"direct", "land"}, nil, noRefs, ""},
		{"rule to endpoint", `{"route":{"rules":[{"action":"route","outbound":"wg"}]}}`, []string{"direct"}, []string{"wg"}, noRefs, ""},
		{"new dangling rule rejected", `{"route":{"rules":[{"action":"route","outbound":"x"}]}}`, []string{"direct"}, nil, noRefs, `rule #1 (route.rules, counting from 1) points to outbound "x"`},
		{"second rule numbered", `{"route":{"rules":[{"action":"sniff"},{"action":"route","outbound":"x"}]}}`, []string{"direct"}, nil, noRefs, "rule #2"},
		{"legacy rule without action", `{"route":{"rules":[{"outbound":"x"}]}}`, []string{"direct"}, nil, noRefs, `"x"`},
		{"dangling final rejected", `{"route":{"final":"x"}}`, []string{"direct"}, nil, noRefs, "route final"},
		{"old dangling ref kept", `{"route":{"rules":[{"action":"route","outbound":"x"}]}}`, []string{"direct"}, nil, `{"route":{"rules":[{"outbound":"x"}]}}`, ""},
		{"no outbounds: sing-box adds direct", `{"route":{"rules":[{"action":"route","outbound":"direct"}]}}`, nil, nil, noRefs, ""},
		{"no outbounds: block is not added", `{"route":{"rules":[{"action":"route","outbound":"block"}]}}`, nil, nil, noRefs, `"block"`},
		{"no outbounds but final set: no auto direct", `{"route":{"rules":[{"action":"route","outbound":"direct"}],"final":"wg"}}`, nil, []string{"wg"}, noRefs, `"direct"`},
		{"outbounds exist without direct: direct rejected", `{"route":{"rules":[{"action":"route","outbound":"direct"}]}}`, []string{"land"}, nil, noRefs, `"direct"`},
		{"no refs", noRefs, nil, nil, "", ""},
		{"bad old config ignored", `{"route":{"rules":[{"outbound":"land"}]}}`, []string{"land"}, nil, "", ""},
	}
	for _, c := range cases {
		err := checkRouteRefs([]byte(c.cfg), c.out, c.ep, []byte(c.old))
		if c.wantErr == "" {
			if err != nil {
				t.Errorf("%s: unexpected error %v", c.name, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), c.wantErr) {
			t.Errorf("%s: error = %v, want containing %q", c.name, err, c.wantErr)
		}
	}
}
