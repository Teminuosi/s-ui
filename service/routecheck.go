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

func routeOutboundRefs(data []byte) []routeRef {
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
			refs = append(refs, routeRef{where: "route rule #" + strconv.Itoa(i+1), tag: tag})
		}
	}
	if cfg.Route.Final != "" {
		refs = append(refs, routeRef{where: "route final", tag: cfg.Route.Final})
	}
	return refs
}

func (s *ConfigService) validateRouteOutbounds(tx *gorm.DB, data json.RawMessage) error {
	refs := routeOutboundRefs(data)
	if len(refs) == 0 {
		return nil
	}
	exist := map[string]bool{}
	var tags []string
	if err := tx.Model(model.Outbound{}).Pluck("tag", &tags).Error; err != nil {
		return err
	}
	for _, t := range tags {
		exist[t] = true
	}
	tags = nil
	if err := tx.Model(model.Endpoint{}).Pluck("tag", &tags).Error; err != nil {
		return err
	}
	for _, t := range tags {
		exist[t] = true
	}
	old := map[string]bool{}
	if oldCfg, err := s.SettingService.GetConfig(); err == nil {
		for _, r := range routeOutboundRefs([]byte(oldCfg)) {
			old[r.tag] = true
		}
	}
	for _, r := range refs {
		if !exist[r.tag] && !old[r.tag] {
			return common.NewErrorf("%s points to outbound %q, which does not exist", r.where, r.tag)
		}
	}
	return nil
}
