package main

import (
	"database/sql"
	"fmt"
	"sort"
	"time"
)

func registerDispatchRoutes() {
	handle("GET /api/dispatch/preview", hDispatchPreview, "street")
	handle("POST /api/dispatch/generate", hDispatchGenerate, "street")
	handle("GET /api/teams", hListTeams)
	handle("POST /api/teams/{id}/schedules", hCreateSchedule, "street")
	handle("GET /api/schedules", hListSchedules)
	handle("GET /api/chemicals", hListChemicals)
	handle("POST /api/chemicals", hCreateChemical, "street")
	handle("POST /api/chemicals/{id}/restock", hRestockChemical, "street")
	handle("POST /api/rainfall", hCreateRainfall, "street", "grid")
	handle("GET /api/rainfall", hListRainfall)
}

// ---------- 派单评分 ----------

type candidate struct {
	Kind          string   `json:"kind"` // report | water_point
	RefID         int64    `json:"ref_id"`
	RefNo         string   `json:"ref_no"`
	CommunityID   int64    `json:"community_id"`
	CommunityName string   `json:"community_name"`
	Type          string   `json:"type"`
	TypeLabel     string   `json:"type_label"`
	LocationDesc  string   `json:"location_desc"`
	Score         float64  `json:"score"`
	Reasons       []string `json:"reasons"`
}

// complaintDensity 近30天小区投诉数（Redis 缓存 10 分钟）
func complaintDensity(communityID int64) int {
	key := fmt.Sprintf("density:%d", communityID)
	var n int
	if cacheGet(key, &n) {
		return n
	}
	db.QueryRow(`SELECT count(*) FROM reports WHERE community_id=$1 AND created_at > now() - interval '30 days'`, communityID).Scan(&n)
	cacheSet(key, n, 10*time.Minute)
	return n
}

func rainfallLast7Days(communityID int64) float64 {
	var sum sql.NullFloat64
	db.QueryRow(`SELECT sum(amount_mm) FROM rainfall_records WHERE community_id=$1 AND rain_date > current_date - 7`, communityID).Scan(&sum)
	return sum.Float64
}

func computeCandidates() []candidate {
	list := []candidate{}
	risk := activeRiskPeriod()

	rows, err := db.Query(`
		SELECT r.id, r.report_no, r.community_id, cm.name, r.type, r.location_desc
		FROM reports r JOIN communities cm ON cm.id=r.community_id
		WHERE r.status='pending'`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			c := candidate{Kind: "report"}
			rows.Scan(&c.RefID, &c.RefNo, &c.CommunityID, &c.CommunityName, &c.Type, &c.LocationDesc)
			c.TypeLabel = labelOf(ReportTypes, c.Type)
			list = append(list, c)
		}
	}
	rows2, err := db.Query(`
		SELECT w.id, 'WP'||LPAD(w.id::text,6,'0'), w.community_id, cm.name, w.type, w.location_desc
		FROM water_points w JOIN communities cm ON cm.id=w.community_id
		WHERE w.status='pending'`)
	if err == nil {
		defer rows2.Close()
		for rows2.Next() {
			c := candidate{Kind: "water_point"}
			rows2.Scan(&c.RefID, &c.RefNo, &c.CommunityID, &c.CommunityName, &c.Type, &c.LocationDesc)
			c.TypeLabel = labelOf(WaterPointTypes, c.Type)
			list = append(list, c)
		}
	}

	for i := range list {
		c := &list[i]
		if c.Kind == "report" {
			c.Score = ReportTypeWeight[c.Type]
			c.Reasons = append(c.Reasons, fmt.Sprintf("上报类型「%s」基础分 %.0f", c.TypeLabel, ReportTypeWeight[c.Type]))
		} else {
			c.Score = WaterPointTypeWeight[c.Type]
			c.Reasons = append(c.Reasons, fmt.Sprintf("积水点类型「%s」基础分 %.0f", c.TypeLabel, WaterPointTypeWeight[c.Type]))
		}
		density := complaintDensity(c.CommunityID)
		if density > 0 {
			c.Score += float64(density) * 2
			c.Reasons = append(c.Reasons, fmt.Sprintf("小区近30天投诉 %d 件 +%d", density, density*2))
		}
		rain := rainfallLast7Days(c.CommunityID)
		if rain >= 50 {
			c.Score += 15
			c.Reasons = append(c.Reasons, fmt.Sprintf("近7天降雨 %.0fmm ≥50 +15", rain))
		} else if rain >= 20 {
			c.Score += 8
			c.Reasons = append(c.Reasons, fmt.Sprintf("近7天降雨 %.0fmm ≥20 +8", rain))
		}
		if c.Kind == "water_point" {
			var isKey bool
			db.QueryRow(`SELECT is_key FROM water_points WHERE id=$1`, c.RefID).Scan(&isKey)
			if isKey {
				c.Score += 20
				c.Reasons = append(c.Reasons, "重点积水点 +20")
			}
		}
		if risk != nil {
			c.Score = c.Score * 1.5
			c.Reasons = append(c.Reasons, fmt.Sprintf("「%s」重点风险期 ×1.5", risk.Name))
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Score > list[j].Score })
	return list
}

