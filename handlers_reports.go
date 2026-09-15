package main

import (
	"fmt"
	"strings"
	"time"
)

func registerReportRoutes() {
	handle("GET /api/meta", hMeta)
	handle("POST /api/reports", hCreateReport, "resident", "property", "grid")
	handle("GET /api/reports", hListReports)
	handle("GET /api/reports/{id}", hGetReport)
	handle("GET /api/water-points", hListWaterPoints)
	handle("POST /api/water-points", hCreateWaterPoint, "grid", "street", "property")
	handle("GET /api/water-points/key-list", hKeyWaterPoints, "street", "supervisor", "operator", "grid")
	handle("POST /api/water-points/refresh-key", hRefreshKeyPoints, "street")
	handle("POST /api/notifications", hCreateNotification, "street", "property", "operator")
	handle("GET /api/notifications", hListNotifications)
}

func hMeta(c *Ctx) {
	type comm struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	comms := []comm{}
	rows, err := db.Query(`SELECT id, name FROM communities ORDER BY id`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var x comm
			rows.Scan(&x.ID, &x.Name)
			comms = append(comms, x)
		}
	}
	type team struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	teams := []team{}
	rows2, err := db.Query(`SELECT id, name FROM teams ORDER BY id`)
	if err == nil {
		defer rows2.Close()
		for rows2.Next() {
			var x team
			rows2.Scan(&x.ID, &x.Name)
			teams = append(teams, x)
		}
	}
	type userBrief struct {
		ID          int64  `json:"id"`
		Name        string `json:"name"`
		Role        string `json:"role"`
		CommunityID *int64 `json:"community_id"`
		TeamID      *int64 `json:"team_id"`
	}
	users := []userBrief{}
	if c.User.Role == "street" || c.User.Role == "supervisor" {
		rows3, err := db.Query(`SELECT id, name, role, community_id, team_id FROM users ORDER BY role, id`)
		if err == nil {
			defer rows3.Close()
			for rows3.Next() {
				var x userBrief
				rows3.Scan(&x.ID, &x.Name, &x.Role, &x.CommunityID, &x.TeamID)
				users = append(users, x)
			}
		}
	}
	jsonOK(c.W, map[string]any{
		"report_types":              ReportTypes,
		"water_point_types":         WaterPointTypes,
		"issue_types":               IssueTypes,
		"report_status_labels":      ReportStatusLabels,
		"water_point_status_labels": WaterPointStatusLabels,
		"order_status_labels":       OrderStatusLabels,
		"rectification_status_labels": RectificationStatusLabels,
		"prop_rect_status_labels":     PropRectStatusLabels,
		"prop_rect_method_labels":     PropRectMethodLabels,
		"prop_rect_reminder_kind_labels": PropRectReminderKindLabels,
		"issue_status_labels":       IssueStatusLabels,
		"role_labels":               RoleLabels,
		"child_zone_types":          ChildZoneTypes,
		"plan_status_labels":        PlanStatusLabels,
		"reminder_audience_labels":  ReminderAudienceLabels,
		"delivery_status_labels":    DeliveryStatusLabels,
		"wind_directions":           WindDirections,
		"pet_types":                 PetTypes,
		"pet_complaint_status_labels": PetComplaintStatusLabels,
		"community_risk_labels":       CommunityRiskLabels,
		"emerg_trigger_labels":        EmergTriggerLabels,
		"emerg_community_role_labels": EmergCommunityRoleLabels,
		"emerg_case_status_labels":    EmergCaseStatusLabels,
		"emerg_resource_labels":       EmergResourceLabels,
		"communities":               comms,
		"teams":                     teams,
		"users":                     users,
	})
}

// ---------- 上报 ----------

