#!/usr/bin/env python3
"""关键业务流验证：上报→派单→消杀→复查→异常协同→整改→告知→闭环→看板"""
import json, urllib.request, sys

import os
B = os.environ.get("BASE_URL", "http://localhost:3076")
PASS, FAIL = 0, 0

def call(method, path, token=None, body=None, expect_ok=True):
    req = urllib.request.Request(B + path, method=method)
    req.add_header("Content-Type", "application/json")
    if token: req.add_header("Authorization", "Bearer " + token)
    data = None
    if body is not None: data = json.dumps(body).encode()
    try:
        with urllib.request.urlopen(req, data, timeout=15) as r:
            return r.status, json.loads(r.read())
    except urllib.error.HTTPError as e:
        return e.code, json.loads(e.read() or b"{}")

def check(name, cond, extra=""):
    global PASS, FAIL
    if cond: PASS += 1; print(f"  ✔ {name} {extra}")
    else: FAIL += 1; print(f"  ✘ {name} {extra}")

def login(u, p):
    s, r = call("POST", "/api/auth/login", body={"username": u, "password": p})
    assert s == 200, f"login {u} failed: {r}"
    return r["data"]["token"]

print("== 1. 六角色登录 ==")
toks = {
    "resident": login("resident", "Resident@123"),
    "property": login("property", "Property@123"),
    "grid": login("grid", "Grid@123"),
    "operator": login("operator", "Operator@123"),
    "street": login("street", "Street@123"),
    "supervisor": login("supervisor", "Supervisor@123"),
}
check("六角色全部登录成功", all(toks.values()))

print("== 2. 居民上报（位置/照片/周边人群/宠物） ==")
s, r = call("POST", "/api/reports", toks["resident"], {
    "type": "child_bite", "location_desc": "中心花园儿童滑梯旁", "nearby_population": "老人儿童居多",
    "has_pets": True, "photos": ["https://example.com/bite1.jpg"], "description": "孩子被叮咬多处红肿"})
check("儿童被叮咬上报", s == 200 and r["data"]["report_no"].startswith("RPT"))
s, r = call("POST", "/api/reports", toks["resident"], {
    "type": "rooftop_water", "location_desc": "5号楼楼顶", "nearby_population": "顶楼住户",
    "has_pets": False, "description": "雨后积水两天"})
roof_report = r["data"]
check("楼顶积水上报", s == 200)
s, r = call("GET", "/api/water-points?type=rooftop_water", toks["street"])
auto_wp = [w for w in r["data"] if w["source"] == "report"]
check("积水类上报自动生成积水点", len(auto_wp) >= 1)

print("== 3. 街道派单（评分含降雨/投诉密度/风险期） ==")
s, r = call("GET", "/api/dispatch/preview", toks["street"])
cands = r["data"]["candidates"]
check("派单预览有候选", len(cands) > 0, f"({len(cands)} 条)")
check("评分含风险期加权", any("风险期" in "".join(c["reasons"]) for c in cands))
check("评分含降雨因子", any("降雨" in "".join(c["reasons"]) for c in cands))
s, r = call("POST", "/api/dispatch/generate", toks["street"], {"date": "2026-09-15"})
gen = r["data"]
check("生成派单", s == 200 and gen["created"] > 0, f"(创建 {gen['created']}，指派 {gen['assigned']})")

print("== 4. 消杀人员开工并提交消杀记录 ==")
s, r = call("GET", "/api/work-orders?status=assigned", toks["operator"])
orders = r["data"]
check("消杀一队有已派工单", len(orders) > 0, f"({len(orders)} 单)")
oid = orders[0]["id"]
s, r = call("POST", f"/api/work-orders/{oid}/start", toks["operator"], {})
check("到场开工", s == 200)
s, r = call("GET", "/api/chemicals", toks["operator"])
chem = r["data"][0]
stock_before = chem["stock"]
s, r = call("POST", f"/api/work-orders/{oid}/treatments", toks["operator"], {
    "chemical_id": chem["id"], "concentration": "1:100", "spray_area": "中心花园及周边绿化带",
    "chemical_used": 2.5, "warning_sign": True, "resident_notified": True, "pet_avoided": True,
    "new_water_point_found": True, "new_water_point_desc": "5号楼后废弃花盆积水"})