func hDispatchPreview(c *Ctx) {
	// 各小区宠物投诉跟踪（告知调整效果），供下次计划参考
	petTracking := []*PetTracking{}
	rows, err := db.Query(`SELECT DISTINCT community_id FROM pet_complaints`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var id int64
			rows.Scan(&id)
			petTracking = append(petTracking, petTrackingForCommunity(id))
		}
	}
	jsonOK(c.W, map[string]any{
		"risk_period":  activeRiskPeriod(),
		"candidates":   computeCandidates(),
		"pet_tracking": petTracking,
	})
}

// ---------- 生成派单 ----------

func hDispatchGenerate(c *Ctx) {
	var req struct {
		Date string `json:"date"` // 计划消杀日期，默认今天
	}
	decodeBody(c.R, &req)
	if req.Date == "" {
		req.Date = time.Now().Format("2006-01-02")
	}

	// 消杀队当日剩余产能
	type cap struct {
		max, used int
	}
	capacity := map[int64]*cap{}
	rows, err := db.Query(`SELECT team_id, max_orders FROM team_schedules WHERE work_date=$1`, req.Date)
	if err != nil {
		jsonErr(c.W, 500, "查询排班失败: "+err.Error())
		return
	}
	for rows.Next() {
		var tid int64
		var m int
		rows.Scan(&tid, &m)
		capacity[tid] = &cap{max: m}
	}
	rows.Close()
	for tid, cp := range capacity {
		db.QueryRow(`SELECT count(*) FROM work_orders WHERE team_id=$1 AND scheduled_date=$2 AND status != 'closed'`, tid, req.Date).Scan(&cp.used)
	}

	// 默认药剂（库存表第一种），每单预估 2 个单位
	var chemID int64
	var chemName, chemUnit string
	var chemStock float64
	err = db.QueryRow(`SELECT id, name, unit, stock FROM chemicals ORDER BY id LIMIT 1`).Scan(&chemID, &chemName, &chemUnit, &chemStock)
	if err != nil {
		jsonErr(c.W, 400, "尚未配置药剂库存，请先在药剂管理中添加")
		return
	}
	const estNeed = 2.0
	reserved := 0.0

	cands := computeCandidates()
	if len(cands) == 0 {
		jsonOK(c.W, map[string]any{"message": "当前没有待派单的上报或积水点", "created": 0})
		return
	}

	created := []map[string]any{}
	for _, cand := range cands {
		var reportID, wpID sql.NullInt64
		if cand.Kind == "report" {
			reportID = sql.NullInt64{Int64: cand.RefID, Valid: true}
		} else {
			wpID = sql.NullInt64{Int64: cand.RefID, Valid: true}
		}
		// 选择剩余产能最大的队
		var teamID sql.NullInt64
		bestLeft := 0
		for tid, cp := range capacity {
			left := cp.max - cp.used
			if left > bestLeft {
				bestLeft = left
				teamID = sql.NullInt64{Int64: tid, Valid: true}
			}
		}
		status := "pending"
		var schedDate any
		if teamID.Valid {
			status = "assigned"
			schedDate = req.Date
			capacity[teamID.Int64].used++
		}
		var orderID int64
		err := db.QueryRow(`INSERT INTO work_orders(community_id, water_point_id, report_id, team_id, priority, status, scheduled_date, created_by)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
			cand.CommunityID, wpID, reportID, teamID, cand.Score, status, schedDate, c.User.ID).Scan(&orderID)
		if err != nil {
			jsonErr(c.W, 500, "创建工单失败: "+err.Error())
			return
		}
		db.Exec(`UPDATE work_orders SET order_no='WO'||LPAD(id::text,8,'0') WHERE id=$1`, orderID)
		if reportID.Valid {
			db.Exec(`UPDATE reports SET status='dispatched' WHERE id=$1`, reportID.Int64)
		}
		if wpID.Valid {
			db.Exec(`UPDATE water_points SET status='treating' WHERE id=$1`, wpID.Int64)
		}
		// 工单参与方：街道、消杀队、物业、上报居民
		ensureParty(orderID, "street", &c.User.ID, "派单街道")
		if teamID.Valid {
			addTeamParties(orderID, teamID.Int64)
		}
		addCommunityPropertyParties(orderID, cand.CommunityID)
		if reportID.Valid {
			var rid int64
			if db.QueryRow(`SELECT reporter_id FROM reports WHERE id=$1`, reportID.Int64).Scan(&rid) == nil {
				ensureParty(orderID, "resident", &rid, "上报居民")
			}
		}
		addLog(orderID, c.User, "生成派单", fmt.Sprintf("来源 %s（%s），优先级 %.1f；%s", cand.RefNo, cand.TypeLabel, cand.Score, joinReasons(cand.Reasons)))

		// 药剂库存预估不足 → 工单内自动生成「药剂不足」异常
		chemWarn := false
		if chemStock-reserved < estNeed {
			chemWarn = true
			createIssue(orderID, nil, "chemical_shortage",
				fmt.Sprintf("按每单 %.1f%s 预估，%s 库存 %.1f%s 不足，请街道及时补货", estNeed, chemUnit, chemName, chemStock, chemUnit))
		} else {
			reserved += estNeed
		}
		created = append(created, map[string]any{
			"order_id": orderID, "source": cand.RefNo, "type_label": cand.TypeLabel,
			"community": cand.CommunityName, "score": cand.Score, "status": status,
			"team_assigned": teamID.Valid, "chemical_warning": chemWarn,
		})
	}
	cacheDel("dashboard:closedloop")
	assigned := 0
	for _, x := range created {
		if x["team_assigned"].(bool) {
			assigned++
		}
	}
	jsonOK(c.W, map[string]any{
		"created": len(created), "assigned": assigned, "unassigned": len(created) - assigned,
		"date": req.Date, "orders": created,
	})
}

func joinReasons(rs []string) string {
	s := ""
	for i, r := range rs {
		if i > 0 {
			s += "；"
		}
		s += r
	}
	return s
}

// ---------- 消杀队与排班 ----------

func hListTeams(c *Ctx) {
	rows, err := db.Query(`SELECT t.id, t.name, t.description,
		(SELECT count(*) FROM users u WHERE u.team_id=t.id) AS members
		FROM teams t ORDER BY t.id`)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	defer rows.Close()
	type T struct {
		ID          int64  `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Members     int    `json:"members"`
	}
	list := []T{}
	for rows.Next() {
		var t T
		rows.Scan(&t.ID, &t.Name, &t.Description, &t.Members)
		list = append(list, t)
	}
	jsonOK(c.W, list)
}

func hCreateSchedule(c *Ctx) {
	teamID, ok := pathID(c, "id")
	if !ok {
		return
	}
	var req struct {
		WorkDate  string `json:"work_date"`
		Shift     string `json:"shift"`
		MaxOrders int    `json:"max_orders"`
	}
	if err := decodeBody(c.R, &req); err != nil || req.WorkDate == "" {
		jsonErr(c.W, 400, "请提供排班日期")
		return
	}
	if req.Shift == "" {
		req.Shift = "allday"
	}
	if req.MaxOrders <= 0 {
		req.MaxOrders = 5
	}
	_, err := db.Exec(`INSERT INTO team_schedules(team_id, work_date, shift, max_orders) VALUES($1,$2,$3,$4)
		ON CONFLICT(team_id, work_date, shift) DO UPDATE SET max_orders=EXCLUDED.max_orders`,
		teamID, req.WorkDate, req.Shift, req.MaxOrders)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	jsonOK(c.W, map[string]string{"message": "排班已保存"})
}

func hListSchedules(c *Ctx) {
	date := c.R.URL.Query().Get("date")
	q := `SELECT s.id, s.team_id, t.name, to_char(s.work_date,'YYYY-MM-DD'), s.shift, s.max_orders,
		(SELECT count(*) FROM work_orders o WHERE o.team_id=s.team_id AND o.scheduled_date=s.work_date AND o.status != 'closed') AS used
		FROM team_schedules s JOIN teams t ON t.id=s.team_id`
	args := []any{}
	if date != "" {
		q += ` WHERE s.work_date=$1`
		args = append(args, date)
	} else {
		q += ` WHERE s.work_date >= current_date - 1`
	}
	q += ` ORDER BY s.work_date, s.team_id`
	rows, err := db.Query(q, args...)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	defer rows.Close()
	type S struct {
		ID        int64  `json:"id"`
		TeamID    int64  `json:"team_id"`
		TeamName  string `json:"team_name"`
		WorkDate  string `json:"work_date"`
		Shift     string `json:"shift"`
		MaxOrders int    `json:"max_orders"`
		Used      int    `json:"used"`
	}
	list := []S{}
	for rows.Next() {
		var s S
		rows.Scan(&s.ID, &s.TeamID, &s.TeamName, &s.WorkDate, &s.Shift, &s.MaxOrders, &s.Used)
		list = append(list, s)
	}
	jsonOK(c.W, list)
}

// ---------- 药剂库存 ----------

func hListChemicals(c *Ctx) {
	rows, err := db.Query(`SELECT id, name, unit, stock, safe_stock, updated_at FROM chemicals ORDER BY id`)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	defer rows.Close()
	type Chem struct {
		ID        int64     `json:"id"`
		Name      string    `json:"name"`
		Unit      string    `json:"unit"`
		Stock     float64   `json:"stock"`
		SafeStock float64   `json:"safe_stock"`
		Low       bool      `json:"low"`
		UpdatedAt time.Time `json:"updated_at"`
	}
	list := []Chem{}
	for rows.Next() {
		var ch Chem
		rows.Scan(&ch.ID, &ch.Name, &ch.Unit, &ch.Stock, &ch.SafeStock, &ch.UpdatedAt)
		ch.Low = ch.Stock < ch.SafeStock
		list = append(list, ch)
	}
	jsonOK(c.W, list)
}

func hCreateChemical(c *Ctx) {
	var req struct {
		Name      string  `json:"name"`
		Unit      string  `json:"unit"`
		Stock     float64 `json:"stock"`
		SafeStock float64 `json:"safe_stock"`
	}
	if err := decodeBody(c.R, &req); err != nil || req.Name == "" {
		jsonErr(c.W, 400, "请提供药剂名称")
		return
	}
	if req.Unit == "" {
		req.Unit = "升"
	}
	var id int64
	err := db.QueryRow(`INSERT INTO chemicals(name, unit, stock, safe_stock) VALUES($1,$2,$3,$4) RETURNING id`,
		req.Name, req.Unit, req.Stock, req.SafeStock).Scan(&id)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	jsonOK(c.W, map[string]any{"id": id, "message": "药剂已添加"})
}

func hRestockChemical(c *Ctx) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var req struct {
		Amount float64 `json:"amount"`
	}
	if err := decodeBody(c.R, &req); err != nil || req.Amount <= 0 {
		jsonErr(c.W, 400, "补货数量必须大于 0")
		return
	}
	_, err := db.Exec(`UPDATE chemicals SET stock=stock+$1, updated_at=now() WHERE id=$2`, req.Amount, id)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	jsonOK(c.W, map[string]string{"message": "补货成功"})
}