type Report struct {
	ID              int64      `json:"id"`
	ReportNo        string     `json:"report_no"`
	ReporterID      int64      `json:"reporter_id"`
	ReporterName    string     `json:"reporter_name"`
	ReporterRole    string     `json:"reporter_role"`
	CommunityID     int64      `json:"community_id"`
	CommunityName   string     `json:"community_name"`
	Type            string     `json:"type"`
	TypeLabel       string     `json:"type_label"`
	LocationDesc    string     `json:"location_desc"`
	Latitude        *float64   `json:"latitude"`
	Longitude       *float64   `json:"longitude"`
	Photos          StringList `json:"photos"`
	NearbyPopulation string    `json:"nearby_population"`
	HasPets         bool       `json:"has_pets"`
	Description     string     `json:"description"`
	Status          string     `json:"status"`
	StatusLabel     string     `json:"status_label"`
	CreatedAt       time.Time  `json:"created_at"`
}

const reportSelect = `
	SELECT r.id, r.report_no, r.reporter_id, u.name, u.role, r.community_id, cm.name,
	       r.type, r.location_desc, r.latitude, r.longitude, r.photos, r.nearby_population,
	       r.has_pets, r.description, r.status, r.created_at
	FROM reports r
	JOIN users u ON u.id = r.reporter_id
	JOIN communities cm ON cm.id = r.community_id`

func scanReport(row interface{ Scan(...any) error }) (*Report, error) {
	var r Report
	err := row.Scan(&r.ID, &r.ReportNo, &r.ReporterID, &r.ReporterName, &r.ReporterRole,
		&r.CommunityID, &r.CommunityName, &r.Type, &r.LocationDesc, &r.Latitude, &r.Longitude,
		&r.Photos, &r.NearbyPopulation, &r.HasPets, &r.Description, &r.Status, &r.CreatedAt)
	if err != nil {
		return nil, err
	}
	r.TypeLabel = labelOf(ReportTypes, r.Type)
	r.StatusLabel = labelOf(ReportStatusLabels, r.Status)
	return &r, nil
}

func hCreateReport(c *Ctx) {
	var req struct {
		CommunityID      int64    `json:"community_id"`
		Type             string   `json:"type"`
		LocationDesc     string   `json:"location_desc"`
		Latitude         *float64 `json:"latitude"`
		Longitude        *float64 `json:"longitude"`
		Photos           []string `json:"photos"`
		NearbyPopulation string   `json:"nearby_population"`
		HasPets          bool     `json:"has_pets"`
		Description      string   `json:"description"`
	}
	if err := decodeBody(c.R, &req); err != nil {
		jsonErr(c.W, 400, "请求格式错误")
		return
	}
	if _, ok := ReportTypes[req.Type]; !ok {
		jsonErr(c.W, 400, "上报类型非法")
		return
	}
	if req.LocationDesc == "" {
		jsonErr(c.W, 400, "请填写位置描述")
		return
	}
	// 居民/物业只能报到本小区；网格员可代报任意小区
	if c.User.Role != "grid" && c.User.CommunityID != nil {
		req.CommunityID = *c.User.CommunityID
	}
	if req.CommunityID == 0 {
		jsonErr(c.W, 400, "请选择小区")
		return
	}
	photos := StringList(req.Photos)
	var id int64
	err := db.QueryRow(`INSERT INTO reports(reporter_id, community_id, type, location_desc, latitude, longitude, photos, nearby_population, has_pets, description)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id`,
		c.User.ID, req.CommunityID, req.Type, req.LocationDesc, req.Latitude, req.Longitude,
		photos, req.NearbyPopulation, req.HasPets, req.Description).Scan(&id)
	if err != nil {
		jsonErr(c.W, 500, "上报保存失败: "+err.Error())
		return
	}
	db.Exec(`UPDATE reports SET report_no='RPT'||LPAD(id::text,8,'0') WHERE id=$1`, id)

	// 积水类上报自动生成积水点，进入待派单池
	if wpType, ok := ReportToWaterType[req.Type]; ok {
		db.Exec(`INSERT INTO water_points(community_id, type, location_desc, source, report_id, discovered_by)
			VALUES($1,$2,$3,'report',$4,$5)`, req.CommunityID, wpType, req.LocationDesc, id, c.User.ID)
	}
	// 投诉密度突增 → 自动提升小区风险等级（重点风险期应急响应）
	maybeElevateCommunityRisk(req.CommunityID)
	cacheDel(fmt.Sprintf("density:%d", req.CommunityID), "dashboard:closedloop")
	row := db.QueryRow(reportSelect+` WHERE r.id=$1`, id)
	rep, err := scanReport(row)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	jsonOK(c.W, rep)
}