check("提交消杀记录", s == 200, f"({r['data']['message']})")
s, r = call("GET", "/api/chemicals", toks["operator"])
check("药剂库存已扣减", abs(r["data"][0]["stock"] - (stock_before - 2.5)) < 1e-9,
      f"({stock_before} -> {r['data'][0]['stock']})")
s, r = call("GET", "/api/work-orders/" + str(oid), toks["operator"])
d = r["data"]
check("工单转待复查且有复查期限", d["order"]["status"] == "recheck_pending" and d["order"]["recheck_due_at"])
check("消杀记录含警示牌/告知/宠物避让", d["treatments"][0]["warning_sign"] and d["treatments"][0]["resident_notified"] and d["treatments"][0]["pet_avoided"])
s, r = call("GET", "/api/water-points?type=other", toks["street"])
check("新积水点已登记", any("废弃花盆" in w["location_desc"] for w in r["data"]))

print("== 5. 复查发现幼虫 → 五方协同 ==")
s, r = call("POST", f"/api/work-orders/{oid}/rechecks", toks["supervisor"], {"result": "larvae_found", "notes": "集水井仍见孑孓"})
check("复查提交（有幼虫）", s == 200)
s, r = call("GET", "/api/work-orders/" + str(oid), toks["street"])
d = r["data"]
roles = {p["role"] for p in d["parties"]}
check("工单升级为异常协同", d["order"]["status"] == "escalated")
check("五方（居民/物业/消杀/街道/卫监）同单协同", {"resident","property","operator","street","supervisor"} <= roles, f"({roles})")
check("自动生成「复查仍有幼虫」异常", any(i["type"] == "larvae_remaining" and i["status"] == "open" for i in d["issues"]))

print("== 6. 物业设施异常 + 整改流转 ==")
s, r = call("POST", f"/api/work-orders/{oid}/issues", toks["property"], {"type": "property_facility", "description": "积水由楼顶水箱排水管破裂造成"})
check("物业设施异常上报", s == 200)
s, r = call("POST", f"/api/work-orders/{oid}/rectifications", toks["street"], {"description": "修复排水管并清理积水", "deadline": "2026-09-20"})
check("街道下发整改", s == 200)
rect_id = r["data"]["id"]
s, r = call("GET", "/api/rectifications", toks["property"])
check("物业可见整改任务", any(x["id"] == rect_id for x in r["data"]))
s, r = call("POST", f"/api/rectifications/{rect_id}/start", toks["property"], {})
check("物业受理", s == 200)
s, r = call("POST", f"/api/rectifications/{rect_id}/complete", toks["property"], {})
check("物业完成整改", s == 200)
s, r = call("POST", f"/api/rectifications/{rect_id}/verify", toks["supervisor"], {"pass": True, "notes": "现场核验通过"})
check("卫生监督核验通过", s == 200)

print("== 7. 异常解决 + 居民告知 ==")
s, r = call("GET", "/api/work-orders/" + str(oid), toks["street"])
for i in r["data"]["issues"]:
    if i["status"] == "open":
        s2, r2 = call("POST", f"/api/work-orders/{oid}/issues/{i['id']}/resolve", toks["street"], {"resolution": "已协调处理完毕"})
        assert s2 == 200, r2
s, r = call("GET", "/api/work-orders/" + str(oid), toks["street"])
check("异常全部解决", all(i["status"] == "resolved" for i in r["data"]["issues"]))
s, r = call("POST", "/api/notifications", toks["street"], {"work_order_id": oid, "title": "消杀作业通知", "content": "本周五上午对中心花园消杀，请看管宠物", "channel": "app"})
check("发布居民告知", s == 200)
s, r = call("GET", "/api/notifications", toks["resident"])
check("居民可见告知", any("消杀作业通知" in (n["title"] or "") for n in r["data"]))

print("== 8. 二次消杀 → 复查通过 → 自动闭环 ==")
s, r = call("POST", f"/api/work-orders/{oid}/treatments", toks["operator"], {
    "chemical_id": chem["id"], "concentration": "1:100", "spray_area": "5号楼楼顶及集水井",
    "chemical_used": 1.5, "warning_sign": True, "resident_notified": True, "pet_avoided": True})
