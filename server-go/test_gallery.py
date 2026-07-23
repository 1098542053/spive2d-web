#!/usr/bin/env python3
"""Gallery E2E: tests that handleModels respects user-configured modelsRoot."""
import json, urllib.request, sys, time, os

BASE = "http://localhost:3000"
PASS = 0
FAIL = 0

def test(name, passed, detail=""):
    global PASS, FAIL
    if passed:
        print(f"  [PASS] {name}")
        PASS += 1
    else:
        print(f"  [FAIL] {name} - {detail}")
        FAIL += 1

class Session:
    def __init__(self):
        self.cookie = None
    def req(self, method, path, body=None):
        url = f"{BASE}{path}"
        data = json.dumps(body).encode() if body else None
        r = urllib.request.Request(url, data=data, method=method)
        r.add_header("Content-Type", "application/json")
        if self.cookie:
            r.add_header("Cookie", self.cookie)
        try:
            resp = urllib.request.urlopen(r)
            for h, v in resp.getheaders():
                if h.lower() == "set-cookie":
                    self.cookie = v.split(";")[0]
            return resp.status, json.loads(resp.read().decode())
        except urllib.error.HTTPError as e:
            try:
                return e.code, json.loads(e.read().decode())
            except:
                return e.code, {"error": str(e)}
        except Exception as e:
            return 0, {"error": str(e)}

s = Session()
uid = str(int(time.time() * 1000))[-8:]
MODELS_ENV = r"I:\AICode\spive2d\spive2d-web\test-data\models"
USER_MODELS = r"I:\AICode\spive2d\spive2d-web\test-data\user_models"

print("=" * 60)
print("GALLERY E2E TEST")
print("=" * 60)

# Phase 1: needsSetup=true (fresh server)
print("\n--- Phase 1: Fresh state ---")
status, data = s.req("GET", "/api/auth/me")
test("needsSetup=true", data.get("needsSetup") == True, str(data))

# Phase 2: Register admin
print("\n--- Phase 2: Register ---")
status, data = s.req("POST", "/api/auth/register",
    {"username": f"g{uid}", "password": "testpass", "nickname": "Gal"})
test("register ok", status == 200, str(data))
test("cookie set", s.cookie is not None)
test("role=admin", data.get("user",{}).get("role")=="admin", str(data))

# Phase 3: Initial /api/models (reads from MODELS_ENV by default)
print("\n--- Phase 3: Initial models (from env var) ---")
status, data = s.req("GET", "/api/models")
test("/api/models returns 200", status == 200, str(status))
test("has 'models' key", "models" in data, str(list(data.keys())))
initial_count = len(data.get("models", []))
print(f"  Initial model count: {initial_count} (env dir has gallery/model.moc3)")

# Phase 4: Save different modelsRoot via Settings
print("\n--- Phase 4: Save different path in Settings ---")
status, data = s.req("POST", "/api/settings",
    {"modelsRoot": USER_MODELS})
test("settings save ok", status == 200, str(data))

status, data = s.req("GET", "/api/settings")
test("settings read back", data.get("modelsRoot") == USER_MODELS,
     f"got {data.get('modelsRoot')}")

# Phase 5: /api/models after config change
print("\n--- Phase 5: /api/models (should now use config path) ---")
status, data = s.req("GET", "/api/models")
test("/api/models returns 200", status == 200, str(status))
test("has 'models' key", "models" in data)
config_count = len(data.get("models", []))
print(f"  After config change model count: {config_count}")
# USER_MODELS is empty, so should be 0
# MODELS_ENV has gallery/model.moc3, so without fix would be >=1
test("[RED/GREEN] models reads from config (empty=0)",
     config_count == 0, f"Got {config_count} models. "
     f"If >0, handleModels ignored the configured path '{USER_MODELS}' "
     f"and used env var '{MODELS_ENV}' instead")

print(f"\n{'='*60}")
print(f"FINAL: {PASS} PASS, {FAIL} FAIL")
print(f"{'='*60}")
sys.exit(FAIL)
