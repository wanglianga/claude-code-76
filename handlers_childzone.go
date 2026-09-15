package main

import (
	"fmt"
	"strings"
	"time"
)

func registerChildZoneRoutes() {
	handle("GET /api/child-zones", hListChildZones)
	handle("POST /api/child-zones", hCreateChildZone, "street")
	handle("GET /api/child-zone-plans", hListChildZonePlans)
	handle("POST /api/child-zone-plans", hCreateChildZonePlan, "street")
	handle("GET /api/child-zone-plans/{id}", hGetChildZonePlan)
	handle("POST /api/child-zone-plans/{id}/send-reminders", hSendReminders, "street", "operator")
	handle("POST /api/child-zone-plans/{id}/reminders/{rid}/deliver", hDeliverReminder, "street", "operator", "kindergarten")
	handle("POST /api/child-zone-plans/{id}/remove-warning", hRemoveWarning, "operator", "street")
	handle("POST /api/child-zone-plans/{id}/confirm", hConfirmPlan, "kindergarten")
}

// ---------- 儿童活动区 ----------

type ChildZone struct {
	ID            int64     `json:"id"`
	CommunityID   int64     `json:"community_id"`
	CommunityName string    `json:"community_name"`
	Name          string    `json:"name"`
	ZoneType      string    `json:"zone_type"`
	ZoneTypeLabel string    `json:"zone_type_label"`
	ContactName   string    `json:"contact_name"`
	ContactPhone  string    `json:"contact_phone"`
	ContactUserID *int64    `json:"contact_user_id"`
	ActivityTimes string    `json:"activity_times"`
	ParentGroup   string    `json:"parent_group"`
	CreatedAt     time.Time `json:"created_at"`
}

const childZoneSelect = `
	SELECT z.id, z.community_id, cm.name, z.name, z.zone_type, z.contact_name, z.contact_phone,
	       z.contact_user_id, z.activity_times, z.parent_group, z.created_at
	FROM child_zones z JOIN communities cm ON cm.id=z.community_id`

func scanChildZone(row interface{ Scan(...any) error }) (*ChildZone, error) {
	var z ChildZone
	err := row.Scan(&z.ID, &z.CommunityID, &z.CommunityName, &z.Name, &z.ZoneType, &z.ContactName,
		&z.ContactPhone, &z.ContactUserID, &z.ActivityTimes, &z.ParentGroup, &z.CreatedAt)
	if err != nil {
		return nil, err
	}
	z.ZoneTypeLabel = labelOf(ChildZoneTypes, z.ZoneType)
	return &z, nil
}

func hListChildZones(c *Ctx) {
	q := childZoneSelect + ` WHERE 1=1`
	args := []any{}
	if v := c.R.URL.Query().Get("community_id"); v != "" {
		q += ` AND z.community_id=$1`
		args = append(args, v)
	}
	q += ` ORDER BY z.id`
	rows, err := db.Query(q, args...)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	defer rows.Close()
	list := []ChildZone{}
	for rows.Next() {
		z, err := scanChildZone(rows)
		if err != nil {
			jsonErr(c.W, 500, err.Error())
			return
		}
		list = append(list, *z)
	}
	jsonOK(c.W, list)
}