// ---------- 降雨记录 ----------

func hCreateRainfall(c *Ctx) {
	var req struct {
		CommunityID int64   `json:"community_id"`
		RainDate    string  `json:"rain_date"`
		AmountMM    float64 `json:"amount_mm"`
	}
	if err := decodeBody(c.R, &req); err != nil || req.CommunityID == 0 || req.RainDate == "" {
		jsonErr(c.W, 400, "请提供小区、日期与降雨量")
		return
	}
	_, err := db.Exec(`INSERT INTO rainfall_records(community_id, rain_date, amount_mm) VALUES($1,$2,$3)
		ON CONFLICT(community_id, rain_date) DO UPDATE SET amount_mm=EXCLUDED.amount_mm`,
		req.CommunityID, req.RainDate, req.AmountMM)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	jsonOK(c.W, map[string]string{"message": "降雨记录已保存"})
}

func hListRainfall(c *Ctx) {
	rows, err := db.Query(`SELECT r.id, r.community_id, cm.name, to_char(r.rain_date,'YYYY-MM-DD'), r.amount_mm
		FROM rainfall_records r JOIN communities cm ON cm.id=r.community_id
		WHERE r.rain_date > current_date - 14 ORDER BY r.rain_date DESC, r.community_id LIMIT 100`)
	if err != nil {
		jsonErr(c.W, 500, err.Error())
		return
	}
	defer rows.Close()
	type R struct {
		ID            int64   `json:"id"`
		CommunityID   int64   `json:"community_id"`
		CommunityName string  `json:"community_name"`
		RainDate      string  `json:"rain_date"`
		AmountMM      float64 `json:"amount_mm"`
	}
	list := []R{}
	for rows.Next() {
		var r R
		rows.Scan(&r.ID, &r.CommunityID, &r.CommunityName, &r.RainDate, &r.AmountMM)
		list = append(list, r)
	}
	jsonOK(c.W, list)
}
