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

print("== 12. 计划详情与提醒送达范围门禁 ==")
toks["resident2"] = login("resident2", "Resident@123")
toks["operator2"] = login("operator2", "Operator@123")
# pid2 = 第11节已确认的滨江? 否——pid2 为阳光幼儿园关联工单计划（已 confirmed）；pid 为未确认计划
s, r = call("GET", f"/api/child-zone-plans/{pid2}", toks["resident2"])
check("resident2 访问阳光小区计划详情被拒绝(403/404)", s in (403, 404))
s, r = call("GET", f"/api/child-zone-plans/{pid2}", toks["street"])
rem0 = next(x for x in r["data"]["reminders"] if x["delivery_status"] == "sent")
s, r = call("POST", f"/api/child-zone-plans/{pid2}/reminders/{rem0['id']}/deliver", toks["resident2"], {})
check("resident2 更新提醒送达被拒绝(403/404)", s in (403, 404))
s, r = call("GET", f"/api/child-zone-plans/{pid2}", toks["street"])
still = next(x for x in r["data"]["reminders"] if x["id"] == rem0["id"])
check("越权失败后送达状态数据不变", still["delivery_status"] == "sent")
# resident 本小区：未确认计划不可见，已确认计划仅公开字段
s, r = call("GET", f"/api/child-zone-plans/{pid}", toks["resident"])
check("居民不可见未确认计划详情(404)", s == 404)
s, r = call("GET", f"/api/child-zone-plans/{pid2}", toks["resident"])
pub = r["data"]["plan"] if s == 200 else {}
check("居民可见已确认计划公开字段(安全间隔/恢复时间/避让)", s == 200 and pub.get("safety_interval_hours") and pub.get("recovery_time") and pub.get("avoid_period"))
check("居民详情不泄露园方电话与提醒明细", "contact_phone" not in pub and not r["data"].get("reminders"))
s, r = call("GET", "/api/child-zone-plans", toks["resident"])
check("居民列表仅已确认计划", all(p["status"] == "confirmed" for p in r["data"]))
check("居民列表不泄露园方电话", all(not p.get("contact_phone") for p in r["data"]))
# 园方联系人越权：滨江儿童乐园计划（未绑定王园）
zone2 = next(z for z in call("GET", "/api/child-zones", toks["street"])[1]["data"] if "滨江" in z["name"])
s, r = call("POST", "/api/child-zone-plans", toks["street"], {
    "child_zone_id": zone2["id"], "planned_start": dt(21), "planned_end": dt(22),
    "wind_direction": "西风", "safety_interval_hours": 2})
pid3 = r["data"]["id"]
s, r = call("GET", f"/api/child-zone-plans/{pid3}", toks["kindergarten"])
check("园方仅可读绑定活动区计划(404)", s == 404)
s, r = call("GET", f"/api/child-zone-plans/{pid3}", toks["resident2"])
check("本小区居民读未确认计划同样不可见(404)", s == 404)
# 送达权限：非作业队 operator2 → 403；关联 operator、street、绑定 kindergarten → 成功
s, r = call("GET", f"/api/child-zone-plans/{pid2}", toks["street"])
rems2 = {x["audience"]: x for x in r["data"]["reminders"] if x["delivery_status"] == "sent"}
s, r = call("POST", f"/api/child-zone-plans/{pid2}/reminders/{rems2['parents']['id']}/deliver", toks["operator2"], {})
check("非实际作业消杀队更新送达被拒绝(403)", s == 403)
s, r = call("POST", f"/api/child-zone-plans/{pid2}/reminders/{rems2['parents']['id']}/deliver", toks["operator"], {})
check("实际作业消杀队(operator)可更新送达", s == 200)
s, r = call("POST", f"/api/child-zone-plans/{pid2}/reminders/{rems2['residents']['id']}/deliver", toks["street"], {})
check("街道可更新送达", s == 200)
s, r = call("POST", f"/api/child-zone-plans/{pid2}/reminders/{rems2['kindergarten']['id']}/deliver", toks["kindergarten"], {})
check("绑定园方可更新送达", s == 200)
s, r = call("GET", f"/api/child-zone-plans/{pid2}", toks["street"])
check("三类提醒全部送达", all(x["delivery_status"] == "delivered" for x in r["data"]["reminders"]))