func hListReports(c *Ctx) {
	q := reportSelect + ` WHERE 1=1`
	args := []any{}
	i := 0
	next := func() string { i++; return fmt.Sprintf("$%d", i) }

	switch c.User.Role {
	case "resident":
		q += ` AND r.reporter_id=` + next()
		args = append(args, c.User.ID)
	case "property", "grid":
		if c.User.CommunityID != nil {
			q += ` AND r.community_id=` + next()
			args = append(args, *c.User.CommunityID)
		}
	case "operator":
		jsonOK(c.W, []Report{})
		return
	}
	if v := c.R.URL.Query().Get("status"); v != "" {
		q += ` AND r.status=` + next()
		args = append(args, v)
	}
	if v := c.R.URL.Query().Get("type"); v != "" {
		q += ` AND r.type=` + next()
		args = append(args, v)
	}
	if v := c.R.URL.Query().Get("community_id"); v != "" {
		q += ` AND r.community_id=` + next()
		args = append(args, v)
	}
	q += ` ORDER BY r.created_at DESC LIMIT 200`
	rows, err := db.Query(q, args...)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	defer rows.Close()
	list := []Report{}
	for rows.Next() {
		r, err := scanReport(rows)
		if err != nil {
			jsonErr(c.W, 500, err.Error())
			return
		}
		list = append(list, *r)
	}
	jsonOK(c.W, list)
}

func hGetReport(c *Ctx) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	row := db.QueryRow(reportSelect+` WHERE r.id=$1`, id)
	rep, err := scanReport(row)
	if err != nil {
		jsonErr(c.W, 404, "上报不存在")
		return
	}
	if c.User.Role == "resident" && rep.ReporterID != c.User.ID {
		jsonErr(c.W, 403, "只能查看自己的上报")
		return
	}
	jsonOK(c.W, rep)
}

// ---------- 积水点 ----------

type WaterPoint struct {
	ID            int64      `json:"id"`
	CommunityID   int64      `json:"community_id"`
	CommunityName string     `json:"community_name"`
	Type          string     `json:"type"`
	TypeLabel     string     `json:"type_label"`
	LocationDesc  string     `json:"location_desc"`
	Source        string     `json:"source"`
	ReportID      *int64     `json:"report_id"`
	Status        string     `json:"status"`
	StatusLabel   string     `json:"status_label"`
	LarvaeFound   bool       `json:"larvae_found"`
	IsKey         bool       `json:"is_key"`
	KeyReason     string     `json:"key_reason"`
	LastTreatedAt *time.Time `json:"last_treated_at"`
	LastRecheckAt *time.Time `json:"last_recheck_at"`
	CreatedAt     time.Time  `json:"created_at"`
}

const waterPointSelect = `
	SELECT w.id, w.community_id, cm.name, w.type, w.location_desc, w.source, w.report_id,
	       w.status, w.larvae_found, w.is_key, w.key_reason, w.last_treated_at, w.last_recheck_at, w.created_at
	FROM water_points w JOIN communities cm ON cm.id = w.community_id`

func scanWaterPoint(row interface{ Scan(...any) error }) (*WaterPoint, error) {
	var w WaterPoint
	err := row.Scan(&w.ID, &w.CommunityID, &w.CommunityName, &w.Type, &w.LocationDesc, &w.Source,
		&w.ReportID, &w.Status, &w.LarvaeFound, &w.IsKey, &w.KeyReason, &w.LastTreatedAt, &w.LastRecheckAt, &w.CreatedAt)
	if err != nil {
		return nil, err
	}
	w.TypeLabel = labelOf(WaterPointTypes, w.Type)
	w.StatusLabel = labelOf(WaterPointStatusLabels, w.Status)
	return &w, nil
}

