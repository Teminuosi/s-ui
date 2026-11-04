package service

import (
	"encoding/json"
	"strconv"

	"github.com/alireza0/s-ui/database/model"
	"github.com/alireza0/s-ui/util/common"
	"gorm.io/gorm"
)

// 路由规则 / route.final 引用的出站都要存在(出站或 endpoint)。
// 指向不存在的出站时 sing-box 照样启动,但命中这条规则的连接全部报 outbound not found ——
// 挂在原有节点上的中转,整个节点就断了(Docker 实测)。所以新存的配置不许新增这种引用。
// 老配置里本来就有的悬空引用放行:不能因为一条旧的坏规则,让用户别的配置都存不进去。

type routeRef struct {
	where string
	tag   string
}

// 两部分各自解析、互不连累:一部分解析失败只丢自己那几项。
func routeOutboundRefs(data []byte) []routeRef {
	return append(routeRuleRefs(data), configDetourRefs(data)...)
}

func routeRuleRefs(data []byte) []routeRef {
	var cfg struct {
		Route struct {
			Rules []map[string]interface{} `json:"rules"`
			Final string                   `json:"final"`
		} `json:"route"`
	}
	if json.Unmarshal(data, &cfg) != nil {
		return nil
	}
	refs := []routeRef{}
	for i, r := range cfg.Route.Rules {
		if tag, ok := r["outbound"].(string); ok && tag != "" {
			refs = append(refs, routeRef{where: "route rule #" + strconv.Itoa(i+1) + " (route.rules, counting from 1)", tag: tag})
		}
	}
	if cfg.Route.Final != "" {
		refs = append(refs, routeRef{where: "route final", tag: cfg.Route.Final})
	}
	return refs
}

// 同一份配置(设置里存的 config)里其余点名出站的字段:规则集下载、DNS 服务器、NTP、Clash 面板下载的 detour,
// 以及 action 为 direct 的路由规则的 detour。
// 规则集、DNS 服务器、开着的 NTP 的 detour 指向不存在的出站时 sing-box 1.13 直接起不来
// (DNS 实测报 start dns/...: outbound detour not found;规则集不管有没有规则用都会启动),
// 整台服务器所有节点一起断,比路由规则悬空更严重。Clash 面板下载的 detour 悬空只是下载失败。
// 出站 / endpoint / 入站 / 服务 / TLS 里的 detour 不在这份配置里(各自一张表),这里不管。
// 单独按 map 解析:某个字段类型不对时只丢这几项,不连累路由规则的检查(反过来也一样)。
func configDetourRefs(data []byte) []routeRef {
	var cfg map[string]interface{}
	if json.Unmarshal(data, &cfg) != nil {
		return nil
	}
	obj := func(m map[string]interface{}, k string) map[string]interface{} {
		v, _ := m[k].(map[string]interface{})
		return v
	}
	str := func(m map[string]interface{}, k string) string {
		v, _ := m[k].(string)
		return v
	}
	refs := []routeRef{}
	add := func(where string, m map[string]interface{}) {
		if tag := str(m, "detour"); tag != "" {
			refs = append(refs, routeRef{where: where, tag: tag})
		}
	}
	if list, ok := obj(cfg, "route")["rules"].([]interface{}); ok {
		for i, it := range list {
			if r, ok := it.(map[string]interface{}); ok && str(r, "action") == "direct" {
				add("route rule #"+strconv.Itoa(i+1)+" (route.rules, counting from 1) detour", r)
			}
		}
	}
	if list, ok := obj(cfg, "route")["rule_set"].([]interface{}); ok {
		for _, it := range list {
			if rs, ok := it.(map[string]interface{}); ok {
				if tag := str(rs, "download_detour"); tag != "" {
					refs = append(refs, routeRef{where: "rule set " + strconv.Quote(str(rs, "tag")) + " download_detour", tag: tag})
				}
			}
		}
	}
	if list, ok := obj(cfg, "dns")["servers"].([]interface{}); ok {
		for i, it := range list {
			if srv, ok := it.(map[string]interface{}); ok {
				add("dns server #"+strconv.Itoa(i+1)+" "+strconv.Quote(str(srv, "tag"))+" detour", srv)
			}
		}
	}
	// NTP 只有 enabled 为 true 时 sing-box 才会建(box.go),关着的 NTP 里留的 detour 不算引用
	if ntp := obj(cfg, "ntp"); ntp["enabled"] == true {
		add("ntp detour", ntp)
	}
	// Clash 面板只在同时设了 external_controller 和 external_ui 时才去下载(clashapi/server_resources.go),
	// 下载失败只记一条错误日志、不影响启动;其余情况这个 detour 根本不用
	ca := obj(obj(cfg, "experimental"), "clash_api")
	if str(ca, "external_controller") != "" && str(ca, "external_ui") != "" {
		if tag := str(ca, "external_ui_download_detour"); tag != "" {
			refs = append(refs, routeRef{where: "clash_api external_ui_download_detour", tag: tag})
		}
	}
	return refs
}

func (s *ConfigService) validateRouteOutbounds(tx *gorm.DB, data json.RawMessage) error {
	refs := routeOutboundRefs(data)
	if len(refs) == 0 {
		return nil
	}
	var outTags, epTags []string
	if err := tx.Model(model.Outbound{}).Pluck("tag", &outTags).Error; err != nil {
		return err
	}
	if err := tx.Model(model.Endpoint{}).Pluck("tag", &epTags).Error; err != nil {
		return err
	}
	oldCfg, _ := s.SettingService.GetConfig()
	return checkRouteRefs(data, outTags, epTags, []byte(oldCfg))
}

// checkRouteRefs:新配置 data 里的路由引用,出站 / endpoint 里都没有、旧配置里也没引用过的,报错。
func checkRouteRefs(data []byte, outTags, epTags []string, oldCfg []byte) error {
	refs := routeOutboundRefs(data)
	exist := map[string]bool{}
	for _, t := range outTags {
		exist[t] = true
	}
	for _, t := range epTags {
		exist[t] = true
	}
	// 一个出站都没有、也没设 final 时,sing-box 自己补一个 tag 为 direct 的出站(sing-box 1.13
	// adapter/outbound/manager.go 的 defaultOutboundFallback),指向 direct 的规则能用。
	// block 出站 1.13 已删,不补,照样拒。
	hasFinal := false
	for _, r := range refs {
		if r.where == "route final" {
			hasFinal = true
		}
	}
	if len(outTags) == 0 && !hasFinal {
		exist["direct"] = true
	}
	old := map[string]bool{}
	for _, r := range routeOutboundRefs(oldCfg) {
		old[r.tag] = true
	}
	for _, r := range refs {
		if !exist[r.tag] && !old[r.tag] {
			return common.NewErrorf("%s points to outbound %q, which does not exist", r.where, r.tag)
		}
	}
	return nil
}