print("== 13. 园方确认绑定门禁（未绑定/非绑定均 4xx 且无副作用） ==")
toks["kindergarten2"] = login("kindergarten2", "Kindergarten@123")
check("第二园方账号登录", bool(toks["kindergarten2"]))
# 街道创建未绑定联系人的活动区
s, r = call("POST", "/api/child-zones", toks["street"], {
    "community_id": 1, "name": "未绑定联系人活动区", "zone_type": "playground",
    "contact_name": "临时联系人", "contact_phone": "13000000000",
    "activity_times": "08:00-10:00", "parent_group": "临时家长群"})
check("创建未绑定联系人活动区", s == 200)
unbound_zone = r["data"]["id"]
# 新工单载体：上报→派单→取一单
call("POST", "/api/reports", toks["resident"], {"type": "mosquito_dense", "location_desc": "13号楼周边", "nearby_population": "居民", "has_pets": False})
call("POST", "/api/dispatch/generate", toks["street"], {"date": datetime.now().strftime("%Y-%m-%d")})
oid3 = call("GET", "/api/work-orders?status=assigned", toks["operator"])[1]["data"][0]["id"]
# 计划A=未绑定活动区；计划B=王园绑定活动区；挂同一工单
s, r = call("POST", "/api/child-zone-plans", toks["street"], {
    "child_zone_id": unbound_zone, "work_order_id": oid3, "planned_start": dt(20), "planned_end": dt(21),
    "wind_direction": "东风", "safety_interval_hours": 2})
planA = r["data"]["id"]
s, r = call("POST", "/api/child-zone-plans", toks["street"], {
    "child_zone_id": zone["id"], "work_order_id": oid3, "planned_start": dt(21, 30), "planned_end": dt(22, 30),
    "wind_direction": "南风", "safety_interval_hours": 2})
planB = r["data"]["id"]
call("POST", f"/api/work-orders/{oid3}/start", toks["operator"], {})
chem = call("GET", "/api/chemicals", toks["operator"])[1]["data"][0]
call("POST", f"/api/work-orders/{oid3}/treatments", toks["operator"], {
    "chemical_id": chem["id"], "concentration": "1:100", "spray_area": "13号楼及周边", "chemical_used": 1.0,
    "warning_sign": True, "resident_notified": True, "pet_avoided": True})
for p in (planA, planB):
    call("POST", f"/api/child-zone-plans/{p}/remove-warning", toks["operator"], {})
stA = call("GET", f"/api/child-zone-plans/{planA}", toks["street"])[1]["data"]["plan"]["status"]
stB = call("GET", f"/api/child-zone-plans/{planB}", toks["street"])[1]["data"]["plan"]["status"]
check("两计划均推进到警示已撤除", stA == "warning_removed" and stB == "warning_removed")
# 未绑定：任意 kindergarten 确认 → 4xx，且无副作用
s, r = call("POST", f"/api/child-zone-plans/{planA}/confirm", toks["kindergarten"], {"note": "试图确认"})
check("未绑定活动区确认返回4xx", 400 <= s < 500, f"(HTTP {s})")
pA = call("GET", f"/api/child-zone-plans/{planA}", toks["street"])[1]["data"]["plan"]
check("计划状态保持 warning_removed 且确认字段为空", pA["status"] == "warning_removed" and not pA["confirmed_at"] and not pA["confirmed_by"])
s, r = call("GET", "/api/notifications", toks["street"])
check("无公开恢复通知", not any("未绑定联系人活动区" in (n["title"] or "") + (n["content"] or "") for n in r["data"]))
logs3 = call("GET", "/api/work-orders/" + str(oid3), toks["street"])[1]["data"]["logs"]
check("工单无园方确认日志", not any(l["action"] == "园方确认" for l in logs3))
# 非绑定账号：李园长确认王园的计划 → 4xx
s, r = call("POST", f"/api/child-zone-plans/{planB}/confirm", toks["kindergarten2"], {"note": "越权确认"})
check("非绑定园方账号确认返回4xx", 400 <= s < 500, f"(HTTP {s})")
pB = call("GET", f"/api/child-zone-plans/{planB}", toks["street"])[1]["data"]["plan"]
check("计划B状态未被改变", pB["status"] == "warning_removed" and not pB["confirmed_at"])
# 绑定园方确认自身计划 → 成功并发布恢复通知
s, r = call("POST", f"/api/child-zone-plans/{planB}/confirm", toks["kindergarten"], {"note": "确认恢复"})
check("绑定园方确认成功", s == 200)
pB = call("GET", f"/api/child-zone-plans/{planB}", toks["street"])[1]["data"]["plan"]
check("计划B已确认且确认字段已写入", pB["status"] == "confirmed" and pB["confirmed_at"] and pB["confirmed_by"])
s, r = call("GET", "/api/notifications", toks["resident"])
check("居民收到该计划恢复通知", any("阳光幼儿园" in (n["title"] or "") and "恢复时间" in (n["content"] or "") for n in r["data"]))