func hListWaterPoints(c *Ctx) {
	q := waterPointSelect + ` WHERE 1=1`
	args := []any{}
	i := 0
	next := func() string { i++; return fmt.Sprintf("$%d", i) }
	if v := c.R.URL.Query().Get("status"); v != "" {
		q += ` AND w.status=` + next()
		args = append(args, v)
	}
	if v := c.R.URL.Query().Get("type"); v != "" {
		q += ` AND w.type=` + next()
		args = append(args, v)
	}
	if v := c.R.URL.Query().Get("community_id"); v != "" {
		q += ` AND w.community_id=` + next()
		args = append(args, v)
	}
	if c.R.URL.Query().Get("key") == "1" {
		q += ` AND w.is_key=true`
	}
	q += ` ORDER BY w.is_key DESC, w.created_at DESC LIMIT 300`
	rows, err := db.Query(q, args...)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	defer rows.Close()
	list := []WaterPoint{}
	for rows.Next() {
		w, err := scanWaterPoint(rows)
		if err != nil {
			jsonErr(c.W, 500, err.Error())
			return
		}
		list = append(list, *w)
	}
	jsonOK(c.W, list)
}

func hCreateWaterPoint(c *Ctx) {
	var req struct {
		CommunityID  int64  `json:"community_id"`
		Type         string `json:"type"`
		LocationDesc string `json:"location_desc"`
	}
	if err := decodeBody(c.R, &req); err != nil {
		jsonErr(c.W, 400, "请求格式错误")
		return
	}
	if _, ok := WaterPointTypes[req.Type]; !ok {
		jsonErr(c.W, 400, "积水点类型非法")
		return
	}
	if req.CommunityID == 0 || req.LocationDesc == "" {
		jsonErr(c.W, 400, "请填写小区与位置")
		return
	}
	var id int64
	err := db.QueryRow(`INSERT INTO water_points(community_id, type, location_desc, source, discovered_by)
		VALUES($1,$2,$3,'manual',$4) RETURNING id`, req.CommunityID, req.Type, req.LocationDesc, c.User.ID).Scan(&id)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	cacheDel("dashboard:closedloop")
	row := db.QueryRow(waterPointSelect+` WHERE w.id=$1`, id)
	w, err := scanWaterPoint(row)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	jsonOK(c.W, w)
}

// refreshKeyWaterPoints 依据规则自动标记重点积水点
func refreshKeyWaterPoints() {
	rows, err := db.Query(`
		SELECT w.id, w.type, w.larvae_found, w.status, w.community_id,
		       (SELECT count(*) FROM reports r WHERE r.community_id=w.community_id
		          AND r.created_at > now() - interval '30 days') AS complaints
		FROM water_points w`)
	if err != nil {
		return
	}
	defer rows.Close()
	keyTypes := map[string]bool{"construction_site": true, "rooftop_tank": true, "rain_well": true, "underground_garage": true}
	for rows.Next() {
		var id, commID int64
		var typ, status string
		var larvae bool
		var complaints int
		if rows.Scan(&id, &typ, &larvae, &status, &commID, &complaints) != nil {
			continue
		}
		reasons := []string{}
		if larvae {
			reasons = append(reasons, "曾发现幼虫")
		}
		if keyTypes[typ] {
			reasons = append(reasons, "重点类型:"+labelOf(WaterPointTypes, typ))
		}
		if complaints >= 3 {
			reasons = append(reasons, "小区近30天投诉密集")
		}
		isKey := len(reasons) > 0 && status != "cleared"
		db.Exec(`UPDATE water_points SET is_key=$1, key_reason=$2 WHERE id=$3`, isKey, strings.Join(reasons, "；"), id)
	}
}

func hKeyWaterPoints(c *Ctx) {
	refreshKeyWaterPoints()
	rows, err := db.Query(waterPointSelect + ` WHERE w.is_key=true AND w.status != 'cleared' ORDER BY w.community_id, w.id`)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	defer rows.Close()
	list := []WaterPoint{}
	for rows.Next() {
		w, err := scanWaterPoint(rows)
		if err != nil {
			jsonErr(c.W, 500, err.Error())
			return
		}
		list = append(list, *w)
	}
	jsonOK(c.W, map[string]any{
		"risk_period": activeRiskPeriod(),
		"list":        list,
	})
}