check("二次消杀", s == 200)
s, r = call("POST", f"/api/work-orders/{oid}/rechecks", toks["grid"], {"result": "pass", "notes": "未见幼虫"})
check("复查通过", s == 200)
s, r = call("GET", "/api/work-orders/" + str(oid), toks["street"])
d = r["data"]
check("工单自动闭环", d["order"]["status"] == "closed", f"(状态 {d['order']['status_label']})")
check("闭环条件全满足", all(d["close_conditions"].values()))
s, r = call("GET", "/api/reports", toks["resident"])
check("关联上报已闭环", all(x["status"] == "closed" for x in r["data"] if x["id"] == d["report"]["id"]) if d.get("report") else True)

print("== 9. 闭环看板 / 考核 / 趋势 / 重点清单 / 风险期 ==")
s, r = call("GET", "/api/dashboard/closed-loop", toks["street"])
rows = r["data"]["rows"]
yang = next((x for x in rows if x["community_name"] == "阳光小区"), None)
check("闭环看板有小区数据", yang is not None and yang["orders_closed"] >= 1, f"(阳光小区闭环 {yang['orders_closed'] if yang else 0} 单)")
check("风险期生效且复查间隔缩短", r["data"]["risk_period"] and r["data"]["recheck_interval_days"] == 3)
s, r = call("GET", "/api/dashboard/assessment?month=2026-09", toks["supervisor"])
check("考核：小区维度药剂消耗>0", any(x["chemical_used"] > 0 for x in r["data"]["communities"]))
check("考核：消杀队维度有数据", any(t["treatments"] > 0 for t in r["data"]["teams"]))
s, r = call("GET", "/api/dashboard/complaint-trend", toks["street"])
check("投诉趋势有数据", len(r["data"]) > 0)
s, r = call("GET", "/api/water-points/key-list", toks["street"])
check("重点积水点清单自动生成", len(r["data"]["list"]) > 0, f"({len(r['data']['list'])} 处)")
s, r = call("GET", "/api/risk-periods", toks["street"])
check("风险期列表含登革热", any("登革热" in x["disease"] for x in r["data"]["list"]))

print("== 10. 权限与库存边界 ==")
s, r = call("POST", "/api/dispatch/generate", toks["resident"], {})
check("居民无权派单(403)", s == 403)
s, r = call("GET", "/api/work-orders", None)
check("未登录被拒绝(401)", s == 401)
s, r = call("POST", f"/api/work-orders/{oid}/treatments", toks["operator"], {
    "chemical_id": chem["id"], "spray_area": "x", "chemical_used": 99999})
check("已闭环工单不可再消杀(409)", s == 409)

print("== 11. 儿童活动区错峰消杀 ==")
from datetime import datetime, timedelta
toks["kindergarten"] = login("kindergarten", "Kindergarten@123")
check("园方联系人登录", bool(toks["kindergarten"]))
s, r = call("GET", "/api/child-zones", toks["street"])
zones = r["data"]
zone = next((z for z in zones if "幼儿园" in z["name"]), None)
check("儿童活动区已登记（含联系人/活动时段/家长群）", zone and zone["contact_name"] and zone["activity_times"] and zone["parent_group"])
tomorrow = datetime.now() + timedelta(days=1)
def dt(h, m=0): return (tomorrow.replace(hour=h, minute=m)).strftime("%Y-%m-%d %H:%M")
# 与儿童活动时间冲突 → 应被拒绝（活动时段 16:00-18:00）
s, r = call("POST", "/api/child-zone-plans", toks["street"], {
    "child_zone_id": zone["id"], "planned_start": dt(17), "planned_end": dt(18),
    "wind_direction": "东南风", "safety_interval_hours": 1})
check("与儿童活动时间冲突的计划被拒绝(409)", s == 409, f"({r.get('message','')[:40]}...)")
# 错峰时段（晚间）→ 通过
s, r = call("POST", "/api/child-zone-plans", toks["street"], {
    "child_zone_id": zone["id"], "planned_start": dt(20), "planned_end": dt(21),
    "wind_direction": "东南风", "safety_interval_hours": 2})