print("== 14. 宠物误触投诉处理 ==")
# 居民提交投诉：自动关联本小区最近一次消杀（药剂/区域/警示时间）
s, r = call("POST", "/api/pet-complaints", toks["resident"], {
    "pet_type": "dog", "pet_name": "豆豆", "symptom": "喷药后呕吐、精神萎靡",
    "walking_route": "18:00 从 3 号楼沿中心花园遛狗至东门",
    "medical_vouchers": ["https://example.com/vet-receipt-1.jpg"]})
pc = r.get("data") or {}
check("宠物投诉提交成功", s == 200 and pc.get("complaint_no", "").startswith("PC"))
check("自动关联药剂/喷洒区域/警示时间", bool(pc.get("chemical_name")) and bool(pc.get("spray_area")) and bool(pc.get("warning_time")))
check("行走路线已记录", pc.get("walking_route", "").startswith("18:00"))
pcid = pc.get("id")
pc_order = pc.get("work_order_id")
# 补充就医凭证
s, r = call("POST", f"/api/pet-complaints/{pcid}/vouchers", toks["resident"], {"vouchers": ["https://example.com/vet-receipt-2.jpg"]})
check("补充就医凭证", s == 200)
s, r = call("GET", f"/api/pet-complaints/{pcid}", toks["resident"])
check("凭证已合并", len(r["data"]["medical_vouchers"]) == 2)
# 他人不可见
s, r = call("GET", "/api/pet-complaints", toks["resident2"])
check("他人投诉不可见", all(p["id"] != pcid for p in r["data"]))
s, r = call("GET", f"/api/pet-complaints/{pcid}", toks["resident2"])
check("他人详情被拒绝(404)", s == 404)
# 居民无权办结
s, r = call("POST", f"/api/pet-complaints/{pcid}/handle", toks["resident"], {"resolution_note": "x"})
check("居民无权办结(403)", s == 403)
# 工单联动：异常与时间线
if pc_order:
    d = call("GET", "/api/work-orders/" + str(pc_order), toks["street"])[1]["data"]
    check("工单时间线含宠物投诉", any("宠物" in l["action"] for l in d["logs"]))
    check("未闭环工单生成宠物投诉异常", any(i["type"] == "pet_complaint" for i in d["issues"]))
# 卫生监督办结：回访+赔付+药剂说明+告知调整
s, r = call("POST", f"/api/pet-complaints/{pcid}/handle", toks["supervisor"], {
    "need_revisit": True, "compensation": True, "compensation_note": "凭就医票据报销",
    "chemical_note": "高效氯氟氰菊酯 1:100 稀释，安全间隔 4 小时，对犬类低毒",
    "notify_adjustment": "改为短信+公告栏双通道，提前 48 小时告知并标注宠物避让路线",
    "resolution_note": "已电话回访，宠物已好转"})
check("卫生监督办结", s == 200)
s, r = call("GET", f"/api/pet-complaints/{pcid}", toks["supervisor"])
d = r["data"]
check("办结记录完整（回访/赔付/药剂说明/告知调整）", d["status"] == "resolved" and d["need_revisit"] and d["compensation"] and d["chemical_note"] and d["notify_adjustment"])
# 告知调整后再发一起同类投诉 → 跟踪统计
s, r = call("POST", "/api/pet-complaints", toks["resident"], {
    "pet_type": "cat", "pet_name": "咪咪", "symptom": "打喷嚏", "walking_route": "中心花园散步"})
