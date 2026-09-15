package main

import (
	"crypto/rand"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
)

type Ctx struct {
	W    http.ResponseWriter
	R    *http.Request
	User *SessionUser
}

type SessionUser struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	Name         string `json:"name"`
	Role         string `json:"role"`
	RoleLabel    string `json:"role_label"`
	CommunityID  *int64 `json:"community_id"`
	TeamID       *int64 `json:"team_id"`
	CommunityName string `json:"community_name,omitempty"`
	TeamName      string `json:"team_name,omitempty"`
}

type H func(c *Ctx)

// handle 注册需要登录的接口；roles 非空时做角色校验
func handle(pattern string, h H, roles ...string) {
	mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("panic on %s: %v", pattern, rec)
				jsonErr(w, 500, "服务器内部错误")
			}
		}()
		authHeader := r.Header.Get("Authorization")
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token == "" || token == authHeader {
			jsonErr(w, 401, "未登录或令牌缺失")
			return
		}
		u, err := sessGet(r.Context(), token)
		if err != nil {
			jsonErr(w, 401, "登录已过期，请重新登录")
			return
		}
		if len(roles) > 0 {
			ok := false
			for _, ro := range roles {
				if u.Role == ro {
					ok = true
					break
				}
			}
			if !ok {
				jsonErr(w, 403, "当前角色无权执行此操作")
				return
			}
		}
		h(&Ctx{W: w, R: r, User: u})
	})
}

// handlePublic 注册无需登录的接口
func handlePublic(pattern string, h H) {
	mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("panic on %s: %v", pattern, rec)
				jsonErr(w, 500, "服务器内部错误")
			}
		}()
		h(&Ctx{W: w, R: r})
	})
}

func jsonOK(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
}

func jsonErr(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"code": status, "message": msg})
}

func decodeBody(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

func randToken() string {
	b := make([]byte, 24)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func pathID(c *Ctx, name string) (int64, bool) {
	id, err := strconv.ParseInt(c.R.PathValue(name), 10, 64)
	if err != nil {
		jsonErr(c.W, 400, "路径参数 id 非法")
		return 0, false
	}
	return id, true
}

// StringList 用于 JSONB 字符串数组（照片 URL 列表等）
type StringList []string

func (s StringList) Value() (driver.Value, error) {
	if s == nil {
		s = []string{}
	}
	b, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

func (s *StringList) Scan(src any) error {
	switch v := src.(type) {
	case []byte:
		return json.Unmarshal(v, s)
	case string:
		return json.Unmarshal([]byte(v), s)
	case nil:
		*s = []string{}
		return nil
	}
	return fmt.Errorf("StringList: unsupported type %T", src)
}

// ---- 枚举与中文标签 ----

var RoleLabels = map[string]string{
	"resident":   "居民",
	"property":   "物业",
	"grid":       "网格员",
	"operator":   "消杀人员",
	"street":     "街道",
	"supervisor": "卫生监督",
}

var ReportTypes = map[string]string{
	"mosquito_dense":  "蚊虫密集",
	"rooftop_water":   "楼顶积水",
	"basement_damp":   "地下室潮湿",
	"greenbelt_water": "绿化带积水",
	"trash_odor":      "垃圾桶异味",
	"child_bite":      "儿童被叮咬",
}

// 上报类型 → 派单基础权重
var ReportTypeWeight = map[string]float64{
	"child_bite":      50,
	"mosquito_dense":  40,
	"rooftop_water":   30,
	"greenbelt_water": 30,
	"basement_damp":   25,
	"trash_odor":      20,
}

var WaterPointTypes = map[string]string{
	"rooftop_water":     "楼顶积水",
	"rooftop_tank":      "楼顶水箱",
	"basement_damp":     "地下室潮湿",
	"waste_tire":        "废旧轮胎",
	"underground_garage": "地下车库",
	"greenbelt_bush":    "绿化灌木",
	"greenbelt_water":   "绿化带积水",
	"rain_well":         "雨水井",
	"construction_site": "建筑工地",
	"trash_site":        "垃圾收集点",
	"other":             "其他",
}

var WaterPointTypeWeight = map[string]float64{
	"construction_site": 40,
	"rooftop_tank":      35,
	"rain_well":         35,
	"rooftop_water":     30,
	"underground_garage": 30,
	"waste_tire":        30,
	"greenbelt_bush":    30,
	"greenbelt_water":   30,
	"basement_damp":     25,
	"trash_site":        20,
	"other":             20,
}

// 上报类型 → 自动生成的积水点类型（空串表示不生成）
var ReportToWaterType = map[string]string{
	"rooftop_water":   "rooftop_water",
	"basement_damp":   "basement_damp",
	"greenbelt_water": "greenbelt_water",
}

var IssueTypes = map[string]string{
	"resident_refused":       "居民拒绝入户",
	"children_time_conflict": "儿童活动区需避开时段",
	"pet_complaint":          "宠物主人投诉",
	"chemical_shortage":      "药剂不足",
	"property_facility":      "积水由物业设施造成",
	"larvae_remaining":       "复查仍有幼虫",
	"other":                  "其他",
}

var ReportStatusLabels = map[string]string{
	"pending":    "待派单",
	"dispatched": "已派单",
	"processing": "处理中",
	"closed":     "已闭环",
}

var WaterPointStatusLabels = map[string]string{
	"pending":         "待处理",
	"treating":        "处理中",
	"recheck_pending": "待复查",
	"rectifying":      "物业整改中",
	"cleared":         "已清除",
}

var OrderStatusLabels = map[string]string{
	"pending":         "待派单",
	"assigned":        "已派单",
	"in_progress":     "消杀中",
	"recheck_pending": "待复查",
	"rectifying":      "物业整改中",
	"escalated":       "异常协同中",
	"closed":          "已闭环",
}

var RectificationStatusLabels = map[string]string{
	"pending":     "待整改",
	"in_progress": "整改中",
	"done":        "待核验",
	"verified":    "已核验",
}

var IssueStatusLabels = map[string]string{
	"open":     "待处理",
	"resolved": "已解决",
}

func labelOf(m map[string]string, key string) string {
	if v, ok := m[key]; ok {
		return v
	}
	return key
}