plan = r.get("data") or {}
check("错峰计划创建成功", s == 200 and plan.get("status") == "notified")
check("恢复时间=结束+安全间隔", plan.get("recovery_time", "").startswith((tomorrow).strftime("%Y-%m-%d")) and "23:00" in plan.get("recovery_time", ""))
pid = plan.get("id")
s, r = call("GET", f"/api/child-zone-plans/{pid}", toks["street"])
rems = r["data"]["reminders"]
check("提醒推送幼儿园/家长群/附近居民", {x["audience"] for x in rems} == {"kindergarten", "parents", "residents"})
check("提醒标明避让时段与联系人", all(x["avoid_period"] and x["contact_info"] for x in rems))
check("提醒保留送达状态(已发送)", all(x["delivery_status"] == "sent" for x in rems))
kg_rem = next(x for x in rems if x["audience"] == "kindergarten")
s, r = call("POST", f"/api/child-zone-plans/{pid}/reminders/{kg_rem['id']}/deliver", toks["kindergarten"], {})
check("幼儿园提醒确认送达", s == 200)
# 关联工单：新上报→派单→消杀→计划自动已作业→撤警示→园方确认→居民可见
call("POST", "/api/reports", toks["resident"], {"type": "greenbelt_water", "location_desc": "阳光幼儿园旁绿化带", "nearby_population": "儿童", "has_pets": False})
call("POST", "/api/dispatch/generate", toks["street"], {"date": datetime.now().strftime("%Y-%m-%d")})
orders = call("GET", "/api/work-orders?status=assigned", toks["operator"])[1]["data"]
oid2 = orders[0]["id"]
s, r = call("POST", "/api/child-zone-plans", toks["street"], {
    "child_zone_id": zone["id"], "work_order_id": oid2, "planned_start": dt(20, 30), "planned_end": dt(21, 30),
    "wind_direction": "北风", "safety_interval_hours": 2})
pid2 = r["data"]["id"]
check("关联工单的错峰计划", s == 200)
call("POST", f"/api/work-orders/{oid2}/start", toks["operator"], {})
chem = call("GET", "/api/chemicals", toks["operator"])[1]["data"][0]
call("POST", f"/api/work-orders/{oid2}/treatments", toks["operator"], {
    "chemical_id": chem["id"], "concentration": "1:100", "spray_area": "幼儿园旁绿化带",
    "chemical_used": 1.0, "warning_sign": True, "resident_notified": True, "pet_avoided": True})
s, r = call("GET", f"/api/child-zone-plans/{pid2}", toks["street"])
check("消杀提交后计划自动转「已作业」", r["data"]["plan"]["status"] == "treated")
s, r = call("POST", f"/api/child-zone-plans/{pid2}/confirm", toks["kindergarten"], {"note": "提前确认"})
check("未撤警示不可园方确认(409)", s == 409)
s, r = call("POST", f"/api/child-zone-plans/{pid2}/remove-warning", toks["operator"], {})
check("警示撤除", s == 200)
s, r = call("GET", "/api/work-orders/" + str(oid2), toks["street"])
check("警示撤除进入工单时间线", any("警示撤除" in l["action"] for l in r["data"]["logs"]))
s, r = call("POST", f"/api/child-zone-plans/{pid2}/confirm", toks["kindergarten"], {"note": "现场已恢复安全"})
check("园方确认", s == 200 and call("GET", f"/api/child-zone-plans/{pid2}", toks["street"])[1]["data"]["plan"]["status"] == "confirmed")
s, r = call("GET", "/api/work-orders/" + str(oid2), toks["street"])
check("园方确认进入工单时间线", any("园方确认" in l["action"] for l in r["data"]["logs"]))
s, r = call("GET", "/api/notifications", toks["resident"])
rec_notice = next((n for n in r["data"] if "儿童活动恢复" in (n["title"] or "")), None)
check("居民端可见安全间隔与恢复时间", rec_notice and "安全间隔" in rec_notice["content"] and "恢复时间" in rec_notice["content"])

print(f"\n结果：{PASS} 通过，{FAIL} 失败")
sys.exit(1 if FAIL else 0)