check("第二起投诉提交", s == 200)
s, r = call("GET", "/api/pet-complaints/tracking?community_id=1", toks["street"])
t = r["data"]
check("跟踪含最新告知调整", t["latest_adjustment"].startswith("改为短信"))
check("调整后同类投诉被统计", t["after_count"] >= 1 and t["decreased"] is not None)
# 小区消杀档案
s, r = call("GET", "/api/communities/1/archive", toks["supervisor"])
a = r["data"]
check("档案含消杀与药剂统计", a["treatments_total"] > 0 and len(a["chemical_usage"]) > 0)
check("档案含宠物投诉与告知调整", a["pet_complaints"]["total"] >= 2 and len(a["notify_adjustments"]) >= 1)
check("档案含跟踪结果", a["pet_tracking"]["latest_adjustment"] != "")
# 跟踪进入下次计划（派单预览）
s, r = call("GET", "/api/dispatch/preview", toks["street"])
check("派单预览含宠物投诉跟踪", any(pt["community_name"] == "阳光小区" and pt["total_complaints"] >= 2 for pt in r["data"]["pet_tracking"]))

print("== 15. 物业积水整改（地下室排水沟长期积水） ==")
toks["property2"] = login("property2", "Property@123")

# 15.1 列表 / 超期 / 可见范围
s, r = call("GET", "/api/property-rectifications", toks["street"])
prects = r["data"]
check("积水整改列表（含 3 条种子演示）", s == 200 and len(prects) >= 3, f"({len(prects)} 条)")
pr1 = next(p for p in prects if "B2 集水井" in p["water_location"])
pr2 = next(p for p in prects if "A 区排水沟" in p["water_location"])
pr3 = next(p for p in prects if "B1 排水沟" in p["water_location"])
check("种子①整改超期（待整改且复查日期已过）", pr1["status"] == "pending" and pr1["rect_overdue"])
check("种子②复查超期（已报审待复查且日期已过）", pr2["status"] == "recheck_pending" and pr2["recheck_overdue"])
check("种子③已按图复查通过且投诉 2→0", pr3["status"] == "verified" and pr3["complaints_before"] == 2 and pr3["complaints_after"] == 0 and pr3["complaint_decreased"] is True)
s, r = call("GET", "/api/property-rectifications?overdue=1", toks["street"])
check("仅看超期过滤生效", len(r["data"]) >= 2 and all(p["rect_overdue"] or p["recheck_overdue"] for p in r["data"]))
s, r = call("GET", "/api/property-rectifications", toks["resident"])
check("居民列表为空（无权查看）", s == 200 and r["data"] == [])
s, r = call("GET", f"/api/property-rectifications/{pr1['id']}", toks["resident2"])
check("居民查看详情被拒绝(403)", s == 403)
s, r = call("GET", "/api/property-rectifications", toks["property2"])
check("物业2只见滨江任务", all(p["community_name"] == "滨江花园" for p in r["data"]) and any(p["id"] == pr2["id"] for p in r["data"]))
s, r = call("POST", f"/api/property-rectifications/{pr1['id']}/start", toks["property2"], {})
check("非设施责任人受理被拒绝(403)", s == 403)

# 15.2 种子①：整改超期 → 受理 → 标注照片完成 → 按图复查通过 → 投诉变化
s, r = call("POST", f"/api/property-rectifications/{pr1['id']}/start", toks["property"], {})
check("设施责任人受理", s == 200)
s, r = call("POST", f"/api/property-rectifications/{pr1['id']}/complete", toks["property"], {
    "repair_desc": "已清掏集水井淤泥", "repair_method": "dredge_drain",
    "rectify_photos": [], "rectify_photo_remark": ""})
check("未传整改照片/标注被拒绝(400)", s == 400)
s, r = call("POST", f"/api/property-rectifications/{pr1['id']}/complete", toks["property"], {
    "repair_desc": "清掏集水井淤积并更换损坏排水泵，沟通后无积水", "repair_method": "replace_pump",
    "rectify_photos": ["https://example.com/pr1-before.jpg", "https://example.com/pr1-after.jpg"],
    "rectify_photo_remark": "照片①位置：地下车库B2集水井（红圈标注长期积水点），处理方式：抽排清淤；照片②位置：同一集水井，处理方式：更换排水泵后复拍"})