func hCreateChildZone(c *Ctx) {
	var req struct {
		CommunityID   int64  `json:"community_id"`
		Name          string `json:"name"`
		ZoneType      string `json:"zone_type"`
		ContactName   string `json:"contact_name"`
		ContactPhone  string `json:"contact_phone"`
		ContactUserID *int64 `json:"contact_user_id"`
		ActivityTimes string `json:"activity_times"`
		ParentGroup   string `json:"parent_group"`
	}
	if err := decodeBody(c.R, &req); err != nil {
		jsonErr(c.W, 400, "请求格式错误")
		return
	}
	if req.CommunityID == 0 || req.Name == "" {
		jsonErr(c.W, 400, "请填写小区与活动区名称")
		return
	}
	if _, ok := ChildZoneTypes[req.ZoneType]; !ok {
		req.ZoneType = "kindergarten"
	}
	var id int64
	err := db.QueryRow(`INSERT INTO child_zones(community_id, name, zone_type, contact_name, contact_phone, contact_user_id, activity_times, parent_group)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
		req.CommunityID, req.Name, req.ZoneType, req.ContactName, req.ContactPhone, req.ContactUserID, req.ActivityTimes, req.ParentGroup).Scan(&id)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	z, err := scanChildZone(db.QueryRow(childZoneSelect+` WHERE z.id=$1`, id))
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	jsonOK(c.W, z)
}

// ---------- 错峰消杀计划 ----------

type ChildZonePlan struct {
	ID                  int64      `json:"id"`
	PlanNo              string     `json:"plan_no"`
	WorkOrderID         *int64     `json:"work_order_id"`
	OrderNo             *string    `json:"order_no"`
	ChildZoneID         int64      `json:"child_zone_id"`
	ZoneName            string     `json:"zone_name"`
	ZoneType            string     `json:"zone_type"`
	ZoneTypeLabel       string     `json:"zone_type_label"`
	ActivityTimes       string     `json:"activity_times"`
	CommunityID         int64      `json:"community_id"`
	CommunityName       string     `json:"community_name"`
	PlannedStart        time.Time  `json:"planned_start"`
	PlannedEnd          time.Time  `json:"planned_end"`
	WindDirection       string     `json:"wind_direction"`
	SafetyIntervalHours float64    `json:"safety_interval_hours"`
	RecoveryTime        *time.Time `json:"recovery_time"`
	ContactName         string     `json:"contact_name"`
	ContactPhone        string     `json:"contact_phone"`
	Status              string     `json:"status"`
	StatusLabel         string     `json:"status_label"`
	WarningRemovedAt    *time.Time `json:"warning_removed_at"`
	ConfirmedBy         *int64     `json:"confirmed_by"`
	ConfirmedAt         *time.Time `json:"confirmed_at"`
	ConfirmNote         string     `json:"confirm_note"`
	CreatedAt           time.Time  `json:"created_at"`
}

const planSelect = `
	SELECT p.id, p.plan_no, p.work_order_id, o.order_no, p.child_zone_id, z.name, z.zone_type, z.activity_times,
	       p.community_id, cm.name, p.planned_start, p.planned_end, p.wind_direction, p.safety_interval_hours,
	       p.recovery_time, p.contact_name, p.contact_phone, p.status, p.warning_removed_at,
	       p.confirmed_by, p.confirmed_at, p.confirm_note, p.created_at
	FROM child_zone_plans p
	JOIN child_zones z ON z.id=p.child_zone_id
	JOIN communities cm ON cm.id=p.community_id
	LEFT JOIN work_orders o ON o.id=p.work_order_id`

func scanPlan(row interface{ Scan(...any) error }) (*ChildZonePlan, error) {
	var p ChildZonePlan
	err := row.Scan(&p.ID, &p.PlanNo, &p.WorkOrderID, &p.OrderNo, &p.ChildZoneID, &p.ZoneName, &p.ZoneType,
		&p.ActivityTimes, &p.CommunityID, &p.CommunityName, &p.PlannedStart, &p.PlannedEnd, &p.WindDirection,
		&p.SafetyIntervalHours, &p.RecoveryTime, &p.ContactName, &p.ContactPhone, &p.Status, &p.WarningRemovedAt,
		&p.ConfirmedBy, &p.ConfirmedAt, &p.ConfirmNote, &p.CreatedAt)
	if err != nil {
		return nil, err
	}
	p.ZoneTypeLabel = labelOf(ChildZoneTypes, p.ZoneType)
	p.StatusLabel = labelOf(PlanStatusLabels, p.Status)
	return &p, nil
}

// parseActivityWindows 解析儿童活动时段 "07:30-08:30,16:00-18:00" 为当日时间窗
func parseActivityWindows(spec string, day time.Time) [][2]time.Time {
	var wins [][2]time.Time
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		se := strings.Split(part, "-")
		if len(se) != 2 {
			continue
		}
		st, err1 := time.ParseInLocation("2006-01-02 15:04", day.Format("2006-01-02")+" "+strings.TrimSpace(se[0]), time.Local)
		en, err2 := time.ParseInLocation("2006-01-02 15:04", day.Format("2006-01-02")+" "+strings.TrimSpace(se[1]), time.Local)
		if err1 != nil || err2 != nil {
			continue
		}
		wins = append(wins, [2]time.Time{st, en})
	}
	return wins
}

func parseDateTime(s string) (time.Time, error) {
	s = strings.Replace(strings.TrimSpace(s), "T", " ", 1)
	if len(s) == 16 {
		s += ":00"
	}
	return time.ParseInLocation("2006-01-02 15:04:05", s, time.Local)
}

func fmtHM(t time.Time) string { return t.Format("2006-01-02 15:04") }

func hCreateChildZonePlan(c *Ctx) {
	var req struct {
		WorkOrderID         *int64  `json:"work_order_id"`
		ChildZoneID         int64   `json:"child_zone_id"`
		PlannedStart        string  `json:"planned_start"`
		PlannedEnd          string  `json:"planned_end"`
		WindDirection       string  `json:"wind_direction"`
		SafetyIntervalHours float64 `json:"safety_interval_hours"`
	}
	if err := decodeBody(c.R, &req); err != nil {
		jsonErr(c.W, 400, "请求格式错误")
		return
	}
	if req.ChildZoneID == 0 || req.PlannedStart == "" || req.PlannedEnd == "" {
		jsonErr(c.W, 400, "请选择儿童活动区并填写计划作业时段")
		return
	}
	start, err1 := parseDateTime(req.PlannedStart)
	end, err2 := parseDateTime(req.PlannedEnd)
	if err1 != nil || err2 != nil || !end.After(start) {
		jsonErr(c.W, 400, "计划时段非法（结束须晚于开始）")
		return
	}
	if req.SafetyIntervalHours <= 0 {
		req.SafetyIntervalHours = 4
	}
	zone, err := scanChildZone(db.QueryRow(childZoneSelect+` WHERE z.id=$1`, req.ChildZoneID))
	if err != nil {
		jsonErr(c.W, 404, "儿童活动区不存在")
		return
	}
	// 错峰校验：作业时段 + 药剂安全间隔不得与儿童活动时间重叠
	recovery := end.Add(time.Duration(req.SafetyIntervalHours * float64(time.Hour)))
	for _, w := range parseActivityWindows(zone.ActivityTimes, start) {
		if start.Before(w[1]) && recovery.After(w[0]) {
			jsonErr(c.W, 409, fmt.Sprintf("与儿童活动时间 %s~%s 冲突（含安全间隔后恢复时间 %s），请错峰安排",
				w[0].Format("15:04"), w[1].Format("15:04"), recovery.Format("15:04")))
			return
		}
	}
	// 关联工单校验
	if req.WorkOrderID != nil {
		var status string
		if err := db.QueryRow(`SELECT status FROM work_orders WHERE id=$1`, *req.WorkOrderID).Scan(&status); err != nil {
			jsonErr(c.W, 404, "关联工单不存在")
			return
		}
		if status == "closed" {
			jsonErr(c.W, 409, "关联工单已闭环")
			return
		}
	}
	var planID int64
	err = db.QueryRow(`INSERT INTO child_zone_plans(work_order_id, child_zone_id, community_id, planned_start, planned_end,
		wind_direction, safety_interval_hours, recovery_time, contact_name, contact_phone, status, created_by)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'notified',$11) RETURNING id`,
		req.WorkOrderID, req.ChildZoneID, zone.CommunityID, start, end,
		req.WindDirection, req.SafetyIntervalHours, recovery, zone.ContactName, zone.ContactPhone, c.User.ID).Scan(&planID)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	db.Exec(`UPDATE child_zone_plans SET plan_no='CZ'||LPAD(id::text,6,'0') WHERE id=$1`, planID)

	// 作业前提醒：幼儿园、家长群、附近居民（标明避让时段与联系人，保留送达状态）
	avoid := fmt.Sprintf("%s ~ %s", fmtHM(start), fmtHM(recovery))
	contact := strings.TrimSpace(zone.ContactName + " " + zone.ContactPhone)
	content := fmt.Sprintf("【错峰消杀提醒】%s 定于 %s ~ %s 进行灭蚊消杀（风向：%s）。避让时段：%s（含药剂安全间隔 %.1f 小时，预计 %s 恢复儿童活动）。园方联系人：%s。",
		zone.Name, fmtHM(start), fmtHM(end), req.WindDirection, avoid, req.SafetyIntervalHours, recovery.Format("15:04"), contact)
	audiences := []struct{ aud, channel string }{
		{"kindergarten", "sms"},
		{"parents", "wechat_group"},
		{"residents", "app"},
	}
	for _, a := range audiences {
		db.Exec(`INSERT INTO zone_reminders(plan_id, audience, channel, title, content, avoid_period, contact_info, delivery_status, sent_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,'sent',now())`,
			planID, a.aud, a.channel, "错峰消杀提醒（"+zone.Name+"）", content, avoid, contact)
	}
	if req.WorkOrderID != nil {
		addLog(*req.WorkOrderID, c.User, "错峰消杀计划", fmt.Sprintf("儿童活动区「%s」计划 %s~%s 作业（风向 %s，安全间隔 %.1f 小时，%s 恢复活动），提醒已推送幼儿园/家长群/附近居民",
			zone.Name, fmtHM(start), fmtHM(end), req.WindDirection, req.SafetyIntervalHours, recovery.Format("15:04")))
	}
	p, err := scanPlan(db.QueryRow(planSelect+` WHERE p.id=$1`, planID))
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	jsonOK(c.W, p)
}

func hListChildZonePlans(c *Ctx) {
	q := planSelect + ` WHERE 1=1`
	args := []any{}
	i := 0
	next := func() string { i++; return fmt.Sprintf("$%d", i) }
	switch c.User.Role {
	case "kindergarten":
		q += ` AND p.child_zone_id IN (SELECT id FROM child_zones WHERE contact_user_id=` + next() + `)`
		args = append(args, c.User.ID)
	case "resident":
		// 居民仅可见本小区且园方已确认的计划
		q += ` AND p.status='confirmed'`
		if c.User.CommunityID != nil {
			q += ` AND p.community_id=` + next()
			args = append(args, *c.User.CommunityID)
		}
	case "property", "grid":
		if c.User.CommunityID != nil {
			q += ` AND p.community_id=` + next()
			args = append(args, *c.User.CommunityID)
		}
	}
	if v := c.R.URL.Query().Get("status"); v != "" {
		q += ` AND p.status=` + next()
		args = append(args, v)
	}
	if v := c.R.URL.Query().Get("community_id"); v != "" {
		q += ` AND p.community_id=` + next()
		args = append(args, v)
	}
	q += ` ORDER BY p.id DESC LIMIT 200`
	rows, err := db.Query(q, args...)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	defer rows.Close()
	list := []ChildZonePlan{}
	for rows.Next() {
		p, err := scanPlan(rows)
		if err != nil {
			jsonErr(c.W, 500, err.Error())
			return
		}
		if c.User.Role == "resident" {
			// 居民视图不泄露园方联系电话
			p.ContactPhone = ""
		}
		list = append(list, *p)
	}
	jsonOK(c.W, list)
}

type Reminder struct {
	ID               int64      `json:"id"`
	PlanID           int64      `json:"plan_id"`
	Audience         string     `json:"audience"`
	AudienceLabel    string     `json:"audience_label"`
	Channel          string     `json:"channel"`
	Title            string     `json:"title"`
	Content          string     `json:"content"`
	AvoidPeriod      string     `json:"avoid_period"`
	ContactInfo      string     `json:"contact_info"`
	DeliveryStatus   string     `json:"delivery_status"`
	DeliveryLabel    string     `json:"delivery_status_label"`
	SentAt           *time.Time `json:"sent_at"`
	DeliveredAt      *time.Time `json:"delivered_at"`
	CreatedAt        time.Time  `json:"created_at"`
}

func hGetChildZonePlan(c *Ctx) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	p, err := scanPlan(db.QueryRow(planSelect+` WHERE p.id=$1`, id))
	if err != nil {
		jsonErr(c.W, 404, "计划不存在")
		return
	}
	u := c.User
	switch u.Role {
	case "street", "supervisor", "operator":
		// 街道/卫生监督/消杀队：全量可读
	case "property", "grid":
		if u.CommunityID == nil || *u.CommunityID != p.CommunityID {
			jsonErr(c.W, 403, "仅可查看本小区的错峰消杀计划")
			return
		}
	case "kindergarten":
		if !planBoundToUser(id, u.ID) {
			jsonErr(c.W, 404, "计划不存在")
			return
		}
	case "resident":
		// 居民：仅本小区且园方确认后的计划，返回公开视图（安全间隔/恢复时间/避让信息），不含园方电话与提醒明细
		if u.CommunityID == nil || *u.CommunityID != p.CommunityID || p.Status != "confirmed" {
			jsonErr(c.W, 404, "计划不存在或未公开")
			return
		}
		jsonOK(c.W, map[string]any{"plan": publicPlanView(p), "reminders": []any{}})
		return
	default:
		jsonErr(c.W, 403, "无权限")
		return
	}
	rems := loadPlanReminders(id)
	jsonOK(c.W, map[string]any{"plan": p, "reminders": rems})
}

// planBoundToUser 计划所属活动区是否绑定该园方联系人
func planBoundToUser(planID, userID int64) bool {
	var n int
	db.QueryRow(`SELECT count(*) FROM child_zone_plans p JOIN child_zones z ON z.id=p.child_zone_id
		WHERE p.id=$1 AND z.contact_user_id=$2`, planID, userID).Scan(&n)
	return n > 0
}

// publicPlanView 居民公开视图：仅安全间隔、恢复时间与避让信息
func publicPlanView(p *ChildZonePlan) map[string]any {
	avoid := ""
	if p.RecoveryTime != nil {
		avoid = fmt.Sprintf("%s ~ %s", fmtHM(p.PlannedStart), fmtHM(*p.RecoveryTime))
	}
	return map[string]any{
		"plan_no":               p.PlanNo,
		"zone_name":             p.ZoneName,
		"zone_type_label":       p.ZoneTypeLabel,
		"community_name":        p.CommunityName,
		"status":                p.Status,
		"status_label":          p.StatusLabel,
		"safety_interval_hours": p.SafetyIntervalHours,
		"recovery_time":         p.RecoveryTime,
		"avoid_period":          avoid,
	}
}

func loadPlanReminders(planID int64) []Reminder {
	rows, err := db.Query(`SELECT id, plan_id, audience, channel, title, content, avoid_period, contact_info, delivery_status, sent_at, delivered_at, created_at
		FROM zone_reminders WHERE plan_id=$1 ORDER BY id`, planID)
	if err != nil {
		return []Reminder{}
	}
	defer rows.Close()
	rems := []Reminder{}
	for rows.Next() {
		var r Reminder
		rows.Scan(&r.ID, &r.PlanID, &r.Audience, &r.Channel, &r.Title, &r.Content, &r.AvoidPeriod, &r.ContactInfo, &r.DeliveryStatus, &r.SentAt, &r.DeliveredAt, &r.CreatedAt)
		r.AudienceLabel = labelOf(ReminderAudienceLabels, r.Audience)
		r.DeliveryLabel = labelOf(DeliveryStatusLabels, r.DeliveryStatus)
		rems = append(rems, r)
	}
	return rems
}

func hSendReminders(c *Ctx) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	res, err := db.Exec(`UPDATE zone_reminders SET delivery_status='sent', sent_at=now() WHERE plan_id=$1 AND delivery_status='pending'`, id)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	n, _ := res.RowsAffected()
	db.Exec(`UPDATE child_zone_plans SET status='notified' WHERE id=$1 AND status='planned'`, id)
	jsonOK(c.W, map[string]string{"message": fmt.Sprintf("已发送 %d 条提醒", n)})
}

func hDeliverReminder(c *Ctx) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	rid, ok := pathID(c, "rid")
	if !ok {
		return
	}
	// 归属校验：仅对应幼儿园联系人、街道、实际作业消杀队（关联工单所属队）可更新送达状态
	var communityID int64
	var contactUserID, orderTeamID *int64
	if err := db.QueryRow(`SELECT p.community_id, z.contact_user_id, o.team_id
		FROM child_zone_plans p
		JOIN child_zones z ON z.id=p.child_zone_id
		LEFT JOIN work_orders o ON o.id=p.work_order_id
		WHERE p.id=$1`, id).Scan(&communityID, &contactUserID, &orderTeamID); err != nil {
		jsonErr(c.W, 404, "计划不存在")
		return
	}
	u := c.User
	allowed := false
	switch u.Role {
	case "street":
		allowed = true
	case "kindergarten":
		allowed = contactUserID != nil && *contactUserID == u.ID
	case "operator":
		allowed = orderTeamID != nil && u.TeamID != nil && *orderTeamID == *u.TeamID
	}
	if !allowed {
		jsonErr(c.W, 403, "仅对应幼儿园联系人、街道或实际作业消杀队可更新送达状态")
		return
	}
	res, err := db.Exec(`UPDATE zone_reminders SET delivery_status='delivered', delivered_at=now()
		WHERE id=$1 AND plan_id=$2 AND delivery_status='sent'`, rid, id)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		jsonErr(c.W, 409, "提醒不存在或已送达")
		return
	}
	jsonOK(c.W, map[string]string{"message": "已确认送达"})
}

// hRemoveWarning 作业完成后撤除警示，记入工单
func hRemoveWarning(c *Ctx) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var status string
	var orderID *int64
	var zoneName string
	if err := db.QueryRow(`SELECT p.status, p.work_order_id, z.name FROM child_zone_plans p JOIN child_zones z ON z.id=p.child_zone_id WHERE p.id=$1`, id).
		Scan(&status, &orderID, &zoneName); err != nil {
		jsonErr(c.W, 404, "计划不存在")
		return
	}
	if status != "treated" {
		jsonErr(c.W, 409, "仅「已作业」状态可撤除警示（当前："+labelOf(PlanStatusLabels, status)+"）")
		return
	}
	db.Exec(`UPDATE child_zone_plans SET status='warning_removed', warning_removed_at=now(), warning_removed_by=$1 WHERE id=$2`, c.User.ID, id)
	if orderID != nil {
		addLog(*orderID, c.User, "警示撤除", fmt.Sprintf("儿童活动区「%s」消杀警示牌已撤除，待园方确认", zoneName))
	}
	jsonOK(c.W, map[string]string{"message": "警示已撤除，等待园方确认"})
}

// hConfirmPlan 园方确认 → 居民端可见安全间隔与恢复时间
func hConfirmPlan(c *Ctx) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var req struct {
		Note string `json:"note"`
	}
	decodeBody(c.R, &req)
	var status, zoneName string
	var orderID *int64
	var communityID int64
	var interval float64
	var recovery *time.Time
	var ownerID *int64
	err := db.QueryRow(`SELECT p.status, z.name, p.work_order_id, p.community_id, p.safety_interval_hours, p.recovery_time, z.contact_user_id
		FROM child_zone_plans p JOIN child_zones z ON z.id=p.child_zone_id WHERE p.id=$1`, id).
		Scan(&status, &zoneName, &orderID, &communityID, &interval, &recovery, &ownerID)
	if err != nil {
		jsonErr(c.W, 404, "计划不存在")
		return
	}
	if status != "warning_removed" {
		jsonErr(c.W, 409, "需先完成作业并撤除警示后才能确认（当前："+labelOf(PlanStatusLabels, status)+"）")
		return
	}
	// 确认前必须存在绑定的园方联系人账号，且为当前登录园方；否则一律 4xx，不做任何状态变更
	if ownerID == nil {
		jsonErr(c.W, 409, "该活动区未绑定园方联系人账号，无法确认")
		return
	}
	if *ownerID != c.User.ID {
		jsonErr(c.W, 403, "仅该活动区绑定的园方联系人可确认")
		return
	}
	db.Exec(`UPDATE child_zone_plans SET status='confirmed', confirmed_by=$1, confirmed_at=now(), confirm_note=$2 WHERE id=$3`,
		c.User.ID, req.Note, id)
	recoveryText := "待定"
	if recovery != nil {
		recoveryText = fmtHM(*recovery)
	}
	if orderID != nil {
		addLog(*orderID, c.User, "园方确认", fmt.Sprintf("园方确认「%s」消杀完成：%s", zoneName, req.Note))
	}
	// 居民端可见：安全间隔 + 儿童活动恢复时间
	db.Exec(`INSERT INTO notifications(work_order_id, community_id, title, content, channel, sent_by)
		VALUES($1,$2,$3,$4,'app',$5)`,
		orderID, communityID, "儿童活动恢复通知（"+zoneName+"）",
		fmt.Sprintf("「%s」错峰消杀已完成并经园方确认。药剂安全间隔 %.1f 小时，儿童活动恢复时间：%s。%s",
			zoneName, interval, recoveryText, req.Note), c.User.ID)
	jsonOK(c.W, map[string]string{"message": "园方已确认，居民端已可见安全间隔与恢复时间"})
}
