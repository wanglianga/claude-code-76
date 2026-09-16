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

// RawJSON 存储/读取任意 JSON（如病例活动轨迹对象数组）
type RawJSON []byte

func (j RawJSON) Value() (driver.Value, error) {
	if len(j) == 0 {
		return "[]", nil
	}
	return string(j), nil
}

func (j *RawJSON) Scan(src any) error {
	switch v := src.(type) {
	case []byte:
		*j = append((*j)[:0], v...)
	case string:
		*j = []byte(v)
	case nil:
		*j = []byte("[]")
	}
	return nil
}

func (j RawJSON) MarshalJSON() ([]byte, error) {
	if len(j) == 0 {
		return []byte("[]"), nil
	}
	return j, nil
}

// ---- 枚举与中文标签 ----

var RoleLabels = map[string]string{
	"resident":     "居民",
	"property":     "物业",
	"grid":         "网格员",
	"operator":     "消杀人员",
	"street":       "街道",
	"supervisor":   "卫生监督",
	"kindergarten": "园方联系人",
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

// 物业积水整改任务状态
var PropRectStatusLabels = map[string]string{
	"pending":         "待整改",
	"rectifying":      "整改中",
	"recheck_pending": "待复查",
	"verified":        "复查通过",
	"rejected":        "复查不通过",
}

// 地下室排水沟长期积水 —— 物业排水维修/处理方式
var PropRectMethodLabels = map[string]string{
	"dredge_drain":   "疏通排水沟/集水井",
	"repair_pipe":    "维修破损排水管",
	"rebuild_drain":  "改造排水坡度/重建排水沟",
	"replace_pump":   "检修/更换排水泵",
	"seal_leak":      "封堵渗漏点",
	"other":          "其他工程措施",
}

// 物业积水整改督办提醒类型
var PropRectReminderKindLabels = map[string]string{
	"rect_overdue":    "整改超期",
	"recheck_overdue": "复查超期",
	"supervise":       "街道督办",
}

// ========== 重点风险期应急响应 ==========

// 小区风险等级
var CommunityRiskLabels = map[string]string{
	"normal":  "常态",
	"elevated": "风险升高",
	"warning": "预警",
	"emergency": "应急",
}

// 应急触发类型
var EmergTriggerLabels = map[string]string{
	"cdc_warning":      "疾控蚊媒传染病预警",
	"nearby_case":      "周边小区疑似/确诊病例",
	"complaint_surge":  "投诉密度突增",
	"manual":           "街道人工启动",
}

// 联防小区角色
var EmergCommunityRoleLabels = map[string]string{
	"affected":    "主疫区",
	"surrounding": "周边联防",
}

// 病例状态
var EmergCaseStatusLabels = map[string]string{
	"suspect":   "疑似",
	"confirmed": "确诊",
}

// 应急调度资源类型
var EmergResourceLabels = map[string]string{
	"team":       "消杀队支援",
	"chemical":   "药剂调配",
	"grid":       "社区网格",
	"property":   "物业",
	"supervisor": "卫生监督",
}

// ========== 居民拒绝入户与入户授权 ==========

// 入户案例状态
var AccessStatusLabels = map[string]string{
	"refused":          "拒绝入户",
	"negotiating":      "多方沟通中",
	"mandatory_review": "卫监强制入户评估",
	"external_only":    "仅外围公共区域处理",
	"authorized":       "已授权待作业",
	"in_progress":      "入户作业中",
	"withdrawn":        "居民临时反悔暂停",
	"completed":        "入户作业已完成",
	"risk_continued":   "风险延续（无法根治）",
}

// 拒绝原因
var AccessRejectReasonLabels = map[string]string{
	"privacy":           "隐私顾虑",
	"children":          "家中有儿童",
	"elderly":           "家中有老人",
	"pregnant":          "家中有孕妇",
	"pets":              "家中有宠物",
	"respiratory":       "呼吸道疾病患者",
	"distrust_chemical": "对药剂不信任",
	"other":             "其他",
}

// 家中敏感人群
var AccessSensitiveLabels = map[string]string{
	"children":    "儿童",
	"elderly":     "老人",
	"pregnant":    "孕妇",
	"respiratory": "呼吸道疾病",
}

// 可授权处理的外围区域
var AccessOutdoorAreaLabels = map[string]string{
	"doorway":   "门口",
	"staircase": "楼道",
	"balcony":   "阳台外围",
	"sewer":     "下水道/地漏",
}

var IssueStatusLabels = map[string]string{
	"open":     "待处理",
	"resolved": "已解决",
}

var ChildZoneTypes = map[string]string{
	"kindergarten": "幼儿园",
	"playground":   "儿童乐园",
	"school":       "学校周边",
}

var PlanStatusLabels = map[string]string{
	"planned":         "已计划",
	"notified":        "已提醒",
	"treated":         "已作业",
	"warning_removed": "警示已撤除",
	"confirmed":       "园方已确认",
}

var ReminderAudienceLabels = map[string]string{
	"kindergarten": "幼儿园",
	"parents":      "家长群",
	"residents":    "附近居民",
}

var DeliveryStatusLabels = map[string]string{
	"pending":   "待发送",
	"sent":      "已发送",
	"delivered": "已送达",
}

var WindDirections = []string{"东风", "南风", "西风", "北风", "东南风", "东北风", "西南风", "西北风"}

var PetComplaintStatusLabels = map[string]string{
	"pending":  "待处理",
	"resolved": "已办结",
}

var PetTypes = map[string]string{
	"dog":   "犬",
	"cat":   "猫",
	"bird":  "鸟类",
	"other": "其他",
}

func labelOf(m map[string]string, key string) string {
	if v, ok := m[key]; ok {
		return v
	}
	return key
}