check("物业提交整改完成（照片标明位置+方式）", s == 200)
s, r = call("GET", f"/api/property-rectifications/{pr1['id']}", toks["street"])
p1 = r["data"]
check("进入待复查且整改照片/方式已记录", p1["status"] == "recheck_pending" and len(p1["rectify_photos"]) == 2 and p1["repair_method_label"] == "检修/更换排水泵")
s, r = call("POST", f"/api/property-rectifications/{pr1['id']}/supervise", toks["street"], {"content": "请尽快安排复查，超期纳入物业考核"})
check("街道督办", s == 200)
s, r = call("POST", f"/api/property-rectifications/{pr1['id']}/recheck", toks["supervisor"], {
    "pass": True, "recheck_photos": [], "recheck_remark": "无照片"})
check("复查未传复查照片被拒绝(400)", s == 400)
s, r = call("POST", f"/api/property-rectifications/{pr1['id']}/recheck", toks["supervisor"], {
    "pass": True, "recheck_photos": ["https://example.com/pr1-recheck.jpg"],
    "recheck_remark": "对照整改照片同一位置按图核验，集水井已无积水、无孑孓"})
check("卫生监督按图复查通过", s == 200 and r["data"]["complaints_after"] is not None)
s, r = call("GET", f"/api/property-rectifications/{pr1['id']}", toks["street"])
p1 = r["data"]
check("任务复查通过且投诉变化已回写", p1["status"] == "verified" and p1["complaints_before"] >= p1["complaints_after"])
check("督办记录保留", any(m["kind"] == "supervise" for m in p1["reminders"]))
s, r = call("GET", "/api/water-points?type=underground_garage", toks["street"])
check("复查通过后积水点清除", any(w["location_desc"] == "地下车库 B2 集水井" and w["status"] == "cleared" for w in r["data"]))

# 15.3 种子②：复查超期提示街道督办与物业负责人 → 复查
s, r = call("GET", f"/api/property-rectifications/{pr2['id']}", toks["street"])
check("复查超期自动生成提示", any(m["kind"] == "recheck_overdue" for m in r["data"]["reminders"]))
s, r = call("POST", f"/api/property-rectifications/{pr2['id']}/complete", toks["property2"], {
    "repair_desc": "x", "repair_method": "dredge_drain", "rectify_photos": ["https://x/1.jpg"], "rectify_photo_remark": "x"})
check("待复查状态不可重复提交整改(409)", s == 409)
s, r = call("POST", f"/api/property-rectifications/{pr2['id']}/recheck", toks["street"], {
    "pass": True, "recheck_photos": ["https://example.com/pr2-recheck.jpg"], "recheck_remark": "街道按图核验通过"})
check("街道复查通过（复查超期任务）", s == 200)

# 15.4 看板：超期清零、设施责任人维度入考核
s, r = call("GET", "/api/dashboard/closed-loop", toks["street"])
rowmap = {x["community_name"]: x for x in r["data"]["rows"]}
check("闭环看板含积水整改列", "prop_rect_total" in rowmap["阳光小区"] and rowmap["阳光小区"]["prop_rect_verified"] >= 2)
check("超期已随复查清零", rowmap["阳光小区"]["prop_rect_overdue"] == 0 and rowmap["滨江花园"]["prop_rect_overdue"] == 0)
s, r = call("GET", "/api/dashboard/assessment?month=" + datetime.now().strftime("%Y-%m"), toks["street"])
fac = [f for f in r["data"]["facilities"] if f["user_name"] in ("王强", "赵敏")]
check("考核含物业设施责任人维度", len(fac) == 2)
wq = next(f for f in fac if f["user_name"] == "王强")
check("设施责任人复查合格率与投诉变化入考核", wq["rect_verified"] >= 2 and wq["recheck_pass_rate"] == 100.0 and wq["rect_overdue"] == 0)

# 15.5 消杀队重复临时处理 → 自动生成物业整改
chem = call("GET", "/api/chemicals", toks["operator"])[1]["data"][0]
call("POST", f"/api/chemicals/{chem['id']}/restock", toks["street"], {"amount": 100})
future = (datetime.now() + timedelta(days=5)).strftime("%Y-%m-%d")
call("POST", f"/api/teams/1/schedules", toks["street"], {"work_date": future, "shift": "allday", "max_orders": 5})
s, r = call("POST", "/api/reports", toks["property"], {
    "type": "basement_damp", "location_desc": "地下车库 B3 排水沟", "nearby_population": "车主", "has_pets": False,
    "description": "排水沟长期积水返味，消杀后反复"})
