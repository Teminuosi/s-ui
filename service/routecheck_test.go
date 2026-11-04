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
		// 配置里其余点名出站的 detour
		{"rule set download_detour dangling", `{"route":{"rule_set":[{"tag":"geo","type":"remote","download_detour":"x"}]}}`, []string{"direct"}, nil, noRefs, `rule set "geo" download_detour points to outbound "x"`},
		{"rule set download_detour ok", `{"route":{"rule_set":[{"tag":"geo","type":"remote","download_detour":"land"}]}}`, []string{"direct", "land"}, nil, noRefs, ""},
		{"dns server detour dangling", `{"dns":{"servers":[{"tag":"a","type":"udp","server":"203.0.113.1"},{"tag":"b","type":"udp","server":"203.0.113.2","detour":"x"}]}}`, []string{"direct"}, nil, noRefs, `dns server #2 "b" detour`},
		{"dns server detour to endpoint", `{"dns":{"servers":[{"tag":"b","detour":"wg"}]}}`, []string{"direct"}, []string{"wg"}, noRefs, ""},
		{"ntp detour dangling", `{"ntp":{"enabled":true,"detour":"x"}}`, []string{"direct"}, nil, noRefs, "ntp detour"},
		{"disabled ntp detour ignored", `{"ntp":{"enabled":false,"detour":"x"}}`, []string{"direct"}, nil, noRefs, ""},
		{"ntp without enabled ignored", `{"ntp":{"detour":"x"}}`, []string{"direct"}, nil, noRefs, ""},
		{"clash ui detour dangling", `{"experimental":{"clash_api":{"external_controller":"127.0.0.1:9090","external_ui":"ui","external_ui_download_detour":"x"}}}`, []string{"direct"}, nil, noRefs, "clash_api"},
		{"clash ui detour unused without external_ui", `{"experimental":{"clash_api":{"external_controller":"127.0.0.1:9090","external_ui_download_detour":"x"}}}`, []string{"direct"}, nil, noRefs, ""},
		{"clash ui detour unused without controller", `{"experimental":{"clash_api":{"external_ui":"ui","external_ui_download_detour":"x"}}}`, []string{"direct"}, nil, noRefs, ""},
		{"old dangling dns detour kept", `{"dns":{"servers":[{"tag":"b","detour":"x"}]}}`, []string{"direct"}, nil, `{"ntp":{"enabled":true,"detour":"x"}}`, ""},
		{"bad route does not hide detour check", `{"route":{"rules":5},"ntp":{"enabled":true,"detour":"x"}}`, []string{"direct"}, nil, noRefs, "ntp detour"},
		{"direct action detour dangling", `{"route":{"rules":[{"action":"direct","detour":"x"}]}}`, []string{"direct"}, nil, noRefs, `rule #1 (route.rules, counting from 1) detour points to outbound "x"`},
		{"odd field type does not hide route check", `{"ntp":{"detour":5},"route":{"rules":[{"action":"route","outbound":"x"}]}}`, []string{"direct"}, nil, noRefs, "rule #1"},
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