func hRefreshKeyPoints(c *Ctx) {
	refreshKeyWaterPoints()
	jsonOK(c.W, map[string]string{"message": "重点积水点已按规则重新标记"})
}

// ---------- 居民告知 ----------

func hCreateNotification(c *Ctx) {
	var req struct {
		WorkOrderID *int64 `json:"work_order_id"`
		CommunityID int64  `json:"community_id"`
		Title       string `json:"title"`
		Content     string `json:"content"`
		Channel     string `json:"channel"`
	}
	if err := decodeBody(c.R, &req); err != nil {
		jsonErr(c.W, 400, "请求格式错误")
		return
	}
	if req.Content == "" {
		jsonErr(c.W, 400, "请填写告知内容")
		return
	}
	if req.Channel == "" {
		req.Channel = "app"
	}
	// 物业只能向本小区发布
	if c.User.Role == "property" && c.User.CommunityID != nil {
		req.CommunityID = *c.User.CommunityID
	}
	if req.CommunityID == 0 && req.WorkOrderID != nil {
		db.QueryRow(`SELECT community_id FROM work_orders WHERE id=$1`, *req.WorkOrderID).Scan(&req.CommunityID)
	}
	if req.CommunityID == 0 {
		jsonErr(c.W, 400, "请选择小区")
		return
	}
	var id int64
	err := db.QueryRow(`INSERT INTO notifications(work_order_id, community_id, title, content, channel, sent_by)
		VALUES($1,$2,$3,$4,$5,$6) RETURNING id`,
		req.WorkOrderID, req.CommunityID, req.Title, req.Content, req.Channel, c.User.ID).Scan(&id)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	if req.WorkOrderID != nil {
		addLog(*req.WorkOrderID, c.User, "居民告知", fmt.Sprintf("【%s】%s", req.Title, req.Content))
	}
	jsonOK(c.W, map[string]any{"id": id, "message": "居民告知已发布"})
}

func hListNotifications(c *Ctx) {
	q := `SELECT n.id, n.work_order_id, n.community_id, cm.name, n.title, n.content, n.channel, u.name, n.created_at
		FROM notifications n JOIN communities cm ON cm.id=n.community_id LEFT JOIN users u ON u.id=n.sent_by WHERE 1=1`
	args := []any{}
	i := 0
	next := func() string { i++; return fmt.Sprintf("$%d", i) }
	if c.User.Role == "resident" || c.User.Role == "property" {
		if c.User.CommunityID != nil {
			q += ` AND n.community_id=` + next()
			args = append(args, *c.User.CommunityID)
		}
	}
	if v := c.R.URL.Query().Get("community_id"); v != "" {
		q += ` AND n.community_id=` + next()
		args = append(args, v)
	}
	if v := c.R.URL.Query().Get("work_order_id"); v != "" {
		q += ` AND n.work_order_id=` + next()
		args = append(args, v)
	}
	q += ` ORDER BY n.created_at DESC LIMIT 100`
	rows, err := db.Query(q, args...)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	defer rows.Close()
	type N struct {
		ID            int64     `json:"id"`
		WorkOrderID   *int64    `json:"work_order_id"`
		CommunityID   int64     `json:"community_id"`
		CommunityName string    `json:"community_name"`
		Title         string    `json:"title"`
		Content       string    `json:"content"`
		Channel       string    `json:"channel"`
		SentBy        *string   `json:"sent_by"`
		CreatedAt     time.Time `json:"created_at"`
	}
	list := []N{}
	for rows.Next() {
		var n N
		if err := rows.Scan(&n.ID, &n.WorkOrderID, &n.CommunityID, &n.CommunityName, &n.Title, &n.Content, &n.Channel, &n.SentBy, &n.CreatedAt); err != nil {
			jsonErr(c.W, 500, err.Error())
			return
		}
		list = append(list, n)
	}
	jsonOK(c.W, list)
}