check("地下室潮湿上报", s == 200)
call("POST", "/api/dispatch/generate", toks["street"], {"date": datetime.now().strftime("%Y-%m-%d")})
cand = call("GET", "/api/work-orders?community_id=1&status=assigned", toks["street"])[1]["data"]
cand += call("GET", "/api/work-orders?community_id=1&status=pending", toks["street"])[1]["data"]
b3_oid = None
for o in cand:
    dd = call("GET", "/api/work-orders/" + str(o["id"]), toks["street"])[1]["data"]
    if dd.get("water_point") and dd["water_point"]["location_desc"] == "地下车库 B3 排水沟":
        b3_oid = o["id"]
        if o["status"] in ("pending", "assigned") and (not o["team_name"]):
            call("POST", f"/api/work-orders/{b3_oid}/assign", toks["street"], {"team_id": 1, "scheduled_date": future})
        break
check("B3 排水沟已派单", b3_oid is not None, f"(工单 {b3_oid})")
call("POST", f"/api/work-orders/{b3_oid}/start", toks["operator"], {})
s, r = call("POST", f"/api/work-orders/{b3_oid}/property-rectifications", toks["operator"], {})
check("未做临时处理前转整改被拒绝(409)", s == 409)
call("POST", f"/api/work-orders/{b3_oid}/treatments", toks["operator"], {
    "chemical_id": chem["id"], "spray_area": "B3排水沟", "chemical_used": 1.0,
    "warning_sign": True, "resident_notified": True, "pet_avoided": True})
dd = call("GET", "/api/work-orders/" + str(b3_oid), toks["street"])[1]["data"]
check("首次临时处理不自动建档", len(dd["property_rectifications"]) == 0)
call("POST", f"/api/work-orders/{b3_oid}/treatments", toks["operator"], {
    "chemical_id": chem["id"], "spray_area": "B3排水沟再次投药", "chemical_used": 1.0,
    "warning_sign": True, "resident_notified": True, "pet_avoided": True})
dd = call("GET", "/api/work-orders/" + str(b3_oid), toks["street"])[1]["data"]
autos = dd["property_rectifications"]
check("第2次临时处理自动生成物业整改任务（含责任人/复查日期/临时处理2次）",
      len(autos) == 1 and autos[0]["facility_name"] == "王强" and autos[0]["recheck_date_str"] and "王强" in autos[0]["facility_name"])
auto = autos[0]
check("自动建档进入工单时间线", any("生成物业整改任务" in l["action"] for l in dd["logs"]))
s, r = call("POST", f"/api/work-orders/{b3_oid}/property-rectifications", toks["operator"], {})
check("同一积水点不重复建档(409)", s == 409)
# 非地下室积水点 → 拒绝（动态选取一个未闭环且非地下室积水点的工单）
non_base_oid = None
for o in call("GET", "/api/work-orders", toks["street"])[1]["data"]:
    if o["status"] == "closed" or not o.get("water_point_id"):
        continue
    od = call("GET", "/api/work-orders/" + str(o["id"]), toks["street"])[1]["data"]
    wp = od.get("water_point") or {}
    if wp.get("type") not in ("basement_damp", "underground_garage"):
        non_base_oid = o["id"]
        break
check("存在非地下室积水点工单用于负例", non_base_oid is not None)
if non_base_oid:
    s, r = call("POST", f"/api/work-orders/{non_base_oid}/property-rectifications", toks["street"], {})
    check("非地下室积水点转整改被拒绝(400)", s == 400)
# 自动任务流转闭环
call("POST", f"/api/property-rectifications/{auto['id']}/start", toks["property"], {})
s, r = call("POST", f"/api/property-rectifications/{auto['id']}/complete", toks["property"], {
    "repair_desc": "重做B3排水沟找坡", "repair_method": "rebuild_drain",
    "rectify_photos": ["https://example.com/auto-1.jpg"], "rectify_photo_remark": "位置：B3排水沟起点；处理方式：重做找坡"})
check("自动任务物业完成整改", s == 200)
s, r = call("POST", f"/api/property-rectifications/{auto['id']}/recheck", toks["grid"], {
    "pass": True, "recheck_photos": ["https://example.com/auto-rc.jpg"], "recheck_remark": "按图核验无积水"})
check("自动任务复查通过、投诉下降", s == 200 and r["data"]["complaints_after"] == 0)

print(f"\n结果：{PASS} 通过，{FAIL} 失败")
sys.exit(1 if FAIL else 0)
